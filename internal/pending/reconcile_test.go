package pending

import (
	"testing"
	"time"

	"github.com/you/coc-progress/internal/snapshot"
)

// idsOf is a small test helper for checking which actions came back.
func idsOf(as []Action) map[string]bool {
	out := map[string]bool{}
	for _, a := range as {
		out[a.ID] = true
	}
	return out
}

func TestReconcileConfirmsStartedChange(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	a := Action{ID: "a1", ItemID: 1000008, Village: "home", FromLevel: 1, ToLevel: 2, Seconds: 60, StartedAt: base}

	prev := &snapshot.Export{Timestamp: base.Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Cnt: 1}}}
	new := &snapshot.Export{Timestamp: base.Add(time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Timer: 600}}}

	retired, mismatches := Reconcile([]Action{a}, prev, new, testCatalog())
	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %+v, want none - a real 'started' change confirms this", mismatches)
	}
	if !idsOf(retired)["a1"] {
		t.Fatalf("retired = %+v, want a1 retired (confirmed)", retired)
	}
}

func TestReconcileConfirmsLandedChange(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	a := Action{ID: "a1", ItemID: 1000002, Village: "home", FromLevel: 4, ToLevel: 5, Seconds: 0, StartedAt: base}

	prev := &snapshot.Export{Timestamp: base.Unix(), Buildings: []snapshot.Item{{Data: 1000002, Lvl: 4, Cnt: 1}}}
	new := &snapshot.Export{Timestamp: base.Add(time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000002, Lvl: 5, Cnt: 1}}}

	retired, mismatches := Reconcile([]Action{a}, prev, new, testCatalog())
	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %+v, want none - a real 'landed' change confirms an instant bump", mismatches)
	}
	if !idsOf(retired)["a1"] {
		t.Fatalf("retired = %+v, want a1 retired (confirmed)", retired)
	}
}

// A copy already mid-upgrade in BOTH prev and new never produces a
// "started" change (that diff only flags a timer newly appeared since
// prev) - Reconcile must still confirm it independently rather than
// calling it a mismatch just because the change log is silent about it.
func TestReconcileConfirmsStillRunningWithNoVisibleChange(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	a := Action{ID: "a1", ItemID: 1000008, Village: "home", FromLevel: 1, ToLevel: 2, Seconds: 600, StartedAt: base.Add(-time.Hour)}

	prev := &snapshot.Export{Timestamp: base.Add(-30 * time.Minute).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Timer: 1800}}}
	new := &snapshot.Export{Timestamp: base.Add(time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Timer: 900}}}

	retired, mismatches := Reconcile([]Action{a}, prev, new, testCatalog())
	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %+v, want none - genuinely still running in both exports", mismatches)
	}
	if !idsOf(retired)["a1"] {
		t.Fatalf("retired = %+v, want a1 retired (confirmed still running)", retired)
	}
}

func TestReconcileFlagsMismatchWhenNothingHappened(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	a := Action{ID: "a1", ItemID: 1000008, Village: "home", FromLevel: 1, ToLevel: 2, Seconds: 60, StartedAt: base}

	prev := &snapshot.Export{Timestamp: base.Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Cnt: 1}}}
	new := &snapshot.Export{Timestamp: base.Add(time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Cnt: 1}}} // unchanged

	retired, mismatches := Reconcile([]Action{a}, prev, new, testCatalog())
	if len(mismatches) != 1 || mismatches[0].Action.ID != "a1" {
		t.Fatalf("mismatches = %+v, want exactly a1 - the export shows no sign this ever started", mismatches)
	}
	if !idsOf(retired)["a1"] {
		t.Fatalf("retired = %+v, want a1 retired (mismatched actions still stop being tracked)", retired)
	}
}

// Re-uploading an export older than when the action was declared must not
// be treated as evidence of anything - it predates the action and cannot
// possibly show it either way.
func TestReconcileSkipsActionsTheExportPredates(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	a := Action{ID: "a1", ItemID: 1000008, Village: "home", FromLevel: 1, ToLevel: 2, Seconds: 60, StartedAt: base}

	prev := &snapshot.Export{Timestamp: base.Add(-2 * time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Cnt: 1}}}
	new := &snapshot.Export{Timestamp: base.Add(-time.Hour).Unix(), Buildings: []snapshot.Item{{Data: 1000008, Lvl: 1, Cnt: 1}}} // captured before the action even started

	retired, mismatches := Reconcile([]Action{a}, prev, new, testCatalog())
	if len(retired) != 0 || len(mismatches) != 0 {
		t.Fatalf("retired = %+v, mismatches = %+v, want neither - this export predates the action", retired, mismatches)
	}
}

func TestReconcileHandlesMultipleActionsIndependently(t *testing.T) {
	base := time.Unix(1700000000, 0).UTC()
	confirmed := Action{ID: "confirmed", ItemID: 1000008, Village: "home", FromLevel: 1, ToLevel: 2, Seconds: 60, StartedAt: base}
	mismatched := Action{ID: "mismatched", ItemID: 1000002, Village: "home", FromLevel: 4, ToLevel: 5, Seconds: 0, StartedAt: base}

	prev := &snapshot.Export{Timestamp: base.Unix(), Buildings: []snapshot.Item{
		{Data: 1000008, Lvl: 1, Cnt: 1},
		{Data: 1000002, Lvl: 4, Cnt: 1},
	}}
	new := &snapshot.Export{Timestamp: base.Add(time.Hour).Unix(), Buildings: []snapshot.Item{
		{Data: 1000008, Lvl: 1, Timer: 600}, // confirmed: started
		{Data: 1000002, Lvl: 4, Cnt: 1},     // mismatched: unchanged, no wall bump
	}}

	retired, mismatches := Reconcile([]Action{confirmed, mismatched}, prev, new, testCatalog())
	if len(retired) != 2 {
		t.Fatalf("retired = %+v, want both retired", retired)
	}
	if len(mismatches) != 1 || mismatches[0].Action.ID != "mismatched" {
		t.Fatalf("mismatches = %+v, want exactly 'mismatched'", mismatches)
	}
}
