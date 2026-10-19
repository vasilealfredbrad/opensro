/*
===========================================================================

minimummagic.go - equipment magic with a guaranteed number of blue options

PORT-ONLY, NOT NATIVE (battle royale, docs/battle-royale.md stage 4). The
native producer (EquipmentMagic, 7276A0) gives a rare item one to six
options, most often one or two (the count is the least of four six-way
draws, and duplicate or refused draws spend a slot). The battle royale
match wants every Seal of Moon or Sun drop to carry at least a given
number of blue options. EquipmentMagicAtLeast keeps the native generator
as the source of every option and value: it reruns it and keeps the
richest result, and only when no run reached the minimum it adds options
of the item's own native pool (the same magicoptionassign rows, degree,
applicability and value roll), so every option is one the client and the
item can carry. Nothing native calls it.

===========================================================================
*/
package loot

import (
	"fmt"

	"opensro.online/server/internal/game/item/alchemy"
)

// minimumMagicRuns bounds the native reruns before the pool top-up.
const minimumMagicRuns = 16

/*
================
EquipmentMagicAtLeast

The encoded magic options of one dropped item with at least minimum
options (capped at the item's MaxMagic): the richest of up to
minimumMagicRuns native rolls, topped up from the item's native pool with
distinct options the item allows and the native table can roll (a
probability above zero, not the nonrepair option). An error for an item
without properties, a failed draw, or a pool too small for the minimum.
================
*/
func EquipmentMagicAtLeast(code string, minimum int, roll func() (uint32, error)) ([]uint64, error) {
	item, exists := properties.Items[code]
	if !exists || roll == nil {
		return nil, fmt.Errorf("missing loot properties for %s", code)
	}
	minimum = min(minimum, item.MaxMagic)
	var best []uint64
	for run := 0; run < minimumMagicRuns && len(best) < minimum; run++ {
		options, entered, err := EquipmentMagic(code, roll)
		if err != nil {
			return nil, err
		}
		if entered && len(options) > len(best) {
			best = options
		}
	}
	if len(best) >= minimum {
		return best, nil
	}
	have := make(map[uint16]bool, len(best))
	for _, option := range best {
		have[uint16(option)] = true
	}
	flags := uint16(item.Type[0])<<2 | uint16(item.Type[1])<<5 | uint16(item.Type[2])<<7 | uint16(item.Type[3])<<11
	var candidates []propertyOption
	for _, option := range properties.pools[code] {
		if !have[option.ID] && option.Probability > 0 && option.Tag != nonRepairTag && option.Allows(flags) {
			have[option.ID] = true
			candidates = append(candidates, option)
		}
	}
	for len(best) < minimum && len(candidates) > 0 {
		n, err := roll()
		if err != nil {
			return nil, err
		}
		index := int(n % uint32(len(candidates)))
		option := candidates[index]
		candidates = append(candidates[:index], candidates[index+1:]...)
		value, err := alchemy.RollMagicValue(option.Magic, alchemy.Roll(roll))
		if err != nil {
			return nil, err
		}
		best = append(best, uint64(value)<<32|uint64(option.ID))
	}
	if len(best) < minimum {
		return nil, fmt.Errorf("loot magic pool of %s holds %d options, want %d", code, len(best), minimum)
	}
	return best, nil
}
