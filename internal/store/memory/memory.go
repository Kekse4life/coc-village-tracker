// Package memory is an in-process Store: history for the lifetime of the
// server, gone on restart. It is the default when no history backend is
// configured, so the rest of the server never needs a nil-store check.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/you/coc-progress/internal/store"
)

var _ store.Store = (*Store)(nil)

type key struct {
	userID int64
	tag    string
}

type entry struct {
	capturedAt time.Time
	raw        []byte
}

type Store struct {
	mu   sync.RWMutex
	data map[key][]entry
	// limit caps how many snapshots any one (userID, tag) keeps, oldest
	// trimmed first as new ones arrive. Zero means unlimited.
	limit int
}

// New builds an unlimited in-process store: every distinct (userID, tag,
// capturedAt) ever saved stays until Prune runs.
func New() *Store {
	return &Store{data: map[key][]entry{}}
}

// NewWithLimit builds a store that keeps at most perTag snapshots for any
// one (userID, tag) - local mode's default, so leaving the dashboard open
// across many exports of the same village cannot grow without bound.
func NewWithLimit(perTag int) *Store {
	return &Store{data: map[key][]entry{}, limit: perTag}
}

func (s *Store) Save(_ context.Context, userID int64, tag string, capturedAt time.Time, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{userID, tag}
	for _, e := range s.data[k] {
		if e.capturedAt.Equal(capturedAt) {
			return nil // already have this exact snapshot
		}
	}
	cp := append([]byte(nil), raw...)
	list := append(s.data[k], entry{capturedAt, cp})
	sort.Slice(list, func(i, j int) bool { return list[i].capturedAt.Before(list[j].capturedAt) })
	if s.limit > 0 && len(list) > s.limit {
		list = list[len(list)-s.limit:] // drop oldest first
	}
	s.data[k] = list
	return nil
}

// Forget removes every snapshot for (userID, tag) - a "stop tracking this
// village" action this in-process store can offer that Postgres, with its
// own retention policy, deliberately does not.
func (s *Store) Forget(_ context.Context, userID int64, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key{userID, tag})
	return nil
}

func (s *Store) Recent(_ context.Context, userID int64, tag string, n int) ([]store.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.data[key{userID, tag}]
	out := make([]store.Snapshot, 0, n)
	for i := len(list) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, store.Snapshot{Tag: tag, CapturedAt: list[i].capturedAt, Raw: list[i].raw})
	}
	return out, nil
}

func (s *Store) Villages(_ context.Context, userID int64) ([]store.Village, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []store.Village
	for k, list := range s.data {
		if k.userID != userID || len(list) == 0 {
			continue
		}
		out = append(out, store.Village{
			Tag:            k.tag,
			LastCapturedAt: list[len(list)-1].capturedAt,
			Snapshots:      len(list),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastCapturedAt.After(out[j].LastCapturedAt) })
	return out, nil
}

func (s *Store) Prune(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for k, list := range s.data {
		if len(list) <= 1 {
			continue
		}
		newest := list[len(list)-1]
		kept := make([]entry, 0, len(list))
		for _, e := range list[:len(list)-1] {
			if e.capturedAt.Before(cutoff) {
				removed++
				continue
			}
			kept = append(kept, e)
		}
		s.data[k] = append(kept, newest)
	}
	return removed, nil
}
