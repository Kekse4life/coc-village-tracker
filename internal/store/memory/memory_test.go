package memory

import (
	"context"
	"testing"
	"time"
)

func TestSaveDedupsOnCapturedAt(t *testing.T) {
	s := New()
	ctx := context.Background()
	ts := time.Now()

	if err := s.Save(ctx, 0, "#TAG", ts, []byte("a")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, 0, "#TAG", ts, []byte("a")); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, _ := s.Recent(ctx, 0, "#TAG", 10)
	if len(got) != 1 {
		t.Fatalf("got %d snapshots, want 1 (the re-save must be a no-op)", len(got))
	}
}

func TestRecentOrdersNewestFirstAndRespectsLimit(t *testing.T) {
	s := New()
	ctx := context.Background()
	base := time.Now()

	for i := 0; i < 5; i++ {
		s.Save(ctx, 0, "#TAG", base.Add(time.Duration(i)*time.Hour), []byte("x"))
	}
	got, _ := s.Recent(ctx, 0, "#TAG", 2)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if !got[0].CapturedAt.Equal(base.Add(4*time.Hour)) || !got[1].CapturedAt.Equal(base.Add(3*time.Hour)) {
		t.Errorf("order = %v, %v; want newest first", got[0].CapturedAt, got[1].CapturedAt)
	}
}

func TestUsersDoNotShareSnapshots(t *testing.T) {
	s := New()
	ctx := context.Background()
	s.Save(ctx, 1, "#TAG", time.Now(), []byte("a"))
	got, _ := s.Recent(ctx, 2, "#TAG", 10)
	if len(got) != 0 {
		t.Errorf("user 2 saw %d snapshots belonging to user 1", len(got))
	}
}

func TestPruneKeepsNewestEvenIfOlderThanCutoff(t *testing.T) {
	s := New()
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)
	cutoff := time.Now().Add(-14 * 24 * time.Hour)

	s.Save(ctx, 0, "#ONLY-OLD", old, []byte("x"))
	removed, err := s.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0 (the sole snapshot is the newest, so it must survive)", removed)
	}
	got, _ := s.Recent(ctx, 0, "#ONLY-OLD", 10)
	if len(got) != 1 {
		t.Errorf("snapshot count after prune = %d, want 1", len(got))
	}
}

func TestVillagesListsNewestFirstWithSnapshotCounts(t *testing.T) {
	s := New()
	ctx := context.Background()
	base := time.Now()

	s.Save(ctx, 0, "#OLD", base.Add(-2*time.Hour), []byte("x"))
	s.Save(ctx, 0, "#NEW", base, []byte("x"))
	s.Save(ctx, 0, "#NEW", base.Add(-1*time.Hour), []byte("y")) // #NEW now has 2 snapshots

	got, err := s.Villages(ctx, 0)
	if err != nil {
		t.Fatalf("villages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d villages, want 2", len(got))
	}
	if got[0].Tag != "#NEW" || !got[0].LastCapturedAt.Equal(base) || got[0].Snapshots != 2 {
		t.Errorf("newest village = %+v, want #NEW captured %v with 2 snapshots", got[0], base)
	}
	if got[1].Tag != "#OLD" || got[1].Snapshots != 1 {
		t.Errorf("second village = %+v, want #OLD with 1 snapshot", got[1])
	}
}

func TestVillagesDoesNotCrossUsers(t *testing.T) {
	s := New()
	ctx := context.Background()
	s.Save(ctx, 1, "#TAG", time.Now(), []byte("a"))
	got, _ := s.Villages(ctx, 2)
	if len(got) != 0 {
		t.Errorf("user 2 saw %d villages belonging to user 1", len(got))
	}
}

func TestNewWithLimitTrimsOldestFirst(t *testing.T) {
	s := NewWithLimit(2)
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 3; i++ {
		s.Save(ctx, 0, "#TAG", base.Add(time.Duration(i)*time.Hour), []byte("x"))
	}
	got, _ := s.Recent(ctx, 0, "#TAG", 10)
	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2 (the limit)", len(got))
	}
	if !got[0].CapturedAt.Equal(base.Add(2*time.Hour)) || !got[1].CapturedAt.Equal(base.Add(1*time.Hour)) {
		t.Errorf("kept = %v, %v; want the two newest, oldest trimmed first", got[0].CapturedAt, got[1].CapturedAt)
	}
}

func TestForgetRemovesAllSnapshotsForThatVillageOnly(t *testing.T) {
	s := New()
	ctx := context.Background()
	s.Save(ctx, 0, "#KEEP", time.Now(), []byte("a"))
	s.Save(ctx, 0, "#DROP", time.Now(), []byte("b"))

	if err := s.Forget(ctx, 0, "#DROP"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if dropped, _ := s.Recent(ctx, 0, "#DROP", 10); len(dropped) != 0 {
		t.Errorf("#DROP still has %d snapshots after Forget", len(dropped))
	}
	if kept, _ := s.Recent(ctx, 0, "#KEEP", 10); len(kept) != 1 {
		t.Errorf("#KEEP has %d snapshots, want 1 (Forget must not touch other villages)", len(kept))
	}
}

func TestPruneRemovesOldKeepsRecent(t *testing.T) {
	s := New()
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	cutoff := time.Now().Add(-14 * 24 * time.Hour)

	s.Save(ctx, 0, "#TAG", old, []byte("x"))
	s.Save(ctx, 0, "#TAG", recent, []byte("y"))

	removed, err := s.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	got, _ := s.Recent(ctx, 0, "#TAG", 10)
	if len(got) != 1 || !got[0].CapturedAt.Equal(recent) {
		t.Errorf("snapshots after prune = %+v, want only the recent one", got)
	}
}
