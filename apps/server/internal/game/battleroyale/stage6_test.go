/*
===========================================================================

stage6_test.go - the stage-6 loot window, potion tiers and zone supply

Port-only, not native (docs/battle-royale.md "Server notes (stage 6:
economy)"). The rules a kill's loot and the zone supply follow, checked
row by row; the whole-match proof is economy_sim_test.go.

===========================================================================
*/
package battleroyale

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

/*
================
checkDrop

Fails t unless drop is a stage-6 drop for killer: a consumable of the
killer's tier, a skin (stage 9), or a Moon/Sun row of the random loot
(any build's, stage 9) whose required level is in the killer's window.
Reports whether it was Moon/Sun equipment and its seal.
================
*/
func checkDrop(t *testing.T, killer Killer, drop Drop) (bool, Seal) {
	t.Helper()
	tier, pill := PotionTier(killer.Level), DegreeForLevel(killer.Level)
	consumables := map[string]bool{}
	for _, family := range []string{"HP_POTION", "MP_POTION", "ALL_POTION"} {
		consumables["ITEM_ETC_"+family+"_0"+string(rune('0'+tier))] = true
	}
	consumables["ITEM_ETC_CURE_ALL_0"+string(rune('0'+pill))] = true
	if consumables[drop.Codename] {
		if drop.Count < 1 || drop.Count > supplyPotions {
			t.Fatalf("level %d: consumable stack %+v", killer.Level, drop)
		}
		return false, -1
	}
	if IsSkin(drop.Codename) {
		if drop.Count != 1 {
			t.Fatalf("skin stack %+v", drop)
		}
		return false, -1
	}
	required, ok := RequiredLevel(drop.Codename)
	if !ok || drop.Count != 1 || !InLootWindow(killer.Level, required) && !liveTopDegree(killer.Level, required) {
		t.Fatalf("level %d build %d: drop %+v (required %d, %v)", killer.Level, killer.Build.ID, drop, required, ok)
	}
	_, seal := lootBase(drop.Codename)
	if !IsLootRow(drop.Codename) {
		t.Fatalf("level %d: %s is not a row of the random loot", killer.Level, drop.Codename)
	}
	return true, seal
}

/*
================
liveTopDegree

Whether a row requiring required is a top-degree row the live tuning
(LiveLootTuning.TopDegreeAtCap) gives a killer of level at the cap
outside its window: degree 4 requires 24 or more.
================
*/
func liveTopDegree(level, required int64) bool {
	const topDegreeLevel = 8 * (maxDegree - 1)
	return LiveLootTuning().TopDegreeAtCap && level >= MatchLevelCap && required >= topDegreeLevel
}

/*
================
TestRequiredLevel

The per-row required levels of the Moon/Sun rows as itemdata holds them
(the licensed test checks every row): weapons, shields and rings 1/8/16/
24, the armor parts offset 0-5 over the degree, earrings +2, necklaces
+4; nothing for a consumable, a degree-5 row or a non-seal row.
================
*/
func TestRequiredLevel(t *testing.T) {
	for codename, want := range map[string]int64{
		"ITEM_CH_BLADE_01_B_RARE": 1, "ITEM_CH_BLADE_02_C_RARE": 8, "ITEM_EU_TSTAFF_04_B_RARE": 24,
		"ITEM_CH_M_HEAVY_02_AA_B_RARE": 8, "ITEM_CH_M_HEAVY_02_SA_B_RARE": 9, "ITEM_CH_W_CLOTHES_02_FA_C_RARE": 10,
		"ITEM_EU_M_LIGHT_03_HA_B_RARE": 19, "ITEM_EU_W_CLOTHES_03_CA_C_RARE": 19, "ITEM_CH_M_LIGHT_04_LA_B_RARE": 28,
		"ITEM_EU_M_HEAVY_04_BA_C_RARE": 29, "ITEM_EU_M_HEAVY_01_BA_C_RARE": 1, "ITEM_CH_EARRING_03_B_RARE": 18,
		"ITEM_EU_NECKLACE_02_C_RARE": 12, "ITEM_CH_RING_04_B_RARE": 24, "ITEM_EU_SHIELD_03_C_RARE": 16,
	} {
		if got, ok := RequiredLevel(codename); !ok || got != want {
			t.Fatalf("%s required %d (%v), want %d", codename, got, ok, want)
		}
	}
	for _, codename := range []string{"ITEM_ETC_HP_POTION_01", "ITEM_CH_BLADE_05_C_RARE", "ITEM_CH_BLADE_02_A", "ITEM_CH_BLADE_02_A_RARE"} {
		if _, ok := RequiredLevel(codename); ok {
			t.Fatalf("%s has a required level", codename)
		}
	}
}

/*
================
TestPotionTiers

PotionTier is the largest heal tier (itemdata 120/220/370/570/820) that
restores at most 30 % of the match max HP of the level; pills follow the
weapon degree bands.
================
*/
func TestPotionTiers(t *testing.T) {
	heals := []int64{120, 220, 370, 570, 820}
	for level := int64(1); level <= MatchLevelCap; level++ {
		want := 1
		for i, heal := range heals {
			if heal*10 <= BotMaxHP(int(level))*3 {
				want = i + 1
			}
		}
		if got := PotionTier(level); got != want {
			t.Fatalf("level %d potion tier %d, want %d", level, got, want)
		}
	}
	for _, tc := range []struct {
		level  int64
		degree int
	}{{1, 1}, {7, 1}, {8, 2}, {15, 2}, {16, 3}, {23, 3}, {24, 4}, {30, 4}} {
		if got := DegreeForLevel(tc.level); got != tc.degree {
			t.Fatalf("level %d degree %d, want %d", tc.level, got, tc.degree)
		}
	}
}

/*
================
TestLootPlanWindow

Every build at every level 1-30, both genders, under the live tuning:
three or four stacks of the table a kill and five equipment-only stacks
more (fewer only at levels 5-7, whose window holds no row and whose
extra draws give only the odd skin), every equipment row of the random
loot and inside [level-3, level] (at level 30 any top-degree row),
every consumable of the level's tier; levels 5-7 drop no Moon/Sun
equipment, every other level does. The stage-9 table (the zero tuning)
at level 18: 3.5 stacks a kill, about 45 % equipment (skins included),
Sun about a third of the Moon/Sun rows, shields among them.
================
*/
func TestLootPlanWindow(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	extra := LiveLootTuning().ExtraEquipment
	for _, build := range Builds() {
		for level := int64(1); level <= MatchLevelCap; level++ {
			equipment := 0
			noWindow := level >= 5 && level <= 7
			for i := range 120 {
				killer := Killer{Build: build, Level: level, Female: i%2 == 1}
				drops := LootPlan(killer, rng)
				least := lootStacks + extra
				if noWindow {
					least = lootStacks
				}
				if len(drops) < least || len(drops) > lootStacks+1+extra {
					t.Fatalf("level %d: %d stacks", level, len(drops))
				}
				for _, drop := range drops {
					if isEquipment, _ := checkDrop(t, killer, drop); isEquipment {
						equipment++
					}
				}
			}
			if noWindow != (equipment == 0) {
				t.Fatalf("build %d level %d: %d equipment drops", build.ID, level, equipment)
			}
		}
	}
	blade, _ := BuildByID(1)
	killer := Killer{Build: blade, Level: 18}
	const kills = 6000
	stacks, moon, sun, shields, skins := 0, 0, 0, 0, 0
	for range kills {
		drops := LootPlanTuned(killer, LootTuning{}, rng)
		stacks += len(drops)
		for _, drop := range drops {
			isEquipment, seal := checkDrop(t, killer, drop)
			switch {
			case IsSkin(drop.Codename):
				skins++
			case !isEquipment:
			case seal == SealMoon:
				moon++
			default:
				sun++
			}
			if strings.Contains(drop.Codename, "SHIELD") {
				shields++
			}
		}
	}
	if stacks < kills*34/10 || stacks > kills*36/10 {
		t.Fatalf("%d stacks in %d kills", stacks, kills)
	}
	if equipped := moon + sun + skins; equipped < stacks*42/100 || equipped > stacks*48/100 || shields == 0 {
		t.Fatalf("%d equipment stacks (%d shields, %d skins) of %d", equipped, shields, skins, stacks)
	}
	if share := float64(sun) / float64(moon+sun); share < 0.30 || share > 0.37 {
		t.Fatalf("Sun share %.3f, want about 1/3", share)
	}
	// Stage 9: a female Wizard also finds shields, male rows, other
	// classes' armor and the other race's rows.
	wizard, _ := BuildByID(15)
	foreign := map[string]bool{}
	for range 500 {
		for _, drop := range LootPlan(Killer{Build: wizard, Level: 26, Female: true}, rng) {
			for _, word := range []string{"SHIELD", "_M_", "HEAVY", "LIGHT", "ITEM_CH_"} {
				if strings.Contains(drop.Codename, word) {
					foreign[word] = true
				}
			}
		}
	}
	if len(foreign) != 5 {
		t.Fatalf("a female Wizard found only %v", foreign)
	}
}

/*
================
TestSupplyLoot

The supply drop: a random weapon of the window (any window equipment
when no weapon row is in it), five HP and MP and three vigor potions of
the tier, then window armor or accessories: 3-6 stacks, every one legal
(levels 5-7 hold no window row: potions only, unless a skin drops).
================
*/
func TestSupplyLoot(t *testing.T) {
	blade, _ := BuildByID(1)
	rng := rand.New(rand.NewPCG(7, 8))
	for level := int64(1); level <= MatchLevelCap; level++ {
		killer := Killer{Build: blade, Level: level}
		for range 40 {
			drops := SupplyDropLoot(killer, rng)
			if len(drops) < 3 || len(drops) > 6 {
				t.Fatalf("level %d supply drops %+v", level, drops)
			}
			for _, drop := range drops {
				checkDrop(t, killer, drop)
			}
		}
	}
	weapons := 0
	for range 200 {
		drops := SupplyDropLoot(Killer{Build: blade, Level: 9}, rng)
		if IsSkin(drops[0].Codename) {
			continue
		}
		if !strings.Contains(drops[0].Codename, "_02_") || strings.Contains(drops[0].Codename, "SHIELD") ||
			strings.Contains(drops[0].Codename, "_M_") || strings.Contains(drops[0].Codename, "_W_") {
			t.Fatalf("level-9 supply weapon %+v", drops)
		}
		weapons++
	}
	if weapons < 190 {
		t.Fatalf("%d level-9 supply weapons", weapons)
	}
}

/*
================
TestZoneSupply

The supply's target is the larger of the per-participant stock and the
area density, capped by the crowding bound and the live cap; one pass
never exceeds its batch or the live cap; its plan stands match wolves on
its ground; RetireOutside spares the margin; the supply elite is a giant
of the same wolf.
================
*/
func TestZoneSupply(t *testing.T) {
	if got := SupplyTarget(Supply{Participants: 36, GroundM2: 6e6}); got != 2000 {
		t.Fatalf("whole-Roc target %d", got)
	}
	if got := SupplyTarget(Supply{Participants: 36, GroundM2: 7e5}); got != 36*ZoneWolvesPerParticipant {
		t.Fatalf("phase-4 target %d", got)
	}
	if got := SupplyTarget(Supply{Participants: 36, GroundM2: 1e5}); got != 250 {
		t.Fatalf("small-circle target %d", got)
	}
	if got := SupplyTarget(Supply{Participants: 36, GroundM2: 1e8}); got != MaxLiveMonsters {
		t.Fatalf("capped target %d", got)
	}
	if got := SupplyCount(Supply{Participants: 36, Inside: 100, GroundM2: 4e6}); got != maxSupplyBatch {
		t.Fatalf("batch %d", got)
	}
	if got := SupplyCount(Supply{Participants: 36, Inside: 100, Live: MaxLiveMonsters - 25, GroundM2: 4e6}); got != 25 {
		t.Fatalf("near the live cap %d", got)
	}
	if SupplyCount(Supply{Participants: 36, Inside: 3000, GroundM2: 4e6}) != 0 ||
		SupplyCount(Supply{Participants: 36, Live: MaxLiveMonsters + 5, GroundM2: 4e6}) != 0 {
		t.Fatal("supply past the stock or the cap")
	}
	mask := BuildGroundMask(nil)
	target := Circle{Center: ArenaCircle().Center, Radius: 6000}
	ground := mask.Within(target)
	plan := SupplyPlan(ground, Supply{Participants: 3, GroundM2: ground.Area()}, rand.New(rand.NewPCG(5, 6)))
	if len(plan) != max(3*ZoneWolvesPerParticipant, int(ground.Area()/ZoneM2PerWolf)) {
		t.Fatalf("%d supply wolves on %.0f m2", len(plan), ground.Area())
	}
	for _, spawn := range plan {
		if !mask.Contains(spawn.At) || Outside(Circle{Center: target.Center, Radius: target.Radius + GroundMaskStep}, spawn.At) ||
			spawn.Codename != MatchMonsterCodename || spawn.Rarity != 0 {
			t.Fatalf("supply spawn %+v", spawn)
		}
	}
	edge := Point{X: target.Center.X + target.Radius + retireMarginM*UnitsPerMeter - 1, Z: target.Center.Z}
	if RetireOutside(target, edge) || !RetireOutside(target, Point{X: edge.X + 2, Z: edge.Z}) {
		t.Fatal("retire margin")
	}
	elite := SupplyElite(target)
	if elite.At != target.Center || elite.Rarity != supplyEliteRarity || elite.Codename != "MOB_EU_BARUSWOLF_CLON" || supplyEliteRarity != 4 {
		t.Fatalf("elite %+v", elite)
	}
	if !IsSupplyElite(elite.Codename, elite.Rarity) || IsSupplyElite(elite.Codename, 0) || IsSupplyElite("MOB_AM_WOLF", supplyEliteRarity) {
		t.Fatal("supply elite identity")
	}
}

/*
================
TestLocalSupplyPlan

Around a participant inside the next circle the local wolves stand in
the ring between localSupplyMinM and LocalSupplyRadiusM and inside the
next circle; around one outside it, nearer the next circle's centre than
the participant; nothing for one outside the current circle or one
already stocked.
================
*/
func TestLocalSupplyPlan(t *testing.T) {
	mask := BuildGroundMask(nil)
	centre := ArenaCircle().Center
	zone := ZoneState{Current: Circle{Center: centre, Radius: 10000}, Target: Circle{Center: centre, Radius: 5000}}
	rng := rand.New(rand.NewPCG(9, 10))
	for _, at := range []Point{{X: centre.X + 1000, Z: centre.Z}, {X: centre.X + 7000, Z: centre.Z}} {
		reach := Circle{Center: at, Radius: LocalSupplyRadiusM * UnitsPerMeter}
		plan := LocalSupplyPlan(mask.Within(reach), at, zone, 2, rng)
		if len(plan) != LocalWolves-2 {
			t.Fatalf("%d local wolves around %+v", len(plan), at)
		}
		for _, spawn := range plan {
			d := math.Hypot(spawn.At.X-at.X, spawn.At.Z-at.Z)
			if d < localSupplyMinM*UnitsPerMeter || d > LocalSupplyRadiusM*UnitsPerMeter || !Inward(zone, at, spawn.At) ||
				spawn.Codename != MatchMonsterCodename {
				t.Fatalf("local wolf %+v at %.0f units of %+v", spawn, d, at)
			}
		}
	}
	far := Point{X: centre.X + 12000, Z: centre.Z}
	if plan := LocalSupplyPlan(mask.Within(Circle{Center: far, Radius: 350}), far, zone, 0, rng); len(plan) != 0 {
		t.Fatal("local wolves outside the zone")
	}
	at := Point{X: centre.X + 1000, Z: centre.Z}
	if plan := LocalSupplyPlan(mask.Within(Circle{Center: at, Radius: 350}), at, zone, LocalWolves, rng); len(plan) != 0 {
		t.Fatal("local wolves for a stocked participant")
	}
}
