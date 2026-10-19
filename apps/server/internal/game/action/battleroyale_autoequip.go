/*
===========================================================================

battleroyale_autoequip.go - a match character wears its best set and
drops its worse duplicates

Port-only, not native (stage 8, docs/battle-royale.md "Server notes
(stage 8: full sets)"). The decision is the core's
(battleroyale.PlanGear, autoequip.go: one score per item, best per
slot); this file reads the character's bag into GearPiece rows, seats
every planned item through the ordinary inventory move
(applyInventoryMove: the client gets the native move result, the visual
and the stat refresh, observers the visual) and drops every planned
discard through the native ground drop (applyGroundDrop: the item lands
at the character's feet, owned by it, and vanishes with the match's
ground policy). A discard is a usable item no better than the one worn;
since stage 9 (random loot) an item the build can never wear stays in
the bag for the player to keep or drop.

Cadence: the equip runs right after a pickup (pickup.go) and after a
level gain (the equipPending tick); the full pass, equip then discard,
runs for every living participant every battleRoyaleGroundSweepMs with
the ground sweep (battleroyale_arena.go advanceBattleRoyaleEconomy).
Consumables never enter the plan (only TID 3.1 equipment does, never a
job suit or avatar), the equipped items are never discarded, and an
item at battleroyale.UltimatePlus is never discarded. Socket 7 of a
build without a shield (ammunition) is never touched.

The caller holds the division lock and no character door.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleMaxEquipMoves bounds the moves of one pass (twelve
	// slots and a margin).
	battleRoyaleMaxEquipMoves = 16
	// itemTID3JobSuit and itemTID3Avatar are equipment the set never
	// plans.
	itemTID3JobSuit = 7
	itemTID3Avatar  = 13
	whitePercentMax = 100
)

/*
================
battleRoyaleSocketOf

The inventory socket of a set slot.
================
*/
func battleRoyaleSocketOf(slot battleroyale.SetSlot) uint8 {
	return [battleroyale.SetSlotCount]uint8{
		battleroyale.SlotHead: inventory.SocketHead, battleroyale.SlotShoulders: inventory.SocketShoulder,
		battleroyale.SlotChest: inventory.SocketBody, battleroyale.SlotLegs: inventory.SocketLeg,
		battleroyale.SlotHands: inventory.SocketArm, battleroyale.SlotFeet: inventory.SocketFoot,
		battleroyale.SlotWeapon: inventory.SocketWeapon, battleroyale.SlotShield: inventory.SocketShield,
		battleroyale.SlotEarring: inventory.SocketEarring, battleroyale.SlotNecklace: inventory.SocketNecklace,
		battleroyale.SlotRing: inventory.SocketRing, battleroyale.SlotRingSecond: inventory.SocketRingSecond,
	}[slot]
}

/*
================
battleRoyaleGear

The character's equipment rows as the planner reads them, with their
bag rows in the same order; false for an ordinary character.
================
*/
func (rt *Runtime) battleRoyaleGear(c *enterworld.Character) (battleroyale.Killer, []battleroyale.GearPiece, []inventory.Item, bool) {
	build, ok := battleroyale.HeldBuild(c)
	items := rt.deps.ItemReferences()
	if !ok || items == nil || c.DeletePending || !enterworld.CharacterAlive(c) {
		return battleroyale.Killer{}, nil, nil, false
	}
	killer := battleroyale.Killer{Build: build, Level: rewardLevel(c),
		Female: enterworld.ResolveCharacterGenderIndex(c) == domain.GenderFemale}
	var pieces []battleroyale.GearPiece
	var rows []inventory.Item
	for _, row := range bagOf(c).Items() {
		ref, known := items.ItemRefByCodename(row.Codename)
		if !known || ref == nil || ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 ||
			ref.TypeIDs[2] == itemTID3JobSuit || ref.TypeIDs[2] == itemTID3Avatar {
			continue
		}
		worn := inventory.IsEquipmentSlot(row.Slot)
		if row.Slot == inventory.SocketShield && !build.Shield {
			continue
		}
		piece := battleroyale.GearPiece{Codename: row.Codename, Plus: row.Plus, Blues: len(row.MagicOptions), Worn: worn}
		for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
			value := (row.VarianceBits >> (field * varianceFieldBits)) & battleRoyaleWhiteMax
			piece.WhitePercent += int(value) * whitePercentMax / battleRoyaleWhiteMax
		}
		for _, option := range row.MagicOptions {
			piece.BlueValues += int64(option >> 32)
		}
		pieces, rows = append(pieces, piece), append(rows, row)
	}
	return killer, pieces, rows, true
}

/*
================
battleRoyaleAutoEquip

Seats, slot by slot, the best item of the plan where it is not worn yet
(a ring into the ring socket the plan names), one native move each,
re-reading the bag after every move; an item the equip gates refuse
stays in the bag. Reports the frames of every move made; false when
nothing moved (or for an ordinary character).
================
*/
func (rt *Runtime) battleRoyaleAutoEquip(division string, c *enterworld.Character) (OpResult, bool) {
	var out OpResult
	moved := false
	refused := map[uint8]bool{}
	for range battleRoyaleMaxEquipMoves {
		move, ok := rt.nextBattleRoyaleEquip(c, refused)
		if !ok {
			break
		}
		result := rt.applyInventoryMove(division, c, move)
		if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemMoveResponse || len(result.Frames[0].Payload) == 0 ||
			result.Frames[0].Payload[0] != wire.ResultSuccess {
			log.WithFields(log.Fields{"character": c.Name, "slot": move.SourceSlot}).Debug("battle royale: auto-equip refused by the move")
			refused[move.SourceSlot] = true
			continue
		}
		out.Frames = append(out.Frames, result.Frames...)
		out.Broadcast = append(out.Broadcast, result.Broadcast...)
		moved = true
	}
	return out, moved
}

/*
================
nextBattleRoyaleEquip

The first planned item that is not in its socket yet and passes the
equip gates, as an inventory move; false when the set is as planned.
================
*/
func (rt *Runtime) nextBattleRoyaleEquip(c *enterworld.Character, refused map[uint8]bool) (wire.ItemMoveRequest, bool) {
	killer, pieces, rows, ok := rt.battleRoyaleGear(c)
	if !ok {
		return wire.ItemMoveRequest{}, false
	}
	plan := battleroyale.PlanGear(killer, pieces)
	requirements := equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
	for slot, index := range plan.Wear {
		if index < 0 {
			continue
		}
		row := rows[index]
		socket := battleRoyaleSocketOf(battleroyale.SetSlot(slot))
		if row.Slot == socket || refused[row.Slot] {
			continue
		}
		// A ring worn in the other ring socket stays there.
		if pieces[index].Worn && (row.Slot == inventory.SocketRing || row.Slot == inventory.SocketRingSecond) &&
			(socket == inventory.SocketRing || socket == inventory.SocketRingSecond) {
			continue
		}
		if requirements.PreExclusivity(row) != nil || requirements.PostExclusivity(row) != nil {
			refused[row.Slot] = true
			continue
		}
		return wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: row.Slot, DestSlot: socket, Quantity: 1}, true
	}
	return wire.ItemMoveRequest{}, false
}

/*
================
battleRoyaleDiscard

Drops on the ground every bag item the plan discards (usable, and no
better than the worn one), one native ground drop each. Reports the
frames; false when nothing dropped.
================
*/
func (rt *Runtime) battleRoyaleDiscard(division string, c *enterworld.Character) (OpResult, bool) {
	killer, pieces, rows, ok := rt.battleRoyaleGear(c)
	if !ok {
		return OpResult{}, false
	}
	var out OpResult
	dropped := false
	worldKey := simulation.WorldKey(division, c.Name)
	for _, index := range battleroyale.PlanGear(killer, pieces).Discard {
		result := rt.applyGroundDrop(division, worldKey, c, rows[index].Slot)
		if len(result.Frames) == 0 || len(result.Broadcast) == 0 {
			continue
		}
		out.Frames = append(out.Frames, result.Frames...)
		out.Broadcast = append(out.Broadcast, result.Broadcast...)
		dropped = true
	}
	return out, dropped
}

/*
================
battleRoyaleGearPass

The periodic pass of one participant: equip, then discard, and push the
frames (the actor's to it, the visuals and ground spawns to its peers).
================
*/
func (rt *Runtime) battleRoyaleGearPass(division string, c *enterworld.Character) {
	if result, equipped := rt.battleRoyaleAutoEquip(division, c); equipped {
		rt.pushBattleRoyaleEquip(division, c, result)
	}
	if result, dropped := rt.battleRoyaleDiscard(division, c); dropped {
		rt.pushBattleRoyaleEquip(division, c, result)
	}
}
