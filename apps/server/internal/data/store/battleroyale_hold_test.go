/*
===========================================================================

battleroyale_hold_test.go - the battle royale hold round-trips the store

Schema 28: a row holding a real character (domain.BattleRoyaleHold) is
written, read back strictly and equal, and the real record inside it is
the one that was held, byte for byte.

===========================================================================
*/
package store

import (
	"bytes"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestBattleRoyaleHoldRoundTrips
================
*/
func TestBattleRoyaleHoldRoundTrips(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	level, gold := int64(42), int64(9000)
	c := &enterworld.Character{Name: "Gina", ModelCodename: "CHAR_CH_WOMAN_FOX", Level: &level, Gold: &gold,
		MissionInventory: []enterworld.InventoryRow{{Slot: 6, RefObjID: 4025, Codename: "ITEM_CH_SWORD_04_C_RARE", Plus: 7, VarianceBits: "123", Durability: 50, StackCount: 1}}}
	if err := s.CreateCharacter(testDivision, "acc", c); err != nil {
		t.Fatal(err)
	}
	live := s.Characters().CharactersForDivision(testDivision)[0]
	real := domain.CloneCharacter(live)
	realJSON, _ := json.Marshal(real)
	s.MutateCharacter(live, "battle-royale-hold", func() {
		level, gold := int64(1), int64(0)
		live.Level, live.Gold = &level, &gold
		live.BattleRoyaleHeld = &domain.BattleRoyaleHold{Real: real, Build: 13, World: 0x001d0001, HeldAtMs: 42}
	})
	s.Close()

	r := openTest(t, dir, clock)
	defer r.Close()
	got := r.Characters().CharactersForDivision(testDivision)[0]
	if got.BattleRoyaleHeld == nil || got.BattleRoyaleHeld.Build != 13 || got.BattleRoyaleHeld.World != 0x001d0001 || got.BattleRoyaleHeld.HeldAtMs != 42 {
		t.Fatalf("hold after reopen: %+v", got.BattleRoyaleHeld)
	}
	if *got.Level != 1 || *got.Gold != 0 {
		t.Fatalf("match fields after reopen: level %d gold %d", *got.Level, *got.Gold)
	}
	heldJSON, _ := json.Marshal(got.BattleRoyaleHeld.Real)
	if !bytes.Equal(heldJSON, realJSON) {
		t.Fatalf("held record differs after the round trip:\n%s\n%s", heldJSON, realJSON)
	}
	// The clone keeps the hold detached: a snapshot's hold is its own copy.
	snapshot := got.Snapshot()
	snapshot.BattleRoyaleHeld.Real.Name = "changed"
	if got.BattleRoyaleHeld.Real.Name != "Gina" {
		t.Fatal("the snapshot's hold aliases the live record")
	}
}
