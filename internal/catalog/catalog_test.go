package catalog

import "testing"

func TestIconURLSubstitutesLevelPlaceholder(t *testing.T) {
	e := Entry{Icon: "buildings/home-village/cannon/level_{level}.webp", IconMaxLevel: 21}
	got := e.IconURL(11)
	want := "https://assets.clashk.ing/buildings/home-village/cannon/level_11.webp"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIconURLClampsToHighestArtThatExists(t *testing.T) {
	e := Entry{Icon: "buildings/home-village/cannon/level_{level}.webp", IconMaxLevel: 21}
	got := e.IconURL(999)
	want := "https://assets.clashk.ing/buildings/home-village/cannon/level_21.webp"
	if got != want {
		t.Errorf("got %q, want %q (clamped to iconMaxLevel)", got, want)
	}
	if got := e.IconURL(0); got != "https://assets.clashk.ing/buildings/home-village/cannon/level_1.webp" {
		t.Errorf("level 0 should floor to level 1, got %q", got)
	}
}

func TestIconURLPassesThroughSingleIconUnchanged(t *testing.T) {
	e := Entry{Icon: "heroes/barbarian_king/icon.webp"} // IconMaxLevel 0: not tiered
	got := e.IconURL(40)
	want := "https://assets.clashk.ing/heroes/barbarian_king/icon.webp"
	if got != want {
		t.Errorf("got %q, want %q (non-tiered icons ignore level)", got, want)
	}
}

func TestIconURLEmptyWhenNoIconKnown(t *testing.T) {
	e := Entry{}
	if got := e.IconURL(5); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestReachableRequiresEveryGateAtOnce(t *testing.T) {
	e := Entry{Levels: []Level{
		{Requires: map[string]int{"th": 1, "herohall": 1}},
		{Requires: map[string]int{"th": 1, "herohall": 2}},
		{Requires: map[string]int{"th": 1, "herohall": 3}},
	}}
	// Town Hall is far ahead, but Hero Hall 1 only clears the first level.
	got := e.Reachable(Gates{TownHall: 10, HeroHall: 1})
	if got != 1 {
		t.Errorf("reachable = %d, want 1 (blocked by Hero Hall despite a high Town Hall)", got)
	}
}

func TestReachableIgnoresGatesNotPresentOnTheLevel(t *testing.T) {
	// A level with no requirement key at all - every Builder Hall level, for
	// instance - must never be blocked by an unrelated gate sitting at 0.
	e := Entry{Levels: []Level{{}, {}, {}}}
	if got := e.Reachable(Gates{}); got != 3 {
		t.Errorf("reachable = %d, want 3 (nothing gates these levels)", got)
	}
}

func TestReachableFloorsAtOneWhenNothingIsUnlockedYet(t *testing.T) {
	e := Entry{Levels: []Level{{Requires: map[string]int{"th": 5}}}}
	got := e.Reachable(Gates{TownHall: 1})
	if got != 1 {
		t.Errorf("reachable = %d, want 1 (the item exists in the village, so it has a floor)", got)
	}
}

func TestBetweenSumsCostOreAndSecondsAcrossLevels(t *testing.T) {
	e := Entry{
		Resource: "Gold",
		Levels: []Level{
			{Cost: 250, Seconds: 10},
			{Cost: 1000, Seconds: 60},
			{Cost: 2000, Seconds: 600, AltRes: "Elixir"},
		},
	}
	cost, ore, seconds := e.Between(0, 3)
	if cost["Gold"] != 1250 || cost["Elixir"] != 2000 {
		t.Errorf("cost = %v, want Gold=1250 Elixir=2000 (the top level's alt resource overrides Gold)", cost)
	}
	if !ore.Zero() {
		t.Errorf("ore = %+v, want zero (this entry spends currency, not ore)", ore)
	}
	if seconds != 670 {
		t.Errorf("seconds = %d, want 670", seconds)
	}
}

func TestBetweenClampsToWhatLevelsExist(t *testing.T) {
	e := Entry{Resource: "Gold", Levels: []Level{{Cost: 100}, {Cost: 200}}}
	cost, _, _ := e.Between(0, 50) // asking for far more levels than exist
	if cost["Gold"] != 300 {
		t.Errorf("cost = %v, want Gold=300 (clamped to the 2 levels that actually exist)", cost)
	}
}

func TestFallbackNameCoversPetsAtTheCorrectBlock(t *testing.T) {
	name, kind := FallbackName(73000099)
	if kind != "pet" {
		t.Errorf("kind = %q, want pet (pets live at the 73,000,000 block, not 88,000,000)", kind)
	}
	if name == "" {
		t.Error("expected a non-empty fallback name")
	}
}

func TestFallbackNameCoversGuardians(t *testing.T) {
	_, kind := FallbackName(107000005)
	if kind != "building" {
		t.Errorf("kind = %q, want building (guardians fold into the same group as other defenses)", kind)
	}
}
