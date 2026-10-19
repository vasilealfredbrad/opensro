/*
===========================================================================

devilspirit_test.go - a port-only Devil's Spirit made, worn, enhanced,
extended and swept

===========================================================================
*/
package action

import (
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// devilSpiritTestID is the later client's ITEM_MALL_AVATAR_M_NASRUN id.
	devilSpiritTestID = 24385
	// devilDressTestID / devilDressTestSlot: the avatar dress the attachment needs.
	devilDressTestID   = 23438
	devilDressTestSlot = 21
	// devilScrollTestID / devilExtensionTestID are the scroll and extension rows.
	devilScrollTestID    = 39738
	devilExtensionTestID = 24395
	// devilUmduDraw is a drop roll that picks UMDU 8 % (the fourth value).
	devilUmduDraw = 3
	devilUmdu     = 8
	// awakeSeconds is the row's period, 28 days.
	awakeSeconds = 2419200
)

/*
================
devilOptionSource

The magic option rows a Devil's Spirit's options resolve against.
================
*/
type devilOptionSource map[uint32]*enterworld.MagicOptionRow

/*
================
MagicOptionByParamID
================
*/
func (s devilOptionSource) MagicOptionByParamID(id uint32) (*enterworld.MagicOptionRow, bool) {
	row, ok := s[id]
	return row, ok
}

/*
================
devilOptions

The MATTR_NASRUN_* rows devilspirit.Apply adds.
================
*/
func devilOptions() devilOptionSource {
	source := devilOptionSource{}
	for _, row := range devilspirit.MagicOptionRows() {
		copied := row
		source[row.ParamID] = &copied
	}
	return source
}

/*
================
devilEtcRef

The scroll or the extension as devilspirit.Apply clones it from the
gender tool (3/3/13/8).
================
*/
func devilEtcRef(id uint32, codename string) *enterworld.ItemRef {
	return &enterworld.ItemRef{RefObjID: id, Codename: codename, Country: 3, TypeIDs: [4]int64{3, 3, 13, 8},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "maxStack": 50})}
}

/*
================
devilAvatarRefs

A retail attachment, an avatar dress, a Devil's Spirit, the scroll and
the extension, as the item references the handlers read.
================
*/
func devilAvatarRefs() staticItemSource {
	none := [4]int64{-1, -1, -1, -1}
	return staticItemSource{
		"ITEM_MALL_AVATAR_M_NASRUN": {RefObjID: devilSpiritTestID, Codename: "ITEM_MALL_AVATAR_M_NASRUN",
			TypeIDs: [4]int64{3, 1, 13, 3}, Country: 3, RequiredSex: 2, ReqQuadTypes: none,
			PortOnlyMagicOptions: devilspirit.Options(devilSpiritTestID, 0, 0)},
		"ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH": {RefObjID: 23440, Codename: "ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH",
			TypeIDs: [4]int64{3, 1, 13, 3}, Country: 3, RequiredSex: 2, ReqQuadTypes: none},
		"ITEM_MALL_AVATAR_M_HALLOWEEN": {RefObjID: devilDressTestID, Codename: "ITEM_MALL_AVATAR_M_HALLOWEEN",
			TypeIDs: [4]int64{3, 1, 13, 2}, Country: 3, RequiredSex: 2, ReqQuadTypes: none},
		"ITEM_ETC_NASRUN_UPGRADE_SCROLL": devilEtcRef(devilScrollTestID, "ITEM_ETC_NASRUN_UPGRADE_SCROLL"),
		"ITEM_MALL_NASRUN_EXTENSION":     devilEtcRef(devilExtensionTestID, "ITEM_MALL_NASRUN_EXTENSION"),
	}
}

/*
================
devilRuntime

A test runtime over devilAvatarRefs with the NASRUN option rows.
================
*/
func devilRuntime(c *enterworld.Character) (*Runtime, *fakeClock) {
	rt, clock := newTestRuntime(c, devilAvatarRefs())
	deps := rt.deps.(*enterworld.Deps)
	deps.MagicOptions = devilOptions()
	// The devil skills a worn Devil's Spirit teaches, as the stat
	// projection resolves every learned skill.
	skills := staticSkillSource{}
	for _, skill := range devilspirit.AllSkills() {
		skills[skill.ID] = enterworld.SkillRow{ID: skill.ID, Codename: skill.Codename}
	}
	deps.Skills = skills
	return rt, clock
}

/*
================
devilAvatarRow
================
*/
func devilAvatarRow(slot int64, refObjID uint32, codename string, tid4 uint8) enterworld.InventoryRow {
	return enterworld.InventoryRow{Slot: slot, RefObjID: refObjID, Codename: codename,
		TypeFlags: wire.PackTypeFlags(3, 1, 13, tid4), VarianceBits: "0", StackCount: 1}
}

/*
================
devilSpiritRow

A Devil's Spirit in slot made by devilspirit.Make at now (awakened when
awake).
================
*/
func devilSpiritRow(slot int64, awake bool, now int64) enterworld.InventoryRow {
	row := devilAvatarRow(slot, devilSpiritTestID, "ITEM_MALL_AVATAR_M_NASRUN", 3)
	in := devilspirit.Make(devilSpiritTestID, devilUmdu, false, now)
	if awake {
		in = devilspirit.Awaken(in, now)
	}
	setDevilSpiritRow(&row, in)
	return row
}

/*
================
devilEtcRow
================
*/
func devilEtcRow(slot int64, ref *enterworld.ItemRef, count int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: count}
}

/*
================
TestGMMadeDevilSpiritIsAwakenedWhenFirstWorn

Port-only, not native: /MAKEITEM of a Devil's Spirit drops a new
instance: its level-0 options with the UMDU the drop roll picked, not yet
awakened. Picked up and worn (here over an avatar dress), the first wear
awakens it for the row's 28 days and tells the client (0x3078 kind 4); the
worn record is what the stat projection reads.
================
*/
func TestGMMadeDevilSpiritIsAwakenedWhenFirstWorn(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	refs := rt.deps.ItemReferences().(staticItemSource)
	for codename, ref := range devilAvatarRefs() {
		refs[codename] = ref
	}
	deps.Items = gmItemSource{refs}
	deps.MagicOptions = devilOptions()
	rt.DropRoll = func() (uint32, error) { return devilUmduDraw, nil }
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	c.GMPrivilege = true
	c.MissionInventory = []enterworld.InventoryRow{devilAvatarRow(devilDressTestSlot, devilDressTestID, "ITEM_MALL_AVATAR_M_HALLOWEEN", 2)}
	if !rt.MakeGMItem(testDivision, c.Name, devilSpiritTestID, 0) {
		t.Fatal("the Devil's Spirit was not made")
	}
	ground := rt.Ground.All(testDivision)[0]
	if want := devilspirit.Options(devilSpiritTestID, 0, devilUmdu); !reflect.DeepEqual(ground.MagicOptions, want) || ground.Durability != 0 {
		t.Fatalf("made %+v, want options %x and no awakening", ground, want)
	}
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: ground.Gid}.Encode())
	var slot int64 = -1
	for _, row := range c.MissionInventory {
		if row.RefObjID == devilSpiritTestID {
			slot = row.Slot
		}
	}
	if slot < 0 {
		t.Fatal("the Devil's Spirit did not reach the bag")
	}
	rt.HandleItemMove(testDivision, c, []byte{0x24, devilDressTestSlot, 0})
	out := rt.HandleItemMove(testDivision, c, []byte{0x24, byte(slot), 1})
	if c.AvatarInventory == nil || len(c.AvatarInventory.Rows) != 2 {
		t.Fatalf("worn avatars: %+v", c.AvatarInventory)
	}
	worn := c.AvatarInventory.Rows[1]
	now := rt.Now().Unix()
	if worn.RefObjID != devilSpiritTestID || worn.Durability != now+awakeSeconds ||
		!reflect.DeepEqual(worn.MagicOptions, devilspirit.Options(devilSpiritTestID, 0, devilUmdu)) {
		t.Fatalf("worn row %+v", worn)
	}
	state, ok := findFrame(out.Frames, OpDevilSpiritState)
	if !ok || state.Payload[0] != devilSpiritKindState || state.Payload[1] != 1 {
		t.Fatalf("no worn state frame in %+v", out.Frames)
	}
	for _, frame := range out.Broadcast {
		if frame.Opcode == OpDevilSpiritState {
			t.Fatal("the worn state reached the observers")
		}
	}
}

/*
================
TestDevilSpiritIsWornWithoutAnAvatarDress

Port-only, not native: a retail attachment still needs a worn avatar
dress (the native rule), but a Devil's Spirit, which the later client
wears on its own, goes from the bag into the attachment socket with no
dress, through the real 0x7034 handler.
================
*/
func TestDevilSpiritIsWornWithoutAnAvatarDress(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		devilAvatarRow(20, 23440, "ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH", 3),
		devilAvatarRow(21, devilSpiritTestID, "ITEM_MALL_AVATAR_M_NASRUN", 3),
	}
	rt, _ := devilRuntime(c)
	refused := rt.HandleItemMove(testDivision, c, []byte{0x24, 20, 0})
	if len(refused.Frames) != 1 || refused.Frames[0].Payload[0] != 2 || c.AvatarInventory != nil && len(c.AvatarInventory.Rows) != 0 {
		t.Fatalf("a retail attachment was worn without a dress: %+v", refused)
	}
	worn := rt.HandleItemMove(testDivision, c, []byte{0x24, 21, 0})
	if len(worn.Frames) == 0 || worn.Frames[0].Opcode != wire.OpItemMoveResponse || worn.Frames[0].Payload[0] != 1 {
		t.Fatalf("the Devil's Spirit was refused: %+v", worn)
	}
	if c.AvatarInventory == nil || len(c.AvatarInventory.Rows) != 1 || c.AvatarInventory.Rows[0].RefObjID != devilSpiritTestID {
		t.Fatalf("worn avatars: %+v", c.AvatarInventory)
	}
}

/*
================
TestDressRemovalLeavesTheDevilSpiritWorn

Port-only, not native: taking the dress off sends a retail attachment
back to the bag with it (native), but not a Devil's Spirit, which never
depended on the dress.
================
*/
func TestDressRemovalLeavesTheDevilSpiritWorn(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		devilAvatarRow(devilDressTestSlot, devilDressTestID, "ITEM_MALL_AVATAR_M_HALLOWEEN", 2),
		devilAvatarRow(22, devilSpiritTestID, "ITEM_MALL_AVATAR_M_NASRUN", 3),
	}
	rt, _ := devilRuntime(c)
	rt.HandleItemMove(testDivision, c, []byte{0x24, devilDressTestSlot, 0})
	rt.HandleItemMove(testDivision, c, []byte{0x24, 22, 1})
	if c.AvatarInventory == nil || len(c.AvatarInventory.Rows) != 2 {
		t.Fatalf("worn avatars: %+v", c.AvatarInventory)
	}
	off := rt.HandleItemMove(testDivision, c, []byte{0x23, 0, 30})
	if len(off.Frames) == 0 || off.Frames[0].Payload[0] != 1 || off.Frames[0].Payload[6] != 0 {
		t.Fatalf("dress removal: %+v", off)
	}
	if len(c.AvatarInventory.Rows) != 1 || c.AvatarInventory.Rows[0].RefObjID != devilSpiritTestID {
		t.Fatalf("worn avatars after the dress came off: %+v", c.AvatarInventory)
	}
}

/*
================
TestExpiredDevilSpiritCannotBeWorn

Past its period a Devil's Spirit stays in the bag.
================
*/
func TestExpiredDevilSpiritCannotBeWorn(t *testing.T) {
	c := testCharacter()
	rt, clock := devilRuntime(c)
	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, true, clock.now.Unix())}
	clock.now = clock.now.Add(awakeSeconds * time.Second)
	out := rt.HandleItemMove(testDivision, c, []byte{0x24, 21, 0})
	if len(out.Frames) == 0 || out.Frames[0].Payload[0] == 1 || c.AvatarInventory != nil && len(c.AvatarInventory.Rows) != 0 {
		t.Fatalf("an expired Devil's Spirit was worn: %+v", out)
	}
}

/*
================
devilUse

Uses the item in slot 25 on the bag item in slot 21.
================
*/
func devilUse(rt *Runtime, c *enterworld.Character, ref *enterworld.ItemRef) OpResult {
	return rt.HandleItemUse(testDivision, c, wire.NewWriter(4).U8(25).U16(ref.TypeFlags()).U8(21).Payload())
}

/*
================
devilBagRow
================
*/
func devilBagRow(c *enterworld.Character, slot int64) (enterworld.InventoryRow, bool) {
	for _, row := range c.MissionInventory {
		if row.Slot == slot {
			return row, true
		}
	}
	return enterworld.InventoryRow{}, false
}

/*
================
TestUpgradeScrollBindsALevelOrFails

Port-only: the scroll dropped on an awakened Devil's Spirit in the bag
binds the rolled level for 3 hours (the row's options follow, the client
gets 0x3645); the draw 0 fails with 0x18DD's low byte and spends the
scroll anyway. A not awakened target refuses and keeps the scroll.
================
*/
func TestUpgradeScrollBindsALevelOrFails(t *testing.T) {
	c := testCharacter()
	rt, clock := devilRuntime(c)
	now := clock.now.Unix()
	scroll := devilAvatarRefs()["ITEM_ETC_NASRUN_UPGRADE_SCROLL"]

	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, false, now), devilEtcRow(25, scroll, 3)}
	if out := devilUse(rt, c, scroll); out.Frames[0].Payload[0] == wire.ResultSuccess || out.Frames[0].Payload[1] != devilSpiritErrNotAwake {
		t.Fatalf("a not awakened target: %+v", out.Frames)
	}
	if row, _ := devilBagRow(c, 25); row.StackCount != 3 {
		t.Fatalf("a refused use spent the scroll: %+v", row)
	}

	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, true, now), devilEtcRow(25, scroll, 3)}
	rt.DropRoll = func() (uint32, error) { return 87, nil } // cumulative 85..89: level 6
	out := devilUse(rt, c, scroll)
	if out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use: %+v", out.Frames)
	}
	if _, ok := findFrame(out.Frames, cosItemStateOpcode); !ok {
		t.Fatalf("no item state in %+v", out.Frames)
	}
	row, _ := devilBagRow(c, 21)
	in := devilSpiritOfRow(row)
	if devilspirit.EffectiveLevel(in, now) != 6 || devilspirit.Decode(in).BindUntil != uint32(now)+10800 ||
		!reflect.DeepEqual(row.MagicOptions, devilspirit.Options(devilSpiritTestID, 6, devilUmdu)) {
		t.Fatalf("upgraded %+v", row)
	}
	if scrolls, _ := devilBagRow(c, 25); scrolls.StackCount != 2 {
		t.Fatalf("scrolls %+v", scrolls)
	}

	rt.DropRoll = func() (uint32, error) { return 100, nil }
	failed := devilUse(rt, c, scroll)
	if failed.Frames[0].Payload[0] == wire.ResultSuccess || failed.Frames[0].Payload[1] != devilSpiritErrFailed {
		t.Fatalf("failure: %+v", failed.Frames)
	}
	if scrolls, _ := devilBagRow(c, 25); scrolls.StackCount != 1 {
		t.Fatalf("a failure kept the scroll: %+v", scrolls)
	}
	if again, _ := devilBagRow(c, 21); !reflect.DeepEqual(again, row) {
		t.Fatal("a failure changed the target")
	}
}

/*
================
TestExtensionRenewsAnExpiredDevilSpirit

Port-only: an expired Devil's Spirit gives nothing; the extension gives it
28 days from now and its options back, with 0x3078 kind 2.
================
*/
func TestExtensionRenewsAnExpiredDevilSpirit(t *testing.T) {
	c := testCharacter()
	rt, clock := devilRuntime(c)
	extension := devilAvatarRefs()["ITEM_MALL_NASRUN_EXTENSION"]
	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, true, clock.now.Unix()), devilEtcRow(25, extension, 1)}
	clock.now = clock.now.Add(2 * awakeSeconds * time.Second)
	now := clock.now.Unix()
	out := devilUse(rt, c, extension)
	if out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("use: %+v", out.Frames)
	}
	extended, ok := findFrame(out.Frames, OpDevilSpiritState)
	if !ok || extended.Payload[0] != devilSpiritKindExtended || extended.Payload[1] != 21 {
		t.Fatalf("no extension notice in %+v", out.Frames)
	}
	row, _ := devilBagRow(c, 21)
	if row.Durability != now+awakeSeconds || len(row.MagicOptions) != 3 {
		t.Fatalf("extended %+v", row)
	}
	if _, used := devilBagRow(c, 25); used {
		t.Fatal("the extension was not spent")
	}
}

/*
================
TestSweepExpiresAWornDevilSpirit

Port-only: the resident sweep finds a worn Devil's Spirit past its period,
strips its options, persists it and sends 0x3078 kinds 3 and 4 and the
refreshed stats, and withdraws its devil skill (kind 5); a second sweep
has nothing to do.
================
*/
func TestSweepExpiresAWornDevilSpirit(t *testing.T) {
	c := testCharacter()
	rt, clock := devilRuntime(c)
	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, true, clock.now.Unix())}
	if out := rt.HandleItemMove(testDivision, c, []byte{0x24, 21, 0}); out.Frames[0].Payload[0] != 1 {
		t.Fatalf("wear: %+v", out.Frames)
	}
	rt.BindRecoverySession(testDivision, c, 1)
	clock.now = clock.now.Add(awakeSeconds * time.Second)
	key := recoveryKey{testDivision, "asd2"}
	frames := rt.advanceResidentDevilSpirit(key, clock.now.UnixMilli())
	if len(frames) != 1 {
		t.Fatalf("sweep frames %+v", frames)
	}
	var kinds []byte
	stats := false
	for _, frame := range frames[0].Frames {
		if frame.Opcode == OpDevilSpiritState {
			kinds = append(kinds, frame.Payload[0])
		}
		stats = stats || frame.Opcode == wire.OpBaseStats
	}
	if !reflect.DeepEqual(kinds, []byte{devilSpiritKindExpired, devilSpiritKindState, devilSpiritKindForget}) || !stats {
		t.Fatalf("kinds %v, stats %v", kinds, stats)
	}
	if worn := c.AvatarInventory.Rows[0]; len(worn.MagicOptions) != 0 {
		t.Fatalf("an expired Devil's Spirit kept its options: %+v", worn)
	}
	if again := rt.advanceResidentDevilSpirit(key, clock.now.UnixMilli()+devilSpiritSweepInterval); len(again) != 0 {
		t.Fatalf("a second sweep: %+v", again)
	}
}

/*
================
devilSkillIDs

The devil skills c has learned.
================
*/
func devilSkillIDs(c *enterworld.Character) []uint32 {
	var out []uint32
	for _, id := range c.Skills {
		if devilspirit.IsSkill(id) {
			out = append(out, id)
		}
	}
	return out
}

/*
================
TestDevilSkillsFollowTheWornDevilSpirit

Port-only: worn, a Devil's Spirit teaches its level's devil skill (0xB2CB);
when a live enhancement takes it to level 6 the sweep swaps in the stage-3
skills (0x3078 kind 5 for the old, 0xB2CB for the new); taken off, every
devil skill is withdrawn.
================
*/
func TestDevilSkillsFollowTheWornDevilSpirit(t *testing.T) {
	c := testCharacter()
	rt, clock := devilRuntime(c)
	now := clock.now.Unix()
	c.MissionInventory = []enterworld.InventoryRow{devilSpiritRow(21, true, now)}
	out := rt.HandleItemMove(testDivision, c, []byte{0x24, 21, 0})
	if got := devilSkillIDs(c); !reflect.DeepEqual(got, []uint32{31146}) {
		t.Fatalf("worn skills %v", got)
	}
	if learned, ok := findFrame(out.Frames, wire.OpSkillLearnResponse); !ok || !reflect.DeepEqual(learned.Payload, wire.EncodeSkillLearnAck(31146)) {
		t.Fatalf("no learn answer in %+v", out.Frames)
	}

	worn := &c.AvatarInventory.Rows[0]
	upgraded, err := devilspirit.Upgrade(devilSpiritOfRow(*worn), 6, 10800, now)
	if err != nil {
		t.Fatal(err)
	}
	setDevilSpiritRow(worn, upgraded)
	rt.BindRecoverySession(testDivision, c, 1)
	frames := rt.advanceResidentDevilSpirit(recoveryKey{testDivision, "asd2"}, clock.now.UnixMilli())
	if got := devilSkillIDs(c); !reflect.DeepEqual(got, []uint32{31148, 31150}) {
		t.Fatalf("level 6 skills %v", got)
	}
	var withdrawn, learned int
	for _, frame := range frames[0].Frames {
		switch {
		case frame.Opcode == OpDevilSpiritState && frame.Payload[0] == devilSpiritKindForget:
			withdrawn++
		case frame.Opcode == wire.OpSkillLearnResponse:
			learned++
		}
	}
	if withdrawn != 1 || learned != 2 {
		t.Fatalf("withdrawn %d, learned %d", withdrawn, learned)
	}

	rt.HandleItemMove(testDivision, c, []byte{0x23, 0, 30})
	if got := devilSkillIDs(c); len(got) != 0 {
		t.Fatalf("skills after taking it off %v", got)
	}
}

/*
================
TestDevilSkillIsCastOnlyWithTheDevilSpiritWorn

Port-only (licensed: the shipped skill data): the Malicious Devil Skill,
parsed from the later client's row, is refused without a worn Devil's
Spirit (its reqi, 0x300D) and, worn, released as a 10-minute buff of 20 %
damage and 10 % movement speed.
================
*/
func TestDevilSkillIsCastOnlyWithTheDevilSpiritWorn(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	skills := enterworld.NewTextdataSkills(dir)
	rows, err := devilspirit.SkillRows()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skills.AddPortOnlySkillRows(rows); err != nil {
		t.Fatal(err)
	}
	row, ok := skills.SkillByID(31146)
	if !ok || !row.TimedEffect.Pinned || !row.MovementModifier.Supported {
		t.Fatalf("the devil skill is not admitted: %+v", row.TimedEffect)
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(enterworld.DerivedMaxMP(c))
	refused := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(refused.Frames) != 1 || !reflect.DeepEqual(refused.Frames[0].Payload, []byte{2, 0x0d}) {
		t.Fatalf("cast without the Devil's Spirit: %+v", refused.Frames)
	}

	rt.deps.(*enterworld.Deps).MagicOptions = devilOptions()
	refs := rt.deps.ItemReferences().(staticItemSource)
	for codename, ref := range devilAvatarRefs() {
		refs[codename] = ref
	}
	c.AvatarInventory = &enterworld.AvatarInventory{Capacity: 4, Rows: []enterworld.InventoryRow{devilSpiritRow(0, true, clock.now.Unix())}}
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("cast with the Devil's Spirit: %+v %q", start.Frames, start.DiagnosticRefusal)
	}
	rt.advanceProjectileCasts(clock.NowMs() + int64(row.ActionCastingTimeMs) + 1)
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 || effects[0].SkillID != row.ID || !effects[0].Movement || effects[0].MovementPercent != 10 ||
		effects[0].ExpiresAtMs-effects[0].StartedAtMs != 600000 {
		t.Fatalf("installed %+v", effects)
	}
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil || after.PhysicalSkillRate != before.PhysicalSkillRate+20 || after.MagicalSkillRate != before.MagicalSkillRate+20 {
		t.Fatalf("rates %v/%v, before %v/%v (%v)", after.PhysicalSkillRate, after.MagicalSkillRate, before.PhysicalSkillRate, before.MagicalSkillRate, err)
	}
}

/*
================
TestDevilBuffFormIsPublishedToObservers

Port-only (licensed: the shipped skill data): the client draws a devil
buff's cast and devil form from the skill id alone (the later client's
skillaniset / skilleffectset / charactervisualchange rows, published by
the asset build), for its caster and every observer. So the Vallainous
Devil Skill's cast (0xB245) and its attached effect (0xB419: caster,
skill, token) must name 31148 and reach the caster's observed scope, not
only the caster; the form then lasts exactly as long as that effect.
================
*/
func TestDevilBuffFormIsPublishedToObservers(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	skills := enterworld.NewTextdataSkills(dir)
	rows, err := devilspirit.SkillRows()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skills.AddPortOnlySkillRows(rows); err != nil {
		t.Fatal(err)
	}
	const vallainous = 31148
	row, ok := skills.SkillByID(vallainous)
	if !ok {
		t.Fatal("the Vallainous Devil Skill is not admitted")
	}
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(enterworld.DerivedMaxMP(c))
	rt.deps.(*enterworld.Deps).MagicOptions = devilOptions()
	refs := rt.deps.ItemReferences().(staticItemSource)
	for codename, ref := range devilAvatarRefs() {
		refs[codename] = ref
	}
	c.AvatarInventory = &enterworld.AvatarInventory{Capacity: 4, Rows: []enterworld.InventoryRow{devilSpiritRow(0, true, clock.now.Unix())}}
	gid := enterworld.ObjectIDForCharacter(c)

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	cast, ok := findFrame(start.Frames, wire.OpSkillCastResult)
	if !ok || len(cast.Payload) < 7 || cast.Payload[0] != 1 || binary.LittleEndian.Uint32(cast.Payload[2:]) != vallainous {
		t.Fatalf("cast %+v", start.Frames)
	}
	var attached int
	for _, division := range rt.advanceProjectileCasts(clock.NowMs() + int64(row.ActionCastingTimeMs) + 1) {
		for _, frame := range division.Frames {
			if frame.Opcode != wire.OpAttachedEffect {
				continue
			}
			if division.SourceGID != gid || division.OnlyCharacterID != 0 {
				t.Fatalf("the devil form reaches only %d, not %d's observers", division.OnlyCharacterID, gid)
			}
			if len(frame.Payload) < 8 || binary.LittleEndian.Uint32(frame.Payload) != gid ||
				binary.LittleEndian.Uint32(frame.Payload[4:]) != vallainous {
				t.Fatalf("attached effect % x", frame.Payload)
			}
			attached++
		}
	}
	if attached != 1 {
		t.Fatalf("%d attached devil effects", attached)
	}
}
