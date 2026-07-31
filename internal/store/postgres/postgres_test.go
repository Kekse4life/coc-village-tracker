package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// These tests hit a real Postgres and are skipped unless TEST_DATABASE_URL
// is set, e.g.:
//
//	TEST_DATABASE_URL=postgres://postgres:test@localhost:15432/cocprogress?sslmode=disable go test ./internal/store/postgres/...
//
// internal/server's hosted-mode tests point at the same database by the
// same convention. Running both together via `go test ./...` needs -p 1:
// go test parallelizes across packages by default, and two packages
// TRUNCATE-ing the same shared database at once will strand the other
// mid-test with a dangling foreign key.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), "TRUNCATE users, sessions, villages, snapshots CASCADE")
		s.Close()
	})
	return s
}

var seedUserCounter int

func seedUser(t *testing.T, s *Store) int64 {
	t.Helper()
	seedUserCounter++
	var id int64
	providerID := fmt.Sprintf("%s-%d", t.Name(), seedUserCounter)
	err := s.pool.QueryRow(context.Background(),
		"INSERT INTO users (provider, provider_id) VALUES ('github', $1) RETURNING id", providerID).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func TestSaveAndRecentRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := seedUser(t, s)

	t1 := time.Date(2026, 7, 29, 13, 48, 18, 0, time.UTC)
	t2 := time.Date(2026, 7, 30, 7, 54, 55, 0, time.UTC)

	if err := s.Save(ctx, uid, "#TAG", t1, []byte(`{"tag":"#TAG","timestamp":1}`)); err != nil {
		t.Fatalf("save t1: %v", err)
	}
	if err := s.Save(ctx, uid, "#TAG", t2, []byte(`{"tag":"#TAG","timestamp":2}`)); err != nil {
		t.Fatalf("save t2: %v", err)
	}
	// Re-saving the same capturedAt must be a no-op, not a duplicate or an error.
	if err := s.Save(ctx, uid, "#TAG", t2, []byte(`{"tag":"#TAG","timestamp":2}`)); err != nil {
		t.Fatalf("re-save t2: %v", err)
	}

	got, err := s.Recent(ctx, uid, "#TAG", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(got))
	}
	if !got[0].CapturedAt.Equal(t2) || !got[1].CapturedAt.Equal(t1) {
		t.Errorf("order = %v, %v; want newest first (%v, %v)", got[0].CapturedAt, got[1].CapturedAt, t2, t1)
	}

	// A different user with the same tag must not see these snapshots.
	other := seedUser(t, s)
	none, err := s.Recent(ctx, other, "#TAG", 10)
	if err != nil {
		t.Fatalf("recent (other user): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("a different user saw %d snapshots for the same tag, want 0", len(none))
	}
}

func TestVillagesListsNewestFirstWithSnapshotCounts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := seedUser(t, s)

	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now()

	if err := s.Save(ctx, uid, "#OLD", old, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, uid, "#NEW", newer, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, uid, "#NEW", newer.Add(-30*time.Minute), []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Villages(ctx, uid)
	if err != nil {
		t.Fatalf("villages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d villages, want 2", len(got))
	}
	if got[0].Tag != "#NEW" || got[0].Snapshots != 2 {
		t.Errorf("newest village = %+v, want #NEW with 2 snapshots", got[0])
	}
	if got[1].Tag != "#OLD" || got[1].Snapshots != 1 {
		t.Errorf("second village = %+v, want #OLD with 1 snapshot", got[1])
	}

	other := seedUser(t, s)
	if none, _ := s.Villages(ctx, other); len(none) != 0 {
		t.Errorf("a different user saw %d villages, want 0", len(none))
	}
}

func TestPruneKeepsNewestPerVillage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	uid := seedUser(t, s)

	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	cutoff := time.Now().Add(-14 * 24 * time.Hour)

	if err := s.Save(ctx, uid, "#OLD-ONLY", old, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, uid, "#MIXED", old, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.Save(ctx, uid, "#MIXED", recent, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}

	removed, err := s.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1 (only #MIXED's old snapshot)", removed)
	}

	oldOnly, _ := s.Recent(ctx, uid, "#OLD-ONLY", 10)
	if len(oldOnly) != 1 {
		t.Errorf("#OLD-ONLY has %d snapshots after prune, want 1 (the sole snapshot must survive as the newest)", len(oldOnly))
	}

	mixed, _ := s.Recent(ctx, uid, "#MIXED", 10)
	if len(mixed) != 1 || mixed[0].CapturedAt.Before(cutoff) {
		t.Errorf("#MIXED snapshots after prune = %+v, want only the one newer than cutoff (%v)", mixed, cutoff)
	}
}
