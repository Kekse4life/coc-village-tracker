// Package pending lets a village declare "I just started this in-game"
// without a fresh export to back it up, and reconciles those declarations
// against the next real one. Nothing here is ever persisted as if it were
// the export itself - an Action only ever overlays on top of a parsed one,
// on the way into analyze.Run, so a stale or wrong declaration can never
// corrupt the ground truth the next real export provides.
package pending

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/snapshot"
)

// Action is one hand-declared upgrade.
type Action struct {
	ID      string `json:"id"` // random hex, so it can be cancelled later
	Tag     string `json:"tag"`
	ItemID  int    `json:"itemId"`
	Name    string `json:"name"` // denormalized for display
	Icon    string `json:"icon,omitempty"`
	Village string `json:"village"` // "home" | "builder"
	Lane    string `json:"lane,omitempty"`

	FromLevel int `json:"fromLevel"`
	ToLevel   int `json:"toLevel"`
	// Seconds is the catalog's own build time for this step. Zero means an
	// instant upgrade (equipment, Walls, helpers, most super troops) - Apply
	// bumps the level immediately rather than manufacturing a timer for
	// something the game itself never times.
	Seconds int64 `json:"seconds"`
	// StartedAt anchors Seconds to a wall-clock finish time. Storing this
	// rather than a precomputed remaining-seconds figure lets Apply rebase
	// the timer onto whatever export it is layered on top of, however much
	// later that export was actually captured.
	StartedAt time.Time `json:"startedAt"`
}

// NewID returns a random identifier for a new Action.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand on a fixed-size buffer does not fail in practice
	return hex.EncodeToString(b)
}

// Apply layers every action onto a copy of e so the result flows through
// analyze.Run exactly as if the export itself already reflected them. e is
// never mutated.
//
// applied is every action that found a matching idle copy to split off.
// stale is every action the export has already moved past - its own timer
// would have finished before e was even captured, its item is not in the
// catalog any more, or no matching idle copy exists (the real export has
// overtaken it, for better or worse). The caller should stop persisting
// stale actions and hand them to reconciliation instead of silently
// dropping them.
func Apply(e *snapshot.Export, actions []Action, cat *catalog.Catalog) (out *snapshot.Export, applied, stale []Action) {
	out = shallowCopy(e)
	capturedAt := e.CapturedAt()

	for _, a := range actions {
		entry, ok := cat.Lookup(a.ItemID)
		if !ok {
			stale = append(stale, a)
			continue
		}

		// finalLevel/timerSeconds describe the row the split-off copy ends
		// up as. A real export's mid-upgrade row sits at the level it is
		// LEAVING plus a timer (see analyze.go's own Job comment); an
		// instant bump has already arrived, so it sits at the level it is
		// GAINING with no timer at all.
		finalLevel, timerSeconds := a.ToLevel, 0
		if a.Seconds > 0 {
			remaining := int(a.StartedAt.Add(time.Duration(a.Seconds) * time.Second).Sub(capturedAt).Seconds())
			if remaining <= 0 {
				stale = append(stale, a) // the export already postdates this action either way
				continue
			}
			finalLevel, timerSeconds = a.FromLevel, remaining
		}

		if !splitOne(out, entry.Kind, a.Village, a.ItemID, a.FromLevel, finalLevel, timerSeconds) {
			stale = append(stale, a)
			continue
		}
		applied = append(applied, a)
	}
	return out, applied, stale
}

// splitOne finds one idle copy - Data == itemID, Lvl == fromLevel, no timer
// already running - in the array matching kind and village, and turns
// exactly one of them into finalLevel/timerSeconds. A real export
// represents an in-progress copy by splitting it off its sibling row
// rather than inflating the count, and this mirrors that rather than
// appending a phantom copy - appending would double-count it in every
// completion percentage, the bill, and Strength.
//
// Other per-row fields (GearUp, helper cooldown) carry over from whichever
// copy is split, rather than being zeroed by starting from a bare Item.
func splitOne(exp *snapshot.Export, kind, village string, itemID, fromLevel, finalLevel, timerSeconds int) bool {
	arr, ok := arrayFor(exp, kind, village)
	if !ok {
		return false
	}
	for i, it := range *arr {
		if it.Data != itemID || it.Lvl != fromLevel || it.Timer > 0 {
			continue
		}
		// *arr still aliases the same backing array shallowCopy started
		// from - a struct copy only duplicates the slice header, not what
		// it points to. Cloning before the first write to this array keeps
		// e's own backing array untouched; on any later call this Apply
		// makes into the same array, it clones the already-updated copy
		// forward, which is exactly the accumulation wanted.
		*arr = append([]snapshot.Item(nil), *arr...)

		moved := it
		moved.Cnt = 0 // Item.Count() reads a zero Cnt as one copy - this row is now exactly one
		moved.Lvl = finalLevel
		moved.Timer = timerSeconds
		if it.Count() > 1 {
			// A one-copy row can't be "decremented" further - Cnt=0 already
			// means one copy, not none - so only split by appending when
			// more than one copy actually remains behind.
			(*arr)[i].Cnt = it.Count() - 1
			*arr = append(*arr, moved)
		} else {
			(*arr)[i] = moved
		}
		return true
	}
	return false
}

// arrayFor resolves which export array a catalog entry's copies live in,
// mirroring the dispatch analyze.Run hardcodes for each of e.Buildings,
// e.Units, and so on. Troops and siege machines are deliberately
// indistinguishable here (both are kind "unit", village "home") because
// Run itself routes both to the same group and lane - it does not matter
// which one a synthetic row lands in.
func arrayFor(exp *snapshot.Export, kind, village string) (*[]snapshot.Item, bool) {
	switch {
	case village == "home" && kind == "building":
		return &exp.Buildings, true
	case village == "home" && kind == "trap":
		return &exp.Traps, true
	case village == "home" && kind == "unit":
		return &exp.Units, true
	case village == "home" && kind == "spell":
		return &exp.Spells, true
	case village == "home" && kind == "hero":
		return &exp.Heroes, true
	case village == "home" && kind == "pet":
		return &exp.Pets, true
	case village == "home" && kind == "equipment":
		return &exp.Equipment, true
	case village == "builder" && kind == "building":
		return &exp.Buildings2, true
	case village == "builder" && kind == "trap":
		return &exp.Traps2, true
	case village == "builder" && kind == "unit":
		return &exp.Units2, true
	case village == "builder" && kind == "hero":
		return &exp.Heroes2, true
	}
	return nil, false
}

// shallowCopy copies the Export struct itself, but every slice field still
// aliases the same backing array e has - a struct copy only duplicates the
// slice header (pointer, length, capacity), not what it points to. splitOne
// clones an array the first time it actually needs to write to it, so e's
// own arrays are never touched; until then, the two exports harmlessly
// share storage for every array Apply does not need to change.
func shallowCopy(e *snapshot.Export) *snapshot.Export {
	cp := *e
	return &cp
}
