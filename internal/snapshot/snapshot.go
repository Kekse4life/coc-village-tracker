// Package snapshot parses a Clash of Clans village export.
//
// The export lists what a village contains, not where anything sits: each row
// is an item ID plus a level, and either a count of copies at that level or a
// timer counting down an upgrade in flight. Arrays ending in "2" belong to the
// Builder Base.
package snapshot

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Item is one row of an export array.
type Item struct {
	Data int `json:"data"`
	Lvl  int `json:"lvl"`
	// Cnt is how many copies sit at this level. Absent means a single copy.
	Cnt int `json:"cnt"`
	// Timer is seconds left on an upgrade, measured from the export timestamp.
	Timer int `json:"timer"`
	// GearUp marks a defense that has been geared up in the Builder Base.
	GearUp int `json:"gear_up"`
	// HelperCooldown is seconds until a helper such as the Apprentice Builder
	// can be assigned again.
	HelperCooldown  int  `json:"helper_cooldown"`
	HelperRecurrent bool `json:"helper_recurrent"`
}

// Count reports how many copies this row represents.
func (i Item) Count() int {
	if i.Cnt > 0 {
		return i.Cnt
	}
	return 1
}

// Export is a whole village snapshot.
type Export struct {
	Tag       string `json:"tag"`
	Timestamp int64  `json:"timestamp"`

	Helpers []Item `json:"helpers"`

	// Home Village.
	Buildings     []Item `json:"buildings"`
	Traps         []Item `json:"traps"`
	Decos         []Item `json:"decos"`
	Obstacles     []Item `json:"obstacles"`
	Units         []Item `json:"units"`
	SiegeMachines []Item `json:"siege_machines"`
	Heroes        []Item `json:"heroes"`
	Spells        []Item `json:"spells"`
	Pets          []Item `json:"pets"`
	Equipment     []Item `json:"equipment"`

	HouseParts []int `json:"house_parts"`
	Skins      []int `json:"skins"`
	Sceneries  []int `json:"sceneries"`

	// Builder Base.
	Buildings2 []Item `json:"buildings2"`
	Traps2     []Item `json:"traps2"`
	Decos2     []Item `json:"decos2"`
	Obstacles2 []Item `json:"obstacles2"`
	Units2     []Item `json:"units2"`
	Heroes2    []Item `json:"heroes2"`
	Skins2     []int  `json:"skins2"`
	Sceneries2 []int  `json:"sceneries2"`
}

// CapturedAt is the moment the export was taken. Every timer in the file
// counts down from here, so absolute finish times depend on it.
func (e *Export) CapturedAt() time.Time {
	if e.Timestamp <= 0 {
		return time.Time{}
	}
	return time.Unix(e.Timestamp, 0).UTC()
}

// Parse reads an export and checks it is actually one.
func Parse(r io.Reader) (*Export, error) {
	var e Export
	dec := json.NewDecoder(r)
	if err := dec.Decode(&e); err != nil {
		return nil, fmt.Errorf("that file is not valid JSON: %w", err)
	}
	if len(e.Buildings) == 0 && len(e.Buildings2) == 0 {
		return nil, fmt.Errorf("no buildings found - this does not look like a village export")
	}
	if e.Timestamp <= 0 {
		return nil, fmt.Errorf("no timestamp found - upgrade timers cannot be placed on a clock")
	}
	return &e, nil
}
