/*
===========================================================================

lootset_licensed_test.go - the full-set slots against the shipped itemdata

Port-only, not native (stage 8 item 4). For every build, gender and
level 1-30, every row the match loot can drop for a set slot exists in
itemdata as Moon and Sun, sits in the socket of that slot (TID3/TID4:
the armor class of the build with TID4 1 head, 2 shoulders, 3 chest, 4
legs, 5 hands, 6 feet; weapon 6 of the build's kinds; shield 4;
accessories 5/12 with TID4 1 earring, 2 necklace, 3 ring), fits the
body (sex, race) and needs a level inside the window; and over the
levels every slot drops at every degree 1-4. Stage 9: every row of the
random loot exists as Moon and Sun, and every skin is an avatar hat,
dress or attachment (TID 3/1/13/1-3) of one gender with room for a
blue option and a pool of two or more. Skips without the licensed
data.

===========================================================================
*/
package battleroyale

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
slotTID4

The itemdata TID4 of a set slot's rows (0 for the weapon, whose TID4 is
its kind).
================
*/
func slotTID4(slot SetSlot) int64 {
	return map[SetSlot]int64{SlotHead: 1, SlotShoulders: 2, SlotChest: 3, SlotLegs: 4, SlotHands: 5, SlotFeet: 6,
		SlotEarring: 1, SlotNecklace: 2, SlotRing: 3, SlotRingSecond: 3}[slot]
}

/*
================
TestSetSlotsMatchItemdata
================
*/
func TestSetSlotsMatchItemdata(t *testing.T) {
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	armorTID3 := map[Race]map[ArmorClass]int64{
		RaceChinese:  {ArmorClothes: 1, ArmorLight: 2, ArmorHeavy: 3},
		RaceEuropean: {ArmorClothes: 9, ArmorLight: 10, ArmorHeavy: 11},
	}
	checked := 0
	for _, build := range Builds() {
		for _, female := range []bool{false, true} {
			var seen [SetSlotCount][maxDegree + 1]bool
			for level := int64(1); level <= MatchLevelCap; level++ {
				killer := Killer{Build: build, Level: level, Female: female}
				for slot, rows := range buildWindow(killer) {
					for _, row := range rows {
						for _, seal := range []Seal{SealMoon, SealSun} {
							ref, ok := items.ItemRefByCodename(row.base + "_" + seal.suffix())
							if !ok || ref == nil {
								t.Fatalf("build %d level %d: %s_%s missing from itemdata", build.ID, level, row.base, seal.suffix())
							}
							checkSlotRef(t, build, female, SetSlot(slot), ref, armorTID3)
							if required, _ := RequiredLevel(ref.Codename); !InLootWindow(level, required) {
								t.Fatalf("level %d: %s needs %d", level, ref.Codename, required)
							}
							checked++
						}
						seen[slot][row.degree] = true
					}
				}
			}
			for _, slot := range BuildSlots(build) {
				for degree := 1; degree <= maxDegree; degree++ {
					if !seen[slot][degree] {
						t.Fatalf("build %d female %v: %s degree %d never in a window", build.ID, female, slot.Name(), degree)
					}
				}
			}
		}
	}
	t.Logf("%d window rows checked against itemdata", checked)
}

/*
================
checkSlotRef

Fails t unless ref is an equipment row of slot for the build's race,
class and gender.
================
*/
func checkSlotRef(t *testing.T, build Build, female bool, slot SetSlot, ref *enterworld.ItemRef, armorTID3 map[Race]map[ArmorClass]int64) {
	t.Helper()
	tid := ref.TypeIDs
	ok := tid[0] == 3 && tid[1] == 1
	switch {
	case slot == SlotWeapon:
		kind := false
		for _, k := range build.WeaponKinds {
			kind = kind || tid[3] == int64(k)
		}
		ok = ok && tid[2] == 6 && kind
	case slot == SlotShield:
		ok = ok && tid[2] == 4
	case slot <= SlotFeet:
		ok = ok && tid[2] == armorTID3[build.Race][build.Armor] && tid[3] == slotTID4(slot)
		sex := int64(1) // itemdata ReqGender: 1 male, 0 female
		if female {
			sex = 0
		}
		ok = ok && ref.RequiredSex == sex
	default:
		accessory := int64(5)
		if build.Race == RaceEuropean {
			accessory = 12
		}
		ok = ok && tid[2] == accessory && tid[3] == slotTID4(slot)
	}
	if !ok {
		t.Fatalf("build %d female %v: %s (TID %v, sex %d) is not a %s", build.ID, female, ref.Codename, tid, ref.RequiredSex, slot.Name())
	}
}

/*
================
TestRandomLootAndSkinsMatchItemdata
================
*/
func TestRandomLootAndSkinsMatchItemdata(t *testing.T) {
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	for _, row := range randomLoot().rows {
		for _, seal := range []Seal{SealMoon, SealSun} {
			ref, ok := items.ItemRefByCodename(row.base + "_" + seal.suffix())
			if !ok || ref == nil || ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 {
				t.Fatalf("loot row %s_%s missing or not equipment", row.base, seal.suffix())
			}
			if required, _ := RequiredLevel(ref.Codename); required != row.level {
				t.Fatalf("%s needs %d, row says %d", ref.Codename, required, row.level)
			}
		}
	}
	const minimumSkinBlues = 2
	sexes := map[int64]int{}
	for _, skin := range SkinCodenames() {
		ref, ok := items.ItemRefByCodename(skin)
		if !ok || ref == nil || ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 || ref.TypeIDs[2] != 13 || ref.TypeIDs[3] < 1 || ref.TypeIDs[3] > 3 {
			t.Fatalf("skin %s is not an avatar hat, dress or attachment (%+v)", skin, ref)
		}
		if ref.RequiredSex != 0 && ref.RequiredSex != 1 {
			t.Fatalf("skin %s sex %d", skin, ref.RequiredSex)
		}
		sexes[ref.RequiredSex]++
		if pool := loot.AvatarPool(uint8(ref.TypeIDs[3]), 1); len(pool) < minimumSkinBlues {
			t.Fatalf("skin %s pool %d", skin, len(pool))
		}
		if ref.NativeFields.Get("maxMagicOptions51c") < 1 {
			t.Fatalf("skin %s holds no blue", skin)
		}
	}
	if sexes[0] != sexes[1] || sexes[0] == 0 {
		t.Fatalf("skin genders %v", sexes)
	}
	t.Logf("%d loot rows and %d skins checked", len(randomLoot().rows), len(SkinCodenames()))
}
