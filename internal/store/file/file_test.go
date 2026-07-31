package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveDedupsOnCapturedAt(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	ts := time.Now()

	if err := s.Save(ctx, 0, "#TAG", ts, []byte("a")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, 0, "#TAG", ts, []byte("a-different-body-but-same-timestamp")); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, err := s.Recent(ctx, 0, "#TAG", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(got))
	}
	if string(got[0].Raw) != "a" {
		t.Errorf("raw = %q, want the original body untouched by the re-save", got[0].Raw)
	}
}

func TestTagIsSanitizedIntoASafeDirectoryName(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()
	// A real tag looks like "#QYJYUU0YY" - the leading # and any path-ish
	// characters must never reach the filesystem unescaped.
	if err := s.Save(ctx, 0, "#QY/JY..UU", time.Now(), []byte("x")); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one village directory, got %d", len(entries))
	}
	if entries[0].Name() != "QY_JY__UU" {
		t.Errorf("sanitized dir = %q, want %q", entries[0].Name(), "QY_JY__UU")
	}
}

func TestUserScopingKeepsDirectoriesSeparate(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()
	s.Save(ctx, 0, "#TAG", time.Now(), []byte("local"))
	s.Save(ctx, 42, "#TAG", time.Now(), []byte("hosted"))

	local, _ := s.Recent(ctx, 0, "#TAG", 10)
	hosted, _ := s.Recent(ctx, 42, "#TAG", 10)
	if len(local) != 1 || len(hosted) != 1 {
		t.Fatalf("local = %d, hosted = %d, want 1 each", len(local), len(hosted))
	}
	if _, err := os.Stat(filepath.Join(dir, "TAG")); err != nil {
		t.Errorf("expected a plain top-level directory for the unauthenticated (userID 0) case: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "u42", "TAG")); err != nil {
		t.Errorf("expected a u42/ subdirectory for the authenticated case: %v", err)
	}
}

func TestVillagesListsNewestFirstWithSnapshotCounts(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()
	base := time.Now()

	s.Save(ctx, 0, "#OLD", base.Add(-2*time.Hour), []byte("x"))
	s.Save(ctx, 0, "#NEW", base, []byte("x"))
	s.Save(ctx, 0, "#NEW", base.Add(-1*time.Hour), []byte("y"))

	got, err := s.Villages(ctx, 0)
	if err != nil {
		t.Fatalf("villages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d villages, want 2", len(got))
	}
	if got[0].Tag != "NEW" || got[0].Snapshots != 2 {
		t.Errorf("newest village = %+v, want NEW with 2 snapshots", got[0])
	}
	if got[1].Tag != "OLD" || got[1].Snapshots != 1 {
		t.Errorf("second village = %+v, want OLD with 1 snapshot", got[1])
	}
}

func TestForgetRemovesVillageDirectory(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()
	s.Save(ctx, 0, "#KEEP", time.Now(), []byte("a"))
	s.Save(ctx, 0, "#DROP", time.Now(), []byte("b"))

	if err := s.Forget(ctx, 0, "#DROP"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "DROP")); !os.IsNotExist(err) {
		t.Errorf("expected #DROP's directory to be gone, stat err = %v", err)
	}
	if kept, _ := s.Recent(ctx, 0, "#KEEP", 10); len(kept) != 1 {
		t.Errorf("#KEEP has %d snapshots, want 1", len(kept))
	}
}

func TestForgetOnUnknownTagIsANoOp(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	if err := s.Forget(context.Background(), 0, "#NEVER-SEEN"); err != nil {
		t.Errorf("forget on an unknown tag should be a no-op, got: %v", err)
	}
}

func TestPruneKeepsNewestPerVillageAcrossDirectories(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	cutoff := time.Now().Add(-14 * 24 * time.Hour)

	s.Save(ctx, 0, "#OLD-ONLY", old, []byte("x"))
	s.Save(ctx, 0, "#MIXED", old, []byte("x"))
	s.Save(ctx, 0, "#MIXED", recent, []byte("y"))

	removed, err := s.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	oldOnly, _ := s.Recent(ctx, 0, "#OLD-ONLY", 10)
	if len(oldOnly) != 1 {
		t.Errorf("#OLD-ONLY snapshots = %d, want 1 (the sole snapshot must survive as the newest)", len(oldOnly))
	}
	mixed, _ := s.Recent(ctx, 0, "#MIXED", 10)
	if len(mixed) != 1 || string(mixed[0].Raw) != "y" {
		t.Errorf("#MIXED snapshots after prune = %+v, want only the recent one", mixed)
	}
}

func TestRecentOnUnknownTagReturnsEmptyNotError(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	got, err := s.Recent(context.Background(), 0, "#NEVER-SEEN", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d snapshots for an unknown tag, want 0", len(got))
	}
}
