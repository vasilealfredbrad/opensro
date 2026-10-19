/*
===========================================================================

battleroyale_loot.go - what a match kill drops and how its gear is rolled

Port-only, not native (battleroyale.go). A match character's kill drops
the match table only (battleroyale/loot.go: potions, pills and Seal of
Moon or Sun equipment), never the monster's own drops: it is a battle
royale. Every dropped equipment item is finished here instead of by the
native drop finisher (prepareSelectedDrop):

  - White stats. The native roll (rollLowBiasedVarianceField: the least of
    three draws, 0-31 per stat) is folded into the upper half,
    16 + v x 15 / 31, so every stat is at 16/31 (51.6 %) or better and the
    native low bias still shapes the spread; the durability follows the
    first field as natively (combat.DurabilityFromVariance). The client
    prints each percentage from the rounded stat, which stays at 50 % or
    more for every range.
  - Random loot (stage 9). The table reads the killer's level only
    (battleroyale/lootrandom.go): its equipment is any build's, race's
    and gender's, and about one equipment draw in a hundred is a skin.
  - Blue options. loot.EquipmentMagicAtLeast: the native rare-item
    generator rerun until it gives three options or more, topped up from
    the item's own native option pool when no run did. The options ride
    the ordinary item rows (inventory.Item.MagicOptions), so the ground
    item, the pickup and the inventory frames carry them as they carry
    any native blue.
  - Skins. An avatar drop (TID3 13) has no whites (its variance has no
    field) and carries battleRoyaleSkinMinBlues or more options of its
    part's native avatar pool (loot.AvatarMagic: the smith's options
    and values), up to its itemdata MaxMagic when that is larger.
  - The Devil's Spirit (game/item/devilspirit, SRO_DEVIL_SPIRIT). When
    its rows are loaded (devilspirit.Loaded), the airdrop guardian's
    killer rolls devilspirit.AirdropReward on the loot stream (one in
    40, the S grade one in 8 of those), of the killer's match
    character's sex; a hit lies with the guardian's loot and names the
    finder shard-wide on channel 7. It is made as a new instance
    awakened for good (a match is shorter than any period), its options
    its level-0 ones and its MATTR_NASRUN_UMDU drawn (devilspirit.go in
    this package). With the flag off nothing rolls.

The caller holds the division lock.

===========================================================================
*/

package action

import (
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleMinBlues is the fewest blue options a match equipment
	// drop carries (stage 4).
	battleRoyaleMinBlues = 3
	// battleRoyaleWhiteFloor is the lowest white-stat value a match
	// equipment drop rolls: 16 of 31, 51.6 %.
	battleRoyaleWhiteFloor = 16
	// battleRoyaleWhiteMax is the top of a 5-bit variance field.
	battleRoyaleWhiteMax = 31
	// varianceFieldBits is the width of one variance field.
	varianceFieldBits = 5
	// battleRoyaleSkinMinBlues is the fewest blue options a skin drop
	// carries (stage 9 item 4), when its pool holds that many.
	battleRoyaleSkinMinBlues = 2
	// battleRoyaleSkinDegree is the degree of every avatar row (class 1).
	battleRoyaleSkinDegree = 1
)

/*
================
battleRoyaleLoot

A match character's kill drops the match table instead of the monster's
own, rolled from the mode's seeded stream at the killer's level: the
supply-drop elite (told by its giant rarity) SupplyDropLoot, plus the
ultimate weapon when its airdrop holds one (stage 8b,
battleroyale_airdrop.go: the marker goes, the finder is named
shard-wide) and, for an airdrop's guardian, a Devil's Spirit when its
roll hits (battleRoyaleDevilSpirit), a bot's body two LootPlan rolls
(stage 5), anything else LootPlan.
================
*/
func (rt *Runtime) battleRoyaleLoot(snapshot *enterworld.Character, m monster.Instance, at simulation.Spawn, now time.Time) ([]grounditem.Item, bool) {
	build, ok := battleroyale.HeldBuild(snapshot)
	if !ok || rt.battleRoyale == nil {
		return nil, false
	}
	killer := battleroyale.Killer{Build: build, Level: rewardLevel(snapshot),
		Female: enterworld.ResolveCharacterGenderIndex(snapshot) == domain.GenderFemale}
	elite := m.Nest.HasRarityOverride && battleroyale.IsSupplyElite(m.Ref.Codename, m.Nest.RarityOverride)
	var airdrop *battleRoyaleAirdrop
	if elite {
		airdrop, _ = rt.takeBattleRoyaleAirdrop(m.Gid)
	}
	br := rt.battleRoyale
	br.lootMu.Lock()
	var plan []battleroyale.Drop
	ultimate, devil := "", ""
	_, bot := rt.battleRoyaleBotOf(m.Gid)
	switch {
	case elite:
		if airdrop != nil {
			devil = rt.battleRoyaleDevilSpirit(killer)
		}
		plan = battleroyale.SupplyDropLoot(killer, br.loot)
		if airdrop != nil && airdrop.ultimate {
			ultimate, _ = battleroyale.UltimateWeaponCodename(killer, br.loot)
		}
	case bot:
		plan = append(battleroyale.LootPlan(killer, br.loot), battleroyale.LootPlan(killer, br.loot)...)
	default:
		plan = battleroyale.LootPlan(killer, br.loot)
	}
	br.lootMu.Unlock()
	var out []grounditem.Item
	if ultimate != "" {
		if item, ok := rt.prepareBattleRoyaleUltimate(ultimate, at, snapshot.Name, now); ok {
			out = append(out, item)
			rt.announceBattleRoyaleShard(airdrop.division, battleroyale.UltimateBanner(snapshot.Name))
		}
	}
	if devil != "" {
		if item, ok := rt.prepareBattleRoyaleDrop(battleroyale.Drop{Codename: devil, Count: 1}, at, snapshot.Name, now); ok {
			out = append(out, item)
			rt.announceBattleRoyaleShard(airdrop.division, battleroyale.DevilSpiritBanner(snapshot.Name))
		}
	}
	for _, drop := range plan {
		if item, ok := rt.prepareBattleRoyaleDrop(drop, at, snapshot.Name, now); ok {
			out = append(out, item)
		}
	}
	return out, true
}

/*
================
battleRoyaleDevilSpirit

The Devil's Spirit an airdrop's guardian leaves its killer, or "": only
when the rows are loaded (devilspirit.Loaded: SRO_DEVIL_SPIRIT on), so
with the flag off the loot stream is not touched. The roll
(devilspirit.AirdropReward) reads the match's loot stream; the row is
of the killer's match character's sex. The caller holds lootMu.
================
*/
func (rt *Runtime) battleRoyaleDevilSpirit(killer battleroyale.Killer) string {
	if !devilspirit.Loaded(rt.deps.ItemReferences()) {
		return ""
	}
	stream := rt.battleRoyale.loot
	codename, ok, err := devilspirit.AirdropReward(!killer.Female, func() (uint32, error) { return stream.Uint32(), nil })
	if err != nil {
		log.WithError(err).Warn("battle royale: Devil's Spirit roll refused")
		return ""
	}
	if !ok {
		return ""
	}
	return codename
}

/*
================
prepareBattleRoyaleDrop

One match drop as a ground item: a consumable as itemdata names it, an
equipment item with its white stats folded to 50 % or more and at least
battleRoyaleMinBlues blue options, a skin with its avatar options (see
the file header). A row itemdata lacks, or a failed draw, drops nothing.
================
*/
func (rt *Runtime) prepareBattleRoyaleDrop(drop battleroyale.Drop, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil || drop.Count <= 0 {
		return grounditem.Item{}, false
	}
	ref, ok := items.ItemRefByCodename(drop.Codename)
	if !ok || ref == nil {
		log.WithField("codename", drop.Codename).Warn("battle royale: drop missing from itemdata")
		return grounditem.Item{}, false
	}
	count := uint16(min(drop.Count, int64(^uint16(0))))
	row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: count}
	if devilspirit.Is(ref.RefObjID) {
		// Port-only: the airdrop's Devil's Spirit is awakened for good (a
		// match is shorter than any period, and the item stays in it).
		rt.makeDevilSpirit(&row, true)
		return PlanItemDrop(row, count, at, owner, now), true
	}
	if ref.TypeIDs[1] == 1 {
		variance, durability, ok := rt.rollBattleRoyaleVariance(ref)
		if !ok {
			return grounditem.Item{}, false
		}
		magic, err := rt.battleRoyaleDropMagic(ref)
		if err != nil {
			log.WithError(err).WithField("codename", ref.Codename).Warn("battle royale: drop magic refused")
			return grounditem.Item{}, false
		}
		row.VarianceBits, row.Durability, row.MagicOptions = variance, durability, magic
	}
	return PlanItemDrop(row, count, at, owner, now), true
}

/*
================
rollBattleRoyaleVariance

The dropped-equipment variance roll (rollDroppedEquipmentVariance's
fields and durability) with every field folded into
battleRoyaleWhiteFloor..battleRoyaleWhiteMax.
================
*/
func (rt *Runtime) rollBattleRoyaleVariance(ref *enterworld.ItemRef) (uint64, uint32, bool) {
	var bits uint64
	var first uint8
	for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
		value, ok := rt.rollLowBiasedVarianceField()
		if !ok {
			return 0, 0, false
		}
		value = battleRoyaleWhite(value)
		if field == 0 {
			first = value
		}
		bits |= uint64(value) << (field * varianceFieldBits)
	}
	return bits, combat.DurabilityFromVariance(ref, first), true
}

/*
================
battleRoyaleWhite

Folds a native 0-31 variance field into 16-31.
================
*/
func battleRoyaleWhite(native uint8) uint8 {
	folded := battleRoyaleWhiteFloor + int(min(native, battleRoyaleWhiteMax))*(battleRoyaleWhiteMax-battleRoyaleWhiteFloor)/battleRoyaleWhiteMax
	return uint8(folded)
}

/*
================
battleRoyaleDropMagic

The blue options of a dropped equipment row: a port-only row made with
fixed options (ItemRef.PortOnlyMagicOptions) gets exactly those; an
avatar (a skin) gets
battleRoyaleSkinMinBlues options of its part's pool, one more per draw
up to its MaxMagic (loot.AvatarMagic); anything else at least
battleRoyaleMinBlues (loot.EquipmentMagicAtLeast).
================
*/
func (rt *Runtime) battleRoyaleDropMagic(ref *enterworld.ItemRef) ([]uint64, error) {
	if len(ref.PortOnlyMagicOptions) > 0 {
		return append([]uint64(nil), ref.PortOnlyMagicOptions...), nil
	}
	if ref.TypeIDs[2] != itemTID3Avatar {
		return loot.EquipmentMagicAtLeast(ref.Codename, battleRoyaleMinBlues, rt.DropRoll)
	}
	count := battleRoyaleSkinMinBlues
	if extra := int(ref.NativeFields.Get("maxMagicOptions51c")) - count; extra > 0 && rt.DropRoll != nil {
		n, err := rt.DropRoll()
		if err != nil {
			return nil, err
		}
		count += int(n % uint32(extra+1))
	}
	// Every avatar row is class 1, degree 1 (alchemy Reference.Degree).
	return loot.AvatarMagic(uint8(ref.TypeIDs[3]), battleRoyaleSkinDegree, count, rt.DropRoll)
}
