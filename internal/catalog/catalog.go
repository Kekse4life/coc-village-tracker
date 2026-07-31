// Package catalog turns the numeric IDs in a village export into names,
// categories, upgrade ceilings, costs and icons.
package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Ore is the triple currency hero equipment upgrades spend instead of a
// single resource.
type Ore struct {
	Shiny  int64 `json:"shiny,omitempty"`
	Glowy  int64 `json:"glowy,omitempty"`
	Starry int64 `json:"starry,omitempty"`
}

// Add accumulates another Ore's amounts into this one.
func (o *Ore) Add(other Ore) {
	o.Shiny += other.Shiny
	o.Glowy += other.Glowy
	o.Starry += other.Starry
}

// Zero reports whether no ore at all is required.
func (o Ore) Zero() bool { return o.Shiny == 0 && o.Glowy == 0 && o.Starry == 0 }

// Unlock is one thing a Town Hall or Builder Hall level grants.
type Unlock struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// Level describes what it costs to reach one level of an Entry. Level i in
// an Entry's Levels slice describes reaching level i+1 from level i - the
// same "index is the level below" convention the export itself uses.
type Level struct {
	// Requires maps a gate name ("th", "bh", "lab", "starlab", "smith",
	// "pethouse", "herohall") to the level of that gate needed to reach this
	// level. A level with no entries at all - such as every Builder Hall
	// level - has nothing external gating it.
	Requires map[string]int `json:"requires,omitempty"`
	Cost     int64          `json:"cost,omitempty"`
	// AltRes overrides Entry.Resource for this level only - the top few Wall
	// levels spend Elixir instead of Gold.
	AltRes   string `json:"altRes,omitempty"`
	Ore      Ore    `json:"ore,omitzero"`
	Seconds  int64  `json:"seconds,omitempty"`
	Strength int    `json:"strength,omitempty"`
	// Unlocks is only populated on Town Hall and Builder Hall levels.
	Unlocks []Unlock `json:"unlocks,omitempty"`
}

// Entry describes one thing that can exist in a village.
type Entry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Class   string `json:"class,omitempty"`
	Village int    `json:"village"`
	// Hero and Rarity are set on equipment entries only.
	Hero     string `json:"hero,omitempty"`
	Rarity   string `json:"rarity,omitempty"`
	Resource string `json:"resource,omitempty"`
	MaxLevel int    `json:"maxLevel"`
	// Icon is a path under https://assets.clashk.ing/, with a "{level}"
	// placeholder for entries whose art changes per level. Empty means no
	// icon is known for this entry.
	Icon         string  `json:"icon,omitempty"`
	IconMaxLevel int     `json:"iconMaxLevel,omitempty"`
	Levels       []Level `json:"levels,omitempty"`
}

// Catalog is the whole lookup table.
type Catalog struct {
	GeneratedAt string           `json:"generatedAt"`
	Source      string           `json:"source"`
	Entries     map[string]Entry `json:"entries"`
}

// Gates holds the player's current unlock levels, which decide how far each
// item can be upgraded today.
type Gates struct {
	TownHall       int `json:"townHall"`
	BuilderHall    int `json:"builderHall"`
	Laboratory     int `json:"laboratory"`
	StarLaboratory int `json:"starLaboratory"`
	Blacksmith     int `json:"blacksmith"`
	PetHouse       int `json:"petHouse"`
	HeroHall       int `json:"heroHall"`
}

// Load reads a catalog from disk.
func Load(path string) (*Catalog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read decodes a catalog from any source, so it can be served from an embedded
// copy as well as from a file the player regenerated.
func Read(r io.Reader) (*Catalog, error) {
	var c Catalog
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	if len(c.Entries) == 0 {
		return nil, fmt.Errorf("catalog has no entries")
	}
	return &c, nil
}

// Lookup finds an entry by numeric ID. The second result is false for IDs the
// catalog has never heard of, which happens after a game update adds content.
func (c *Catalog) Lookup(id int) (Entry, bool) {
	e, ok := c.Entries[strconv.Itoa(id)]
	return e, ok
}

// Reachable reports the highest level this entry can currently be taken to.
// Entries with no level data at all (cosmetic items, and a handful of
// buildings the game tracks with no independent upgrade path) fall back to
// their recorded maximum, which is 0 unless the caller has no ceiling to
// measure against in the first place.
func (e Entry) Reachable(g Gates) int {
	if len(e.Levels) == 0 {
		return e.MaxLevel
	}
	best := 0
	for i, lvl := range e.Levels {
		ok := true
		for gate, need := range lvl.Requires {
			if g.level(gate) < need {
				ok = false
				break
			}
		}
		if ok {
			best = i + 1
		}
	}
	if best == 0 {
		// Nothing is unlocked yet, but the item exists in the village, so
		// treat level 1 as the floor rather than reporting a ceiling of zero.
		return 1
	}
	return best
}

// Between sums the cost, ore and time needed to go from level from
// (exclusive) to level to (inclusive) - the bill for however many levels
// remain. Cost is keyed by currency name, since a single item (Walls, at
// the top few levels) can span more than one.
func (e Entry) Between(from, to int) (cost map[string]int64, ore Ore, seconds int64) {
	cost = map[string]int64{}
	if to > len(e.Levels) {
		to = len(e.Levels)
	}
	for i := from; i < to; i++ {
		if i < 0 {
			continue
		}
		lvl := e.Levels[i]
		if lvl.Cost > 0 {
			res := lvl.AltRes
			if res == "" {
				res = e.Resource
			}
			if res != "" {
				cost[res] += lvl.Cost
			}
		}
		ore.Add(lvl.Ore)
		seconds += lvl.Seconds
	}
	return cost, ore, seconds
}

// IconURL resolves this entry's icon at a given level (ignored for
// non-tiered icons) against the ClashKing asset CDN. It returns "" when no
// icon is known, or when the requested level is beyond the art that exists.
func (e Entry) IconURL(level int) string {
	if e.Icon == "" {
		return ""
	}
	path := e.Icon
	if e.IconMaxLevel > 0 {
		if level <= 0 {
			level = 1
		}
		if level > e.IconMaxLevel {
			level = e.IconMaxLevel
		}
		path = replaceLevelPlaceholder(path, level)
	}
	return "https://assets.clashk.ing/" + path
}

func replaceLevelPlaceholder(path string, level int) string {
	const placeholder = "{level}"
	out := make([]byte, 0, len(path)+2)
	for i := 0; i < len(path); i++ {
		if i+len(placeholder) <= len(path) && path[i:i+len(placeholder)] == placeholder {
			out = append(out, []byte(strconv.Itoa(level))...)
			i += len(placeholder) - 1 // the loop's own i++ covers the last character
			continue
		}
		out = append(out, path[i])
	}
	return string(out)
}

func (g Gates) level(gate string) int {
	switch gate {
	case "th":
		return g.TownHall
	case "bh":
		return g.BuilderHall
	case "lab":
		return g.Laboratory
	case "starlab":
		return g.StarLaboratory
	case "smith":
		return g.Blacksmith
	case "pethouse":
		return g.PetHouse
	case "herohall":
		return g.HeroHall
	}
	return 0
}

// FallbackName labels an ID the catalog does not cover. The ID block still
// says what kind of thing it is, so an unnamed item can be counted and tracked
// rather than silently dropped.
func FallbackName(id int) (name, kind string) {
	switch {
	case id >= 1000000 && id < 2000000:
		return fmt.Sprintf("Building #%d", id), "building"
	case id >= 4000000 && id < 5000000:
		return fmt.Sprintf("Troop #%d", id), "unit"
	case id >= 8000000 && id < 9000000:
		return fmt.Sprintf("Obstacle #%d", id), "obstacle"
	case id >= 12000000 && id < 13000000:
		return fmt.Sprintf("Trap #%d", id), "trap"
	case id >= 18000000 && id < 19000000:
		return fmt.Sprintf("Decoration #%d", id), "deco"
	case id >= 26000000 && id < 27000000:
		return fmt.Sprintf("Spell #%d", id), "spell"
	case id >= 28000000 && id < 29000000:
		return fmt.Sprintf("Hero #%d", id), "hero"
	case id >= 52000000 && id < 53000000:
		return fmt.Sprintf("Skin #%d", id), "skin"
	case id >= 60000000 && id < 61000000:
		return fmt.Sprintf("Scenery #%d", id), "scenery"
	case id >= 73000000 && id < 74000000:
		return fmt.Sprintf("Pet #%d", id), "pet"
	case id >= 82000000 && id < 83000000:
		return fmt.Sprintf("House part #%d", id), "housepart"
	case id >= 90000000 && id < 91000000:
		return fmt.Sprintf("Equipment #%d", id), "equipment"
	case id >= 93000000 && id < 94000000:
		return fmt.Sprintf("Helper #%d", id), "helper"
	case id >= 107000000 && id < 108000000:
		return fmt.Sprintf("Guardian #%d", id), "building"
	}
	return fmt.Sprintf("Item #%d", id), "other"
}
