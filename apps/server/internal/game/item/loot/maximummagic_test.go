/*
===========================================================================

maximummagic_test.go - the battle royale ultimate weapon's blue options

Port-only, not native. Every Seal of Sun weapon of degrees 1-4 gets from
EquipmentMagicMaximum every distinct eligible option of its own native
pool (one per tag) up to its MaxMagic, each at its top value; more than
the guaranteed three; an unknown row refuses.

===========================================================================
*/
package loot

import (
	"regexp"
	"slices"
	"testing"
)

/*
================
TestEquipmentMagicMaximum
================
*/
func TestEquipmentMagicMaximum(t *testing.T) {
	sunWeapon := regexp.MustCompile(`^ITEM_(CH|EU)_[A-Z]+_0[1-4]_C_RARE$`)
	checked := 0
	for code, item := range properties.Items {
		if !sunWeapon.MatchString(code) || item.Type[2] != 6 {
			continue
		}
		options, err := EquipmentMagicMaximum(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
		tags := map[uint32]bool{}
		top := map[uint16]uint32{}
		for _, option := range properties.pools[code] {
			if option.Probability > 0 && option.Tag != nonRepairTag && option.Allows(flags) {
				tags[option.Tag] = true
				top[option.ID] = slices.Max([]uint32{option.Params[0] >> 16, option.Params[0] & 0xFFFF, option.Params[1] >> 16,
					option.Params[1] & 0xFFFF, option.Params[2] >> 16, option.Params[2] & 0xFFFF})
			}
		}
		if want := min(len(tags), item.MaxMagic); len(options) != want || want <= 3 {
			t.Fatalf("%s: %d options, want %d (pool tags %d, max %d)", code, len(options), want, len(tags), item.MaxMagic)
		}
		seen := map[uint16]bool{}
		for _, encoded := range options {
			id := uint16(encoded)
			if seen[id] || top[id] == 0 || uint32(encoded>>32) != top[id] {
				t.Fatalf("%s: option %x duplicate, foreign or below its top %d", code, encoded, top[id])
			}
			seen[id] = true
		}
		checked++
	}
	if checked < 40 {
		t.Fatalf("only %d Sun weapons checked", checked)
	}
	if _, err := EquipmentMagicMaximum("ITEM_NOT_A_ROW"); err == nil {
		t.Fatal("an unknown row produced options")
	}
}
