// Command catalogen builds data/catalog.json, the table that turns numeric
// game IDs from a village export into names, categories, upgrade ceilings,
// costs and icon paths.
//
// The source is ClashKingInc/ClashKingAssets' static_data.json: every entity
// carries its own numeric _id, so there is no need to derive IDs from row
// order the way Supercell's raw CSVs require. manifest.json supplies the
// matching icon asset paths, matched by the same name-to-slug rule the
// ClashKing README documents.
//
//	go run ./cmd/catalogen -data ./gamedata -out ./data/catalog.json
//
// Re-run it after a game update to pick up new content.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// static_data.json shapes. Only the fields this program reads are declared;
// everything else in the source is ignored.

type srcUnlock struct {
	ID       int    `json:"_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type srcLevel struct {
	Level                   int             `json:"level"`
	BuildCost               int             `json:"build_cost"`
	BuildTime               int             `json:"build_time"`
	UpgradeTime             int             `json:"upgrade_time"`
	UpgradeCost             json.RawMessage `json:"upgrade_cost"` // a number, or {shiny_ore,glowy_ore,starry_ore} for equipment
	RequiredTownhall        *int            `json:"required_townhall"`
	RequiredLabLevel        *int            `json:"required_lab_level"`
	RequiredBlacksmithLevel *int            `json:"required_blacksmith_level"`
	RequiredPetHouseLevel   *int            `json:"required_pet_house_level"`
	RequiredHeroTavernLevel *int            `json:"required_hero_tavern_level"`
	StrengthWeight          int             `json:"strength_weight"`
	AltUpgradeResource      string          `json:"alt_upgrade_resource"`
	Unlocks                 []srcUnlock     `json:"unlocks"`
}

// srcEntity covers the categories that carry per-level data: buildings,
// traps, troops, spells, heroes, pets, equipment, helpers, guardians.
type srcEntity struct {
	ID              int        `json:"_id"`
	Name            string     `json:"name"`
	Village         string     `json:"village"` // "home", "builderBase", or absent (pets/equipment/helpers/guardians are home-only)
	UpgradeResource string     `json:"upgrade_resource"`
	Rarity          string     `json:"rarity"`
	Hero            string     `json:"hero"`
	Type            string     `json:"type"` // buildings only: Defense, Wall, Army, Resource, Town Hall, Worker...
	Levels          []srcLevel `json:"levels"`
}

// srcCosmetic covers the categories with no upgrade path: decorations,
// obstacles, skins, sceneries, capital house parts. They get a name and
// nothing else - the analysis never measures them against a ceiling.
type srcCosmetic struct {
	ID      int    `json:"_id"`
	Name    string `json:"name"`
	Village string `json:"village"`
}

var leveledCategories = []string{"buildings", "traps", "troops", "spells", "heroes", "pets", "equipment", "helpers", "guardians"}
var cosmeticCategories = []string{"decorations", "obstacles", "skins", "sceneries", "capital_house_parts"}

func loadLeveled(top map[string]json.RawMessage) (map[string][]srcEntity, error) {
	out := map[string][]srcEntity{}
	for _, cat := range leveledCategories {
		msg, ok := top[cat]
		if !ok {
			continue
		}
		var ents []srcEntity
		if err := json.Unmarshal(msg, &ents); err != nil {
			return nil, fmt.Errorf("%s: %w", cat, err)
		}
		out[cat] = ents
	}
	return out, nil
}

func loadCosmetic(top map[string]json.RawMessage) (map[string][]srcCosmetic, error) {
	out := map[string][]srcCosmetic{}
	for _, cat := range cosmeticCategories {
		msg, ok := top[cat]
		if !ok {
			continue
		}
		var ents []srcCosmetic
		if err := json.Unmarshal(msg, &ents); err != nil {
			return nil, fmt.Errorf("%s: %w", cat, err)
		}
		out[cat] = ents
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// manifest.json: a flat list of icon asset paths. Paths are matched back to
// entities by slugifying the entity's name with the same rule the assets
// were published under, rather than reconstructing URLs blind - a wrong
// guess there would be a silently broken image, not a build error.

type manifestAsset struct {
	Path     string `json:"path"`
	Category string `json:"category"`
}

type manifestFile struct {
	Assets []manifestAsset `json:"assets"`
}

type iconIndex struct {
	// "category|village-slug|slug" -> level -> asset path (buildings, traps)
	tiered map[string]map[int]string
	// "category|slug" -> asset path (heroes, troops, pets, guardians, spells, equipment, helpers)
	single map[string]string
}

var levelFileRe = regexp.MustCompile(`level_(\d+)\.\w+$`)

func buildIconIndex(assets []manifestAsset) iconIndex {
	idx := iconIndex{tiered: map[string]map[int]string{}, single: map[string]string{}}
	for _, a := range assets {
		parts := strings.Split(a.Path, "/")
		switch a.Category {
		case "buildings", "traps":
			if len(parts) != 4 {
				continue
			}
			village, slug, file := parts[1], parts[2], parts[3]
			m := levelFileRe.FindStringSubmatch(file)
			if m == nil {
				continue
			}
			lvl, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			key := a.Category + "|" + village + "|" + slug
			if idx.tiered[key] == nil {
				idx.tiered[key] = map[int]string{}
			}
			idx.tiered[key][lvl] = a.Path
		case "heroes", "troops", "pets", "guardians", "spells", "equipment", "helpers":
			if len(parts) < 2 {
				continue
			}
			slug := strings.TrimSuffix(parts[len(parts)-1], filepath.Ext(parts[len(parts)-1]))
			if len(parts) >= 3 {
				slug = parts[len(parts)-2] // "{cat}/{slug}/icon.webp"
			}
			key := a.Category + "|" + slug
			idx.single[key] = a.Path
		}
	}
	return idx
}

// curlyApostrophe is the only quote character ClashKing's slug rule strips;
// a plain ' is kept, which is why "Builder's Apprentice" survives intact.
const curlyApostrophe = "’"

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "?", "")
	s = strings.ReplaceAll(s, curlyApostrophe, "")
	return s
}

func villageSlug(village string) string {
	if village == "builderBase" {
		return "builder-base"
	}
	return "home-village"
}

// lookupIcon returns an icon path template (with a "{level}" placeholder for
// tiered categories) and the highest level art actually exists for. An empty
// path means no icon was found, which the frontend treats as "no icon".
func lookupIcon(idx iconIndex, category, village, name string) (path string, maxLevel int) {
	slug := slugify(name)
	switch category {
	case "buildings", "traps":
		key := category + "|" + villageSlug(village) + "|" + slug
		byLevel := idx.tiered[key]
		if len(byLevel) == 0 {
			return "", 0
		}
		var ext string
		for lvl, p := range byLevel {
			if lvl > maxLevel {
				maxLevel = lvl
				ext = filepath.Ext(p)
			}
		}
		if ext == "" {
			ext = ".webp"
		}
		return fmt.Sprintf("%s/%s/%s/level_{level}%s", category, villageSlug(village), slug, ext), maxLevel
	default:
		key := category + "|" + slug
		if p, ok := idx.single[key]; ok {
			return p, 0
		}
		return "", 0
	}
}

// ---------------------------------------------------------------------------
// Output shapes. These mirror internal/catalog.Entry/Level/Ore/Unlock -
// keep both definitions in sync when either changes.

type Entry struct {
	Name         string  `json:"name"`
	Kind         string  `json:"kind"`
	Class        string  `json:"class,omitempty"`
	Village      int     `json:"village"`
	Hero         string  `json:"hero,omitempty"`
	Rarity       string  `json:"rarity,omitempty"`
	Resource     string  `json:"resource,omitempty"`
	MaxLevel     int     `json:"maxLevel"`
	Icon         string  `json:"icon,omitempty"`
	IconMaxLevel int     `json:"iconMaxLevel,omitempty"`
	Levels       []Level `json:"levels,omitempty"`
}

type Level struct {
	Requires map[string]int `json:"requires,omitempty"`
	Cost     int64          `json:"cost,omitempty"`
	AltRes   string         `json:"altRes,omitempty"`
	Ore      Ore            `json:"ore,omitzero"`
	Seconds  int64          `json:"seconds,omitempty"`
	Strength int            `json:"strength,omitempty"`
	Unlocks  []Unlock       `json:"unlocks,omitempty"`
}

type Ore struct {
	Shiny  int64 `json:"shiny,omitempty"`
	Glowy  int64 `json:"glowy,omitempty"`
	Starry int64 `json:"starry,omitempty"`
}

type Unlock struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type Catalog struct {
	GeneratedAt string           `json:"generatedAt"`
	Source      string           `json:"source"`
	Entries     map[string]Entry `json:"entries"`
}

func main() {
	dataDir := flag.String("data", "gamedata", "directory holding static_data.json and manifest.json")
	out := flag.String("out", filepath.Join("data", "catalog.json"), "where to write catalog.json")
	source := flag.String("source", "ClashKingInc/ClashKingAssets static_data.json", "note recorded in the catalog")
	flag.Parse()

	raw, err := os.ReadFile(filepath.Join(*dataDir, "static_data.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		fmt.Fprintf(os.Stderr, "static_data.json: %v\n", err)
		os.Exit(1)
	}

	leveled, err := loadLeveled(top)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cosmetic, err := loadCosmetic(top)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var idx iconIndex
	if mraw, err := os.ReadFile(filepath.Join(*dataDir, "manifest.json")); err != nil {
		fmt.Fprintf(os.Stderr, "no manifest.json (%v) - icons will be skipped\n", err)
	} else {
		var mf manifestFile
		if err := json.Unmarshal(mraw, &mf); err != nil {
			fmt.Fprintf(os.Stderr, "manifest.json: %v - icons will be skipped\n", err)
		} else {
			idx = buildIconIndex(mf.Assets)
		}
	}

	cat := Catalog{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      *source,
		Entries:     map[string]Entry{},
	}

	for _, category := range leveledCategories {
		n := 0
		for _, e := range leveled[category] {
			cat.Entries[strconv.Itoa(e.ID)] = convert(category, e, idx)
			n++
		}
		fmt.Printf("%-10s %4d entries\n", category, n)
	}
	for _, category := range cosmeticCategories {
		n := 0
		kind := cosmeticKind(category)
		for _, e := range cosmetic[category] {
			village := 0
			if e.Village == "builderBase" {
				village = 1
			}
			icon, iconMax := lookupIcon(idx, category, e.Village, e.Name)
			cat.Entries[strconv.Itoa(e.ID)] = Entry{
				Name: e.Name, Kind: kind, Village: village,
				Icon: icon, IconMaxLevel: iconMax,
			}
			n++
		}
		fmt.Printf("%-10s %4d entries (no ceiling)\n", category, n)
	}

	if err := writeJSON(*out, cat); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\nwrote %s (%d entries)\n", *out, len(cat.Entries))
}

// convert turns one leveled source entity into a catalog Entry. category is
// the static_data.json key it came from, which decides which requirement and
// cost fields on each level actually apply.
func convert(category string, e srcEntity, idx iconIndex) Entry {
	kind, class := kindFor(category, e)
	village := 0
	if e.Village == "builderBase" {
		village = 1
	}
	icon, iconMax := lookupIcon(idx, category, e.Village, e.Name)

	out := Entry{
		Name: e.Name, Kind: kind, Class: class, Village: village,
		Hero: e.Hero, Rarity: e.Rarity, Resource: e.UpgradeResource,
		MaxLevel: len(e.Levels), Icon: icon, IconMaxLevel: iconMax,
	}
	for _, lv := range e.Levels {
		cost, ore := costFor(category, lv)
		out.Levels = append(out.Levels, Level{
			Requires: requiresFor(category, e.Village, lv),
			Cost:     cost,
			AltRes:   lv.AltUpgradeResource,
			Ore:      ore,
			Seconds:  secondsFor(lv),
			Strength: lv.StrengthWeight,
			Unlocks:  unlocksFor(lv),
		})
	}
	return out
}

func kindFor(category string, e srcEntity) (kind, class string) {
	switch category {
	case "buildings":
		return "building", e.Type
	case "traps":
		return "trap", ""
	case "troops":
		return "unit", ""
	case "spells":
		return "spell", ""
	case "heroes":
		return "hero", ""
	case "pets":
		return "pet", ""
	case "equipment":
		return "equipment", ""
	case "helpers":
		return "helper", ""
	case "guardians":
		// Guardians are Town-Hall-18 home defenses shipped as their own
		// static_data.json table; folding them into "Defense" lands them in
		// the same group as every other turret.
		return "building", "Defense"
	}
	return "other", ""
}

func cosmeticKind(category string) string {
	switch category {
	case "decorations":
		return "deco"
	case "obstacles":
		return "obstacle"
	case "skins":
		return "skin"
	case "sceneries":
		return "scenery"
	case "capital_house_parts":
		return "housepart"
	}
	return "other"
}

// requiresFor names the gate (or gates) that must be cleared to reach the
// level this srcLevel describes. Two rules matter here: on Builder Base,
// required_townhall actually holds the Builder Hall level, and Builder Base
// troops gate on the Star Laboratory only - their required_townhall column
// runs on a scale the export never reaches and would cap them far too low.
func requiresFor(category, village string, lv srcLevel) map[string]int {
	req := map[string]int{}
	add := func(gate string, v *int) {
		if v != nil {
			req[gate] = *v
		}
	}
	switch category {
	case "buildings", "traps":
		gate := "th"
		if village == "builderBase" {
			gate = "bh"
		}
		add(gate, lv.RequiredTownhall)
	case "troops", "spells":
		gate := "lab"
		if village == "builderBase" {
			gate = "starlab"
		}
		add(gate, lv.RequiredLabLevel)
	case "heroes":
		if village == "builderBase" {
			add("bh", lv.RequiredTownhall)
		} else {
			add("th", lv.RequiredTownhall)
			add("herohall", lv.RequiredHeroTavernLevel)
		}
	case "equipment":
		add("th", lv.RequiredTownhall)
		add("smith", lv.RequiredBlacksmithLevel)
	case "pets":
		add("th", lv.RequiredTownhall)
		add("pethouse", lv.RequiredPetHouseLevel)
	case "helpers", "guardians":
		add("th", lv.RequiredTownhall)
	}
	if len(req) == 0 {
		return nil
	}
	return req
}

// costFor reads whichever cost field this category actually uses. Equipment
// is the one case with no single currency: it spends a triple of ores.
func costFor(category string, lv srcLevel) (int64, Ore) {
	if category == "equipment" {
		if len(lv.UpgradeCost) == 0 {
			return 0, Ore{}
		}
		var raw struct {
			Shiny  int64 `json:"shiny_ore"`
			Glowy  int64 `json:"glowy_ore"`
			Starry int64 `json:"starry_ore"`
		}
		_ = json.Unmarshal(lv.UpgradeCost, &raw)
		return 0, Ore{Shiny: raw.Shiny, Glowy: raw.Glowy, Starry: raw.Starry}
	}
	if lv.BuildCost > 0 {
		return int64(lv.BuildCost), Ore{}
	}
	if len(lv.UpgradeCost) > 0 {
		var n int64
		if err := json.Unmarshal(lv.UpgradeCost, &n); err == nil {
			return n, Ore{}
		}
	}
	return 0, Ore{}
}

func secondsFor(lv srcLevel) int64 {
	if lv.BuildTime > 0 {
		return int64(lv.BuildTime)
	}
	return int64(lv.UpgradeTime)
}

func unlocksFor(lv srcLevel) []Unlock {
	if len(lv.Unlocks) == 0 {
		return nil
	}
	out := make([]Unlock, 0, len(lv.Unlocks))
	for _, u := range lv.Unlocks {
		out = append(out, Unlock{ID: u.ID, Name: u.Name, Quantity: u.Quantity})
	}
	return out
}

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
