package pending

import (
	"context"
	"testing"
)

func TestMemoryStoreAddListRemove(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	a1 := Action{ID: "a1", ItemID: 1000008}
	a2 := Action{ID: "a2", ItemID: 1000002}
	if err := s.Add(ctx, 0, "#TAG", a1); err != nil {
		t.Fatalf("add a1: %v", err)
	}
	if err := s.Add(ctx, 0, "#TAG", a2); err != nil {
		t.Fatalf("add a2: %v", err)
	}

	got, err := s.List(ctx, 0, "#TAG")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d actions, want 2", len(got))
	}

	if err := s.Remove(ctx, 0, "#TAG", []string{"a1"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ = s.List(ctx, 0, "#TAG")
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("after removing a1, got %+v, want only a2", got)
	}
}

func TestMemoryStoreScopesByUserAndTag(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	s.Add(ctx, 1, "#TAG", Action{ID: "a"})
	s.Add(ctx, 0, "#OTHER", Action{ID: "b"})

	if got, _ := s.List(ctx, 2, "#TAG"); len(got) != 0 {
		t.Errorf("a different user saw %d actions belonging to user 1", len(got))
	}
	if got, _ := s.List(ctx, 1, "#TAG"); len(got) != 1 {
		t.Errorf("user 1 saw %d actions for #TAG, want 1", len(got))
	}
	if got, _ := s.List(ctx, 0, "#OTHER"); len(got) != 1 {
		t.Errorf("village #OTHER saw %d actions, want 1", len(got))
	}
}

func TestMemoryStoreRemoveOfUnknownIDIsANoOp(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	s.Add(ctx, 0, "#TAG", Action{ID: "a"})
	if err := s.Remove(ctx, 0, "#TAG", []string{"never-existed"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ := s.List(ctx, 0, "#TAG")
	if len(got) != 1 {
		t.Errorf("got %d actions after removing an unknown id, want 1 untouched", len(got))
	}
}
