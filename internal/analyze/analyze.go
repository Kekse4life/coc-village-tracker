// Package analyze turns a parsed export into what is worth knowing: how
// complete each part of the village is, what finishes when, what it costs to
// finish the rest, and what changed since the last export.
package analyze

import (
	"fmt"
	"sort"
	"time"

	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/snapshot"
)

// IDs of the buildings that gate everything else.
const (
	idTownHall       = 1000001
	idLaboratory     = 1000007
	idBuildersHut    = 1000015
	idPetHouse       = 1000068
	idBlacksmith     = 1000070
	idHeroHall       = 1000071
	idBuilderHall    = 1000034
	idStarLaboratory = 1000046
	idBuildersHut2   = 1000047
)

// Report is the full analysis, and the shape the API returns.
type Report struct {
	Tag        string         `json:"tag"`
	CapturedAt time.Time      `json:"capturedAt"`
	Gates      catalog.Gates  `json:"gates"`
	Villages   []Village      `json:"villages"`
	Jobs       []Job          `json:"jobs"`
	Lanes      []Lane         `json:"lanes"`
	Extras     map[string]int `json:"extras"`
	Notes      []string       `json:"notes"`
	Catalog    CatalogMeta    `json:"catalog"`

	// Bill is the total cost and time left to reach every current ceiling.
	Bill Bill `json:"bill"`
	// NextUp is the single next upgrade step for every item not yet at its
	// ceiling. It does not know which specific copy is idle versus already
	// mid-upgrade - cross-check against Lanes for real capacity.
	NextUp []Suggestion `json:"nextUp"`
	// Missing lists things the current hall allows that have not been built.
	Missing []Shortfall `json:"missing"`
	// NextHall previews the cost, time and reach of the next Town Hall or
	// Builder Hall level, one entry per village that has a level left to take.
	NextHall []HallPreview `json:"nextHall"`
	// Strength is a composite "war weight earned vs reachable" headline,
	// built from the game's own strength_weight figures. It is an
	// approximation, not the game's real matchmaking formula.
	Strength Strength `json:"strength"`
	// Balance ranks every measured group, across both villages, weakest
	// completion first - "what's dragging".
	Balance []GroupScore `json:"balance"`
}

// CatalogMeta tells the reader how much to trust the labels.
type CatalogMeta struct {
	GeneratedAt string `json:"generatedAt"`
	Source      string `json:"source"`
	Unnamed     int    `json:"unnamed"`
	BeyondMax   int    `json:"beyondMax"`
}

// Remaining is what is left to reach a ceiling: how many level-ups, what
// they cost by currency (ore included, under keys like "Shiny Ore"), and how
// long they take end to end if run one after another.
type Remaining struct {
	Steps   int              `json:"steps"`
	Cost    map[string]int64 `json:"cost"`
	Seconds int64            `json:"seconds"`
}

func newRemaining() Remaining { return Remaining{Cost: map[string]int64{}} }

func (r *Remaining) add(other Remaining) {
	if r.Cost == nil {
		r.Cost = map[string]int64{}
	}
	for k, v := range other.Cost {
		r.Cost[k] += v
	}
	r.Steps += other.Steps
	r.Seconds += other.Seconds
}

// Bill is the village-wide Remaining, additionally split by which lane the
// time is spent in - a builder can run in parallel with the laboratory, so
// their seconds do not add together the way currency amounts do.
type Bill struct {
	Steps          int              `json:"steps"`
	Cost           map[string]int64 `json:"cost"`
	BuilderSeconds int64            `json:"builderSeconds"`
	LabSeconds     int64            `json:"labSeconds"`
	OtherSeconds   int64            `json:"otherSeconds"`
}

func (bill *Bill) add(r Remaining, lane string) {
	if bill.Cost == nil {
		bill.Cost = map[string]int64{}
	}
	for k, v := range r.Cost {
		bill.Cost[k] += v
	}
	bill.Steps += r.Steps
	switch lane {
	case "builder", "builder2":
		bill.BuilderSeconds += r.Seconds
	case "lab", "starlab":
		bill.LabSeconds += r.Seconds
	default:
		bill.OtherSeconds += r.Seconds
	}
}

// Suggestion is one upgrade startable right now: its gate is satisfied and
// it is not already at ceiling.
type Suggestion struct {
	ID           int              `json:"id"`
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Village      string           `json:"village"`
	Lane         string           `json:"lane"`
	FromLevel    int              `json:"fromLevel"`
	ToLevel      int              `json:"toLevel"`
	Cost         map[string]int64 `json:"cost"`
	Seconds      int64            `json:"seconds"`
	StrengthGain int              `json:"strengthGain"`
	Icon         string           `json:"icon,omitempty"`
}

// Shortfall is something the current hall level allows that has not been
// built yet, such as a second Builder's Hut sitting unbuilt at Town Hall 3.
type Shortfall struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Village string `json:"village"`
	Owned   int    `json:"owned"`
	Allowed int    `json:"allowed"`
	Icon    string `json:"icon,omitempty"`
}

// HallPreview previews the next Town Hall or Builder Hall level.
type HallPreview struct {
	Village       string           `json:"village"`
	FromLevel     int              `json:"fromLevel"`
	ToLevel       int              `json:"toLevel"`
	Cost          map[string]int64 `json:"cost"`
	Seconds       int64            `json:"seconds"`
	Unlocks       []catalog.Unlock `json:"unlocks"`
	ItemsAffected int              `json:"itemsAffected"`
}

// Strength is total strength_weight earned versus what is currently
// reachable, across every measured item in both villages.
type Strength struct {
	Done      int64 `json:"done"`
	Reachable int64 `json:"reachable"`
}

// GroupScore is one group's completion, used to rank "what's dragging"
// across both villages at once.
type GroupScore struct {
	Village     string  `json:"village"`
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Gate        string  `json:"gate"`
	Completion  float64 `json:"completion"`
	Copies      int     `json:"copies"`
	CopiesAtMax int     `json:"copiesAtMax"`
}

// Village is one base: the Home Village or the Builder Base.
type Village struct {
	Key          string    `json:"key"`
	Label        string    `json:"label"`
	Hall         int       `json:"hall"`
	Completion   float64   `json:"completion"`
	LevelsDone   int       `json:"levelsDone"`
	LevelsTarget int       `json:"levelsTarget"`
	Copies       int       `json:"copies"`
	CopiesAtMax  int       `json:"copiesAtMax"`
	Unmeasured   int       `json:"unmeasured"`
	Remaining    Remaining `json:"remaining"`
	Groups       []Group   `json:"groups"`
}

// Group is a category within a village, such as defenses or spells.
type Group struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Gate         string  `json:"gate"`
	Completion   float64 `json:"completion"`
	LevelsDone   int     `json:"levelsDone"`
	LevelsTarget int     `json:"levelsTarget"`
	Copies       int     `json:"copies"`
	CopiesAtMax  int     `json:"copiesAtMax"`
	// Measured is false when nothing in this group has a known ceiling, so the
	// interface can say "ceiling unknown" instead of showing a false zero.
	Measured  bool       `json:"measured"`
	Remaining Remaining  `json:"remaining"`
	Items     []ItemStat `json:"items"`
}

// ItemStat is one kind of thing, with every copy of it accounted for.
type ItemStat struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Copies    int    `json:"copies"`
	Reachable int    `json:"reachable"`
	GlobalMax int    `json:"globalMax"`
	// Lane is the capacity queue this item upgrades through - the same key
	// used in Lanes and Jobs.
	Lane string `json:"lane"`
	Icon string `json:"icon,omitempty"`
	// Hero and Rarity are set on equipment items only.
	Hero   string `json:"hero,omitempty"`
	Rarity string `json:"rarity,omitempty"`
	// Buckets groups copies by level, so eight walls at 11 and two at 9 stay
	// distinguishable instead of collapsing into an average.
	Buckets      []Bucket  `json:"buckets"`
	Upgrading    int       `json:"upgrading"`
	LevelsDone   int       `json:"levelsDone"`
	LevelsTarget int       `json:"levelsTarget"`
	Completion   float64   `json:"completion"`
	CopiesAtMax  int       `json:"copiesAtMax"`
	Measured     bool      `json:"measured"`
	Unnamed      bool      `json:"unnamed"`
	BeyondMax    bool      `json:"beyondMax"`
	Remaining    Remaining `json:"remaining"`
}

// Bucket is a count of copies sitting at one level.
type Bucket struct {
	Level int `json:"level"`
	Count int `json:"count"`
}

// Job is one upgrade in flight.
type Job struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	Village    string    `json:"village"`
	Lane       string    `json:"lane"`
	FromLevel  int       `json:"fromLevel"`
	ToLevel    int       `json:"toLevel"`
	Remaining  int       `json:"remainingSeconds"`
	FinishesAt time.Time `json:"finishesAt"`
	New        bool      `json:"new"`
	Icon       string    `json:"icon,omitempty"`
}

// Lane is a capacity constraint: five builders, one laboratory. Knowing a lane
// is free matters more than knowing a single upgrade finished.
type Lane struct {
	Key        string     `json:"key"`
	Label      string     `json:"label"`
	Village    string     `json:"village"`
	Busy       int        `json:"busy"`
	Total      int        `json:"total"`
	NextFreeAt *time.Time `json:"nextFreeAt"`
}

// group definitions, in the order they should be read.
var homeGroups = []struct{ key, label string }{
	{"core", "Town Hall"},
	{"defense", "Defenses"},
	{"trap", "Traps"},
	{"wall", "Walls"},
	{"army", "Army buildings"},
	{"resource", "Resources"},
	{"hut", "Builder's Huts"},
	{"helper", "Helpers"},
	{"unit", "Troops"},
	{"spell", "Spells"},
	{"hero", "Heroes"},
	{"pet", "Pets"},
	{"equipment", "Hero equipment"},
	{"siege", "Siege machines"},
	{"other", "Everything else"},
}

var builderGroups = []struct{ key, label string }{
	{"core", "Builder Hall"},
	{"defense", "Defenses"},
	{"trap", "Traps"},
	{"wall", "Walls"},
	{"army", "Army buildings"},
	{"resource", "Resources"},
	{"hut", "Builder's Huts"},
	{"unit", "Troops"},
	{"hero", "Heroes"},
	{"other", "Everything else"},
}

// Run analyses an export against a catalog, as of now. now lets an upgrade
// whose timer has run out since the export was captured count as landed
// everywhere - gates, levels, lane capacity - even though the export itself
// will not say so until the next one. Without it, a village looked at hours
// after its last export would show long-finished upgrades as still running.
func Run(e *snapshot.Export, cat *catalog.Catalog, now time.Time) *Report {
	gates := readGates(e, now)

	r := &Report{
		Tag:        e.Tag,
		CapturedAt: e.CapturedAt(),
		Gates:      gates,
		Extras:     map[string]int{},
		Bill:       Bill{Cost: map[string]int64{}},
		Catalog: CatalogMeta{
			GeneratedAt: cat.GeneratedAt,
			Source:      cat.Source,
		},
	}

	b := &builder{cat: cat, gates: gates, report: r, now: now, ownedByVillage: map[string]map[int]int{}}

	// Home Village.
	b.begin("home", "Home Village", gates.TownHall)
	b.addAll(e.Buildings, "home", "builder")
	b.addAll(e.Traps, "home", "builder")
	b.addAll(e.Units, "home", "lab")
	b.addAll(e.Spells, "home", "lab")
	b.addAll(e.Heroes, "home", "hero")
	b.addAll(e.Pets, "home", "pethouse")
	b.addAll(e.Equipment, "home", "blacksmith")
	b.addAll(e.SiegeMachines, "home", "lab")
	b.addAll(e.Helpers, "home", "helper")
	b.end(homeGroups)

	// Builder Base.
	b.begin("builder", "Builder Base", gates.BuilderHall)
	b.addAll(e.Buildings2, "builder", "builder2")
	b.addAll(e.Traps2, "builder", "builder2")
	b.addAll(e.Units2, "builder", "starlab")
	b.addAll(e.Heroes2, "builder", "hero2")
	b.end(builderGroups)

	b.addHelpers(e.Helpers)

	sort.Slice(r.Jobs, func(i, j int) bool { return r.Jobs[i].Remaining < r.Jobs[j].Remaining })

	r.Lanes = b.lanes(e)
	r.Extras["skins"] = len(e.Skins) + len(e.Skins2)
	r.Extras["houseParts"] = len(e.HouseParts)
	r.Extras["sceneries"] = len(e.Sceneries) + len(e.Sceneries2)
	r.Extras["decorations"] = countCopies(e.Decos) + countCopies(e.Decos2)
	r.Extras["obstacles"] = countCopies(e.Obstacles) + countCopies(e.Obstacles2)

	r.NextUp = b.nextUp()
	r.Missing = b.missing()
	if p := nextHallPreview(cat, gates, b.ownedByVillage["home"], idTownHall, gates.TownHall, "home"); p != nil {
		r.NextHall = append(r.NextHall, *p)
	}
	if p := nextHallPreview(cat, gates, b.ownedByVillage["builder"], idBuilderHall, gates.BuilderHall, "builder"); p != nil {
		r.NextHall = append(r.NextHall, *p)
	}
	r.Balance = computeBalance(r.Villages)
	r.Notes = b.notes()

	return r
}

// builder accumulates items into groups as they are read.
type builder struct {
	cat    *catalog.Catalog
	gates  catalog.Gates
	report *Report
	// now is when the caller is looking, not when the export was captured -
	// it decides whether an in-flight timer has already run out.
	now time.Time

	village Village
	groups  map[string]map[int]*ItemStat

	// ownedByVillage tracks raw owned counts per ID, independent of the
	// group/kind bucketing above - Missing and NextHall need "how many of ID
	// X exist" regardless of what group X belongs to.
	ownedByVillage map[string]map[int]int
}

func (b *builder) begin(key, label string, hall int) {
	b.village = Village{Key: key, Label: label, Hall: hall, Remaining: newRemaining()}
	b.groups = map[string]map[int]*ItemStat{}
}

func (b *builder) addAll(items []snapshot.Item, village, lane string) {
	for _, it := range items {
		b.add(it, village, lane)
	}
}

func (b *builder) add(it snapshot.Item, village, lane string) {
	if it.Data == 0 {
		return
	}
	entry, known := b.cat.Lookup(it.Data)
	name, kind := entry.Name, entry.Kind
	if !known {
		name, kind = catalog.FallbackName(it.Data)
	}

	n := it.Count()
	if b.ownedByVillage[village] == nil {
		b.ownedByVillage[village] = map[int]int{}
	}
	b.ownedByVillage[village][it.Data] += n

	// An item with a timer is mid-upgrade: its stored level is the level it is
	// leaving, and it climbs by one when the timer expires. If that moment is
	// already behind now, treat it as landed - the export itself won't say so
	// until the next one, but there is no reason to keep reporting a finished
	// upgrade as still in progress.
	lvl := it.Lvl
	upgrading := false
	if it.Timer > 0 {
		finishesAt := b.report.CapturedAt.Add(time.Duration(it.Timer) * time.Second)
		icon := ""
		if known {
			icon = entry.IconURL(it.Lvl + 1)
		}
		b.report.Jobs = append(b.report.Jobs, Job{
			ID:         it.Data,
			Name:       name,
			Kind:       kind,
			Village:    village,
			Lane:       lane,
			FromLevel:  it.Lvl,
			ToLevel:    it.Lvl + 1,
			Remaining:  it.Timer,
			FinishesAt: finishesAt,
			New:        it.Lvl == 0,
			Icon:       icon,
		})
		if timerElapsed(finishesAt, b.now) {
			lvl = it.Lvl + 1
		} else {
			upgrading = true
		}
	}

	gk := groupKey(kind, entry.Class, known)
	g, ok := b.groups[gk]
	if !ok {
		g = map[int]*ItemStat{}
		b.groups[gk] = g
	}

	st, ok := g[it.Data]
	if !ok {
		st = &ItemStat{
			ID:        it.Data,
			Name:      name,
			GlobalMax: entry.MaxLevel,
			Reachable: entry.Reachable(b.gates),
			Unnamed:   !known,
			Lane:      lane,
			Remaining: newRemaining(),
		}
		if !known {
			// Without catalog data there is no honest ceiling to measure
			// against, so this item is counted but left out of the percentage.
			st.Reachable = 0
		}
		g[it.Data] = st
	}

	st.Copies += n
	if upgrading {
		st.Upgrading += n
	}
	st.Buckets = append(st.Buckets, Bucket{Level: lvl, Count: n})
	if entry.MaxLevel > 0 && lvl > entry.MaxLevel {
		st.BeyondMax = true
	}
}

// end finalises the village: it merges buckets, works out completion and the
// remaining bill, tallies strength, and orders groups so the most-upgraded
// parts of the base are not buried.
func (b *builder) end(order []struct{ key, label string }) {
	for _, def := range order {
		items := b.groups[def.key]
		if len(items) == 0 {
			continue
		}
		g := Group{Key: def.key, Label: def.label, Remaining: newRemaining()}

		for _, st := range items {
			st.Buckets = mergeBuckets(st.Buckets)

			target := st.Reachable
			for _, bk := range st.Buckets {
				st.LevelsDone += bk.Level * bk.Count
				if bk.Level > target {
					// Already past the computed ceiling, so trust the village
					// over the catalog and treat this level as the target.
					target = bk.Level
				}
				if st.Reachable > 0 && bk.Level >= st.Reachable {
					st.CopiesAtMax += bk.Count
				}
			}

			entry, entryKnown := b.cat.Lookup(st.ID)
			if entryKnown {
				if g.Gate == "" {
					g.Gate = primaryGate(entry)
				}
				iconLevel := st.Reachable
				if len(st.Buckets) > 0 && st.Buckets[0].Level > iconLevel {
					iconLevel = st.Buckets[0].Level // buckets sort high to low; index 0 is the highest owned
				}
				st.Icon = entry.IconURL(iconLevel)
				st.Hero = entry.Hero
				st.Rarity = entry.Rarity
			}

			if st.Reachable > 0 {
				st.Measured = true
				st.LevelsTarget = target * st.Copies
				st.Completion = ratio(st.LevelsDone, st.LevelsTarget)

				if entryKnown {
					for _, bk := range st.Buckets {
						addRemaining(&st.Remaining, entry, bk.Level, st.Reachable, bk.Count)
						if bk.Level >= 1 && bk.Level <= len(entry.Levels) {
							b.report.Strength.Done += int64(entry.Levels[bk.Level-1].Strength) * int64(bk.Count)
						}
					}
					if st.Reachable >= 1 && st.Reachable <= len(entry.Levels) {
						b.report.Strength.Reachable += int64(entry.Levels[st.Reachable-1].Strength) * int64(st.Copies)
					}
					b.report.Bill.add(st.Remaining, st.Lane)
				}
			}

			g.Items = append(g.Items, *st)
			g.Copies += st.Copies
			g.CopiesAtMax += st.CopiesAtMax
			g.Remaining.add(st.Remaining)
			if st.Measured {
				// Only measurable items feed the percentages. Counting levels
				// earned without a matching ceiling would push totals past 100%.
				g.Measured = true
				g.LevelsDone += st.LevelsDone
				g.LevelsTarget += st.LevelsTarget
			}
		}

		sort.Slice(g.Items, func(i, j int) bool {
			if g.Items[i].Completion != g.Items[j].Completion {
				return g.Items[i].Completion < g.Items[j].Completion
			}
			return g.Items[i].Name < g.Items[j].Name
		})
		g.Completion = ratio(g.LevelsDone, g.LevelsTarget)

		b.village.Groups = append(b.village.Groups, g)
		b.village.Copies += g.Copies
		b.village.CopiesAtMax += g.CopiesAtMax
		b.village.LevelsDone += g.LevelsDone
		b.village.LevelsTarget += g.LevelsTarget
		b.village.Remaining.add(g.Remaining)
		if !g.Measured {
			b.village.Unmeasured += g.Copies
		}
	}

	if len(b.village.Groups) == 0 {
		return
	}
	b.village.Completion = ratio(b.village.LevelsDone, b.village.LevelsTarget)
	b.report.Villages = append(b.report.Villages, b.village)
}

func (b *builder) addHelpers(items []snapshot.Item) {
	for _, it := range items {
		if it.HelperCooldown <= 0 {
			continue
		}
		name, _ := catalog.FallbackName(it.Data)
		if e, ok := b.cat.Lookup(it.Data); ok {
			name = e.Name
		}
		b.report.Jobs = append(b.report.Jobs, Job{
			ID:         it.Data,
			Name:       name,
			Kind:       "helper",
			Village:    "home",
			Lane:       "helper",
			FromLevel:  it.Lvl,
			ToLevel:    it.Lvl,
			Remaining:  it.HelperCooldown,
			FinishesAt: b.report.CapturedAt.Add(time.Duration(it.HelperCooldown) * time.Second),
		})
	}
}

// nextUp lists the single next upgrade step for every measured item that has
// not yet reached its ceiling. It does not distinguish an idle copy from one
// already mid-upgrade - Lanes is where real capacity lives.
func (b *builder) nextUp() []Suggestion {
	var out []Suggestion
	for _, v := range b.report.Villages {
		for _, g := range v.Groups {
			for _, st := range g.Items {
				if !st.Measured || st.Copies == 0 || st.CopiesAtMax >= st.Copies || len(st.Buckets) == 0 {
					continue
				}
				lvl := st.Buckets[len(st.Buckets)-1].Level // buckets sort high to low; last is lowest
				if lvl >= st.Reachable {
					continue
				}
				entry, ok := b.cat.Lookup(st.ID)
				if !ok || lvl >= len(entry.Levels) {
					continue
				}
				cost, ore, seconds := entry.Between(lvl, lvl+1)
				foldOre(cost, ore)
				gain := entry.Levels[lvl].Strength
				if lvl > 0 {
					gain -= entry.Levels[lvl-1].Strength
				}
				out = append(out, Suggestion{
					ID: st.ID, Name: st.Name, Kind: entry.Kind, Village: v.Key, Lane: st.Lane,
					FromLevel: lvl, ToLevel: lvl + 1, Cost: cost, Seconds: seconds, StrengthGain: gain,
					Icon: entry.IconURL(lvl + 1),
				})
			}
		}
	}
	return out
}

// missing lists things the current hall allows that have not been built.
func (b *builder) missing() []Shortfall {
	var out []Shortfall
	out = append(out, hallShortfall(b.cat, b.ownedByVillage["home"], idTownHall, b.gates.TownHall, "home")...)
	out = append(out, hallShortfall(b.cat, b.ownedByVillage["builder"], idBuilderHall, b.gates.BuilderHall, "builder")...)
	return out
}

// hallShortfall compares a hall's cumulative unlocks against what is owned.
// Unlocks that name an entry with no level data of its own are skipped: a
// handful of buildings the game tracks with no independent cost or build
// time would otherwise show up as a "missing" item with nothing to act on.
func hallShortfall(cat *catalog.Catalog, owned map[int]int, hallID, hallLevel int, village string) []Shortfall {
	if hallLevel <= 0 {
		return nil
	}
	hall, ok := cat.Lookup(hallID)
	if !ok {
		return nil
	}
	allowed := map[int]int{}
	for i := 0; i < hallLevel && i < len(hall.Levels); i++ {
		for _, u := range hall.Levels[i].Unlocks {
			allowed[u.ID] += u.Quantity
		}
	}
	var out []Shortfall
	for id, want := range allowed {
		e, ok := cat.Lookup(id)
		if !ok || len(e.Levels) == 0 {
			continue
		}
		if have := owned[id]; have < want {
			out = append(out, Shortfall{ID: id, Name: e.Name, Village: village, Owned: have, Allowed: want, Icon: e.IconURL(1)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// nextHallPreview describes the next Town Hall or Builder Hall level: its
// own cost and time, what it unlocks, and how many owned items would gain
// ceiling room. Returns nil once the hall has no level left to take.
func nextHallPreview(cat *catalog.Catalog, gates catalog.Gates, owned map[int]int, hallID, hallLevel int, village string) *HallPreview {
	if hallLevel <= 0 {
		return nil
	}
	hall, ok := cat.Lookup(hallID)
	if !ok || hallLevel >= len(hall.Levels) {
		return nil
	}
	cost, ore, seconds := hall.Between(hallLevel, hallLevel+1)
	foldOre(cost, ore)

	nextGates := gates
	switch village {
	case "home":
		nextGates.TownHall++
	case "builder":
		nextGates.BuilderHall++
	}
	opened := 0
	for id := range owned {
		e, ok := cat.Lookup(id)
		if !ok {
			continue
		}
		if e.Reachable(nextGates) > e.Reachable(gates) {
			opened++
		}
	}
	return &HallPreview{
		Village: village, FromLevel: hallLevel, ToLevel: hallLevel + 1,
		Cost: cost, Seconds: seconds, Unlocks: hall.Levels[hallLevel].Unlocks, ItemsAffected: opened,
	}
}

// computeBalance ranks every measured group across both villages, weakest
// completion first, so a Balance panel can show what is dragging without
// needing to flip between village tabs.
func computeBalance(villages []Village) []GroupScore {
	var out []GroupScore
	for _, v := range villages {
		for _, g := range v.Groups {
			if !g.Measured {
				continue
			}
			out = append(out, GroupScore{
				Village: v.Key, Key: g.Key, Label: g.Label, Gate: g.Gate,
				Completion: g.Completion, Copies: g.Copies, CopiesAtMax: g.CopiesAtMax,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Completion < out[j].Completion })
	return out
}

// addRemaining folds the cost of taking count copies from level from up to
// level to into dst.
func addRemaining(dst *Remaining, e catalog.Entry, from, to, count int) {
	if to <= from || count <= 0 {
		return
	}
	cost, ore, seconds := e.Between(from, to)
	for k, v := range cost {
		dst.Cost[k] += v * int64(count)
	}
	foldOre(dst.Cost, catalog.Ore{Shiny: ore.Shiny * int64(count), Glowy: ore.Glowy * int64(count), Starry: ore.Starry * int64(count)})
	dst.Seconds += seconds * int64(count)
	dst.Steps += (to - from) * count
}

// foldOre merges an equipment ore cost into a currency map under named keys,
// so callers can treat "gold" and "ore" the same way once folded.
func foldOre(cost map[string]int64, ore catalog.Ore) {
	if ore.Shiny > 0 {
		cost["Shiny Ore"] += ore.Shiny
	}
	if ore.Glowy > 0 {
		cost["Glowy Ore"] += ore.Glowy
	}
	if ore.Starry > 0 {
		cost["Starry Ore"] += ore.Starry
	}
}

// primaryGate names the one gate that most specifically controls this
// entry's levels - the laboratory rather than the Town Hall for troops, the
// Hero Hall rather than the Town Hall for home heroes, and so on.
func primaryGate(e catalog.Entry) string {
	if len(e.Levels) == 0 {
		return ""
	}
	req := e.Levels[0].Requires
	for _, gate := range []string{"lab", "starlab", "herohall", "smith", "pethouse"} {
		if _, ok := req[gate]; ok {
			return gate
		}
	}
	for _, gate := range []string{"th", "bh"} {
		if _, ok := req[gate]; ok {
			return gate
		}
	}
	return ""
}

// lanes reports capacity per lane: how many slots exist and how many are busy.
func (b *builder) lanes(e *snapshot.Export) []Lane {
	defs := []struct {
		key, label, village string
		total               int
	}{
		{"builder", "Builders", "home", countOf(e.Buildings, idBuildersHut)},
		{"lab", "Laboratory", "home", has(e.Buildings, idLaboratory)},
		{"helper", "Helpers", "home", len(e.Helpers)},
		{"builder2", "Builders", "builder", countOf(e.Buildings2, idBuildersHut2)},
		{"starlab", "Star Laboratory", "builder", has(e.Buildings2, idStarLaboratory)},
	}

	out := make([]Lane, 0, len(defs))
	for _, d := range defs {
		lane := Lane{Key: d.key, Label: d.label, Village: d.village, Total: d.total}
		var soonest *time.Time
		for i := range b.report.Jobs {
			j := b.report.Jobs[i]
			if j.Lane != d.key || timerElapsed(j.FinishesAt, b.now) {
				continue
			}
			lane.Busy++
			if soonest == nil || j.FinishesAt.Before(*soonest) {
				t := j.FinishesAt
				soonest = &t
			}
		}
		// A lane with nothing in it and no capacity found does not exist in
		// this village. But if work is running, the lane must still be shown
		// even when its capacity building could not be counted, otherwise the
		// upgrade would silently lose its slot.
		if lane.Total == 0 {
			if lane.Busy == 0 {
				continue
			}
			lane.Total = lane.Busy
		}
		if lane.Busy >= lane.Total {
			lane.NextFreeAt = soonest
		}
		out = append(out, lane)
	}
	return out
}

// notes surfaces the limits of the analysis rather than hiding them.
func (b *builder) notes() []string {
	var unnamed, beyond int
	for _, v := range b.report.Villages {
		for _, g := range v.Groups {
			for _, it := range g.Items {
				if it.Unnamed {
					unnamed++
				}
				if it.BeyondMax {
					beyond++
				}
			}
		}
	}
	b.report.Catalog.Unnamed = unnamed
	b.report.Catalog.BeyondMax = beyond

	out := []string{}
	if unnamed > 0 {
		out = append(out, fmt.Sprintf(
			"%d items are not in the catalog, so they are counted but left out of the percentages. Regenerate the catalog from current game data to name them.", unnamed))
	}
	if beyond > 0 {
		out = append(out, fmt.Sprintf(
			"%d items sit above the highest level the catalog knows about, which means the catalog predates a game update.", beyond))
	}
	if b.report.Bill.Steps > 0 {
		out = append(out, "The bill assumes upgrades run one after another at full catalog time; it does not subtract time already spent on upgrades in flight.")
	}
	return out
}

// groupKey decides which category an item belongs to. Buildings carry their own
// class in the game data; everything else is grouped by what it is.
func groupKey(kind, class string, known bool) string {
	if !known {
		switch kind {
		case "building", "trap", "unit", "spell", "hero", "equipment", "pet", "helper":
			return kind
		}
		return "other"
	}
	switch kind {
	case "building":
		switch class {
		case "Defense":
			return "defense"
		case "Wall":
			return "wall"
		case "Resource":
			return "resource"
		case "Army":
			return "army"
		case "Town Hall", "Town Hall2", "BB Builder Hall":
			return "core"
		case "Worker", "Worker2":
			return "hut"
		case "Helper":
			return "helper"
		}
		return "other"
	case "trap", "unit", "spell", "hero", "pet", "equipment", "helper":
		return kind
	}
	return "other"
}

func readGates(e *snapshot.Export, now time.Time) catalog.Gates {
	capturedAt := e.CapturedAt()
	return catalog.Gates{
		TownHall:       maxLevel(e.Buildings, idTownHall, capturedAt, now),
		Laboratory:     maxLevel(e.Buildings, idLaboratory, capturedAt, now),
		BuilderHall:    maxLevel(e.Buildings2, idBuilderHall, capturedAt, now),
		StarLaboratory: maxLevel(e.Buildings2, idStarLaboratory, capturedAt, now),
		Blacksmith:     maxLevel(e.Buildings, idBlacksmith, capturedAt, now),
		PetHouse:       maxLevel(e.Buildings, idPetHouse, capturedAt, now),
		HeroHall:       maxLevel(e.Buildings, idHeroHall, capturedAt, now),
	}
}

// maxLevel finds the highest level any copy of id sits at, counting a copy
// whose own upgrade timer has already run out (relative to now) as having
// landed - a Town Hall or Laboratory mid-upgrade must not keep gating
// everything else at its old level once that moment has passed.
func maxLevel(items []snapshot.Item, id int, capturedAt, now time.Time) int {
	best := 0
	for _, it := range items {
		if it.Data != id {
			continue
		}
		lvl := it.Lvl
		if it.Timer > 0 && timerElapsed(capturedAt.Add(time.Duration(it.Timer)*time.Second), now) {
			lvl++
		}
		if lvl > best {
			best = lvl
		}
	}
	return best
}

// timerElapsed reports whether finishesAt is at or before now - true means
// an in-flight upgrade's timer would already have run out, even though the
// export itself won't say so until the next one.
func timerElapsed(finishesAt, now time.Time) bool {
	return !finishesAt.After(now)
}

func countOf(items []snapshot.Item, id int) int {
	n := 0
	for _, it := range items {
		if it.Data == id {
			n += it.Count()
		}
	}
	return n
}

func has(items []snapshot.Item, id int) int {
	if countOf(items, id) > 0 {
		return 1
	}
	return 0
}

func countCopies(items []snapshot.Item) int {
	n := 0
	for _, it := range items {
		n += it.Count()
	}
	return n
}

// mergeBuckets combines rows at the same level and sorts them high to low.
func mergeBuckets(in []Bucket) []Bucket {
	byLevel := map[int]int{}
	for _, b := range in {
		byLevel[b.Level] += b.Count
	}
	out := make([]Bucket, 0, len(byLevel))
	for lvl, n := range byLevel {
		out = append(out, Bucket{Level: lvl, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Level > out[j].Level })
	return out
}

func ratio(done, target int) float64 {
	if target <= 0 {
		return 0
	}
	r := float64(done) / float64(target)
	if r > 1 {
		return 1
	}
	return r
}

// ---------------------------------------------------------------------------
// Change log: what happened between two exports of the same village.

// Change is one thing that happened between two exports.
type Change struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Village string `json:"village"`
	Type    string `json:"type"` // "landed", "built", "started", "cleared", "appeared"
	// FromLevel and ToLevel are 0 for "cleared"/"appeared" changes, which
	// have no level to report - deliberately not omitempty, since a level-0
	// FromLevel on a "built" change (built means arriving at level 1 from
	// nothing) is meaningful and must not be dropped.
	FromLevel int    `json:"fromLevel"`
	ToLevel   int    `json:"toLevel"`
	Count     int    `json:"count"`
	Icon      string `json:"icon,omitempty"`
}

// ChangeLog is every Change between two snapshots of the same village.
type ChangeLog struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Changes []Change  `json:"changes"`
}

// Diff compares two exports of the same village and reports what changed:
// upgrades that landed or newly started, first-time builds, and obstacles or
// decorations cleared or placed. It reads the raw exports rather than two
// Reports because obstacles and decorations are only ever counted, not
// tracked item by item, in the analysed Report.
func Diff(prev, cur *snapshot.Export, cat *catalog.Catalog) ChangeLog {
	cl := ChangeLog{From: prev.CapturedAt(), To: cur.CapturedAt()}

	type arrDef struct {
		village string
		before  []snapshot.Item
		after   []snapshot.Item
	}
	leveled := []arrDef{
		{"home", prev.Buildings, cur.Buildings},
		{"home", prev.Traps, cur.Traps},
		{"home", prev.Units, cur.Units},
		{"home", prev.Spells, cur.Spells},
		{"home", prev.Heroes, cur.Heroes},
		{"home", prev.Pets, cur.Pets},
		{"home", prev.Equipment, cur.Equipment},
		{"home", prev.SiegeMachines, cur.SiegeMachines},
		{"builder", prev.Buildings2, cur.Buildings2},
		{"builder", prev.Traps2, cur.Traps2},
		{"builder", prev.Units2, cur.Units2},
		{"builder", prev.Heroes2, cur.Heroes2},
	}
	for _, a := range leveled {
		cl.Changes = append(cl.Changes, levelChanges(a.before, a.after, a.village, cat)...)
		cl.Changes = append(cl.Changes, startedChanges(a.before, a.after, a.village, cat)...)
	}

	cl.Changes = append(cl.Changes, countChanges(prev.Obstacles, cur.Obstacles, "home", "obstacle", cat)...)
	cl.Changes = append(cl.Changes, countChanges(prev.Obstacles2, cur.Obstacles2, "builder", "obstacle", cat)...)
	cl.Changes = append(cl.Changes, countChanges(prev.Decos, cur.Decos, "home", "deco", cat)...)
	cl.Changes = append(cl.Changes, countChanges(prev.Decos2, cur.Decos2, "builder", "deco", cat)...)

	sort.Slice(cl.Changes, func(i, j int) bool {
		if cl.Changes[i].Type != cl.Changes[j].Type {
			return cl.Changes[i].Type < cl.Changes[j].Type
		}
		return cl.Changes[i].Name < cl.Changes[j].Name
	})
	return cl
}

// levelChanges pairs a level that lost copies with the level above it that
// gained the same copies, which is what an upgrade landing (or a first
// build, from level 0) looks like across two snapshots.
func levelChanges(before, after []snapshot.Item, village string, cat *catalog.Catalog) []Change {
	type key struct{ id, lvl int }
	b, a := map[key]int{}, map[key]int{}
	for _, it := range before {
		b[key{it.Data, it.Lvl}] += it.Count()
	}
	for _, it := range after {
		a[key{it.Data, it.Lvl}] += it.Count()
	}

	ids := map[int]bool{}
	levelsOf := map[int]map[int]bool{}
	for k := range b {
		ids[k.id] = true
		if levelsOf[k.id] == nil {
			levelsOf[k.id] = map[int]bool{}
		}
		levelsOf[k.id][k.lvl] = true
	}
	for k := range a {
		ids[k.id] = true
		if levelsOf[k.id] == nil {
			levelsOf[k.id] = map[int]bool{}
		}
		levelsOf[k.id][k.lvl] = true
	}

	var out []Change
	for id := range ids {
		levels := make([]int, 0, len(levelsOf[id]))
		for lvl := range levelsOf[id] {
			levels = append(levels, lvl)
		}
		sort.Ints(levels)
		for _, lvl := range levels {
			delta := a[key{id, lvl}] - b[key{id, lvl}]
			if delta >= 0 {
				continue
			}
			lost := -delta
			gained := a[key{id, lvl + 1}] - b[key{id, lvl + 1}]
			if gained <= 0 {
				continue
			}
			n := min(lost, gained)
			name, kind, icon := nameOf(cat, id, lvl+1)
			typ := "landed"
			if lvl == 0 {
				typ = "built"
			}
			out = append(out, Change{
				ID: id, Name: name, Kind: kind, Village: village, Type: typ,
				FromLevel: lvl, ToLevel: lvl + 1, Count: n, Icon: icon,
			})
		}
	}
	return out
}

// startedChanges reports upgrades whose timer is newly present at a level
// where the previous snapshot had none - a job that started sometime in the
// gap between the two exports, whether or not it has landed yet.
func startedChanges(before, after []snapshot.Item, village string, cat *catalog.Catalog) []Change {
	type key struct{ id, lvl int }
	b, a := map[key]int{}, map[key]int{}
	for _, it := range before {
		if it.Timer > 0 {
			b[key{it.Data, it.Lvl}]++
		}
	}
	for _, it := range after {
		if it.Timer > 0 {
			a[key{it.Data, it.Lvl}]++
		}
	}
	var out []Change
	for k, n := range a {
		if extra := n - b[k]; extra > 0 {
			name, kind, icon := nameOf(cat, k.id, k.lvl+1)
			out = append(out, Change{
				ID: k.id, Name: name, Kind: kind, Village: village, Type: "started",
				FromLevel: k.lvl, ToLevel: k.lvl + 1, Count: extra, Icon: icon,
			})
		}
	}
	return out
}

// countChanges compares plain owned counts - for obstacles and decorations,
// which have no level to climb, a count going down means something was
// cleared and a count going up means something appeared.
func countChanges(before, after []snapshot.Item, village, kind string, cat *catalog.Catalog) []Change {
	b, a := map[int]int{}, map[int]int{}
	for _, it := range before {
		b[it.Data] += it.Count()
	}
	for _, it := range after {
		a[it.Data] += it.Count()
	}
	ids := map[int]bool{}
	for id := range b {
		ids[id] = true
	}
	for id := range a {
		ids[id] = true
	}
	var out []Change
	for id := range ids {
		delta := a[id] - b[id]
		if delta == 0 {
			continue
		}
		name, k, icon := nameOf(cat, id, 1)
		if k == "other" {
			k = kind
		}
		typ, n := "appeared", delta
		if delta < 0 {
			typ, n = "cleared", -delta
		}
		out = append(out, Change{ID: id, Name: name, Kind: k, Village: village, Type: typ, Count: n, Icon: icon})
	}
	return out
}

// nameOf resolves a raw export ID to a name, kind and icon (at the given
// level, for entries with tiered art), falling back to the ID-block guess
// FallbackName makes when the catalog has never heard of it.
func nameOf(cat *catalog.Catalog, id, level int) (string, string, string) {
	if e, ok := cat.Lookup(id); ok {
		return e.Name, e.Kind, e.IconURL(level)
	}
	name, kind := catalog.FallbackName(id)
	return name, kind, ""
}
