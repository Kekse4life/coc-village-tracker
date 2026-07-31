package pending

import (
	"time"

	"github.com/you/coc-progress/internal/analyze"
	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/snapshot"
)

// Mismatch is a declared action a real export contradicted: the action was
// due to be visible by the time this export was captured, and it was not.
type Mismatch struct {
	Action Action    `json:"action"`
	SeenAt time.Time `json:"seenAt"` // the export's own capture time, not now
}

// Reconcile checks every declared action against two real exports of the
// same village - never against a synthetic overlay, which would just be
// checking Apply's own work against itself. prev is whatever the village's
// latest snapshot was immediately before new was saved; new is the export
// that just landed.
//
// retired is every action that no longer needs tracking, whether or not it
// panned out - the caller should drop these from the pending store either
// way. mismatches is the subset of those that did not: declared, but new
// shows no sign it happened.
//
// An action new predates (StartedAt after new's own capture time - typically
// re-uploading an older file) is left out of both: there is nothing to
// judge yet, since a stale export cannot possibly show something that
// hadn't happened when it was captured.
func Reconcile(actions []Action, prev, new *snapshot.Export, cat *catalog.Catalog) (retired []Action, mismatches []Mismatch) {
	if len(actions) == 0 || prev == nil || new == nil {
		return nil, nil
	}
	changes := analyze.Diff(prev, new, cat).Changes
	newCapturedAt := new.CapturedAt()

	for _, a := range actions {
		if !newCapturedAt.After(a.StartedAt) {
			continue // new cannot possibly show this yet
		}
		if confirmedByChange(a, changes) || confirmedStillRunning(a, new, cat) {
			retired = append(retired, a)
			continue
		}
		retired = append(retired, a)
		mismatches = append(mismatches, Mismatch{Action: a, SeenAt: newCapturedAt})
	}
	return retired, mismatches
}

// confirmedByChange reports whether the prev->new change log shows this
// action's item actually starting or landing at the declared levels.
func confirmedByChange(a Action, changes []analyze.Change) bool {
	for _, c := range changes {
		if c.ID != a.ItemID || c.Village != a.Village {
			continue
		}
		switch c.Type {
		case "started":
			if c.FromLevel == a.FromLevel {
				return true
			}
		case "landed", "built":
			if c.FromLevel == a.FromLevel && c.ToLevel == a.ToLevel {
				return true
			}
		}
	}
	return false
}

// confirmedStillRunning covers the one case a plain change log cannot: the
// action's item was already mid-upgrade at the declared level in BOTH prev
// and new, so nothing about it changed between them and it never shows up
// as a "started" change (that diff only flags a timer that is new since
// prev) - but new still independently confirms it is genuinely running.
func confirmedStillRunning(a Action, new *snapshot.Export, cat *catalog.Catalog) bool {
	entry, ok := cat.Lookup(a.ItemID)
	if !ok {
		return false
	}
	arr, ok := arrayFor(new, entry.Kind, a.Village)
	if !ok {
		return false
	}
	for _, it := range *arr {
		if it.Data == a.ItemID && it.Lvl == a.FromLevel && it.Timer > 0 {
			return true
		}
	}
	return false
}
