package pending

import (
	"context"
	"sync"
)

// Store persists pending actions per (userID, tag).
type Store interface {
	// List returns every pending action for (userID, tag), in the order
	// they were added.
	List(ctx context.Context, userID int64, tag string) ([]Action, error)
	// Add appends one new action.
	Add(ctx context.Context, userID int64, tag string, a Action) error
	// Remove drops the actions in ids from (userID, tag) - a single-element
	// ids is an undo click; more than one is Apply or reconciliation
	// retiring everything it determined no longer needs tracking. Removing
	// an id that is not present is not an error.
	Remove(ctx context.Context, userID int64, tag string, ids []string) error
}

type memoryKey struct {
	userID int64
	tag    string
}

// MemoryStore is an in-process Store: pending actions for the lifetime of
// the server, gone on restart. It is local mode's default, the same way
// internal/store/memory is for snapshots themselves.
type MemoryStore struct {
	mu   sync.Mutex
	data map[memoryKey][]Action
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[memoryKey][]Action{}}
}

func (s *MemoryStore) List(_ context.Context, userID int64, tag string) ([]Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.data[memoryKey{userID, tag}]
	out := make([]Action, len(list))
	copy(out, list)
	return out, nil
}

func (s *MemoryStore) Add(_ context.Context, userID int64, tag string, a Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := memoryKey{userID, tag}
	s.data[k] = append(s.data[k], a)
	return nil
}

func (s *MemoryStore) Remove(_ context.Context, userID int64, tag string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	k := memoryKey{userID, tag}
	kept := make([]Action, 0, len(s.data[k]))
	for _, a := range s.data[k] {
		if !drop[a.ID] {
			kept = append(kept, a)
		}
	}
	s.data[k] = kept
	return nil
}
