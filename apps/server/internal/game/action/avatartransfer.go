/*
===========================================================================

avatartransfer.go - moving an avatar between the bag and its sockets

The 0x7034 avatar moves (bag to avatar storage and back): the socket and
dress rules, the attachment that leaves with its dress, and the visuals
and stat refresh a worn avatar changes.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
needsDress

An avatar attachment (TID4 3) is worn only over a worn avatar dress, and
leaves with it. Port-only, not native: a Devil's Spirit row
(game/item/devilspirit) sits in the attachment socket but, as in the
later client it comes from, needs no dress.
================
*/
func needsDress(item inventory.Item) bool {
	return item.TypeFlags>>11 == 3 && !devilspirit.Is(item.RefObjID)
}

/*
================
applyAvatarTransfer

594980: avatar records occupy independent storage slots; their TID4 selects
the visible socket. Occupied costume sockets refuse replacement.
================
*/
func (rt *Runtime) applyAvatarTransfer(divisionID string, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	result := failureResult(wire.ErrCodeInvalidRequest)
	rt.deps.Update(c, "avatar-transfer", func() bool {
		if c.DeletePending {
			return false
		}
		rows := []enterworld.InventoryRow(nil)
		if c.AvatarInventory != nil {
			rows = c.AvatarInventory.Rows
		}
		avatars, fault := inventory.NewContainer(invItemsFromRowsWithin(rows, 4), 4)
		if fault != nil {
			return false
		}
		player := bagOf(c)
		source, dest := player, avatars
		equip := q.MovementType == wire.MoveTypePlayerToAvatar
		if !equip {
			source, dest = avatars, player
		}
		item, ok := source.At(q.SourceSlot)
		if !ok || item.TypeFlags&0x7fe != 0x6ac || item.TypeFlags>>11 < 1 || item.TypeFlags>>11 > 4 {
			return false
		}
		if equip {
			if _, occupied := dest.At(q.DestSlot); occupied {
				return false
			}
		} else {
			// Retail 525AC0/525B42 owns destination selection, ignoring the
			// requested bag cell. Reserve it before finding an attachment cell.
			if q.DestSlot < inventory.EquipmentSlotEnd || q.DestSlot >= inventory.BagEnd(c) {
				return false
			}
			var free bool
			q.DestSlot, free = player.FirstFreeBagSlot()
			if !free {
				result = failureResult(wire.ErrCodeStorageFull)
				return false
			}
		}
		if equip {
			dress := false
			for _, worn := range avatars.Items() {
				if worn.TypeFlags>>11 == item.TypeFlags>>11 {
					return false
				}
				if worn.TypeFlags>>11 == 2 {
					dress = true
				}
			}
			if needsDress(item) && !dress {
				return false
			}
			// Port-only: an expired Devil's Spirit cannot be worn ("You
			// cannot equip the item if the awakening period has expired").
			if devilspirit.Is(item.RefObjID) && devilspirit.Decode(devilSpiritOfItem(item)).Expired(rt.Now().Unix()) {
				return false
			}
			requirements := equipRequirements(rt.deps.ItemReferences(), c, rt.FortressGuildRole)
			if requirements != nil {
				if requirements.PreExclusivity(item) != nil || requirements.PostExclusivity(item) != nil {
					return false
				}
			}
		}
		// All mutations below are on private containers. Never publish a dress
		// removal unless its dependent attachment has somewhere to go too.
		if fault := source.TransferWholeTo(dest, q.SourceSlot, q.DestSlot, 1); fault != nil {
			return false
		}
		var attachment *inventory.Item
		var attachmentDestination uint8
		if !equip && item.TypeFlags>>11 == 2 {
			for _, worn := range avatars.Items() {
				if !needsDress(worn) {
					continue
				}
				free, ok := player.FirstFreeBagSlot()
				if !ok {
					result = failureResult(wire.ErrCodeStorageFull)
					return false
				}
				if fault := avatars.TransferWholeTo(player, worn.Slot, free, 1); fault != nil {
					return false
				}
				attachment, attachmentDestination = &worn, free
				break
			}
		}

		nextBag := rowsFromInvItems(player.Items())
		nextAvatars := &domain.AvatarInventory{Capacity: 4, Rows: rowsFromInvItems(avatars.Items())}
		// Port-only: the first wear of a Devil's Spirit awakens it
		// (devilspirit.Awaken); the client learns its new state below.
		var awakened *devilspirit.Instance
		if equip && devilspirit.Is(item.RefObjID) {
			for i := range nextAvatars.Rows {
				if nextAvatars.Rows[i].Slot != int64(q.DestSlot) {
					continue
				}
				in := devilspirit.Awaken(devilSpiritOfRow(nextAvatars.Rows[i]), rt.Now().Unix())
				in, _ = devilspirit.Sync(in, rt.Now().Unix())
				setDevilSpiritRow(&nextAvatars.Rows[i], in)
				awakened = &in
			}
		}
		// 50F1F0 installs or removes the avatar's contributions (4E3760 /
		// 4E3860) and the actor's stats refresh, as for equipment: the
		// blessed options count only while the avatar is worn. Derived on
		// the next snapshot so a broken record refuses before anything moves.
		next := c.Snapshot()
		next.MissionInventory, next.AvatarInventory = nextBag, nextAvatars
		display, err := rt.PlayerBaseStats(divisionID, next)
		if err != nil {
			log.Warnf("action: avatar move %d->%d refused - %v", q.SourceSlot, q.DestSlot, err)
			return false
		}
		statFrame := wire.Frame{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(next, display)}
		c.MissionInventory = nextBag
		c.AvatarInventory = nextAvatars
		visual := wire.UnequipVisualFrame(wire.UnequipVisual{Gid: enterworld.ObjectIDForCharacter(c), Slot: q.SourceSlot, RefObjID: item.RefObjID})
		if equip {
			visual = wire.EquipVisualFrame(wire.EquipVisual{Gid: enterworld.ObjectIDForCharacter(c), RefObjID: item.RefObjID, TypeFlags: item.TypeFlags, OptLevel: item.Plus})
		}
		count := uint8(0)
		if attachment != nil {
			count = 1
		}
		payload := wire.NewWriter(12).U8(1).U8(q.MovementType).U8(q.SourceSlot).U8(q.DestSlot).U16(1).U8(count)
		if attachment != nil {
			payload.U8(wire.MoveTypeAvatarToPlayer).U8(attachment.Slot).U8(attachmentDestination).U16(1)
		}
		result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: payload.Payload()}}}
		// 525C50 removes the attachment before the main dress continuation.
		if attachment != nil {
			result.Frames = append(result.Frames, wire.UnequipVisualFrame(wire.UnequipVisual{Gid: enterworld.ObjectIDForCharacter(c), Slot: attachment.Slot, RefObjID: attachment.RefObjID}))
		}
		// Every audience, the owner included, gets a worn avatar's reference
		// right before the visual names it (peer_references.go): 777800
		// resolves the item for any gid, and a browser that lacks it ends
		// the session ("Unknown peer equipment reference").
		if equip {
			result.Frames = append(result.Frames, rt.itemReferenceFrames([]inventory.Item{item})...)
		}
		result.Frames = append(result.Frames, visual)
		// Viewers see the dress change as the owner does (applyInventoryMove).
		// A copy: the owner's stats frame is appended to Frames below.
		result.Broadcast = append([]wire.Frame(nil), result.Frames[1:]...)
		// The stats are the owner's alone, after the move and visuals.
		hp, mp := rt.clampStoredGaugeToKeeper(divisionID, c)
		if awakened != nil {
			result.Frames = append(result.Frames, wornDevilSpiritFrame(q.DestSlot, *awakened))
		}
		// Port-only: the devil skills follow the worn Devil's Spirit.
		result.Frames = append(result.Frames, devilSkillFrames(c, rt.Now().Unix())...)
		result.Frames = append(result.Frames, statFrame)
		result.Frames = append(result.Frames, rt.gaugeDropFrames(divisionID, c, hp, mp, false)...)
		return true
	})
	return result
}
