/*
===========================================================================

battleroyale_stage4_test.go - the stage-4 match loot through the wiring

Port-only, not native. A match kill drops only the match table (never
the monster's own drops), Moon and Sun equipment mixed with Sun the
rarer, of every build since stage 9, and every equipment drop carries
at least three blue options and white stats of 50 % or more; a skin
carries two or more options of its avatar pool and no whites; a pickup
hands the options to the client in the ordinary item body. The arena's
live monsters stay under the cap (docs/battle-royale.md, "Server notes
(stage 4)", "Stage 9").

===========================================================================
*/

package action

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
addStage4Items

The fixture kit rows plus every row the random loot drops at level 1
(stage 9: every build's degree-1 weapons, shields, armor and
accessories, both seals) and the skins, under their itemdata type ids
(battleRoyaleFixtureTypeIDs).
================
*/
func (fx *battleRoyaleFixture) addStage4Items() staticItemSource {
	items := fx.addKitItems()
	id := uint32(6000)
	add := func(codename string, tid [4]int64, fields enterworld.NativeFields) {
		if items[codename] != nil {
			return
		}
		id++
		items[codename] = &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: tid, NativeFields: fields,
			Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
	}
	for _, codename := range battleroyale.LootWindowCodenames(1) {
		add(codename, battleRoyaleFixtureTypeIDs(codename), "")
	}
	for _, skin := range battleroyale.SkinCodenames() {
		part := int64(2) // a dress
		switch {
		case strings.HasSuffix(skin, "_HAT"):
			part = 1
		case strings.HasSuffix(skin, "_ATTACH"):
			part = 3
		}
		add(skin, [4]int64{3, 1, 13, part}, enterworld.NativeFields("").With("maxMagicOptions51c", 4))
	}
	return items
}

/*
================
battleRoyaleFixtureTypeIDs

The itemdata type ids of a Moon/Sun codename: armor TID3 by race and
class (CH clothes 1, light 2, heavy 3; EU 9, 10, 11) with the part in
TID4, shields 4, accessories 5 (CH) or 12 (EU) with the word in TID4,
weapons 6.
================
*/
func battleRoyaleFixtureTypeIDs(codename string) [4]int64 {
	eu := strings.HasPrefix(codename, "ITEM_EU_")
	parts := map[string]int64{"HA": 1, "CA": 1, "SA": 2, "BA": 3, "LA": 4, "AA": 5, "FA": 6}
	for class, tid3 := range map[string]int64{"_CLOTHES_": 1, "_LIGHT_": 2, "_HEAVY_": 3} {
		if !strings.Contains(codename, class) {
			continue
		}
		if eu {
			tid3 += 8
		}
		words := strings.Split(codename, "_")
		return [4]int64{3, 1, tid3, parts[words[len(words)-3]]}
	}
	for word, tid4 := range map[string]int64{"_EARRING_": 1, "_NECKLACE_": 2, "_RING_": 3} {
		if strings.Contains(codename, word) {
			if eu {
				return [4]int64{3, 1, 12, tid4}
			}
			return [4]int64{3, 1, 5, tid4}
		}
	}
	if strings.Contains(codename, "_SHIELD_") {
		return [4]int64{3, 1, 4, 1}
	}
	return [4]int64{3, 1, 6, 2}
}

/*
================
seededDropRoll

A deterministic stand-in for the shard's 0..32767 drop roll.
================
*/
func seededDropRoll(seed uint64) func() (uint32, error) {
	r := rand.New(rand.NewPCG(seed, seed^0x5eed))
	return func() (uint32, error) { return uint32(r.IntN(32768)), nil }
}

/*
================
TestBattleRoyaleDropsCarryBluesAndHighWhites

A level-1 Blade-Fire killer's wolf kills drop only the match table
under the live tuning (three or four stacks and five equipment-only
ones): degree-1 potions and pills, Moon/Sun equipment of any build
(stage 9) or a skin, never a native drop. Every equipment drop has three or more
distinct blue options and every white-stat field at 16 of 31 (51.6 %)
or more; Sun is about a third of the equipment; other builds' rows drop
too. Every skin carries two to four distinct options of its avatar pool
(the smith's values) and no white field. A picked-up drop reaches the
bag and the client's item body with its options.
================
*/
func TestBattleRoyaleDropsCarryBluesAndHighWhites(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	items := fx.addStage4Items()
	fx.rt.DropRoll = seededDropRoll(11)
	a := fx.chars[0]
	fx.pick(t, a, 1)
	fx.startMatch(t)
	wolf := monster.Instance{Gid: 99, Ref: monster.MonsterRef{RefObjID: 5854, Codename: battleroyale.MatchMonsterCodename, Level: 5}}
	consumables := map[string]bool{"ITEM_ETC_HP_POTION_01": true, "ITEM_ETC_MP_POTION_01": true, "ITEM_ETC_ALL_POTION_01": true,
		"ITEM_ETC_CURE_ALL_01": true}
	moon, sun, foreign, skins := 0, 0, 0, 0
	var sample grounditem.Item
	for kill := 0; kill < 1500; kill++ {
		drops, match := fx.rt.battleRoyaleLoot(a, wolf, simulation.Spawn{RegionID: 0x5b6f, X: 1, Z: 1}, fx.clock.Now())
		// The table's three or four stacks and the live tuning's
		// equipment-only ones (battleroyale.LiveLootTuning).
		extra := battleroyale.LiveLootTuning().ExtraEquipment
		if !match || len(drops) < 3+extra || len(drops) > 4+extra {
			t.Fatalf("kill %d: %d drops (match %v)", kill, len(drops), match)
		}
		for _, drop := range drops {
			if consumables[drop.Codename] {
				continue
			}
			ref, known := items[drop.Codename]
			if battleroyale.IsSkin(drop.Codename) {
				assertBattleRoyaleSkin(t, ref, drop)
				skins++
				continue
			}
			if !known || !battleroyale.IsLootRow(drop.Codename) {
				t.Fatalf("drop %s is not match equipment", drop.Codename)
			}
			if !strings.HasPrefix(drop.Codename, "ITEM_CH_") || strings.Contains(drop.Codename, "_W_") || strings.Contains(drop.Codename, "LIGHT") {
				foreign++
			}
			switch {
			case strings.HasSuffix(drop.Codename, "_B_RARE"):
				moon++
			case strings.HasSuffix(drop.Codename, "_C_RARE"):
				sun++
			default:
				t.Fatalf("a non-seal equipment drop %s", drop.Codename)
			}
			assertBattleRoyaleGear(t, ref, drop.Codename, drop.VarianceBits, drop.Durability, drop.MagicOptions)
			if sample.Codename == "" && strings.Contains(drop.Codename, "BLADE") {
				sample = drop
			}
		}
	}
	if moon+sun < 1500 || foreign < (moon+sun)/2 || skins == 0 {
		t.Fatalf("%d equipment drops (%d of other builds), %d skins in 1500 kills", moon+sun, foreign, skins)
	}
	if share := float64(sun) / float64(moon+sun); share < 0.25 || share > 0.42 {
		t.Fatalf("Sun share %.2f (moon %d sun %d), want about a third", share, moon, sun)
	}
	if sample.Codename == "" {
		t.Fatal("no blade dropped")
	}
	at := fx.rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, fx.clock.NowMs())
	sample.Position = grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}
	laid := fx.rt.Ground.Add(testDivision, sample)
	fx.rt.grantPickup(testDivision, simulation.WorldKey(testDivision, a.Name), a, a.Snapshot(), laid)
	found := false
	for _, row := range a.MissionInventory {
		if row.Codename == sample.Codename && row.VarianceBits == strconv.FormatUint(sample.VarianceBits, 10) {
			found = len(row.MagicOptions) == len(sample.MagicOptions) && len(row.MagicOptions) >= 3
		}
	}
	if !found {
		t.Fatalf("the picked-up %s lost its blues or whites", sample.Codename)
	}
}

/*
================
assertBattleRoyaleGear

Three or more distinct valued blues, every white field 16..31, a
durability.
================
*/
func assertBattleRoyaleGear(t *testing.T, ref *enterworld.ItemRef, codename string, variance uint64, durability uint32, magic []uint64) {
	t.Helper()
	if len(magic) < battleRoyaleMinBlues {
		t.Fatalf("%s carries %d blues", codename, len(magic))
	}
	seen := map[uint16]bool{}
	for _, option := range magic {
		if seen[uint16(option)] || option>>32 == 0 {
			t.Fatalf("%s blue %x duplicate or valueless", codename, option)
		}
		seen[uint16(option)] = true
	}
	fields := equipmentVarianceFieldCount(ref)
	if fields == 0 {
		t.Fatalf("%s has no white fields", codename)
	}
	for field := 0; field < fields; field++ {
		if value := (variance >> (field * varianceFieldBits)) & battleRoyaleWhiteMax; value < battleRoyaleWhiteFloor {
			t.Fatalf("%s white field %d at %d/31 (%.0f %%)", codename, field, value, float64(value)*100/31)
		}
	}
	if variance>>(fields*varianceFieldBits) != 0 || durability == 0 {
		t.Fatalf("%s variance %x durability %d", codename, variance, durability)
	}
}

/*
================
assertBattleRoyaleSkin

A skin drop: two to four distinct options of its part's avatar pool,
each at a value the smith grants, and no white field.
================
*/
func assertBattleRoyaleSkin(t *testing.T, ref *enterworld.ItemRef, drop grounditem.Item) {
	t.Helper()
	if ref == nil || drop.VarianceBits != 0 || len(drop.MagicOptions) < battleRoyaleSkinMinBlues || len(drop.MagicOptions) > 4 {
		t.Fatalf("skin %s: %d blues, variance %x", drop.Codename, len(drop.MagicOptions), drop.VarianceBits)
	}
	pool := map[uint16]alchemy.Magic{}
	for _, option := range loot.AvatarPool(uint8(ref.TypeIDs[3]), battleRoyaleSkinDegree) {
		pool[option.ID] = option
	}
	seen := map[uint16]bool{}
	for _, option := range drop.MagicOptions {
		magic, ok := pool[uint16(option)]
		if !ok || seen[uint16(option)] || uint32(option>>32) != magic.Params[1] {
			t.Fatalf("skin %s option %x outside its pool or value", drop.Codename, option)
		}
		seen[uint16(option)] = true
	}
}

/*
================
TestBattleRoyaleWhiteFold

The fold keeps the native order and lands every native 0..31 at 16..31,
so the client's printed grade is 50 % or more on every stat range.
================
*/
func TestBattleRoyaleWhiteFold(t *testing.T) {
	previous := uint8(0)
	for native := uint8(0); native <= 31; native++ {
		folded := battleRoyaleWhite(native)
		if folded < 16 || folded > 31 || folded < previous {
			t.Fatalf("native %d folds to %d", native, folded)
		}
		previous = folded
		// The client grade: the stat rounds (hi-lo)*v/31 to the nearest
		// integer and prints trunc((stat-lo)/(hi-lo)*100).
		for span := 1; span <= 400; span++ {
			stat := int(float64(span)*float64(folded)/31 + 0.5)
			if grade := stat * 100 / span; grade < 50 {
				t.Fatalf("native %d span %d prints %d %%", native, span, grade)
			}
		}
	}
	if battleRoyaleWhite(0) != 16 || battleRoyaleWhite(31) != 31 {
		t.Fatal("fold ends")
	}
}

/*
================
TestBattleRoyaleLiveMonstersStayCapped

The drop stands battleroyale.ArenaMonsters wolves and the first supply
passes top the zone up, never past MaxLiveMonsters living monsters; the
census counts what stands; a zone pass at the cap adds no wolves.
================
*/
func TestBattleRoyaleLiveMonstersStayCapped(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.startMatch(t)
	m := fx.match()
	nowMs := fx.clock.NowMs()
	wolves, live := fx.rt.battleRoyaleCensus(m, nowMs)
	if live != len(fx.arenaMonsters()) || len(wolves) <= battleroyale.ArenaMonsters || live > battleroyale.MaxLiveMonsters {
		t.Fatalf("%d live monsters, %d wolves", live, len(wolves))
	}
	zone, _ := m.match.ZoneAt(nowMs)
	for range 8 {
		fx.rt.supplyBattleRoyaleZone(m, zone, nowMs)
		fx.rt.localBattleRoyaleSupply(m, zone, nowMs)
	}
	if _, live = fx.rt.battleRoyaleCensus(m, nowMs); live > battleroyale.MaxLiveMonsters {
		t.Fatalf("%d live monsters after repeated supply", live)
	}
}
