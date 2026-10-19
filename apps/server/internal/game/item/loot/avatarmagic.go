/*
===========================================================================

avatarmagic.go - blue options for a battle royale skin (an avatar drop)

PORT-ONLY, NOT NATIVE (battle royale, docs/battle-royale.md stage 9
item 4). No native producer drops an avatar with blue options: an
avatar gains them only at the smith (alchemy.BlessAvatar,
CBless_AvatarItemWithNPC 505810), one option of the part's
magicoptionassign row at a time, valued by the option's own generator
(mfunc_single_range, alchemy.RollSingleRange) and clamped to
1..avatarValueMax. AvatarMagic hands a skin drop several of those
options at once, from the same pool and with the same value rule, so
every option is one the smith could have granted and the client can
show. The pool is the embedded catalog's assignment of TID3 13 and the
part's TID4 joined to its option rows at the avatar's degree (the
equipment pools key on the item row, which the catalog does not carry
for avatars). Nothing native calls it.

===========================================================================
*/
package loot

import (
	"fmt"

	"opensro.online/server/internal/game/item/alchemy"
)

const (
	// avatarTypeID3 is the avatar TID3 (itemdata 3/1/13/part).
	avatarTypeID3 = 13
	// avatarValueMax is 505810's clamp on a granted value (0x6A4).
	avatarValueMax = 0x6a4
)

/*
================
AvatarPool

The distinct options (one per magic tag, in assignment order) the smith
may grant an avatar part (TID4: 1 hat, 2 dress, 3 attachment) of
degree; empty for a part without an assignment.
================
*/
func AvatarPool(part uint8, degree int) []alchemy.Magic {
	var out []alchemy.Magic
	tags := map[uint32]bool{}
	for _, assignment := range properties.Assignments {
		if assignment.Type3 != avatarTypeID3 || assignment.Type4 != part {
			continue
		}
		for _, name := range assignment.Options {
			for _, option := range properties.Magic {
				if option.Name != name || option.Degree != degree || tags[option.Tag] || option.Tag == nonRepairTag {
					continue
				}
				tags[option.Tag] = true
				out = append(out, option.Magic)
				break
			}
		}
	}
	return out
}

/*
================
AvatarMagic

The encoded magic options of one skin drop: count distinct options of
the part's pool (all of them when it holds fewer), drawn without
repeat, each valued as the smith values it. Nil for a part without a
pool (the plain avatar); an error for a failed draw.
================
*/
func AvatarMagic(part uint8, degree, count int, roll func() (uint32, error)) ([]uint64, error) {
	if roll == nil {
		return nil, fmt.Errorf("avatar magic needs a roll")
	}
	pool := AvatarPool(part, degree)
	var out []uint64
	for len(out) < count && len(pool) > 0 {
		n, err := roll()
		if err != nil {
			return nil, err
		}
		index := int(n % uint32(len(pool)))
		option := pool[index]
		pool = append(pool[:index], pool[index+1:]...)
		value, err := alchemy.RollSingleRange(option, alchemy.Roll(roll))
		if err != nil {
			return nil, err
		}
		value = min(max(value, 1), avatarValueMax)
		out = append(out, uint64(value)<<32|uint64(option.ID))
	}
	return out, nil
}
