package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func ip(n int) *int { return &n }

func TestRequiresForHomeBuildingUsesTownHall(t *testing.T) {
	got := requiresFor("buildings", "home", srcLevel{RequiredTownhall: ip(7)})
	want := map[string]int{"th": 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// On Builder Base, required_townhall actually holds the Builder Hall level -
// the source data reuses the column rather than adding a new one.
func TestRequiresForBuilderBaseBuildingUsesBuilderHallNotTownHall(t *testing.T) {
	got := requiresFor("buildings", "builderBase", srcLevel{RequiredTownhall: ip(4)})
	want := map[string]int{"bh": 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The one rule this whole converter hinges on: Builder Base troops carry a
// required_townhall column that runs 1-12, on a scale the export never
// reaches (Builder Hall tops out at 10). Honouring it would cap every
// Builder Base troop far below what it should reach - only the Star
// Laboratory actually gates them.
func TestRequiresForBuilderBaseTroopIgnoresRequiredTownhall(t *testing.T) {
	got := requiresFor("troops", "builderBase", srcLevel{RequiredTownhall: ip(12), RequiredLabLevel: ip(3)})
	want := map[string]int{"starlab": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v (required_townhall must never leak into a troop's gates)", got, want)
	}
}

func TestRequiresForHomeTroopUsesLabNotTownHall(t *testing.T) {
	got := requiresFor("troops", "home", srcLevel{RequiredTownhall: ip(5), RequiredLabLevel: ip(2)})
	want := map[string]int{"lab": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRequiresForHomeHeroUsesTownHallAndHeroHall(t *testing.T) {
	got := requiresFor("heroes", "home", srcLevel{RequiredTownhall: ip(9), RequiredHeroTavernLevel: ip(3)})
	want := map[string]int{"th": 9, "herohall": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRequiresForBuilderBaseHeroUsesBuilderHallOnly(t *testing.T) {
	got := requiresFor("heroes", "builderBase", srcLevel{RequiredTownhall: ip(5)})
	want := map[string]int{"bh": 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRequiresForEquipmentUsesTownHallAndBlacksmith(t *testing.T) {
	got := requiresFor("equipment", "", srcLevel{RequiredTownhall: ip(8), RequiredBlacksmithLevel: ip(1)})
	want := map[string]int{"th": 8, "smith": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRequiresForPetUsesTownHallAndPetHouse(t *testing.T) {
	got := requiresFor("pets", "", srcLevel{RequiredTownhall: ip(14), RequiredPetHouseLevel: ip(1)})
	want := map[string]int{"th": 14, "pethouse": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Every Builder Hall level carries a null required_townhall - nothing
// external gates it. Reachable() must read an absent key as "always open",
// not "requires level 0", or Builder Hall would never be reachable at all.
func TestRequiresForUngatedLevelReturnsNil(t *testing.T) {
	if got := requiresFor("buildings", "builderBase", srcLevel{RequiredTownhall: nil}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestCostForEquipmentReadsOreTripleNotACurrency(t *testing.T) {
	lvl := srcLevel{UpgradeCost: json.RawMessage(`{"shiny_ore":120,"glowy_ore":5,"starry_ore":0}`)}
	cost, ore := costFor("equipment", lvl)
	if cost != 0 {
		t.Errorf("equipment cost = %d, want 0 (it spends ore, not a single currency)", cost)
	}
	if ore.Shiny != 120 || ore.Glowy != 5 || ore.Starry != 0 {
		t.Errorf("ore = %+v, want {120 5 0}", ore)
	}
}

func TestCostForBuildingReadsBuildCost(t *testing.T) {
	cost, ore := costFor("buildings", srcLevel{BuildCost: 250})
	if cost != 250 || ore.Shiny != 0 || ore.Glowy != 0 || ore.Starry != 0 {
		t.Errorf("cost = %d ore = %+v, want 250 and zero ore", cost, ore)
	}
}

func TestCostForTroopReadsUpgradeCostAsPlainNumber(t *testing.T) {
	cost, ore := costFor("troops", srcLevel{UpgradeCost: json.RawMessage(`10000`)})
	if cost != 10000 || ore.Shiny != 0 || ore.Glowy != 0 || ore.Starry != 0 {
		t.Errorf("cost = %d ore = %+v, want 10000 and zero ore", cost, ore)
	}
}

func TestSlugifyKeepsStraightApostropheDropsCurlyOne(t *testing.T) {
	if got := slugify("Builder's Apprentice"); got != "builder's_apprentice" {
		t.Errorf("slugify = %q, want %q", got, "builder's_apprentice")
	}
	if got := slugify("B.O.T.O’s Shack"); got != "botos_shack" {
		t.Errorf("slugify = %q, want %q", got, "botos_shack")
	}
}

func TestIconIndexMatchesTieredBuildingArtAndSingleHeroIcon(t *testing.T) {
	idx := buildIconIndex([]manifestAsset{
		{Path: "buildings/home-village/cannon/level_1.webp", Category: "buildings"},
		{Path: "buildings/home-village/cannon/level_21.webp", Category: "buildings"},
		{Path: "heroes/barbarian_king/icon.webp", Category: "heroes"},
	})

	path, max := lookupIcon(idx, "buildings", "home", "Cannon")
	if path != "buildings/home-village/cannon/level_{level}.webp" || max != 21 {
		t.Errorf("got (%q, %d), want (buildings/home-village/cannon/level_{level}.webp, 21)", path, max)
	}

	if path, _ := lookupIcon(idx, "heroes", "home", "Barbarian King"); path != "heroes/barbarian_king/icon.webp" {
		t.Errorf("hero icon = %q, want heroes/barbarian_king/icon.webp", path)
	}

	if path, _ := lookupIcon(idx, "heroes", "home", "Nobody"); path != "" {
		t.Errorf("expected no icon for an unmatched name, got %q", path)
	}
}

func TestKindForFoldsGuardiansIntoBuildingDefense(t *testing.T) {
	kind, class := kindFor("guardians", srcEntity{})
	if kind != "building" || class != "Defense" {
		t.Errorf("guardian kind/class = %q/%q, want building/Defense", kind, class)
	}
}
