/*
===========================================================================

battleroyale_autoequip_test.go - the stage-8 full auto-equip and discard

Port-only, not native. A match character seats every better item of
its set through the native move (shoulders included), drops on the
ground the usable items that can never beat the worn one, never touches
consumables or an UltimatePlus item, keeps (stage 9) every item its
build can never use, and a two-handed build never takes a shield.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
battleRoyaleGive

Puts one row of codename into the first free bag slot of c and returns
the slot.
================
*/
func battleRoyaleGive(t *testing.T, fx *battleRoyaleFixture, items staticItemSource, c *enterworld.Character, codename string, plus int64) int64 {
	t.Helper()
	ref := items[codename]
	slot, ok := firstFreeBagSlot(c.MissionInventory, inventory.BagEnd(c))
	if ref == nil || !ok {
		t.Fatalf("cannot give %s", codename)
	}
	row := fx.rt.battleRoyaleKitRow(ref, slot, 1)
	row.Plus = plus
	c.MissionInventory = append(c.MissionInventory, row)
	return slot
}

/*
================
TestBattleRoyaleFullAutoEquip
================
*/
func TestBattleRoyaleFullAutoEquip(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	items := fx.addKitItems()
	a, b := fx.chars[0], fx.chars[1]
	gender := "M"
	if enterworld.ResolveCharacterGenderIndex(a) == domain.GenderFemale {
		gender = "W"
	}
	shoulder := func(degree int, seal string) string {
		return fmt.Sprintf("ITEM_CH_%s_HEAVY_%02d_SA_%s_RARE", gender, degree, seal)
	}
	for degree := 1; degree <= 2; degree++ {
		for i, seal := range []string{"B", "C"} {
			items[shoulder(degree, seal)] = &enterworld.ItemRef{RefObjID: uint32(6100 + 2*degree + i), Codename: shoulder(degree, seal),
				TypeIDs: [4]int64{3, 1, 3, 2}, Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
		}
	}
	items["ITEM_EU_SHIELD_02_C_RARE"] = &enterworld.ItemRef{RefObjID: 6200, Codename: "ITEM_EU_SHIELD_02_C_RARE", TypeIDs: [4]int64{3, 1, 4, 2},
		Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
	fx.pick(t, a, 1)
	fx.startMatch(t)

	battleRoyaleGive(t, fx, items, a, shoulder(1, "B"), 0)
	battleRoyaleGive(t, fx, items, a, shoulder(2, "C"), 0)
	battleRoyaleGive(t, fx, items, a, "ITEM_CH_BLADE_01_B_RARE", 0)
	ultimate := battleRoyaleGive(t, fx, items, a, "ITEM_CH_SWORD_01_B_RARE", battleroyale.UltimatePlus)
	// Stage 9: another race's shield and another body's shoulder stay in
	// the bag.
	battleRoyaleGive(t, fx, items, a, "ITEM_EU_SHIELD_02_C_RARE", 0)
	foreign := fmt.Sprintf("ITEM_CH_%s_HEAVY_%02d_SA_%s_RARE", map[string]string{"M": "W", "W": "M"}[gender], 2, "C")
	items[foreign] = &enterworld.ItemRef{RefObjID: 6300, Codename: foreign, TypeIDs: [4]int64{3, 1, 3, 2}, Country: 3, RequiredSex: 2,
		ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
	battleRoyaleGive(t, fx, items, a, foreign, 0)
	result, equipped := fx.rt.battleRoyaleAutoEquip(testDivision, a)
	if worn, _ := rowAt(a, int64(inventory.SocketShoulder)); !equipped || worn.Codename != shoulder(2, "C") {
		t.Fatalf("shoulder socket %+v (equipped %v)", worn, equipped)
	}
	moves := 0
	for _, f := range result.Frames {
		if f.Opcode == wire.OpItemMoveResponse && f.Payload[0] == wire.ResultSuccess {
			moves++
		}
	}
	if moves != 1 || len(result.Broadcast) == 0 {
		t.Fatalf("equip burst: %d moves, %d broadcast frames", moves, len(result.Broadcast))
	}
	if weapon, _ := rowAt(a, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_CH_BLADE_01_C_RARE" {
		t.Fatalf("a worse Moon blade replaced the Sun blade: %+v", weapon)
	}

	potions := map[string]int64{}
	for _, row := range a.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) && items[row.Codename] != nil && items[row.Codename].TypeIDs[1] == 3 {
			potions[row.Codename] += row.StackCount
		}
	}
	result, dropped := fx.rt.battleRoyaleDiscard(testDivision, a)
	if !dropped || len(result.Broadcast) == 0 {
		t.Fatalf("nothing dropped (%v)", dropped)
	}
	left := map[string]bool{}
	after := map[string]int64{}
	for _, row := range a.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) {
			left[row.Codename] = true
			if items[row.Codename] != nil && items[row.Codename].TypeIDs[1] == 3 {
				after[row.Codename] += row.StackCount
			}
		}
	}
	if left[shoulder(1, "B")] || left["ITEM_CH_BLADE_01_B_RARE"] {
		t.Fatalf("worse items kept: %v", left)
	}
	if !left["ITEM_EU_SHIELD_02_C_RARE"] || !left[foreign] {
		t.Fatalf("unusable items dropped: %v", left)
	}
	if row, ok := rowAt(a, ultimate); !ok || row.Codename != "ITEM_CH_SWORD_01_B_RARE" {
		t.Fatal("the +8 weapon was dropped")
	}
	if fmt.Sprint(potions) != fmt.Sprint(after) || len(after) == 0 {
		t.Fatalf("consumables changed: %v -> %v", potions, after)
	}
	if worn, _ := rowAt(a, int64(inventory.SocketShoulder)); worn.Codename != shoulder(2, "C") {
		t.Fatalf("the worn shoulder moved: %+v", worn)
	}

	// The two-handed Warrior never wears a shield; since stage 9 it keeps
	// it in the bag.
	shield := battleRoyaleGive(t, fx, items, b, "ITEM_EU_SHIELD_02_C_RARE", 0)
	fx.rt.battleRoyaleGearPass(testDivision, b)
	if _, off := rowAt(b, int64(inventory.SocketShield)); off {
		t.Fatal("the two-handed Warrior wears a shield")
	}
	if row, kept := rowAt(b, shield); !kept || row.Codename != "ITEM_EU_SHIELD_02_C_RARE" {
		t.Fatal("the Warrior dropped a shield it can never wear")
	}
	if weapon, _ := rowAt(b, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_EU_TSWORD_01_C_RARE" {
		t.Fatalf("the Warrior's weapon %+v", weapon)
	}
}

/*
================
TestPlanGearScoreOrder

The score is lexicographic: degree, Sun, plus, whites, blues; a worn
item wins a tie; the two best rings fill both ring slots.
================
*/
func TestPlanGearScoreOrder(t *testing.T) {
	blade, _ := battleroyale.BuildByID(1)
	killer := battleroyale.Killer{Build: blade, Level: 30}
	score := func(p battleroyale.GearPiece) int64 {
		_, s, ok := battleroyale.GearScore(killer, p)
		if !ok {
			t.Fatalf("%s unusable", p.Codename)
		}
		return s
	}
	ordered := []battleroyale.GearPiece{
		{Codename: "ITEM_CH_BLADE_03_C_RARE", Plus: 8, WhitePercent: 700, Blues: 9, BlueValues: 500},
		{Codename: "ITEM_CH_BLADE_04_B_RARE"},
		{Codename: "ITEM_CH_BLADE_04_B_RARE", Plus: 1},
		{Codename: "ITEM_CH_BLADE_04_C_RARE"},
	}
	for i := 1; i < len(ordered); i++ {
		if score(ordered[i]) <= score(ordered[i-1]) {
			t.Fatalf("%+v does not beat %+v", ordered[i], ordered[i-1])
		}
	}
	if score(battleroyale.GearPiece{Codename: "ITEM_CH_RING_02_B_RARE", WhitePercent: 1}) <=
		score(battleroyale.GearPiece{Codename: "ITEM_CH_RING_02_B_RARE", Blues: 9, BlueValues: 99}) {
		t.Fatal("blues outrank a white point")
	}
	plan := battleroyale.PlanGear(killer, []battleroyale.GearPiece{
		{Codename: "ITEM_CH_RING_02_B_RARE", Worn: true}, {Codename: "ITEM_CH_RING_03_B_RARE"},
		{Codename: "ITEM_CH_RING_01_B_RARE"}, {Codename: "ITEM_CH_RING_03_B_RARE"},
		{Codename: "ITEM_EU_RING_04_C_RARE"}, {Codename: "ITEM_CH_W_HEAVY_04_SA_C_RARE"}, {Codename: "ITEM_EU_TSWORD_04_C_RARE"},
	})
	// The European ring, the female shoulder and the two-handed sword are
	// not the male Blade-Fire's: never worn, never discarded (stage 9).
	if plan.Wear[battleroyale.SlotRing] != 1 || plan.Wear[battleroyale.SlotRingSecond] != 3 || fmt.Sprint(plan.Discard) != "[2]" {
		t.Fatalf("rings %+v", plan)
	}
}
