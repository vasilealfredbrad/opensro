/*
===========================================================================

battleroyale_stage2_test.go - the stage-2 rules on the action runtime

Port-only, not native. The build-pick regression paths, the Hotan lobby,
the monster plan (level-5 wolves only) with its top-ups and supply drop, the starter
kit and the auto-equip, the kill count with its streak banners and the
0x3FB3 scoreboard, and the beta player map's exclusions
(docs/battle-royale.md, "Stage 2").

===========================================================================
*/

package action

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
addKitItems

The itemdata rows the stage-2 tests need: the Blade-Fire kit and loot
of every degree, the Warrior kit, the potions. Equipment rows pass every
equip gate except where a test sets one.
================
*/
func (fx *battleRoyaleFixture) addKitItems() staticItemSource {
	items := fx.rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	equipment := func(id uint32, codename string, kind int64, tid3 int64) {
		items[codename] = &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 1, tid3, kind},
			Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
	}
	etc := func(id uint32, codename string, tid3, tid4 int64) {
		items[codename] = &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 3, tid3, tid4},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1})}
	}
	for degree := 1; degree <= 4; degree++ {
		equipment(uint32(4040+degree), fmt.Sprintf("ITEM_CH_BLADE_%02d_C_RARE", degree), 3, 6)
		equipment(uint32(4015+degree), fmt.Sprintf("ITEM_CH_SWORD_%02d_C_RARE", degree), 2, 6)
		equipment(uint32(4195+degree), fmt.Sprintf("ITEM_CH_SHIELD_%02d_C_RARE", degree), 1, 4)
		equipment(uint32(5040+degree), fmt.Sprintf("ITEM_CH_BLADE_%02d_B_RARE", degree), 3, 6)
		equipment(uint32(5015+degree), fmt.Sprintf("ITEM_CH_SWORD_%02d_B_RARE", degree), 2, 6)
		equipment(uint32(5195+degree), fmt.Sprintf("ITEM_CH_SHIELD_%02d_B_RARE", degree), 1, 4)
		etc(uint32(3+degree), fmt.Sprintf("ITEM_ETC_HP_POTION_%02d", degree), 1, 1)
		etc(uint32(10+degree), fmt.Sprintf("ITEM_ETC_MP_POTION_%02d", degree), 1, 2)
		etc(uint32(17+degree), fmt.Sprintf("ITEM_ETC_ALL_POTION_%02d", degree), 1, 3)
		etc(uint32(54+degree), fmt.Sprintf("ITEM_ETC_CURE_ALL_%02d", degree), 2, 6)
	}
	equipment(11137, "ITEM_EU_TSWORD_01_C_RARE", 8, 6)
	return items
}

/*
================
pick
================
*/
func (fx *battleRoyaleFixture) pick(t *testing.T, c *enterworld.Character, build uint8) wire.BattleRoyaleState {
	t.Helper()
	out := fx.rt.HandleBattleRoyalePick(testDivision, c, []byte{build})
	if out.DiagnosticRefusal != "" || len(out.Frames) != 1 {
		t.Fatalf("%s pick %d: %q %+v", c.Name, build, out.DiagnosticRefusal, out.Frames)
	}
	return decodeBattleRoyaleState(t, out.Frames[0].Payload)
}

/*
================
arenaMonsters

The match layer's monsters within six regions of the arena centre.
================
*/
func (fx *battleRoyaleFixture) arenaMonsters() []monster.Instance {
	return fx.rt.Monsters.PopulationInstances(testDivision, fx.match().lease, battleRoyaleRocRegions())
}

/*
================
rowAt
================
*/
func rowAt(c *enterworld.Character, slot int64) (domain.InventoryRow, bool) {
	for _, row := range c.MissionInventory {
		if row.Slot == slot {
			return row, true
		}
	}
	return domain.InventoryRow{}, false
}

/*
================
noticeTexts

The channel-7 and kill-feed texts a character received, in order.
================
*/
func noticeTexts(frames []wire.Frame) []string {
	var out []string
	for _, f := range frames {
		if f.Opcode != 0x3667 || len(f.Payload) < 3 {
			continue
		}
		r := wire.NewReader(f.Payload)
		_, _ = r.U8()
		n, _ := r.U16()
		text := make([]uint16, n)
		for i := range text {
			text[i], _ = r.U16()
		}
		out = append(out, string(utf16.Decode(text)))
	}
	return out
}

/*
================
hasNotice
================
*/
func hasNotice(frames []wire.Frame, text string) bool {
	for _, got := range noticeTexts(frames) {
		if got == text {
			return true
		}
	}
	return false
}

/*
================
decodeScoreboard

The last 0x3FB3 frame a character received.
================
*/
func decodeScoreboard(t *testing.T, frames []wire.Frame) (final bool, entries []wire.BattleRoyaleScore) {
	t.Helper()
	var payload []byte
	for _, f := range frames {
		if f.Opcode == wire.OpBattleRoyaleScoreboard {
			payload = f.Payload
		}
	}
	if payload == nil {
		t.Fatal("no scoreboard frame")
	}
	r := wire.NewReader(payload)
	version, _ := r.U8()
	finalByte, _ := r.U8()
	count, _ := r.U8()
	if version != wire.BattleRoyaleScoreboardVersion {
		t.Fatalf("scoreboard version %d", version)
	}
	for i := 0; i < int(count); i++ {
		n, _ := r.U16()
		name, _ := r.Bytes(int(n))
		kills, _ := r.U16()
		placement, _ := r.U8()
		self, _ := r.U8()
		entries = append(entries, wire.BattleRoyaleScore{Name: string(name), Kills: kills, Placement: placement, Self: self == 1})
	}
	if err := r.Done(); err != nil {
		t.Fatalf("scoreboard tail: %v", err)
	}
	return finalByte == 1, entries
}

/*
================
kill

a strikes v, who stands beside it at 1 HP, and the next tick settles the
elimination. The previous strike's retained command is released first, as
a player's stop would.
================
*/
func (fx *battleRoyaleFixture) kill(t *testing.T, a, v *enterworld.Character) {
	t.Helper()
	fx.rt.ClearCombatIntent(testDivision, a.Name)
	fx.rt.clearSkillFinalizes(testDivision, a.Name)
	center := battleRoyaleSpawnAt(fx.match().match.Arena().Center, 20)
	fx.place(a, center)
	fx.place(v, simulation.Spawn{RegionID: center.RegionID, X: center.X + 3, Y: 20, Z: center.Z})
	one := int64(1)
	v.CurrentHP = &one
	out := fx.rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || enterworld.CharacterAlive(v) {
		t.Fatalf("%s did not kill %s: %+v (%s)", a.Name, v.Name, out.Frames, out.DiagnosticRefusal)
	}
	fx.tick(time.Second)
}

//============================================================================

/*
================
TestBattleRoyalePickBeforeFirstScanStartsWithThatBuild

The regression of the owner's report: a pick sent as soon as the player
stands in the circle, before the tick's scan admitted it, is accepted
(the handler scans at once) and the match starts with it.
================
*/
func TestBattleRoyalePickBeforeFirstScanStartsWithThatBuild(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	a := fx.chars[0]
	if got := fx.pick(t, a, 3); got.State != wire.BattleRoyaleStateLobby || got.Role != wire.BattleRoyaleRoleMember || got.Build != 3 {
		t.Fatalf("pick before the first scan answered %+v", got)
	}
	fx.startMatch(t)
	p, _ := fx.match().match.Participant(battleroyale.PlayerID(a.Name))
	if p.Build != "3" || a.BattleRoyaleHeld.Build != 3 || a.ModelCodename != "CHAR_CH_MAN_ADVENTURER" {
		t.Fatalf("started with build %q held %d body %s", p.Build, a.BattleRoyaleHeld.Build, a.ModelCodename)
	}
	if got := fx.lastState(t, a.Name); got.Build != 3 {
		t.Fatalf("running frame build %d", got.Build)
	}
}

/*
================
TestBattleRoyalePickSurvivesScanFlickerAndLastSecond

A pick during the lobby survives the member being scanned out of the
circle and back; a re-pick during the countdown and a pick 500 ms before
the start win; each path lands the body of its build.
================
*/
func TestBattleRoyalePickSurvivesScanFlickerAndLastSecond(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 3)
	a, b, c := fx.chars[0], fx.chars[1], fx.chars[2]
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.place(c, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.tick(500 * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateLobby {
		t.Fatalf("state %v with one member", fx.match().match.State())
	}
	fx.pick(t, a, 1)
	// a steps out (or a scan misses it) and back: the pick holds.
	fx.place(a, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.tick(500 * time.Millisecond)
	if got := fx.lastState(t, a.Name); got.State != wire.BattleRoyaleStateNone {
		t.Fatalf("a still a member outside: %+v", got)
	}
	fx.place(a, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	fx.place(c, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	fx.tick(500 * time.Millisecond)
	if got := fx.lastState(t, a.Name); got.State != wire.BattleRoyaleStateCountdown || got.Build != 1 {
		t.Fatalf("a's pick after the flicker: %+v", got)
	}
	// b picks and re-picks during the countdown.
	fx.pick(t, b, 15)
	if got := fx.pick(t, b, 13); got.Build != 13 || got.State != wire.BattleRoyaleStateCountdown {
		t.Fatalf("re-pick answered %+v", got)
	}
	fx.tick(time.Duration(battleroyale.DefaultCountdownMs-500) * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateCountdown {
		t.Fatalf("state %v before the last tick", fx.match().match.State())
	}
	// c picks in the last half second.
	fx.pick(t, c, 3)
	fx.tick(500 * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateRunning {
		t.Fatalf("state %v after the countdown", fx.match().match.State())
	}
	for _, tc := range []struct {
		c     *enterworld.Character
		build uint8
		model string
	}{{a, 1, "CHAR_CH_MAN_ADVENTURER"}, {b, 13, "CHAR_EU_MAN_NOBLE"}, {c, 3, "CHAR_CH_MAN_ADVENTURER"}} {
		p, _ := fx.match().match.Participant(battleroyale.PlayerID(tc.c.Name))
		if p.Build != battleroyale.BuildName(tc.build) || tc.c.BattleRoyaleHeld.Build != tc.build || tc.c.ModelCodename != tc.model {
			t.Fatalf("%s: build %q held %d body %s, want %d", tc.c.Name, p.Build, tc.c.BattleRoyaleHeld.Build, tc.c.ModelCodename, tc.build)
		}
		if got := fx.lastState(t, tc.c.Name); got.Build != tc.build {
			t.Fatalf("%s frame build %d", tc.c.Name, got.Build)
		}
	}
	// A pick after the start is refused but still answered with the view.
	if out := fx.rt.HandleBattleRoyalePick(testDivision, a, []byte{2}); out.DiagnosticRefusal == "" || len(out.Frames) != 1 ||
		decodeBattleRoyaleState(t, out.Frames[0].Payload).Build != 1 {
		t.Fatalf("late pick %+v", out)
	}
}

/*
================
TestBattleRoyaleLobbyIsHotanBesideTheStorageKeeper

The lobby circle stands in Hotan (0x5C87), 25 m from the keeper, and a
player in Wind Town is no member; a restored character stands there.
================
*/
func TestBattleRoyaleLobbyIsHotanBesideTheStorageKeeper(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	lobby := battleRoyaleLobby()
	if battleRoyaleLobbyRegion != 0x5C87 || lobby.Radius != 150 {
		t.Fatalf("lobby %+v in %04x", lobby, battleRoyaleLobbyRegion)
	}
	keeper := battleRoyaleSpawnAt(lobby.Center, 0)
	dx, dz := keeper.X-1134.28, keeper.Z-610.79
	if d := dx*dx + dz*dz; d < 240*240 || d > 260*260 || keeper.RegionID != 0x5C87 {
		t.Fatalf("lobby centre %.1f/%.1f is not 25 m from the keeper", keeper.X, keeper.Z)
	}
	fx.place(fx.chars[1], simulation.Spawn{RegionID: 0x5A74, X: 1710, Y: 20, Z: 330})
	fx.tick(500 * time.Millisecond)
	if got := fx.lastState(t, fx.chars[0].Name); got.State != wire.BattleRoyaleStateLobby || got.Members != 1 ||
		got.Lobby.X != float32(lobby.Center.X) || got.Lobby.Z != float32(lobby.Center.Z) {
		t.Fatalf("lobby frame %+v", got)
	}
	home := BattleRoyaleHome()
	if *home.RegionID != int64(battleRoyaleLobbyRegion) || *home.X != battleRoyaleLobbyX || *home.Z != battleRoyaleLobbyZ {
		t.Fatalf("home %+v", home)
	}
}

/*
================
TestBattleRoyaleArenaCoversRocAndStaysWalkable

Stage 4: the arena is all of Roc for every lobby size; the drop points,
the zone's targets and every monster land on ground the world accepts
(a west-half veto is honoured through the ground mask), and the monsters
stand in Roc regions only.
================
*/
func TestBattleRoyaleArenaCoversRocAndStaysWalkable(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 3)
	arena := battleRoyaleArena()
	// Only the eastern half of the arena is walkable ground.
	fx.rt.CompanionSurfaceHeight = func(region uint16, x, _, z float64) (float64, bool) {
		point := battleRoyaleSpawnAt(arena.Center, 0)
		if region == point.RegionID && x < point.X || region != point.RegionID && region&0xff < point.RegionID&0xff {
			return 0, false
		}
		return 20, true
	}
	fx.startMatch(t)
	m := fx.match()
	if m.match.Arena() != battleroyale.ArenaCircle() || m.match.Arena().Radius != 20400 {
		t.Fatalf("arena %+v for 3 players", m.match.Arena())
	}
	zone, _ := m.match.Zone()
	for i, target := range zone.Targets {
		if _, ok := fx.rt.battleRoyaleGround(target.Center); !ok {
			t.Fatalf("target %d on refused ground: %+v", i, target)
		}
	}
	for _, c := range fx.chars {
		at := fx.rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, fx.clock.NowMs())
		if _, ok := fx.rt.CompanionSurfaceHeight(at.RegionID, at.X, at.Y, at.Z); !ok {
			t.Fatalf("%s dropped on refused ground: %+v", c.Name, at)
		}
	}
	monsters := fx.arenaMonsters()
	for _, instance := range monsters {
		at := instance.Spawn
		if _, ok := fx.rt.CompanionSurfaceHeight(at.RegionID, at.X, at.Y, at.Z); !ok {
			t.Fatalf("monster %s on refused ground: %+v", instance.Ref.Codename, at)
		}
		if !battleroyale.InRoc(worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})) {
			t.Fatalf("monster outside Roc: %+v", at)
		}
	}
	if len(monsters) < battleroyale.ArenaMonsters {
		t.Fatalf("%d monsters stand, want the %d of the drop at least", len(monsters), battleroyale.ArenaMonsters)
	}
}

/*
================
TestBattleRoyaleMonstersTopUpsAndSupplyDrop

The drop fills the layer with the plan's level-5 wolves only, plus the
first phase's zone supply (stage 6, never past the live cap); the
banner announces the airdrop and a minute later its supply elite, a
giant of the same wolf, stands at the landing point inside the first
target (stage 9); the elite drops the supply table with a weapon of the
killer's level window, an ordinary wolf the combined table at the
killer's level.
================
*/
func TestBattleRoyaleMonstersTopUpsAndSupplyDrop(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.addKitItems()
	fx.rt.DropRoll = func() (uint32, error) { return 16000, nil }
	for _, c := range fx.chars {
		fx.pick(t, c, 1)
	}
	fx.startMatch(t)
	m := fx.match()
	fx.tick(battleroyale.AirdropLeadMs * time.Millisecond)
	wolves, elites := 0, 0
	var elite monster.Instance
	for _, instance := range fx.arenaMonsters() {
		if instance.Ref.Codename != battleroyale.MatchMonsterCodename {
			t.Fatalf("a %s in the arena", instance.Ref.Codename)
		}
		if instance.Nest.HasRarityOverride && instance.Rarity() == 4 {
			elites++
			elite = instance
			continue
		}
		wolves++
	}
	// 2000 drop wolves plus the first supply passes; the fixture ground
	// is walkable everywhere, so every one stands.
	if wolves <= battleroyale.ArenaMonsters || wolves+elites > battleroyale.MaxLiveMonsters || elites != 1 {
		t.Fatalf("wolves %d elites %d", wolves, elites)
	}
	zone, _ := m.match.Zone()
	landing := m.airdrop.line.Landing
	at := battleRoyaleSpawnAt(landing, 0)
	if elite.Spawn.RegionID != at.RegionID || battleroyale.Outside(zone.Targets[0], landing) {
		t.Fatalf("elite at %+v, want the landing %+v inside the first target %+v", elite.Spawn, at, zone.Targets[0])
	}
	for _, c := range fx.chars {
		if !hasNotice(fx.frames[c.Name], battleRoyaleSupplyBanner) {
			t.Fatalf("%s got no supply banner: %v", c.Name, noticeTexts(fx.frames[c.Name]))
		}
	}
	a := fx.chars[0]
	drops, match := fx.rt.battleRoyaleLoot(a, elite, simulation.Spawn{RegionID: at.RegionID, X: 1, Z: 1}, fx.clock.Now())
	if !match || len(drops) < 1 || !strings.HasSuffix(drops[0].Codename, "_01_C_RARE") && !strings.HasSuffix(drops[0].Codename, "_01_B_RARE") {
		t.Fatalf("supply loot %+v", drops)
	}
	wolf := monster.Instance{Gid: 1, Ref: elite.Ref}
	for i := 0; i < 20; i++ {
		drops, _ = fx.rt.battleRoyaleLoot(a, wolf, simulation.Spawn{RegionID: at.RegionID, X: 1, Z: 1}, fx.clock.Now())
		for _, drop := range drops {
			if !strings.HasSuffix(drop.Codename, "_01") && !strings.HasSuffix(drop.Codename, "_01_C_RARE") && !strings.HasSuffix(drop.Codename, "_01_B_RARE") {
				t.Fatalf("a level-1 killer's wolf loot %+v", drops)
			}
		}
	}
}

/*
================
TestBattleRoyaleKitEquippedAtDrop

A Blade-Fire character starts the match wearing the degree-1 Sun blade
and shield with twenty HP and MP potions in the bag; a Warrior wears the
two-handed sword and nothing in socket 7.
================
*/
func TestBattleRoyaleKitEquippedAtDrop(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.addKitItems()
	a, b := fx.chars[0], fx.chars[1]
	fx.pick(t, a, 1)
	fx.startMatch(t)
	weapon, _ := rowAt(a, battleroyale.KitWeaponSlot)
	shield, _ := rowAt(a, battleroyale.KitShieldSlot)
	if weapon.Codename != "ITEM_CH_BLADE_01_C_RARE" || weapon.StackCount != 1 || shield.Codename != "ITEM_CH_SHIELD_01_C_RARE" {
		t.Fatalf("a's sockets: %+v %+v", weapon, shield)
	}
	hp, _ := rowAt(a, int64(inventory.EquipmentSlotEnd))
	mp, _ := rowAt(a, int64(inventory.EquipmentSlotEnd)+1)
	if hp.Codename != "ITEM_ETC_HP_POTION_01" || hp.StackCount != 20 || mp.Codename != "ITEM_ETC_MP_POTION_01" || mp.StackCount != 20 {
		t.Fatalf("a's potions: %+v %+v", hp, mp)
	}
	weapon, _ = rowAt(b, battleroyale.KitWeaponSlot)
	if _, off := rowAt(b, battleroyale.KitShieldSlot); weapon.Codename != "ITEM_EU_TSWORD_01_C_RARE" || off {
		t.Fatalf("b's sockets: %+v off-hand %v", weapon, off)
	}
	// The kit never leaks into the real record.
	if _, held := rowAt(a.BattleRoyaleHeld.Real, battleroyale.KitWeaponSlot); held && a.BattleRoyaleHeld.Real.MissionInventory[0].Codename == "ITEM_CH_BLADE_01_C_RARE" {
		t.Fatal("kit in the held record")
	}
}

/*
================
TestBattleRoyaleAutoEquipsBetterWeapon

A looted higher-degree blade is seated in socket 6 in the pickup's own
burst (the move result and the visual ride behind the scoop); a weapon
the level gate refuses waits in the bag and is seated by the tick after
the level that unlocks it.
================
*/
func TestBattleRoyaleAutoEquipsBetterWeapon(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	items := fx.addKitItems()
	a := fx.chars[0]
	fx.pick(t, a, 1)
	fx.startMatch(t)
	at := fx.rt.liveSpawn(simulation.WorldKey(testDivision, a.Name), a, fx.clock.NowMs())
	lay := func(codename string, id uint32) grounditem.Item {
		return fx.rt.Ground.Add(testDivision, grounditem.Item{RefObjID: id, Codename: codename, TypeFlags: wire.PackTypeFlags(3, 1, 6, 3),
			StackCount: 1, Durability: 50, Position: grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}})
	}
	out := fx.rt.grantPickup(testDivision, simulation.WorldKey(testDivision, a.Name), a, a.Snapshot(), lay("ITEM_CH_BLADE_02_C_RARE", 4042))
	if weapon, _ := rowAt(a, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_CH_BLADE_02_C_RARE" {
		t.Fatalf("socket 6 after the pickup: %+v", weapon)
	}
	moves := 0
	for _, f := range out.Frames {
		if f.Opcode == wire.OpItemMoveResponse && f.Payload[0] == wire.ResultSuccess && f.Payload[1] == wire.MoveTypeInventory {
			moves++
		}
	}
	if moves != 1 || len(out.Broadcast) < 2 {
		t.Fatalf("pickup burst: %d moves, %d broadcast frames", moves, len(out.Broadcast))
	}
	if bag, _ := rowAt(a, int64(inventory.EquipmentSlotEnd)+2); bag.Codename != "ITEM_CH_BLADE_01_C_RARE" {
		t.Fatalf("the old blade did not go to the bag: %+v", bag)
	}
	// A worse weapon stays in the bag.
	fx.rt.grantPickup(testDivision, simulation.WorldKey(testDivision, a.Name), a, a.Snapshot(), lay("ITEM_CH_BLADE_01_C_RARE", 4041))
	if weapon, _ := rowAt(a, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_CH_BLADE_02_C_RARE" {
		t.Fatalf("socket 6 after a worse pickup: %+v", weapon)
	}
	// A degree-3 blade gated at level 16 waits for the level.
	items["ITEM_CH_BLADE_03_C_RARE"].ReqQuadTypes, items["ITEM_CH_BLADE_03_C_RARE"].ReqQuadValues = [4]int64{1, -1, -1, -1}, [4]int64{16, 0, 0, 0}
	fx.rt.grantPickup(testDivision, simulation.WorldKey(testDivision, a.Name), a, a.Snapshot(), lay("ITEM_CH_BLADE_03_C_RARE", 4043))
	if weapon, _ := rowAt(a, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_CH_BLADE_02_C_RARE" {
		t.Fatalf("socket 6 holds a gated weapon: %+v", weapon)
	}
	fx.rt.CommitExperience = func(c *enterworld.Character, _, _ int64, _ uint32) ([]wire.Frame, bool) {
		level := int64(16)
		c.Level, c.MaxLevel = &level, &level
		return nil, true
	}
	before := len(fx.frames[a.Name])
	fx.rt.grantMonsterExperience(testDivision, a, 100, 0, 1)
	fx.tick(500 * time.Millisecond)
	if weapon, _ := rowAt(a, battleroyale.KitWeaponSlot); weapon.Codename != "ITEM_CH_BLADE_03_C_RARE" {
		t.Fatalf("socket 6 after the level: %+v", weapon)
	}
	if _, ok := findFrame(fx.frames[a.Name][before:], wire.OpItemMoveResponse); !ok {
		t.Fatal("the level-up equip sent no move result")
	}
}

/*
================
TestBattleRoyaleKillsStreaksAndScoreboard

Six players; one kills the other five: the kill count climbs, the
rampage and unstoppable banners fire once at 3 and 5, every elimination
sends the 0x3FB3 scoreboard with the receiver's own entry marked, and
the end sends it final with the placements.
================
*/
func TestBattleRoyaleKillsStreaksAndScoreboard(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 6)
	a := fx.chars[0]
	fx.pick(t, a, 1)
	fx.startMatch(t)
	m := fx.match()
	for i, v := range fx.chars[1:] {
		fx.kill(t, a, v)
		killer, _ := m.match.Participant(battleroyale.PlayerID(a.Name))
		if killer.Kills != i+1 {
			t.Fatalf("kills after %d: %d", i+1, killer.Kills)
		}
		// The last victim's own board is already the final one.
		final, entries := decodeScoreboard(t, fx.frames[v.Name])
		if final != (i == 4) || len(entries) != 6 {
			t.Fatalf("scoreboard after kill %d: final %v entries %+v", i+1, final, entries)
		}
		for _, entry := range entries {
			switch entry.Name {
			case a.Name:
				if entry.Kills != uint16(i+1) || entry.Self {
					t.Fatalf("killer entry %+v", entry)
				}
			case v.Name:
				if entry.Placement != uint8(6-i) || !entry.Self || entry.Kills != 0 {
					t.Fatalf("victim entry %+v", entry)
				}
			}
		}
		rampage := strings.Count(strings.Join(noticeTexts(fx.frames[a.Name]), "\n"), a.Name+" is on a rampage")
		unstoppable := strings.Count(strings.Join(noticeTexts(fx.frames[a.Name]), "\n"), a.Name+" is unstoppable")
		if wantRampage, wantUnstoppable := min(i+1, 3)/3, min(i+1, 5)/5; rampage != wantRampage || unstoppable != wantUnstoppable {
			t.Fatalf("after kill %d: rampage %d unstoppable %d", i+1, rampage, unstoppable)
		}
	}
	if m.match.State() != battleroyale.StateFinished || m.winner != a.Name {
		t.Fatalf("state %v winner %q", m.match.State(), m.winner)
	}
	final, entries := decodeScoreboard(t, fx.frames[a.Name])
	if !final || len(entries) != 6 {
		t.Fatalf("final scoreboard %v %+v", final, entries)
	}
	for _, entry := range entries {
		if entry.Name == a.Name && (entry.Placement != 1 || entry.Kills != 5 || !entry.Self) {
			t.Fatalf("winner entry %+v", entry)
		}
		if entry.Name != a.Name && (entry.Placement < 2 || entry.Self) {
			t.Fatalf("loser entry %+v", entry)
		}
	}
	if _, entries := decodeScoreboard(t, fx.frames[fx.chars[5].Name]); !entries[5].Self || entries[5].Placement != 2 {
		t.Fatalf("last victim's final entry %+v", entries[5])
	}
}

/*
================
TestBattleRoyaleHiddenOnBetaMap

A lobby member, a participant and an eliminated participant stay off the
beta player map; a character who walks out of the lobby, or whose match
closed and who left the circle, is shown again. Off, nobody hides.
================
*/
func TestBattleRoyaleHiddenOnBetaMap(t *testing.T) {
	plain, _ := newTestRuntime(testCharacter(), testItems())
	if plain.BattleRoyaleMapHidden(testDivision, testCharacter()) {
		t.Fatal("hidden while the mode is off")
	}
	fx := newBattleRoyaleFixture(t, 2)
	a, b := fx.chars[0], fx.chars[1]
	if fx.rt.BattleRoyaleMapHidden(testDivision, a) {
		t.Fatal("hidden before the scan")
	}
	fx.tick(500 * time.Millisecond)
	if !fx.rt.BattleRoyaleMapHidden(testDivision, a) || !fx.rt.BattleRoyaleMapHidden(testDivision, b) {
		t.Fatal("lobby members shown")
	}
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.tick(500 * time.Millisecond)
	if fx.rt.BattleRoyaleMapHidden(testDivision, b) {
		t.Fatal("a leaver hidden")
	}
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	fx.pick(t, a, 1)
	fx.startMatch(t)
	fx.kill(t, a, b)
	if !fx.rt.BattleRoyaleMapHidden(testDivision, a) || !fx.rt.BattleRoyaleMapHidden(testDivision, b) {
		t.Fatal("participant or eliminated player shown")
	}
	fx.tick(battleRoyaleLingerMs * time.Millisecond)
	fx.place(a, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.tick(time.Second)
	if fx.rt.BattleRoyaleMapHidden(testDivision, a) || fx.rt.BattleRoyaleMapHidden(testDivision, b) {
		t.Fatal("still hidden after the match closed and both left the circle")
	}
}
