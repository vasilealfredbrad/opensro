/*
===========================================================================

lootset_test.go - full sets and the stage-9 random loot

Port-only, not native (stage 8 item 4, stage 9 items 1 and 4). Every
build and gender can still find every slot of its set at every degree
1-4 (shoulders included) inside the level window; the loot is the same
for every killer of a level (no slot bias, no pity); it spans every
build, race and gender; about 1 % of the equipment draws are skins;
SetState and Complete read a set as the docs define it.

===========================================================================
*/
package battleroyale

import (
	"math/rand/v2"
	"slices"
	"testing"
)

/*
================
buildWindow

The killer's own rows per set slot inside its level window (both ring
slots see the ring rows): what its set can take at that level.
================
*/
func buildWindow(killer Killer) [SetSlotCount][]equipmentRow {
	var out [SetSlotCount][]equipmentRow
	for _, row := range equipmentRows(killer) {
		if !InLootWindow(killer.Level, row.level) {
			continue
		}
		out[row.slot] = append(out[row.slot], row)
		if row.slot == SlotRing {
			out[SlotRingSecond] = append(out[SlotRingSecond], row)
		}
	}
	return out
}

/*
================
TestEverySlotDropsEveryDegree

The loot of a level is the same for every killer (TestLootIgnoresTheKiller),
so it is sampled once per level 1-30: every plan's equipment is a row of
the random loot inside the window, and for every build and gender every
window row of its own drops at its level; over the levels every slot of
the build (all six armor sockets with the shoulders, the weapon kinds,
the shield of the shield builds, earring, necklace, ring) drops at every
degree 1-4.
================
*/
func TestEverySlotDropsEveryDegree(t *testing.T) {
	rng := rand.New(rand.NewPCG(8, 1))
	var dropped [MatchLevelCap + 1]map[string]bool
	for level := int64(1); level <= MatchLevelCap; level++ {
		dropped[level] = map[string]bool{}
		killer := Killer{Build: DefaultPreset(), Level: level}
		for range 3000 {
			for _, drop := range LootPlan(killer, rng) {
				if _, equipment := RequiredLevel(drop.Codename); equipment {
					checkDrop(t, killer, drop)
					base, _ := lootBaseOf(drop.Codename)
					dropped[level][base] = true
				}
			}
		}
	}
	for _, build := range Builds() {
		for _, female := range []bool{false, true} {
			var seen [SetSlotCount][maxDegree + 1]bool
			for level := int64(1); level <= MatchLevelCap; level++ {
				killer := Killer{Build: build, Level: level, Female: female}
				for slot, rows := range buildWindow(killer) {
					for _, row := range rows {
						if !dropped[level][row.base] {
							t.Fatalf("build %d female %v level %d: window row %s never dropped", build.ID, female, level, row.base)
						}
						seen[slot][row.degree] = true
					}
				}
			}
			for _, slot := range BuildSlots(build) {
				for degree := 1; degree <= maxDegree; degree++ {
					if !seen[slot][degree] {
						t.Fatalf("build %d female %v: %s degree %d never dropped", build.ID, female, slot.Name(), degree)
					}
				}
			}
			if seen[SlotShield][1] != build.Shield {
				t.Fatalf("build %d: usable shield drops %v", build.ID, seen[SlotShield][1])
			}
		}
	}
}

/*
================
TestLootIgnoresTheKiller

Two killers of the same level and different builds, races and genders
draw the same plan from the same stream: nothing of the killer but its
level shapes the loot (no slot bias, no pity).
================
*/
func TestLootIgnoresTheKiller(t *testing.T) {
	blade, _ := BuildByID(1)
	wizard, _ := BuildByID(15)
	for level := int64(1); level <= MatchLevelCap; level++ {
		for seed := uint64(1); seed <= 20; seed++ {
			a := LootPlan(Killer{Build: blade, Level: level}, rand.New(rand.NewPCG(seed, uint64(level))))
			b := LootPlan(Killer{Build: wizard, Level: level, Female: true}, rand.New(rand.NewPCG(seed, uint64(level))))
			if !slices.Equal(a, b) {
				t.Fatalf("level %d seed %d: %+v vs %+v", level, seed, a, b)
			}
			sa := SupplyDropLoot(Killer{Build: blade, Level: level}, rand.New(rand.NewPCG(seed, uint64(level))))
			sb := SupplyDropLoot(Killer{Build: wizard, Level: level, Female: true}, rand.New(rand.NewPCG(seed, uint64(level))))
			if !slices.Equal(sa, sb) {
				t.Fatalf("supply level %d seed %d: %+v vs %+v", level, seed, sa, sb)
			}
		}
	}
}

/*
================
TestRandomLootSpansEveryBuild

At level 18 the equipment drops cover every row of the random loot's
window (47 rows: the eleven weapon kinds of degree 3, both shields, the
hands, shoulders and feet of the ten armor bodies, both races' earrings
and rings; 356 rows over every level); a Blade-Fire male can wear only
a minority of them; about 1 % of the equipment draws are skins, of every
skin row, any gender.
================
*/
func TestRandomLootSpansEveryBuild(t *testing.T) {
	rng := rand.New(rand.NewPCG(19, 2))
	blade, _ := BuildByID(1)
	killer := Killer{Build: blade, Level: 18}
	window := map[string]bool{}
	for _, family := range []lootEntry{lootWeapon, lootArmor, lootAccessory, lootShield} {
		for _, row := range randomLoot().windowRows(18, family, false, LootTuning{}) {
			window[row.base] = true
		}
	}
	got, skins := map[string]bool{}, map[string]bool{}
	equipment, usable, skinDrops := 0, 0, 0
	const kills = 40000
	for range kills {
		for _, drop := range LootPlan(killer, rng) {
			if IsSkin(drop.Codename) {
				skinDrops++
				equipment++
				skins[drop.Codename] = true
				continue
			}
			if _, ok := RequiredLevel(drop.Codename); !ok {
				continue
			}
			checkDrop(t, killer, drop)
			equipment++
			base, _ := lootBaseOf(drop.Codename)
			got[base] = true
			if _, _, ok := SlotOf(killer, drop.Codename); ok {
				usable++
			}
		}
	}
	for base := range window {
		if !got[base] {
			t.Fatalf("window row %s never dropped", base)
		}
	}
	if len(window) != 47 {
		t.Fatalf("%d window rows at level 18, want 47", len(window))
	}
	if share := float64(usable) / float64(equipment); share < 0.10 || share > 0.40 {
		t.Fatalf("a Blade-Fire male can wear %.2f of the drops", share)
	}
	if share := float64(skinDrops) / float64(equipment); share < 0.007 || share > 0.013 {
		t.Fatalf("skins %.4f of the equipment draws, want about 0.01", share)
	}
	if len(skins) != len(SkinCodenames()) {
		t.Fatalf("%d of %d skins dropped", len(skins), len(SkinCodenames()))
	}
	if LootRowCount() != 356 || !IsLootRow("ITEM_EU_W_CLOTHES_03_SA_C_RARE") || !IsLootRow("ITEM_CH_W_LIGHT_02_HA_B_RARE") ||
		IsLootRow("ITEM_CH_W_CLOTHES_02_HA_B_RARE") || IsLootRow("ITEM_EU_AXE_02_B_RARE") {
		t.Fatalf("%d loot rows", LootRowCount())
	}
}

/*
================
TestLiveLootTuning

The live table (owner choice 2026-10-18): five equipment-only stacks
after the table's three or four, so 8.5 stacks a kill at level 18 with
at least five of them equipment; at level 30 the window opens to every
top-degree row (required 24 or more, rows [27, 30] alone would leave out
24-26), at level 29 it stays [26, 29]; the supply drop's weapon at level
30 comes from the top degree too, where the window holds no weapon row.
================
*/
func TestLiveLootTuning(t *testing.T) {
	if tuning := LiveLootTuning(); tuning != (LootTuning{ExtraEquipment: 5, TopDegreeAtCap: true}) {
		t.Fatalf("live tuning %+v", tuning)
	}
	rng := rand.New(rand.NewPCG(23, 5))
	blade, _ := BuildByID(1)
	const kills = 4000
	stacks := 0
	for range kills {
		drops := LootPlan(Killer{Build: blade, Level: 18}, rng)
		stacks += len(drops)
		equipment := 0
		for _, drop := range drops {
			if _, ok := RequiredLevel(drop.Codename); ok || IsSkin(drop.Codename) {
				equipment++
			}
		}
		if equipment < LiveLootTuning().ExtraEquipment {
			t.Fatalf("%d equipment stacks in %+v", equipment, drops)
		}
	}
	if stacks < kills*84/10 || stacks > kills*86/10 {
		t.Fatalf("%d stacks in %d kills", stacks, kills)
	}
	for _, level := range []int64{MatchLevelCap - 1, MatchLevelCap} {
		least := int64(1 << 30)
		for range 1000 {
			for _, drop := range LootPlan(Killer{Build: blade, Level: level}, rng) {
				if required, ok := RequiredLevel(drop.Codename); ok {
					least = min(least, required)
				}
			}
		}
		want := level - LootWindow
		if level == MatchLevelCap {
			want = 8 * (maxDegree - 1)
		}
		if least != want {
			t.Fatalf("level %d: lowest required level %d, want %d", level, least, want)
		}
	}
	topWeapons := 0
	for range 200 {
		drops := SupplyDropLoot(Killer{Build: blade, Level: MatchLevelCap}, rng)
		if required, ok := RequiredLevel(drops[0].Codename); ok && required == 8*(maxDegree-1) {
			topWeapons++
		}
	}
	if topWeapons == 0 {
		t.Fatal("no top-degree supply weapon at the level cap")
	}
}

/*
================
TestSkinCodenames

The skins are 26 distinct item-mall avatars: hats, dresses and the
attachment of both genders.
================
*/
func TestSkinCodenames(t *testing.T) {
	skins := SkinCodenames()
	seen := map[string]bool{}
	for _, skin := range skins {
		if seen[skin] || !IsSkin(skin) {
			t.Fatalf("skin %s", skin)
		}
		seen[skin] = true
	}
	if len(skins) != 26 || !seen["ITEM_MALL_AVATAR_W_HALLOWEEN_ATTACH"] || !seen["ITEM_MALL_AVATAR_M_KNIGHT_HAT"] || IsSkin("ITEM_CH_BLADE_01_C_RARE") {
		t.Fatalf("%d skins", len(skins))
	}
}

/*
================
TestSlotOfArmorParts

The seven armor rows map onto the six sockets: HA and CA the head, SA
the shoulders; another class, gender or race is no slot.
================
*/
func TestSlotOfArmorParts(t *testing.T) {
	knight, _ := BuildByID(12)
	killer := Killer{Build: knight, Female: true}
	for codename, want := range map[string]SetSlot{
		"ITEM_EU_W_HEAVY_02_HA_B_RARE": SlotHead, "ITEM_EU_W_HEAVY_02_CA_C_RARE": SlotHead,
		"ITEM_EU_W_HEAVY_03_SA_B_RARE": SlotShoulders, "ITEM_EU_W_HEAVY_03_BA_B_RARE": SlotChest,
		"ITEM_EU_W_HEAVY_03_LA_B_RARE": SlotLegs, "ITEM_EU_W_HEAVY_03_AA_B_RARE": SlotHands,
		"ITEM_EU_W_HEAVY_03_FA_B_RARE": SlotFeet, "ITEM_EU_SWORD_04_C_RARE": SlotWeapon,
		"ITEM_EU_SHIELD_01_C_RARE": SlotShield, "ITEM_EU_RING_02_B_RARE": SlotRing,
	} {
		if got, _, ok := SlotOf(killer, codename); !ok || got != want {
			t.Fatalf("%s: slot %s (%v), want %s", codename, got.Name(), ok, want.Name())
		}
	}
	for _, codename := range []string{"ITEM_EU_M_HEAVY_02_SA_B_RARE", "ITEM_EU_W_LIGHT_02_SA_B_RARE", "ITEM_CH_W_HEAVY_02_SA_B_RARE"} {
		if _, _, ok := SlotOf(killer, codename); ok {
			t.Fatalf("%s is a slot of a female Knight", codename)
		}
	}
}

/*
================
TestSetStateAndComplete

Rings fill both ring slots best first; a set is complete at level 30
when every slot holds degree 4, and at level 12 when every slot holds
the newest degree its rows offer by then.
================
*/
func TestSetStateAndComplete(t *testing.T) {
	blade, _ := BuildByID(1)
	killer := Killer{Build: blade, Level: 30}
	var s SetState
	for _, codename := range []string{"ITEM_CH_RING_02_B_RARE", "ITEM_CH_RING_04_C_RARE", "ITEM_CH_RING_03_B_RARE", "ITEM_EU_RING_04_C_RARE"} {
		s.Take(killer, codename)
	}
	if s.Degree[SlotRing] != 4 || s.Degree[SlotRingSecond] != 3 {
		t.Fatalf("rings %d/%d", s.Degree[SlotRing], s.Degree[SlotRingSecond])
	}
	var full SetState
	for _, slot := range BuildSlots(blade) {
		full.Degree[slot] = maxDegree
	}
	if !full.Complete(blade, 30) {
		t.Fatal("a degree-4 set is not complete at 30")
	}
	full.Degree[SlotShoulders] = 3
	if full.Complete(blade, 30) || !full.SlotCurrent(SlotHead, 30) {
		t.Fatal("a degree-3 shoulder completes a level-30 set")
	}
	// Level 12: shoulders (9), hands (8), feet (10), head (11), legs (12)
	// at degree 2; chest (13) and necklace (12) and earring (10).
	var mid SetState
	for _, slot := range BuildSlots(blade) {
		mid.Degree[slot] = 2
	}
	mid.Degree[SlotChest] = 1
	if !mid.Complete(blade, 12) {
		t.Fatal("a level-12 set with a degree-1 chest (degree 2 needs 13) is not complete")
	}
	mid.Degree[SlotShoulders] = 1
	if mid.Complete(blade, 12) {
		t.Fatal("a degree-1 shoulder completes a level-12 set")
	}
}
