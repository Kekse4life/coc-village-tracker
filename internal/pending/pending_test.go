package pending

import (
	"testing"
	"time"

	"github.com/you/coc-progress/internal/analyze"
	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/snapshot"
)

// testCatalog is a minimal stand-in with just enough level data for
// analyze.Run to compute real completion numbers: a Town Hall, a timed
// Cannon (a real build time on every level, the common case), and an
// instant Wall (no seconds on any level, matching how Walls and hero
// equipment actually upgrade in the real catalog).
func testCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Entries: map[string]catalog.Entry{
			"1000001": {Name: "Town Hall", Kind: "building", Class: "Town Hall", MaxLevel: 3, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}},
				{Requires: map[string]int{"th": 2}},
				{Requires: map[string]int{"th": 3}},
			}},
			"1000008": {Name: "Cannon", Kind: "building", Class: "Defense", Resource: "Gold", MaxLevel: 5, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}, Cost: 250, Seconds: 10},
				{Requires: map[string]int{"th": 2}, Cost: 1000, Seconds: 60},
				{Requires: map[string]int{"th": 3}, Cost: 2000, Seconds: 600},
				{Requires: map[string]int{"th": 4}, Cost: 4000, Seconds: 1200},
				{Requires: map[string]int{"th": 5}, Cost: 8000, Seconds: 3600},
			}},
			"1000002": {Name: "Wall", Kind: "building", Class: "Wall", Resource: "Gold", MaxLevel: 5, Levels: []catalog.Level{
				{Requires: map[string]int{"th": 1}, Cost: 100},
				{Requires: map[string]int{"th": 2}, Cost: 200},
				{Requires: map[string]int{"th": 3}, Cost: 400},
				{Requires: map[string]int{"th": 4}, Cost: 800},
				{Requires: map[string]int{"th": 5}, Cost: 1600},
			}},
		},
	}
}

func countCopies(items []snapshot.Item, id int) int {
	n := 0
	for _, it := range items {
		if it.Data == id {
			n += it.Count()
		}
	}
	return n
}

func action(itemID, from, to int, seconds int64, startedAt time.Time) Action {
	return Action{ID: NewID(), ItemID: itemID, Village: "home", FromLevel: from, ToLevel: to, Seconds: seconds, StartedAt: startedAt}
}

// The bug a design-review pass caught before this ever ran: appending a
// synthetic row instead of splitting one off inflates every completion
// percentage. This runs the overlay through the real analyzer and checks
// the numbers a Progress tab would actually show.
func TestApplyDoesNotInflateCopiesThroughAnalyze(t *testing.T) {
	capturedAt := time.Unix(1700000000, 0).UTC()
	e := &snapshot.Export{
		Timestamp: capturedAt.Unix(),
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 3, Cnt: 3}, // three idle Cannons at level 3
		},
	}
	a := action(1000008, 3, 4, 600, capturedAt)

	overlaid, applied, stale := Apply(e, []Action{a}, testCatalog())
	if len(applied) != 1 || len(stale) != 0 {
		t.Fatalf("applied = %d, stale = %d, want 1 applied and 0 stale", len(applied), len(stale))
	}
	if got := countCopies(overlaid.Buildings, 1000008); got != 3 {
		t.Fatalf("raw copies after overlay = %d, want 3 (splitting must not inflate the count)", got)
	}
	// e itself must be untouched - Apply is never allowed to mutate the
	// caller's export.
	if got := countCopies(e.Buildings, 1000008); got != 3 {
		t.Fatalf("original export's copies = %d, want 3 (Apply must not mutate its input)", got)
	}
	if e.Buildings[1].Timer != 0 {
		t.Fatalf("original export's Cannon row gained a timer - Apply mutated e")
	}

	rep := analyze.Run(overlaid, testCatalog(), capturedAt.Add(1*time.Second))
	var cannon analyze.ItemStat
	for _, v := range rep.Villages {
		for _, g := range v.Groups {
			for _, it := range g.Items {
				if it.ID == 1000008 {
					cannon = it
				}
			}
		}
	}
	if cannon.Copies != 3 {
		t.Errorf("analyzed copies = %d, want 3", cannon.Copies)
	}
	if cannon.Upgrading != 1 {
		t.Errorf("analyzed upgrading = %d, want 1", cannon.Upgrading)
	}
	// Two copies still at 3, one mid-upgrade (still counted at 3 until its
	// timer elapses): levelsDone = 3+3+3 = 9, not 12.
	if cannon.LevelsDone != 9 {
		t.Errorf("analyzed levelsDone = %d, want 9 (3 copies still worth level 3 apiece, not 4)", cannon.LevelsDone)
	}
	if len(rep.Jobs) != 1 {
		t.Errorf("jobs = %d, want 1", len(rep.Jobs))
	}
}

// A row with a single copy (Cnt left at its zero value) must be mutated in
// place, not appended to - Item.Count() reads a zero Cnt as one copy, so
// "decrementing" it to zero would still describe one copy, not none.
func TestApplySingleCopyMutatesInPlaceRatherThanAppending(t *testing.T) {
	capturedAt := time.Unix(1700000000, 0).UTC()
	e := &snapshot.Export{
		Timestamp: capturedAt.Unix(),
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 3}, // one Cannon, no Cnt field at all
		},
	}
	a := action(1000008, 3, 4, 600, capturedAt)

	overlaid, applied, _ := Apply(e, []Action{a}, testCatalog())
	if len(applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(applied))
	}
	if len(overlaid.Buildings) != len(e.Buildings) {
		t.Fatalf("buildings rows = %d, want %d (a single copy must mutate in place, not append)", len(overlaid.Buildings), len(e.Buildings))
	}
	if countCopies(overlaid.Buildings, 1000008) != 1 {
		t.Fatalf("copies = %d, want 1", countCopies(overlaid.Buildings, 1000008))
	}
	var row snapshot.Item
	for _, it := range overlaid.Buildings {
		if it.Data == 1000008 {
			row = it
		}
	}
	if row.Lvl != 3 || row.Timer != 600 {
		t.Errorf("cannon row = %+v, want level 3 (still leaving it) with a 600s timer", row)
	}
}

// An instant upgrade (no seconds on the catalog level - Walls, equipment,
// helpers, most super troops) must bump the level immediately with no
// timer, no Job, and no lane consumed - manufacturing a countdown for
// something the game itself never times would be actively wrong.
func TestApplyInstantActionBumpsLevelWithNoJobOrLane(t *testing.T) {
	capturedAt := time.Unix(1700000000, 0).UTC()
	e := &snapshot.Export{
		Timestamp: capturedAt.Unix(),
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000002, Lvl: 4, Cnt: 1}, // one Wall at level 4
		},
	}
	a := action(1000002, 4, 5, 0, capturedAt) // Seconds: 0 - instant

	overlaid, applied, _ := Apply(e, []Action{a}, testCatalog())
	if len(applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(applied))
	}
	var wall snapshot.Item
	for _, it := range overlaid.Buildings {
		if it.Data == 1000002 {
			wall = it
		}
	}
	if wall.Lvl != 5 {
		t.Errorf("wall level = %d, want 5 (already arrived)", wall.Lvl)
	}
	if wall.Timer != 0 {
		t.Errorf("wall timer = %d, want 0 (an instant bump has nothing to count down)", wall.Timer)
	}

	rep := analyze.Run(overlaid, testCatalog(), capturedAt)
	if len(rep.Jobs) != 0 {
		t.Errorf("jobs = %d, want 0 - an instant bump must not manufacture a countdown", len(rep.Jobs))
	}
	for _, l := range rep.Lanes {
		if l.Key == "builder" && l.Busy != 0 {
			t.Errorf("builder lane busy = %d, want 0 - an instant bump must not consume a builder slot", l.Busy)
		}
	}
}

// The timer is rebased onto whatever export it is overlaid on, not fixed
// at declaration time - the same action must show less time remaining
// against a later capture, and none at all once the export postdates when
// it would have finished.
func TestApplyRebasesTimerOntoCapturedAt(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	buildExport := func(capturedAt time.Time) *snapshot.Export {
		return &snapshot.Export{
			Timestamp: capturedAt.Unix(),
			Buildings: []snapshot.Item{
				{Data: 1000001, Lvl: 3, Cnt: 1},
				{Data: 1000008, Lvl: 3, Cnt: 1},
			},
		}
	}
	a := action(1000008, 3, 4, 3600, start) // one hour, starting now

	// Captured right at the start: nearly the full hour remains.
	overlaid, applied, stale := Apply(buildExport(start), []Action{a}, testCatalog())
	if len(applied) != 1 || len(stale) != 0 {
		t.Fatalf("at start: applied = %d, stale = %d, want 1/0", len(applied), len(stale))
	}
	var row snapshot.Item
	for _, it := range overlaid.Buildings {
		if it.Data == 1000008 {
			row = it
		}
	}
	if row.Timer != 3600 {
		t.Errorf("timer at start = %d, want 3600", row.Timer)
	}

	// Captured 30 minutes in: half the timer remains.
	overlaid, applied, stale = Apply(buildExport(start.Add(30*time.Minute)), []Action{a}, testCatalog())
	if len(applied) != 1 || len(stale) != 0 {
		t.Fatalf("30 min in: applied = %d, stale = %d, want 1/0", len(applied), len(stale))
	}
	for _, it := range overlaid.Buildings {
		if it.Data == 1000008 {
			row = it
		}
	}
	if row.Timer != 1800 {
		t.Errorf("timer 30 min in = %d, want 1800", row.Timer)
	}

	// Captured two hours later: the action has already been overtaken by a
	// real export postdating it - it must not be applied, and must come
	// back as stale for reconciliation rather than silently vanish.
	overlaid, applied, stale = Apply(buildExport(start.Add(2*time.Hour)), []Action{a}, testCatalog())
	if len(applied) != 0 || len(stale) != 1 {
		t.Fatalf("2h later: applied = %d, stale = %d, want 0/1", len(applied), len(stale))
	}
	if countCopies(overlaid.Buildings, 1000008) != 1 {
		t.Errorf("copies 2h later = %d, want 1 (unchanged - nothing was applied)", countCopies(overlaid.Buildings, 1000008))
	}
}

// Clicking build-now on a copy that either does not exist, or is already
// mid-upgrade, must fail cleanly (reported as stale) rather than silently
// inventing a copy that was never there.
func TestApplyFailsCleanlyWhenNoIdleCopyMatches(t *testing.T) {
	capturedAt := time.Unix(1700000000, 0).UTC()
	e := &snapshot.Export{
		Timestamp: capturedAt.Unix(),
		Buildings: []snapshot.Item{
			{Data: 1000001, Lvl: 3, Cnt: 1},
			{Data: 1000008, Lvl: 3, Timer: 600}, // the sole Cannon is already mid-upgrade
		},
	}
	a := action(1000008, 3, 4, 600, capturedAt)

	overlaid, applied, stale := Apply(e, []Action{a}, testCatalog())
	if len(applied) != 0 || len(stale) != 1 {
		t.Fatalf("applied = %d, stale = %d, want 0/1 (the only copy is already claimed)", len(applied), len(stale))
	}
	if countCopies(overlaid.Buildings, 1000008) != 1 {
		t.Errorf("copies = %d, want 1 (unchanged)", countCopies(overlaid.Buildings, 1000008))
	}
}

func TestApplyReportsUnknownCatalogItemAsStale(t *testing.T) {
	capturedAt := time.Unix(1700000000, 0).UTC()
	e := &snapshot.Export{Timestamp: capturedAt.Unix(), Buildings: []snapshot.Item{{Data: 1000001, Lvl: 3, Cnt: 1}}}
	a := action(99999999, 1, 2, 600, capturedAt)

	_, applied, stale := Apply(e, []Action{a}, testCatalog())
	if len(applied) != 0 || len(stale) != 1 {
		t.Fatalf("applied = %d, stale = %d, want 0/1 for an item the catalog has never heard of", len(applied), len(stale))
	}
}
