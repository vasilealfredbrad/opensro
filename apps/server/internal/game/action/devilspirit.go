/*
===========================================================================

devilspirit.go - the Devil's Spirit's instance rules in the action runtime

Port-only, not native (game/item/devilspirit, SRO_DEVIL_SPIRIT). The
package owns the state of one instance (its awakening, timed enhancement
and derived options); this file applies it where items live:

  - making one (the GM console, the battle royale airdrop): a new
    instance with its MATTR_NASRUN_UMDU value;

  - wearing one (avatartransfer.go): the first wear awakens it, an
    expired one refuses;

  - the Awakening Enhancement Scroll and the extension item, dropped on a
    Devil's Spirit in the bag (0x75BD with a target slot, the gender
    tool's shape their rows are cloned from);

  - the sweep: once a second for each resident, every Devil's Spirit in
    the bag and the avatar storage is brought up to date (an expired
    binding or awakening), persisted, and told to the client: a bag item
    by the v1.150 item-state delta 0x3645, a worn one by 0x3078 (below),
    with the stats refreshed when a worn one changed.

  - the devil skills (game/item/devilspirit/skills.go): the skills of the
    worn Devil's Spirit's effective level are learned while it is worn
    (the v1.150 learn answer 0xB2CB, unrequested, as the battle royale's
    free grants), and withdrawn when it comes off, expires or changes
    level (0x3078 kind 5).

0x3078 is the later client's avatar-special notification, its body
[u8 kind][u8 slot]...: kind 2 an extension ([u32 remaining seconds]),
kind 3 an expiry. v1.150 has no such packet; the port adds the two
native kinds and two of its own: kind 4, a worn item's state in the
0x3645 body layout ([u8 avatar slot][u8 mask][fields]), since 0x3645
addresses bag slots only, and kind 5, [u32 skill id] withdrawn (v1.150's
withdrawal answer 0xB4D6 only closes a requested withdrawal).

With the flag off no item is a Devil's Spirit, the scroll or the
extension, so none of this runs.

===========================================================================
*/
package action

import (
	"strconv"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// OpDevilSpiritState is the later client's avatar-special notification.
	OpDevilSpiritState uint16 = 0x3078

	devilSpiritKindExtended = 2
	devilSpiritKindExpired  = 3
	devilSpiritKindState    = 4 // port-only
	devilSpiritKindForget   = 5 // port-only: a devil skill withdrawn

	// The 0x3645 fields a Devil's Spirit's state rides in (7654B0 flags):
	// plus 2, variance 4, durability 16, magic options 32.
	devilSpiritStateMask uint8 = 2 | 4 | 16 | 32
	// itemStateQuantityFlag is 0x3645's stack count (flag 8).
	itemStateQuantityFlag uint8 = 8

	// The scroll and extension refusals (0xB5BD [2][code]). 0xDD is the
	// later server's failed roll (0x18DD); the port adds the others next
	// to it for its own target checks.
	devilSpiritErrFailed     uint8 = 0xdd
	devilSpiritErrTarget     uint8 = 0xde
	devilSpiritErrNotAwake   uint8 = 0xdf
	devilSpiritErrExpired    uint8 = 0xe0
	devilSpiritErrNotForUse  uint8 = 0xe1
	devilSpiritSweepInterval int64 = 1000
	millisPerSecond          int64 = 1000
)

//============================================================================

/*
================
devilSpiritOfItem
================
*/
func devilSpiritOfItem(item inventory.Item) devilspirit.Instance {
	return devilspirit.Instance{RefObjID: item.RefObjID, Plus: item.Plus, Variance: item.VarianceBits,
		Durability: item.Durability, Options: item.MagicOptions}
}

/*
================
setDevilSpiritItem
================
*/
func setDevilSpiritItem(item *inventory.Item, in devilspirit.Instance) {
	item.Plus, item.VarianceBits, item.Durability = in.Plus, in.Variance, in.Durability
	item.MagicOptions = append([]uint64(nil), in.Options...)
}

/*
================
devilSpiritOfRow
================
*/
func devilSpiritOfRow(row enterworld.InventoryRow) devilspirit.Instance {
	variance, err := strconv.ParseUint(row.VarianceBits, 10, 64)
	if err != nil {
		variance = 0
	}
	return devilspirit.Instance{RefObjID: row.RefObjID, Plus: uint8(clampInt64(row.Plus, 0, 0xff)), Variance: variance,
		Durability: uint32(clampInt64(row.Durability, 0, 0xffffffff)), Options: row.MagicOptions}
}

/*
================
setDevilSpiritRow
================
*/
func setDevilSpiritRow(row *enterworld.InventoryRow, in devilspirit.Instance) {
	row.Plus, row.VarianceBits, row.Durability = int64(in.Plus), strconv.FormatUint(in.Variance, 10), int64(in.Durability)
	row.MagicOptions = append([]uint64(nil), in.Options...)
}

/*
================
devilSpiritUmdu

The MATTR_NASRUN_UMDU percent a new instance is made with, drawn from the
option's values with the drop roll (devilspirit.UmduValues). Without a
roll source it is made without one.
================
*/
func (rt *Runtime) devilSpiritUmdu() uint8 {
	values := devilspirit.UmduValues()
	if rt.DropRoll == nil || len(values) == 0 {
		return 0
	}
	n, err := rt.DropRoll()
	if err != nil {
		return 0
	}
	return values[n%uint32(len(values))]
}

/*
================
makeDevilSpirit

Turns a freshly built item of a Devil's Spirit row into a new instance:
its base OptLevel (item.Plus) kept, its UMDU drawn, awakened for good
when permanent. Any other item is left alone.
================
*/
func (rt *Runtime) makeDevilSpirit(item *inventory.Item, permanent bool) {
	if !devilspirit.Is(item.RefObjID) {
		return
	}
	in := devilspirit.Make(item.RefObjID, rt.devilSpiritUmdu(), permanent, rt.Now().Unix())
	in.Plus = item.Plus
	in, _ = devilspirit.Sync(in, rt.Now().Unix())
	setDevilSpiritItem(item, in)
}

//============================================================================

/*
================
devilSpiritStateBody

[u8 slot][u8 mask][u8 plus][u64 variance][u32 durability][u8 count]
[count x u64]: the 0x3645 body of a Devil's Spirit's state fields.
================
*/
func devilSpiritStateBody(slot uint8, in devilspirit.Instance) []byte {
	options := in.Options
	if len(options) > wire.MaxMagicOptionsPerItem {
		options = options[:wire.MaxMagicOptionsPerItem]
	}
	w := wire.NewWriter(2 + 1 + 8 + 4 + 1 + 8*len(options)).U8(slot).U8(devilSpiritStateMask).
		U8(in.Plus).U64(in.Variance).U32(in.Durability).U8(uint8(len(options)))
	for _, option := range options {
		w.U64(option)
	}
	return w.Payload()
}

/*
================
bagDevilSpiritFrame
================
*/
func bagDevilSpiritFrame(slot uint8, in devilspirit.Instance) wire.Frame {
	return wire.Frame{Opcode: cosItemStateOpcode, Payload: devilSpiritStateBody(slot, in)}
}

/*
================
wornDevilSpiritFrame
================
*/
func wornDevilSpiritFrame(slot uint8, in devilspirit.Instance) wire.Frame {
	body := devilSpiritStateBody(slot, in)
	return wire.Frame{Opcode: OpDevilSpiritState, Payload: append([]byte{devilSpiritKindState}, body...)}
}

/*
================
devilSpiritExpiredFrame
================
*/
func devilSpiritExpiredFrame(slot uint8) wire.Frame {
	return wire.Frame{Opcode: OpDevilSpiritState, Payload: []byte{devilSpiritKindExpired, slot}}
}

/*
================
devilSpiritExtendedFrame
================
*/
func devilSpiritExtendedFrame(slot uint8, remaining uint32) wire.Frame {
	return wire.Frame{Opcode: OpDevilSpiritState, Payload: wire.NewWriter(6).U8(devilSpiritKindExtended).U8(slot).U32(remaining).Payload()}
}

//============================================================================

/*
================
useDevilSpiritEtc

The Awakening Enhancement Scroll (upgrade) or the extension item dropped
on a bag item, inside the item use's character Update. The target must be
a Devil's Spirit in the bag ("Cannot be used while item is equipped").

  - the scroll: the target must be awakened and not expired; the roll
    (devilspirit.UpgradeLevel) gives a binding level for the scroll's
    3 hours, or 0, the failure 0x18DD. The scroll is spent either way
    (INFERENCE: the original rolls after taking it; a failure that cost
    nothing would make the table's 10 % failure share a free retry);
  - the extension: the target must be awakened; its period becomes
    max(current, now) + 28 days, and 0x3078 kind 2 tells the client.

================
*/
func (rt *Runtime) useDevilSpiritEtc(use skillItemUse, c *enterworld.Character, tail []byte, upgrade bool, result *OpResult) bool {
	r := wire.NewReader(tail)
	target, e := r.U8()
	if e != nil || r.Done() != nil || !inventory.IsBagSlot(target, inventory.BagEnd(c)) {
		*result = itemUseFailure(devilSpiritErrTarget)
		return false
	}
	index := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(target) {
			index = i
			break
		}
	}
	if index < 0 || !devilspirit.Is(c.MissionInventory[index].RefObjID) {
		*result = itemUseFailure(devilSpiritErrTarget)
		return false
	}
	now := rt.Now().Unix()
	in, _ := devilspirit.Sync(devilSpiritOfRow(c.MissionInventory[index]), now)
	state := devilspirit.Decode(in)
	switch {
	case !state.Awakened():
		*result = itemUseFailure(devilSpiritErrNotAwake)
		return false
	case upgrade && state.Expired(now):
		*result = itemUseFailure(devilSpiritErrExpired)
		return false
	case !upgrade && state.Permanent():
		*result = itemUseFailure(devilSpiritErrNotForUse)
		return false
	}
	var frames []wire.Frame
	if upgrade {
		var draw uint32
		if rt.DropRoll != nil {
			n, err := rt.DropRoll()
			if err != nil {
				return false
			}
			draw = n
		}
		level := devilspirit.UpgradeLevel(draw)
		if level == 0 {
			remaining := rt.consumeItemUseRow(c, use.row)
			*result = OpResult{Frames: []wire.Frame{
				{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseError(devilSpiritErrFailed)},
				{Opcode: cosItemStateOpcode, Payload: wire.NewWriter(4).U8(use.request.Slot).U8(itemStateQuantityFlag).U16(remaining).Payload()},
			}}
			return true
		}
		upgraded, err := devilspirit.Upgrade(in, level, devilspirit.UpgradeSeconds(), now)
		if err != nil {
			*result = itemUseFailure(devilSpiritErrTarget)
			return false
		}
		in = upgraded
	} else {
		extended, err := devilspirit.Extend(in, devilspirit.ExtensionSeconds(), now)
		if err != nil {
			*result = itemUseFailure(devilSpiritErrNotAwake)
			return false
		}
		in = extended
		frames = append(frames, devilSpiritExtendedFrame(target, devilspirit.Decode(in).Remaining(now)))
	}
	// Change the target while index still names it: spending the item's
	// last unit rebuilds the bag (gendertool.go).
	setDevilSpiritRow(&c.MissionInventory[index], in)
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = OpResult{Frames: append([]wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		bagDevilSpiritFrame(target, in),
	}, frames...)}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

//============================================================================

/*
================
syncDevilSpirits

Brings every Devil's Spirit of c's bag and avatar storage up to date at
now, in place. It returns the client frames (0x3645 for the bag, 0x3078
for the avatar storage) and whether a worn one changed, which moves the
stats. The caller holds the character door.
================
*/
func syncDevilSpirits(c *enterworld.Character, now int64) (frames []wire.Frame, wornChanged bool) {
	bag := c.MissionInventory
	for i := range bag {
		if !devilspirit.Is(bag[i].RefObjID) || bag[i].Slot < int64(inventory.EquipmentSlotEnd) {
			continue
		}
		in, changed := devilspirit.Sync(devilSpiritOfRow(bag[i]), now)
		if !changed {
			continue
		}
		if frames == nil {
			bag = append([]enterworld.InventoryRow(nil), bag...)
		}
		setDevilSpiritRow(&bag[i], in)
		frames = append(frames, bagDevilSpiritFrame(uint8(bag[i].Slot), in))
	}
	c.MissionInventory = bag
	if c.AvatarInventory == nil {
		return frames, false
	}
	rows := c.AvatarInventory.Rows
	copied := false
	for i := range rows {
		if !devilspirit.Is(rows[i].RefObjID) {
			continue
		}
		before := devilSpiritOfRow(rows[i])
		in, changed := devilspirit.Sync(before, now)
		if !changed {
			continue
		}
		if !copied {
			rows = append([]enterworld.InventoryRow(nil), rows...)
			copied = true
		}
		setDevilSpiritRow(&rows[i], in)
		slot := uint8(rows[i].Slot)
		// The options were live until this sweep: the awakening has just
		// run out (an expired instance carries none).
		if len(before.Options) != 0 && devilspirit.Decode(in).Expired(now) {
			frames = append(frames, devilSpiritExpiredFrame(slot))
		}
		frames = append(frames, wornDevilSpiritFrame(slot, in))
		wornChanged = true
	}
	if copied {
		next := *c.AvatarInventory
		next.Rows = rows
		c.AvatarInventory = &next
	}
	return frames, wornChanged
}

/*
================
advanceResidentDevilSpirit

The once-a-second sweep for one resident (naturalrecovery.go's loop):
syncDevilSpirits under the character door, then the frames and, when a
worn one changed, the refreshed stats.
================
*/
func (rt *Runtime) advanceResidentDevilSpirit(key recoveryKey, nowMs int64) []simulation.DivisionFrames {
	rt.recoveryMu.Lock()
	state := rt.recoverySessions[key]
	if state == nil || nowMs < state.devilSpiritNextMs {
		rt.recoveryMu.Unlock()
		return nil
	}
	state.devilSpiritNextMs = nowMs + devilSpiritSweepInterval
	c := state.character
	rt.recoveryMu.Unlock()
	if c == nil || !hasDevilSpirit(c) && !hasDevilSkill(c) {
		return nil
	}
	var frames []wire.Frame
	rt.deps.UpdateDeferred(c, "devil-spirit-sweep", func() bool {
		changed, worn := syncDevilSpirits(c, nowMs/millisPerSecond)
		changed = append(changed, devilSkillFrames(c, nowMs/millisPerSecond)...)
		if len(changed) == 0 {
			return false
		}
		frames = changed
		if worn {
			hp, mp := rt.clampStoredGaugeToKeeper(key.division, c)
			frames = append(frames, rt.gaugeDropFrames(key.division, c, hp, mp, true)...)
		}
		return true
	})
	if len(frames) == 0 {
		return nil
	}
	return []simulation.DivisionFrames{{DivisionID: key.division, OnlyCharacterID: c.ID, Frames: simFrames(frames)}}
}

/*
================
hasDevilSpirit

Whether c holds a Devil's Spirit in the bag or the avatar storage: the
sweep's cheap gate (read under the division door, as the other resident
pulses read their character).
================
*/
func hasDevilSpirit(c *enterworld.Character) bool {
	for _, row := range c.MissionInventory {
		if devilspirit.Is(row.RefObjID) {
			return true
		}
	}
	if c.AvatarInventory != nil {
		for _, row := range c.AvatarInventory.Rows {
			if devilspirit.Is(row.RefObjID) {
				return true
			}
		}
	}
	return false
}

//============================================================================

/*
================
devilSkillFrames

Brings c's learned devil skills in line with what it wears at now: the
skills of every worn, unexpired Devil's Spirit's effective level
(devilspirit.Skills), no other devil skill. Withdrawn ones are told first
(0x3078 kind 5), then the learned (0xB2CB), so a level change that swaps
a skill of one group (Ignore Monster's defense 1 to 2) reads as one
upgrade. The caller holds the character door and persists c.
================
*/
func devilSkillFrames(c *enterworld.Character, now int64) []wire.Frame {
	want := map[uint32]bool{}
	if c.AvatarInventory != nil {
		for _, row := range c.AvatarInventory.Rows {
			if devilspirit.Is(row.RefObjID) {
				for _, id := range devilspirit.Skills(devilSpiritOfRow(row), now) {
					want[id] = true
				}
			}
		}
	}
	var frames []wire.Frame
	kept := make([]uint32, 0, len(c.Skills))
	have := map[uint32]bool{}
	for _, id := range c.Skills {
		if devilspirit.IsSkill(id) && !want[id] {
			frames = append(frames, wire.Frame{Opcode: OpDevilSpiritState,
				Payload: wire.NewWriter(5).U8(devilSpiritKindForget).U32(id).Payload()})
			continue
		}
		kept = append(kept, id)
		have[id] = true
	}
	for _, skill := range devilspirit.AllSkills() {
		if !want[skill.ID] || have[skill.ID] {
			continue
		}
		kept = append(kept, skill.ID)
		frames = append(frames, wire.Frame{Opcode: wire.OpSkillLearnResponse, Payload: wire.EncodeSkillLearnAck(skill.ID)})
	}
	if len(frames) != 0 {
		c.Skills = kept
	}
	return frames
}

/*
================
hasDevilSkill
================
*/
func hasDevilSkill(c *enterworld.Character) bool {
	for _, id := range c.Skills {
		if devilspirit.IsSkill(id) {
			return true
		}
	}
	return false
}
