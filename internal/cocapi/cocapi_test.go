package cocapi

import (
	"context"
	"testing"
)

func TestMockClientMarksResponsesAsMock(t *testing.T) {
	c := New("")
	p, err := c.Player(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Player: %v", err)
	}
	if !p.Mock {
		t.Error("Player.Mock = false, want true when no token is configured")
	}
	if p.Tag != "#ABC123" {
		t.Errorf("Tag = %q, want normalized to #ABC123", p.Tag)
	}
	if len(p.Heroes) == 0 {
		t.Error("mock player should carry sample heroes to actually preview the UI with")
	}

	cl, err := c.Clan(context.Background(), "#xyz")
	if err != nil {
		t.Fatalf("Clan: %v", err)
	}
	if !cl.Mock {
		t.Error("Clan.Mock = false, want true when no token is configured")
	}
	if len(cl.Members) == 0 || len(cl.RaidSeasons) == 0 {
		t.Error("mock clan should have sample members and raid seasons")
	}
}

// The one property every gated/mock switch in this codebase needs: real
// data and mock data must never be reachable from the same configuration.
func TestNewPicksRealOnlyWithAToken(t *testing.T) {
	if _, ok := New("").(mockClient); !ok {
		t.Error(`New("") should return the mock client`)
	}
	if _, ok := New("tok").(*realClient); !ok {
		t.Error(`New("tok") should return the real client`)
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"abc123":  "#ABC123",
		"#abc123": "#ABC123",
		" #abc ":  "#ABC",
		"":        "",
	}
	for in, want := range cases {
		if got := normalizeTag(in); got != want {
			t.Errorf("normalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}
