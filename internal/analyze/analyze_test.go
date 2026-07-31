package analyze

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/snapshot"
)

// testCatalog is a small stand-in for the generated catalog: a Town Hall, a
// Laboratory, a Builder's Hut, a Cannon gated on the Town Hall, a Barbarian
// gated on the Laboratory, a Builder Base hero and troop, a home hero gated
// by the Hero Hall rather than the Town Hall, and a vestigial building with
// no level data of its own.
func testCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Entries: map[string]catalog.Entry{
			"1000001": {Name: "Town Hall", Kind: "building", Class: "Town Hall", MaxLevel: 3, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}, Unlocks: []catalog.Unlock{
					{ID: 1000015, Name: "Builder's Hut", Quantity: 2},
					{ID: 1000064, Name: "B.O.B's Hut", Quantity: 1},
				}},
				{Requires: map[string]int{"th": 2}},
				{Requires: map[string]int{"th": 3}},
			}},
			"1000007": {Name: "Laboratory", Kind: "building", Class: "Army", MaxLevel: 5, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 3}},
				{Requires: map[string]int{"th": 3}},
				{Requires: map[string]int{"th": 4}},
				{Requires: map[string]int{"th": 5}},
				{Requires: map[string]int{"th": 6}},
			}},
			"1000015": {Name: "Builder's Hut", Kind: "building", Class: "Worker", MaxLevel: 1, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}},
			}},
			// Level 1 needs TH1, level 2 needs TH2 ... level 5 needs TH5. Each
			// level also costs Gold and takes time and strength, so the same
			// fixture covers the remaining-bill and strength maths.
			"1000008": {Name: "Cannon", Kind: "building", Class: "Defense", Resource: "Gold", MaxLevel: 5, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}, Cost: 250, Seconds: 10, Strength: 100},
				{Requires: map[string]int{"th": 2}, Cost: 1000, Seconds: 60, Strength: 200},
				{Requires: map[string]int{"th": 3}, Cost: 2000, Seconds: 600, Strength: 300},
				{Requires: map[string]int{"th": 4}, Cost: 4000, Seconds: 1200, Strength: 400},
				{Requires: map[string]int{"th": 5}, Cost: 8000, Seconds: 3600, Strength: 500},
			}},
			"4000000": {Name: "Barbarian", Kind: "unit", Resource: "Elixir", MaxLevel: 4, Levels: []catalog.Level{
				{Requires: map[string]int{"lab": 1}, Cost: 100, Seconds: 60},
				{Requires: map[string]int{"lab": 2}, Cost: 200, Seconds: 120},
				{Requires: map[string]int{"lab": 3}, Cost: 400, Seconds: 240},
				{Requires: map[string]int{"lab": 4}, Cost: 800, Seconds: 480},
			}},
			// A home hero gated by the Hero Hall, not directly by the Town
			// Hall - a high Town Hall must not let it run ahead.
			"28000000": {Name: "Barbarian King", Kind: "hero", MaxLevel: 3, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1, "herohall": 1}, Strength: 1000},
				{Requires: map[string]int{"th": 1, "herohall": 2}, Strength: 2000},
				{Requires: map[string]int{"th": 1, "herohall": 3}, Strength: 3000},
			}},
			// A Builder Base hero, gated on the Builder Hall - the Town Hall
			// belongs to the other village and must have no say here.
			"28000003": {Name: "Battle Machine", Kind: "hero", Village: 1, MaxLevel: 2, Levels: []catalog.Level{
				{Requires: map[string]int{"bh": 3}},
				{Requires: map[string]int{"bh": 5}},
			}},
			// A Builder Base troop, gated on the Star Laboratory only - the
			// Builder Hall must have no say here either.
			"4000031": {Name: "Raged Barbarian", Kind: "unit", Village: 1, MaxLevel: 3, Levels: []catalog.Level{
				{Requires: map[string]int{"starlab": 1}},
				{Requires: map[string]int{"starlab": 2}},
				{Requires: map[string]int{"starlab": 4}},
			}},
			// Unlocked at Town Hall 1 but carries no level data of its own -
			// Missing must not nag about it.
			"1000064": {Name: "B.O.B's Hut", Kind: "building", Class: "Worker2"},
		},
	}
}

func find(t *testing.T, r *Report, village, group string) Group {
	t.Helper()
	for _, v := range r.Villages {
		if v.Key != village {
			continue
		}
		for _, g := range v.Groups {
			if g.Key == group {
				return g
			}
		}
	}
	t.Fatalf("group %s/%s not found", village, group)
	return Group{}
}

func TestReachableRespectsTownHall(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 3, Cnt: 2},
		},
	}
	r := Run(e, testCatalog(), e.CapturedAt())

	if r.Gates.TownHall != 3 {
		t.Fatalf("town hall gate = %d, want 3", r.Gates.TownHall)
	}
	def := find(t, r, "home", "defense")
	cannon := def.Items[0]
	if cannon.Reachable != 3 {
		t.Errorf("cannon ceiling at TH3 = %d, want 3", cannon.Reachable)
	}
	// Two cannons at level 3 with a ceiling of 3 is finished work.
	if cannon.Completion != 1 || cannon.CopiesAtMax != 2 {
		t.Errorf("cannon completion = %.2f (%d at max), want 1.00 (2 at max)",
			cannon.Completion, cannon.CopiesAtMax)
	}
}

func TestCountsIncludeRowsWithoutCnt(t *testing.T) {
	// Three cannons: two grouped, one on its own row mid-upgrade.
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 5, Cnt: 1},
			{Data: 1000008, Lvl: 4, Cnt: 2},
			{Data: 1000008, Lvl: 3, Timer: 600},
		},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	cannon := find(t, r, "home", "defense").Items[0]

	if cannon.Copies != 3 {
		t.Errorf("copies = %d, want 3 (a row without cnt is one copy)", cannon.Copies)
	}
	if cannon.Upgrading != 1 {
		t.Errorf("upgrading = %d, want 1", cannon.Upgrading)
	}
	// Levels 4+4+3 of a possible 3x5.
	if cannon.LevelsDone != 11 || cannon.LevelsTarget != 15 {
		t.Errorf("levels = %d/%d, want 11/15", cannon.LevelsDone, cannon.LevelsTarget)
	}
}

// Items missing from the catalog have no ceiling to measure against. Their
// levels must stay out of the percentages, or totals climb past 100%.
func TestUnknownItemsDoNotInflateTotals(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 2, Cnt: 1},
		},
		// Not in the catalog, and deliberately high level.
		Equipment: []snapshot.Item{{Data: 90000000, Lvl: 25}},
	}
	r := Run(e, testCatalog(), e.CapturedAt())

	eq := find(t, r, "home", "equipment")
	if eq.Measured {
		t.Error("equipment group should be unmeasured without catalog data")
	}
	if eq.LevelsTarget != 0 || eq.LevelsDone != 0 {
		t.Errorf("unmeasured group contributed %d/%d levels, want 0/0", eq.LevelsDone, eq.LevelsTarget)
	}

	var home Village
	for _, v := range r.Villages {
		if v.Key == "home" {
			home = v
		}
	}
	// Town Hall 3 of 3 plus a Cannon at 2 of 3: 5 levels of 6.
	if home.LevelsDone != 5 || home.LevelsTarget != 6 {
		t.Errorf("village levels = %d/%d, want 5/6", home.LevelsDone, home.LevelsTarget)
	}
	if home.Completion >= 1 {
		t.Errorf("village completion = %.3f, should be under 1", home.Completion)
	}
	if home.Unmeasured != 1 {
		t.Errorf("unmeasured count = %d, want 1", home.Unmeasured)
	}
}

func TestJobsAreOrderedAndTimedFromCapture(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000015, Lvl: 1, Cnt: 2},
			{Data: 1000008, Lvl: 1, Timer: 7200},
		},
		Units:   []snapshot.Item{{Data: 4000000, Lvl: 1, Timer: 60}},
		Helpers: []snapshot.Item{{Data: 93000000, Lvl: 1, HelperCooldown: 30}},
	}
	r := Run(e, testCatalog(), e.CapturedAt())

	if len(r.Jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(r.Jobs))
	}
	if r.Jobs[0].Remaining > r.Jobs[1].Remaining || r.Jobs[1].Remaining > r.Jobs[2].Remaining {
		t.Error("jobs are not sorted by time remaining")
	}
	// Finish times must be absolute, anchored to the export timestamp.
	want := e.CapturedAt().Add(7200e9)
	for _, j := range r.Jobs {
		if j.Name == "Cannon" && !j.FinishesAt.Equal(want) {
			t.Errorf("cannon finishes at %v, want %v", j.FinishesAt, want)
		}
	}

	var builders, lab Lane
	for _, l := range r.Lanes {
		switch l.Key {
		case "builder":
			builders = l
		case "lab":
			lab = l
		}
	}
	if builders.Total != 2 || builders.Busy != 1 {
		t.Errorf("builders = %d/%d, want 1/2", builders.Busy, builders.Total)
	}
	if builders.NextFreeAt != nil {
		t.Error("a lane with a spare slot should not report a next-free time")
	}
	if lab.Busy != 1 || lab.NextFreeAt == nil {
		t.Error("the laboratory is full, so it should report when it frees up")
	}
}

// An upgrade whose timer has already run out by the time someone looks -
// without a fresh export saying so - must count as landed everywhere: the
// item's level, its group's completion, and the lane it was running in. Only
// the jobs list (a record of what recently finished) still mentions it.
func TestElapsedTimerLandsEverywhereNotJustInJobs(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: idTownHall, Lvl: 3, Cnt: 1},
			{Data: 1000015, Lvl: 1, Cnt: 2},
			{Data: 1000008, Lvl: 2, Timer: 600},
		},
	}
	capturedAt := e.CapturedAt()

	before := Run(e, testCatalog(), capturedAt.Add(599*time.Second))
	cannon := find(t, before, "home", "defense").Items[0]
	if cannon.Upgrading != 1 || cannon.LevelsDone != 2 {
		t.Fatalf("just before landing: upgrading=%d levelsDone=%d, want 1/2", cannon.Upgrading, cannon.LevelsDone)
	}
	var builders Lane
	for _, l := range before.Lanes {
		if l.Key == "builder" {
			builders = l
		}
	}
	if builders.Busy != 1 {
		t.Errorf("just before landing: builder busy = %d, want 1", builders.Busy)
	}

	after := Run(e, testCatalog(), capturedAt.Add(700*time.Second))
	cannon = find(t, after, "home", "defense").Items[0]
	if cannon.Upgrading != 0 {
		t.Errorf("after landing: upgrading = %d, want 0", cannon.Upgrading)
	}
	if cannon.LevelsDone != 3 {
		t.Errorf("after landing: levelsDone = %d, want 3 (the landed level, not the pre-upgrade 2)", cannon.LevelsDone)
	}
	if len(after.Jobs) != 1 {
		t.Errorf("after landing: jobs = %d, want 1 (still listed as a recently-landed job)", len(after.Jobs))
	}
	for _, l := range after.Lanes {
		if l.Key == "builder" {
			builders = l
		}
	}
	if builders.Busy != 0 {
		t.Errorf("after landing: builder busy = %d, want 0 (that builder is free again)", builders.Busy)
	}
}

// A hall's own timer elapsing must raise every gate it controls, not just
// its own reported level - an item capped by the old Town Hall must open up
// the moment the Town Hall upgrade lands, without waiting on a fresh export.
func TestElapsedHallTimerRaisesGatesForEverythingElse(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: idTownHall, Lvl: 1, Timer: 3600},
			{Data: 1000008, Lvl: 1, Cnt: 1},
		},
	}
	capturedAt := e.CapturedAt()
	r := Run(e, testCatalog(), capturedAt.Add(2*time.Hour))

	if r.Gates.TownHall != 2 {
		t.Fatalf("town hall gate = %d, want 2 once its own timer has elapsed", r.Gates.TownHall)
	}
	cannon := find(t, r, "home", "defense").Items[0]
	if cannon.Reachable != 2 {
		t.Errorf("cannon ceiling = %d, want 2 (gated on the now-landed Town Hall 2)", cannon.Reachable)
	}
}

func TestNewBuildIsFlagged(t *testing.T) {
	e := &snapshot.Export{
		Timestamp:  1700000000,
		Buildings:  []snapshot.Item{{Data: 1000001, Lvl: 3, Cnt: 1}},
		Buildings2: []snapshot.Item{{Data: 1000008, Lvl: 0, Timer: 300}},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	if len(r.Jobs) != 1 || !r.Jobs[0].New {
		t.Fatal("a level 0 item with a timer is a first-time build")
	}
	if r.Jobs[0].ToLevel != 1 {
		t.Errorf("target level = %d, want 1", r.Jobs[0].ToLevel)
	}
}

func TestRejectsFilesThatAreNotExports(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"empty object", `{}`, "buildings"},
		{"no timestamp", `{"buildings":[{"data":1000001,"lvl":1}]}`, "timestamp"},
		{"not json", `nope`, "valid JSON"},
	} {
		_, err := snapshot.Parse(strings.NewReader(tc.body))
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q should mention %q", tc.name, err, tc.want)
		}
	}
}

func TestBucketsMergeAndSortDescending(t *testing.T) {
	got := mergeBuckets([]Bucket{{Level: 3, Count: 1}, {Level: 5, Count: 2}, {Level: 3, Count: 4}})
	want := []Bucket{{Level: 5, Count: 2}, {Level: 3, Count: 5}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A hero can be gated by two things at once. A Town Hall far ahead of the
// Hero Hall must not let the hero run ahead of what its own hall allows.
func TestHeroCeilingIsLimitedByHeroHallNotTownHall(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: idTownHall, Lvl: 5, Cnt: 1},
			{Data: idHeroHall, Lvl: 1, Cnt: 1},
			{Data: 28000000, Lvl: 1, Cnt: 1},
		},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	if r.Gates.HeroHall != 1 {
		t.Fatalf("hero hall gate = %d, want 1", r.Gates.HeroHall)
	}
	hero := find(t, r, "home", "hero").Items[0]
	if hero.Reachable != 1 {
		t.Errorf("hero ceiling = %d, want 1 (Hero Hall 1 allows only level 1, even at Town Hall 5)", hero.Reachable)
	}
}

// A Builder Base troop must gate on the Star Laboratory alone. A low
// Builder Hall must not cap it - Builder Base troops have no such gate in
// the game itself.
func TestBuilderBaseTroopCeilingIgnoresBuilderHall(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings2: []snapshot.Item{
			{Data: idBuilderHall, Lvl: 1, Cnt: 1},
			{Data: idStarLaboratory, Lvl: 4, Cnt: 1},
		},
		Units2: []snapshot.Item{{Data: 4000031, Lvl: 1, Cnt: 1}},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	unit := find(t, r, "builder", "unit").Items[0]
	if unit.Reachable != 3 {
		t.Errorf("raged barbarian ceiling = %d, want 3 (a Builder Hall of 1 must not cap a Star Lab 4 troop)", unit.Reachable)
	}
}

// A Builder Base hero must gate on the Builder Hall, not the Home Village
// Town Hall, which does not exist from the Builder Base's perspective here.
func TestBuilderBaseHeroGatesOnBuilderHall(t *testing.T) {
	e := &snapshot.Export{
		Timestamp:  1700000000,
		Buildings2: []snapshot.Item{{Data: idBuilderHall, Lvl: 4, Cnt: 1}},
		Heroes2:    []snapshot.Item{{Data: 28000003, Lvl: 1, Cnt: 1}},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	hero := find(t, r, "builder", "hero").Items[0]
	if hero.Reachable != 1 {
		t.Errorf("battle machine ceiling at BH4 = %d, want 1 (level 2 needs BH5)", hero.Reachable)
	}
}

// The remaining bill sums cost, ore-folded currencies and time across every
// copy, from wherever each one currently sits up to the shared ceiling.
func TestRemainingBillAndStrengthSumAcrossCopies(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: idTownHall, Lvl: 5, Cnt: 1},
			{Data: 1000008, Lvl: 2, Cnt: 2},
			{Data: 1000008, Lvl: 4, Cnt: 1},
		},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	cannon := find(t, r, "home", "defense").Items[0]

	// Two copies climb levels 3,4,5 (2000+4000+8000=14000 each); one copy
	// climbs level 5 alone (8000). 14000*2 + 8000 = 36000 gold.
	if got := cannon.Remaining.Cost["Gold"]; got != 36000 {
		t.Errorf("cannon remaining gold = %d, want 36000", got)
	}
	if cannon.Remaining.Steps != 7 {
		t.Errorf("cannon remaining steps = %d, want 7 ((5-2)*2 + (5-4)*1)", cannon.Remaining.Steps)
	}
	if got := (600+1200+3600)*2 + 3600; int64(got) != cannon.Remaining.Seconds {
		t.Errorf("cannon remaining seconds = %d, want %d", cannon.Remaining.Seconds, got)
	}
	if r.Bill.Cost["Gold"] != 36000 {
		t.Errorf("bill gold = %d, want 36000", r.Bill.Cost["Gold"])
	}
	if r.Bill.BuilderSeconds != cannon.Remaining.Seconds {
		t.Errorf("bill builder seconds = %d, want %d (cannons run through the builder lane)", r.Bill.BuilderSeconds, cannon.Remaining.Seconds)
	}
	// Strength done: 2 copies at level 2 (200 each) + 1 copy at level 4 (400) = 800.
	if r.Strength.Done != 800 {
		t.Errorf("strength done = %d, want 800", r.Strength.Done)
	}
	// Strength reachable: ceiling 5 (500) across all 3 copies = 1500.
	if r.Strength.Reachable != 1500 {
		t.Errorf("strength reachable = %d, want 1500", r.Strength.Reachable)
	}
}

// Missing must report a real shortfall (a Builder's Hut the Town Hall
// allows but the village has not built) while silently skipping an unlock
// entry that has no level data of its own to act on.
func TestMissingReportsRealShortfallsAndSkipsVestigialUnlocks(t *testing.T) {
	e := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{
			{Data: idTownHall, Lvl: 1, Cnt: 1},
			{Data: 1000015, Lvl: 1, Cnt: 1},
		},
	}
	r := Run(e, testCatalog(), e.CapturedAt())
	if len(r.Missing) != 1 {
		t.Fatalf("missing = %+v, want exactly 1 entry", r.Missing)
	}
	m := r.Missing[0]
	if m.Name != "Builder's Hut" || m.Owned != 1 || m.Allowed != 2 {
		t.Errorf("missing entry = %+v, want Builder's Hut owned=1 allowed=2", m)
	}
	for _, m := range r.Missing {
		if m.Name == "B.O.B's Hut" {
			t.Error("B.O.B's Hut has no level data and must not be reported as missing")
		}
	}
}

func TestDiffDetectsLandedStartedAndCleared(t *testing.T) {
	cat := testCatalog()
	prev := &snapshot.Export{
		Timestamp: 1700000000,
		Buildings: []snapshot.Item{{Data: 1000008, Lvl: 2, Timer: 600}},
		Obstacles: []snapshot.Item{{Data: 8000000, Cnt: 1}},
	}
	cur := &snapshot.Export{
		Timestamp: 1700001000,
		Buildings: []snapshot.Item{
			{Data: 1000008, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 3, Timer: 300},
		},
	}
	cl := Diff(prev, cur, cat)

	var landed, started, cleared int
	for _, c := range cl.Changes {
		switch c.Type {
		case "landed":
			landed++
			if c.Name != "Cannon" || c.FromLevel != 2 || c.ToLevel != 3 || c.Count != 1 {
				t.Errorf("landed change = %+v, want Cannon 2->3 x1", c)
			}
		case "started":
			started++
			if c.Name != "Cannon" || c.FromLevel != 3 || c.ToLevel != 4 || c.Count != 1 {
				t.Errorf("started change = %+v, want Cannon 3->4 x1", c)
			}
		case "cleared":
			cleared++
			if c.Kind != "obstacle" || c.Count != 1 {
				t.Errorf("cleared change = %+v, want an obstacle x1", c)
			}
		}
	}
	if landed != 1 || started != 1 || cleared != 1 {
		t.Fatalf("changes = %+v, want exactly one landed, one started, one cleared", cl.Changes)
	}
}

// A live integration check against two real exports captured 18 hours
// apart, when they are available on disk. It is skipped rather than failed
// when they are not, since they are a developer's personal village data and
// not part of the repository.
func TestDiffAgainstRealExports(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	catPath := "../../data/catalog.json"
	cat, err := catalog.Load(catPath)
	if err != nil {
		t.Skipf("no generated catalog at %s: %v", catPath, err)
	}

	load := func(name string) *snapshot.Export {
		f, err := os.Open(home + "/Downloads/" + name)
		if err != nil {
			t.Skipf("no %s in ~/Downloads: %v", name, err)
		}
		defer f.Close()
		exp, err := snapshot.Parse(f)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return exp
	}
	prev := load("village.json")
	cur := load("village2.json")

	cl := Diff(prev, cur, cat)
	want := map[string]bool{
		"Cannon|landed|10|11":      false,
		"Wizard Tower|landed|7|8":  false,
		"Skeleton Trap|landed|2|3": false,
		"Giant|landed|6|7":         false,
		"Multi Mortar|built|0|1":   false,
		"Trunk|cleared|0|0":        false,
	}
	for _, c := range cl.Changes {
		key := c.Name + "|" + c.Type + "|" + strconv.Itoa(c.FromLevel) + "|" + strconv.Itoa(c.ToLevel)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("expected change %q was not found in %+v", k, cl.Changes)
		}
	}
}
