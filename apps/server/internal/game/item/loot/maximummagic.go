/*
===========================================================================

maximummagic.go - the battle royale ultimate weapon's blue options

PORT-ONLY, NOT NATIVE (battle royale, docs/battle-royale.md stage 8b).
The airdrop's ultimate weapon carries the most blue options its own
native pool allows: every distinct option of the item's magicoptionassign
pool the native generator could roll on it (allowed on the item, a
probability above zero, not the nonrepair option), one per magic tag,
up to the item's MaxMagic, each at its top value (the largest of the
values RollMagicValue draws from). Nothing native calls it.

===========================================================================
*/
package loot

import (
	"fmt"
	"slices"
)

/*
================
EquipmentMagicMaximum

The encoded magic options of an ultimate item: as many distinct options
of its native pool as it allows, in pool order, each at its top value.
An error for an item without properties or an empty pool.
================
*/
func EquipmentMagicMaximum(code string) ([]uint64, error) {
	item, exists := properties.Items[code]
	if !exists {
		return nil, fmt.Errorf("missing loot properties for %s", code)
	}
	flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
	seenID, seenTag := map[uint16]bool{}, map[uint32]bool{}
	var out []uint64
	for _, option := range properties.pools[code] {
		if len(out) >= item.MaxMagic {
			break
		}
		if seenID[option.ID] || seenTag[option.Tag] || option.Probability <= 0 || option.Tag == nonRepairTag || !option.Allows(flags) {
			continue
		}
		top := magicTopValue(option.Params)
		if top == 0 {
			continue
		}
		seenID[option.ID], seenTag[option.Tag] = true, true
		out = append(out, uint64(top)<<32|uint64(option.ID))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("loot magic pool of %s is empty", code)
	}
	return out, nil
}

/*
================
magicTopValue

The largest value RollMagicValue can draw from an option's params (each
param packs two 16-bit values; zeros are not values).
================
*/
func magicTopValue(params [3]uint32) uint32 {
	var values []uint32
	for _, p := range params {
		values = append(values, p>>16, p&0xFFFF)
	}
	return slices.Max(values)
}
