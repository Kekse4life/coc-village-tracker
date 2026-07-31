// Package store keeps village exports over time so consecutive ones can be
// compared. It is deliberately narrow: save a snapshot, list the most recent
// ones for a village, and prune old ones while always keeping the newest.
package store

import (
	"context"
	"time"
)

// Snapshot is one saved export.
type Snapshot struct {
	Tag        string
	CapturedAt time.Time
	Raw        []byte
}

// Village is one village userID has snapshots for, and how fresh it is -
// enough to render a switcher without parsing every snapshot in it.
type Village struct {
	Tag            string
	LastCapturedAt time.Time
	Snapshots      int
}

// Store persists snapshots. userID is 0 in local (single-player) mode; a
// hosted deployment passes the signed-in user's real ID so villages never
// cross between accounts.
type Store interface {
	// Save keeps a snapshot for later comparison. Two saves with the same
	// (userID, tag, capturedAt) are the same snapshot - the second is a
	// no-op, not an error, since re-loading an export is normal.
	Save(ctx context.Context, userID int64, tag string, capturedAt time.Time, raw []byte) error
	// Recent returns up to n snapshots for (userID, tag), newest first.
	Recent(ctx context.Context, userID int64, tag string, n int) ([]Snapshot, error)
	// Prune deletes snapshots captured before cutoff, for every village, but
	// never deletes the newest snapshot a village has - a village nobody has
	// exported in a month still shows its last known state, not nothing.
	Prune(ctx context.Context, cutoff time.Time) (int, error)
	// Villages lists every village userID has a snapshot for, newest capture
	// first. A caller that just wants "the one to show when nothing was
	// asked for" takes the first element - local mode has no other notion
	// of "currently loaded" any more, and a hosted deployment never did.
	Villages(ctx context.Context, userID int64) ([]Village, error)
}
