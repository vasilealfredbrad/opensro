/*
===========================================================================

minimummagic_test.go - the battle royale's guaranteed blue options

Port-only, not native. Every Seal of Moon and Sun item of degrees 1-4
(the battle royale's drop rows) gets at least three distinct options from
EquipmentMagicAtLeast, each from the item's own native pool and allowed
on it; an unknown row refuses.

===========================================================================
*/
package loot

import (
	"regexp"
	"testing"
)

/*
================
TestEquipmentMagicAtLeastThree
================
*/
func TestEquipmentMagicAtLeastThree(t *testing.T) {
	matchRow := regexp.MustCompile(`^ITEM_(CH|EU)_.*_0[1-4](_[A-Z]{2})?_[BC]_RARE$`)
	roll := seededRoll(7)
	items, counts := 0, map[int]int{}
	for code, item := range properties.Items {
		if !matchRow.MatchString(code) || item.Rarity != 2 {
			continue
		}
		items++
		pool := map[uint16]bool{}
		for _, option := range properties.pools[code] {
			pool[option.ID] = true
		}
		flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
		for run := 0; run < 8; run++ {
			options, err := EquipmentMagicAtLeast(code, 3, roll)
			if err != nil {
				t.Fatalf("%s: %v", code, err)
			}
			if len(options) < 3 {
				t.Fatalf("%s: %d options", code, len(options))
			}
			counts[len(options)]++
			seen := map[uint16]bool{}
			for _, encoded := range options {
				id := uint16(encoded)
				if seen[id] || !pool[id] || encoded>>32 == 0 {
					t.Fatalf("%s: option %x duplicate, foreign or valueless", code, encoded)
				}
				seen[id] = true
				for _, option := range properties.pools[code] {
					if option.ID == id && !option.Allows(flags) {
						t.Fatalf("%s: option %s not allowed on the item", code, option.Name)
					}
				}
			}
		}
	}
	if items < 500 {
		t.Fatalf("only %d match rows checked", items)
	}
	t.Logf("%d rows; option counts %v", items, counts)
	if _, err := EquipmentMagicAtLeast("ITEM_NOT_A_ROW", 3, roll); err == nil {
		t.Fatal("an unknown row produced options")
	}
}
