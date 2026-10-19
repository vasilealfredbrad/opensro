/*
===========================================================================

battleroyale_bots_test.go - the bots on the action runtime (stages 5-8)

Port-only, not native. The battle royale fixture with the Blade-Fire and
Warrior kits known, an ephemeral roster for the bots' characters and the
movement lane's ground click wired, as the server composes them: the GM
switch and its roster, bots that stand as real characters (held by the
store, never stored), a real player's session that spawns one through
the ordinary peer row with its equipment, a bot's skill hurting a player
through the PvP pipeline, a bot levelling on a wolf, picking up and
wearing a drop, dying and leaving, killing the last player and winning,
walking into the zone, and the cleanup when the match closes.

===========================================================================
*/

package action

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
newBattleRoyaleBotFixture

n characters in the lobby, the first a GM, the kits known, the bots'
characters held by an ephemeral roster and walking through the movement
lane's HandleMove on the fixture's world plane.
================
*/
func newBattleRoyaleBotFixture(t *testing.T, n int) *battleRoyaleFixture {
	t.Helper()
	fx := newBattleRoyaleFixture(t, n)
	fx.addKitItems()
	deps := fx.rt.deps.(*enterworld.Deps)
	fx.roster = enterworld.NewEphemeralRoster()
	deps.Ephemeral = fx.roster
	fx.movement = movement.NewRuntime(deps, fx.rt.Worlds)
	fx.movement.Now = fx.clock.Now
	fx.rt.battleRoyale.setup.Move = func(division string, c *enterworld.Character, payload []byte) {
		fx.movement.HandleMove(division, c, payload)
	}
	fx.chars[0].GMPrivilege = true
	return fx
}

/*
================
decodeBotRoster

A 0x3FB4 (version 2) body read back; its difficulty and configured
count are always valid (the client ends the session otherwise).
================
*/
func decodeBotRoster(t *testing.T, payload []byte) (bool, []wire.BattleRoyaleBot) {
	t.Helper()
	r := wire.NewReader(payload)
	version, _ := r.U8()
	enabled, _ := r.U8()
	difficulty, _ := r.U8()
	configured, _ := r.U16()
	count, _ := r.U16()
	if version != wire.BattleRoyaleBotRosterVersion || difficulty > wire.BattleRoyaleBotHard || configured < 1 || configured > wire.BattleRoyaleBotMaxCount {
		t.Fatalf("roster version %d, difficulty %d, configured %d", version, difficulty, configured)
	}
	var bots []wire.BattleRoyaleBot
	for range int(count) {
		gid, _ := r.U32()
		n, _ := r.U16()
		name, _ := r.Bytes(int(n))
		bots = append(bots, wire.BattleRoyaleBot{GID: gid, Name: string(name)})
	}
	if err := r.Done(); err != nil {
		t.Fatalf("roster tail: %v", err)
	}
	return enabled == 1, bots
}

/*
================
lastRoster

The last 0x3FB4 a character received.
================
*/
func (fx *battleRoyaleFixture) lastRoster(t *testing.T, name string) (bool, []wire.BattleRoyaleBot) {
	t.Helper()
	for i := len(fx.frames[name]) - 1; i >= 0; i-- {
		if f := fx.frames[name][i]; f.Opcode == wire.OpBattleRoyaleBotRoster {
			return decodeBotRoster(t, f.Payload)
		}
	}
	t.Fatalf("%s received no roster", name)
	return false, nil
}

/*
================
switchBots

The GM's 0x7FB4; fails the test on a refusal.
================
*/
func (fx *battleRoyaleFixture) switchBots(t *testing.T, c *enterworld.Character, on bool) {
	t.Helper()
	body := []byte{0}
	if on {
		body[0] = 1
	}
	out := fx.rt.HandleBattleRoyaleBots(testDivision, c, body)
	if out.DiagnosticRefusal != "" || len(out.Frames) != 1 || out.Frames[0].Opcode != wire.OpBattleRoyaleBotRoster {
		t.Fatalf("switch refused: %+v", out)
	}
	if enabled, _ := decodeBotRoster(t, out.Frames[0].Payload); enabled != on {
		t.Fatalf("switch answered %v", enabled)
	}
}

/*
================
startBotMatch

The GM switches bots on and the lone lobby starts the match, every
player on Blade-Fire.
================
*/
func (fx *battleRoyaleFixture) startBotMatch(t *testing.T) *battleRoyaleMatch {
	t.Helper()
	fx.tick(500 * time.Millisecond)
	fx.pickBlades(t)
	fx.switchBots(t, fx.chars[0], true)
	fx.startMatch(t)
	return fx.match()
}

/*
================
pickBlades

Every fixture player picks Blade-Fire, the build whose base attack and
kit the fixture knows, so it can fight.
================
*/
func (fx *battleRoyaleFixture) pickBlades(t *testing.T) {
	t.Helper()
	for _, c := range fx.chars {
		fx.pick(t, c, 1)
	}
}

/*
================
standBot

Stands a bot's character at a grid point (idle) and makes its next
brain step think.
================
*/
func (fx *battleRoyaleFixture) standBot(t *testing.T, bot *battleRoyaleBot, at battleroyale.Point) {
	t.Helper()
	if bot.c == nil {
		t.Fatalf("bot %s has no character", bot.name)
	}
	fx.place(bot.c, battleRoyaleSpawnAt(at, 20))
	bot.at, bot.nextThinkMs, bot.routing = at, 0, false
}

/*
================
bladeBot

The first bot of a Blade build (Blade-Fire or Blade-Heal): the fixture
knows the Chinese base attack and the Blade kit only, so this bot can
fight and wears its kit.
================
*/
func (fx *battleRoyaleFixture) bladeBot(t *testing.T, m *battleRoyaleMatch) *battleRoyaleBot {
	t.Helper()
	for _, bot := range m.bots {
		if bot.alive && (bot.build.ID == 1 || bot.build.ID == 4) {
			return bot
		}
	}
	t.Fatal("no Blade bot in the match")
	return nil
}

/*
================
idleBotsBut

Every other bot skips the next minute of decisions and every other
participant stands 600 m east of keep, so a step thinks for keep alone
and nobody is in its sense.
================
*/
func (fx *battleRoyaleFixture) idleBotsBut(m *battleRoyaleMatch, keep *battleRoyaleBot) {
	at := fx.playerAt(keep.c)
	east := battleRoyaleSpawnAt(battleroyale.Point{X: at.X + 6000, Z: at.Z}, 20)
	west := battleRoyaleSpawnAt(battleroyale.Point{X: at.X - 6000, Z: at.Z}, 20)
	for _, bot := range m.bots {
		if bot != keep && bot.c != nil {
			bot.nextThinkMs = fx.clock.NowMs() + 60_000
			fx.place(bot.c, east)
		}
	}
	for _, c := range fx.chars {
		fx.place(c, west)
	}
}

/*
================
runtimeTick

The whole action tick (TickHook: the mode's step, the attack
continuations, the pickups, the recovery) every 100 ms for d.
================
*/
func (fx *battleRoyaleFixture) runtimeTick(d time.Duration) {
	hook := fx.rt.TickHook()
	for elapsed := time.Duration(0); elapsed < d; elapsed += 100 * time.Millisecond {
		fx.clock.Advance(100 * time.Millisecond)
		hook(fx.clock.NowMs())
	}
}

/*
================
botStep

One brain step of the match's bots as the mode's tick runs it: the
decisions under the division lock, then their commands without it.
Returns the commands' publications.
================
*/
func (fx *battleRoyaleFixture) botStep(m *battleRoyaleMatch) []simulation.DivisionFrames {
	unlock := fx.rt.lockDivision(testDivision)
	fx.rt.advanceBattleRoyaleBots(m, fx.clock.NowMs())
	commands := m.botCommands
	m.botCommands = nil
	unlock()
	return fx.rt.runBattleRoyaleBotCommands(testDivision, commands)
}

/*
================
playerAt

Where a fixture character stands, in grid units.
================
*/
func (fx *battleRoyaleFixture) playerAt(c *enterworld.Character) battleroyale.Point {
	return battleRoyaleSpawnGrid(fx.rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, fx.clock.NowMs()))
}

/*
================
headlessPusher

The tick's deliveries, by session.
================
*/
type headlessPusher struct{ frames map[string][]simulation.Frame }

func (p *headlessPusher) PushToSession(id string, frames []simulation.Frame) {
	p.frames[id] = append(p.frames[id], frames...)
}
func (p *headlessPusher) PushToDivision(string, []simulation.Frame, string) {}

/*
================
headlessSource

One real player's session.
================
*/
type headlessSource struct{ sessions []simulation.SessionSnapshot }

func (s *headlessSource) SnapshotSessions() []simulation.SessionSnapshot { return s.sessions }

//============================================================================

/*
================
TestBattleRoyaleBotSwitchIsGMOnly

A player who is not a GM is refused and answered with the switch the
server holds; the GM's switch reaches every lobby member; off, the mode
refuses the opcode.
================
*/
func TestBattleRoyaleBotSwitchIsGMOnly(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 2)
	gm, player := fx.chars[0], fx.chars[1]
	fx.tick(500 * time.Millisecond)
	out := fx.rt.HandleBattleRoyaleBots(testDivision, player, []byte{1})
	if out.DiagnosticRefusal == "" || len(out.Frames) != 1 || fx.match().match.BotsEnabled() {
		t.Fatalf("non-GM switch: %+v", out)
	}
	if enabled, bots := decodeBotRoster(t, out.Frames[0].Payload); enabled || len(bots) != 0 {
		t.Fatal("the refusal reports bots on")
	}
	if out := fx.rt.HandleBattleRoyaleBots(testDivision, gm, []byte{2}); out.DiagnosticRefusal == "" {
		t.Fatal("a malformed switch was accepted")
	}
	fx.switchBots(t, gm, true)
	if enabled, _ := fx.lastRoster(t, player.Name); !enabled || !fx.match().match.BotsEnabled() {
		t.Fatal("the other member did not hear the switch")
	}
	fx.switchBots(t, gm, false)
	if enabled, _ := fx.lastRoster(t, player.Name); enabled {
		t.Fatal("switch off not heard")
	}
	off, _ := newTestRuntime(testCharacter(), testItems())
	if out := off.HandleBattleRoyaleBots(testDivision, gm, []byte{1}); out.DiagnosticRefusal == "" || len(out.Frames) != 0 {
		t.Fatalf("switch while the mode is off: %+v", out)
	}
}

/*
================
TestBattleRoyaleBotsOffIsTheNativeMatch

Without the switch a lone player waits in the lobby and a two-player
match holds no bot: no bot participant, character or roster entry.
================
*/
func TestBattleRoyaleBotsOffIsTheNativeMatch(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	fx.tick(2 * time.Second)
	if fx.match().match.State() != battleroyale.StateLobby {
		t.Fatalf("a lone player started a match without bots: %v", fx.match().match.State())
	}
	fx = newBattleRoyaleBotFixture(t, 2)
	fx.startMatch(t)
	m := fx.match()
	if len(m.bots) != 0 || len(m.match.Participants()) != 2 || len(fx.rt.BattleRoyaleHeadless()) != 0 || fx.roster.Len() != 0 {
		t.Fatalf("%d bots, %d participants, %d headless", len(m.bots), len(m.match.Participants()), len(fx.rt.BattleRoyaleHeadless()))
	}
}

/*
================
TestBattleRoyaleBotsStandAsCharacters

One GM and the switch: the countdown starts alone and the drop stands
twenty bots, each a real match character of a build (held, level 1, the
kit's weapon worn and potions in the bag) with a player object id, a
unique name, a membership of the match layer, on walkable ground, held
by the ephemeral roster and never by the character source; the tick
lists each in its layer and the player hears the roster.
================
*/
func TestBattleRoyaleBotsStandAsCharacters(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	player := fx.chars[0]
	fx.tick(500 * time.Millisecond)
	fx.switchBots(t, player, true)
	fx.tick(500 * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateCountdown {
		t.Fatalf("one player with bots: %v", fx.match().match.State())
	}
	fx.tick(time.Duration(battleroyale.DefaultCountdownMs) * time.Millisecond)
	m := fx.match()
	if m.match.State() != battleroyale.StateRunning || len(m.bots) != battleroyale.DefaultBotCount || m.match.AliveCount() != 1+battleroyale.DefaultBotCount {
		t.Fatalf("state %v, %d bots, %d alive", m.match.State(), len(m.bots), m.match.AliveCount())
	}
	headless := map[string]BattleRoyaleHeadlessCharacter{}
	for _, h := range fx.rt.BattleRoyaleHeadless() {
		headless[h.Character.Name] = h
	}
	names := map[string]bool{player.Name: true}
	for _, bot := range m.bots {
		c := bot.c
		if names[bot.name] || !bot.alive || c == nil || c.Name != bot.name {
			t.Fatalf("bot %+v", bot)
		}
		names[bot.name] = true
		build, held := battleroyale.HeldBuild(c)
		if !held || build.ID != bot.build.ID || rewardLevel(c) != 1 || domain.CharacterWorldInstance(c) != uint32(m.world) {
			t.Fatalf("bot %s is not a level-1 match character of its build in the layer", bot.name)
		}
		if bot.gid != enterworld.ObjectIDForCharacter(c) || bot.gid <= domain.PlayerGIDBase || bot.gid >= domain.NPCGIDBase {
			t.Fatalf("bot %s gid %d is not a player object id", bot.name, bot.gid)
		}
		if weapon, ok := rowAt(c, int64(inventory.SocketWeapon)); !ok || battleRoyaleItemDegree(weapon.Codename) != 1 {
			t.Fatalf("bot %s wears no kit weapon: %+v", bot.name, c.MissionInventory)
		}
		if fx.rt.findCharacter(testDivision, bot.name) != c || fx.rt.findCharacterByGid(testDivision, bot.gid) != c {
			t.Fatalf("bot %s does not resolve", bot.name)
		}
		for _, stored := range fx.rt.deps.CharactersForDivision(testDivision) {
			if stored == c {
				t.Fatalf("bot %s joined the character source", bot.name)
			}
		}
		if lease, ok := fx.rt.EntryPopulationLease(testDivision, bot.name); !ok || lease != m.lease {
			t.Fatalf("bot %s holds no membership of the layer", bot.name)
		}
		if h, ok := headless[bot.name]; !ok || h.Lease != m.lease || h.Character != c {
			t.Fatalf("bot %s is not listed for the tick: %+v", bot.name, h)
		}
		if _, ok := fx.rt.battleRoyaleGround(fx.playerAt(c)); !ok {
			t.Fatalf("bot %s stands off the ground", bot.name)
		}
	}
	if fx.roster.Len() != battleroyale.DefaultBotCount {
		t.Fatalf("%d ephemeral records", fx.roster.Len())
	}
	enabled, roster := fx.lastRoster(t, player.Name)
	if !enabled || len(roster) != battleroyale.DefaultBotCount || m.battleRoyaleBotOf(roster[0].GID) == nil {
		t.Fatalf("roster %v %+v", enabled, roster)
	}
	if got := fx.lastState(t, player.Name); got.Members != 1+battleroyale.DefaultBotCount || got.Alive != 1+battleroyale.DefaultBotCount {
		t.Fatalf("state %+v", got)
	}
}

/*
================
TestBattleRoyaleBotSpawnsForAPlayerAsACharacter

The tick with one real player's session and the bots listed headless (as
the server wires simulation.Ticker.Headless): the player receives the
bot nearby as an ordinary player spawn row (0x30D7, CICUser) carrying its
name, model and worn weapon, then follows its walk; no frame is built
for any bot.
================
*/
func TestBattleRoyaleBotSpawnsForAPlayerAsACharacter(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 20)
	player, bot := fx.chars[0], fx.bladeBot(t, m)
	at := fx.playerAt(player)
	fx.standBot(t, bot, battleroyale.Point{X: at.X + 60, Z: at.Z})
	viewer := fx.movement.HeadlessSnapshot(testDivision, player)
	viewer.Headless, viewer.SessionID = false, "player"
	viewer.Population, _ = fx.rt.EntryPopulationLease(testDivision, player.Name)
	push := &headlessPusher{frames: map[string][]simulation.Frame{}}
	ticker := simulation.NewTicker(&headlessSource{sessions: []simulation.SessionSnapshot{viewer}}, push)
	ticker.Headless = func() []simulation.SessionSnapshot {
		var out []simulation.SessionSnapshot
		for _, h := range fx.rt.BattleRoyaleHeadless() {
			snapshot := fx.movement.HeadlessSnapshot(h.Division, h.Character)
			snapshot.SessionID, snapshot.Population = h.SessionID, h.Lease
			out = append(out, snapshot)
		}
		return out
	}
	ticker.RunTick(fx.clock.NowMs())
	types := map[uint32]uint16{}
	for _, ref := range fx.rt.deps.(*enterworld.Deps).Items.(staticItemSource) {
		types[ref.RefObjID] = ref.TypeFlags()
	}
	var spawned *wire.PlayerSpawnRow
	for _, f := range push.frames["player"] {
		if f.Opcode != wire.OpSingleObjectSpawn {
			continue
		}
		if row, err := wire.DecodePlayerSpawnRow(f.Payload, types, true); err == nil && row.Gid == bot.gid {
			spawned = &row
		}
	}
	weapon, _ := rowAt(bot.c, int64(inventory.SocketWeapon))
	if spawned == nil || spawned.Name != bot.name || spawned.RefObjID != uint32(*bot.c.ModelRef) {
		t.Fatalf("the player never spawned bot %s as a character: %+v", bot.name, spawned)
	}
	worn := false
	for _, item := range spawned.Equipment {
		worn = worn || item.RefObjID == weapon.RefObjID
	}
	if !worn {
		t.Fatalf("the bot's spawn row lacks its weapon %s: %+v", weapon.Codename, spawned.Equipment)
	}
	// The bot walks (its own ground click); the player follows the move.
	fx.travelTo(t, m, bot, battleroyale.Point{X: at.X + 60, Z: at.Z + 200})
	fx.clock.Advance(500 * time.Millisecond)
	ticker.RunTick(fx.clock.NowMs())
	moved := false
	for _, f := range push.frames["player"] {
		moved = moved || f.Opcode == wire.OpObjectSourceMove || f.Opcode == simulation.OpMovementAck
	}
	if !moved {
		t.Fatal("the player never saw the bot walk")
	}
	for id := range push.frames {
		if id != "player" {
			t.Fatalf("frames were built for the headless %s", id)
		}
	}
}

/*
================
travelTo

Makes the bot walk to goal (one ground click of its route) and fails
when its world holds no walk afterwards.
================
*/
func (fx *battleRoyaleFixture) travelTo(t *testing.T, m *battleRoyaleMatch, bot *battleRoyaleBot, goal battleroyale.Point) {
	t.Helper()
	unlock := fx.rt.lockDivision(testDivision)
	fx.rt.travelBattleRoyaleBot(m, bot, goal, fx.clock.NowMs())
	commands := m.botCommands
	m.botCommands = nil
	unlock()
	fx.rt.runBattleRoyaleBotCommands(testDivision, commands)
	world := fx.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, bot.name), func() simulation.WorldState { return simulation.SeedWorldState(bot.c) })
	if !world.MovingAt(fx.clock.NowMs() + 100) {
		t.Fatalf("bot %s does not walk after its click (%d commands)", bot.name, len(commands))
	}
}

/*
================
TestBattleRoyaleBotLevelsOnAWolf

A bot beside a wolf on its last HP, nobody in sense: its brain presses
an attack, the ordinary kill settles, and the match's EXP pace (the
kill's MatchKillExp at its difficulty's pace) levels it, which trains
both build masteries; its wolf count rises.
================
*/
func TestBattleRoyaleBotLevelsOnAWolf(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 20)
	bot := fx.bladeBot(t, m)
	var wolf monster.Instance
	for _, actor := range fx.arenaMonsters() {
		if actor.CurrentHP > 0 && actor.Ref.Codename == battleroyale.MatchMonsterCodename {
			wolf = actor
			break
		}
	}
	if wolf.Gid == 0 {
		t.Fatal("no wolf in the layer")
	}
	if _, ok := fx.rt.Monsters.ApplyDamage(testDivision, wolf.Gid, wolf.CurrentHP-1); !ok {
		t.Fatal("cannot wound the wolf")
	}
	wolfAt := battleRoyalePoseGrid(monster.Pose{RegionID: wolf.Spawn.RegionID, X: wolf.Spawn.X, Z: wolf.Spawn.Z})
	fx.standBot(t, bot, battleroyale.Point{X: wolfAt.X + 30, Z: wolfAt.Z})
	fx.idleBotsBut(m, bot)
	var granted int64
	fx.rt.CommitExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		granted += exp
		level := *c.Level + 1
		c.Level = &level
		return nil, true
	}
	fx.botStep(m)
	intent, _ := fx.rt.combatIntentFor(testDivision, bot.name)
	fx.runtimeTick(3 * time.Second)
	if after, _ := fx.rt.Monsters.Get(testDivision, wolf.Gid); after.CurrentHP != 0 {
		t.Fatalf("the bot did not kill the wolf %d (HP %d; its attack held %d)", wolf.Gid, after.CurrentHP, intent.TargetGid)
	}
	if want := battleroyale.MatchKillExp(118); granted != want || rewardLevel(bot.c) != 2 || bot.monsterKills != 1 {
		t.Fatalf("EXP %d (want %d), level %d, wolves %d", granted, want, rewardLevel(bot.c), bot.monsterKills)
	}
	for _, id := range bot.build.Masteries {
		if level, _ := enterworld.MasteryLevel(bot.c, id); level != 2 {
			t.Fatalf("mastery %d at %d after the level", id, level)
		}
	}
}

/*
================
TestBattleRoyaleBotPicksUpAndWears

A drop beside an idle bot that its set planner would wear (the kit's
blade with full whites): the brain picks it up through the ordinary
pickup press and the pickup's auto-equip seats it.
================
*/
func TestBattleRoyaleBotPicksUpAndWears(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 20)
	bot := fx.bladeBot(t, m)
	worn, _ := rowAt(bot.c, int64(inventory.SocketWeapon))
	at := fx.playerAt(bot.c)
	fx.standBot(t, bot, at)
	fx.idleBotsBut(m, bot)
	ref, _ := fx.rt.deps.ItemReferences().ItemRefByCodename(worn.Codename)
	drop := PlanItemDrop(inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1,
		VarianceBits: 1<<35 - 1, Durability: 60}, 1, battleRoyaleSpawnAt(at, 20), "", fx.clock.Now())
	drop.Population = grounditem.Population{World: uint32(m.lease.ID), Generation: m.lease.Generation}
	drop = fx.rt.Ground.Add(testDivision, drop)
	if drop.Gid == 0 || !fx.rt.battleRoyaleWouldWear(bot.c, drop) {
		t.Fatalf("the planner would not wear the better blade %+v", drop)
	}
	fx.botStep(m)
	fx.runtimeTick(2 * time.Second)
	if _, still := fx.rt.Ground.Get(testDivision, drop.Gid); still {
		t.Fatal("the bot left the drop on the ground")
	}
	now, ok := rowAt(bot.c, int64(inventory.SocketWeapon))
	if !ok || now.VarianceBits != strconv.FormatUint(drop.VarianceBits, 10) {
		t.Fatalf("the bot does not wear the picked blade: %+v (bag %+v)", now, bot.c.MissionInventory)
	}
}

/*
================
TestBattleRoyaleBotDiesAndLeaves

A player strikes a bot down through the ordinary PvP press: the bot is
eliminated with the player credited ("test eliminated Ares"), the
scoreboard and roster still list it, its corpse lies, and then its body
leaves the tick, the population and the ephemeral roster.
================
*/
func TestBattleRoyaleBotDiesAndLeaves(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	player := fx.chars[0]
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 3)
	victim := m.bots[0]
	c := victim.c
	fx.kill(t, player, c)
	if victim.alive {
		t.Fatal("the bot outlived its death")
	}
	if !hasFeed(fx.frames[player.Name], fmt.Sprintf("%s eliminated %s", player.Name, victim.name)) {
		t.Fatalf("feed %v", noticeTexts(fx.frames[player.Name]))
	}
	if p, _ := m.match.Participant(battleroyale.PlayerID(player.Name)); p.Kills != 1 {
		t.Fatalf("player kills %d", p.Kills)
	}
	if _, roster := fx.lastRoster(t, player.Name); len(roster) != 3 {
		t.Fatalf("the roster after a death lists %d bots", len(roster))
	}
	fx.tick(time.Duration(battleRoyaleBotCorpseMs+1000) * time.Millisecond)
	if victim.c != nil || fx.rt.findCharacter(testDivision, victim.name) != nil || fx.roster.Len() != 2 {
		t.Fatalf("the dead bot still stands (%d ephemeral records)", fx.roster.Len())
	}
	if _, ok := fx.rt.BattleRoyaleHeadlessGID(testDivision, victim.name); ok {
		t.Fatal("the dead bot is still listed for the tick")
	}
	if _, ok := fx.rt.EntryPopulationLease(testDivision, victim.name); ok {
		t.Fatal("the dead bot keeps its membership")
	}
	if battleroyale.Held(c) != true || c.BattleRoyaleHeld.Real == nil {
		t.Fatal("the bot's record was restored like a player's")
	}
}

/*
================
TestBattleRoyaleBotKillsTheLastPlayerAndWins

A bot strikes the last player down through the same press a player uses:
the elimination is credited to it ("Ares eliminated test"), the match
ends at once and the bot wins, without silk.
================
*/
func TestBattleRoyaleBotKillsTheLastPlayerAndWins(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	player := fx.chars[0]
	m := fx.startBotMatch(t)
	killer := fx.bladeBot(t, m)
	fx.kill(t, killer.c, player)
	if !hasFeed(fx.frames[player.Name], fmt.Sprintf("%s eliminated %s", killer.name, player.Name)) {
		t.Fatalf("feed %v", noticeTexts(fx.frames[player.Name]))
	}
	if m.match.State() != battleroyale.StateFinished || m.winner != killer.name || len(fx.silk) != 0 {
		t.Fatalf("state %v winner %q silk %v", m.match.State(), m.winner, fx.silk)
	}
	if got := fx.lastState(t, player.Name); got.State != wire.BattleRoyaleStateFinished || got.Winner != killer.name {
		t.Fatalf("final frame %+v", got)
	}
}

/*
================
TestBattleRoyaleBotsWalkIntoTheZone

A bot standing outside the next circle clicks the ground toward its
centre on its next brain step: its world walks there.
================
*/
func TestBattleRoyaleBotsWalkIntoTheZone(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatch(t)
	zone, _ := m.match.ZoneAt(fx.clock.NowMs())
	bot := m.bots[0]
	outside := battleroyale.Point{X: zone.Target.Center.X + zone.Target.Radius + 1000, Z: zone.Target.Center.Z}
	fx.standBot(t, bot, outside)
	fx.idleBotsBut(m, bot)
	before := battleRoyaleDistance(outside, zone.Target.Center)
	fx.botStep(m)
	world := fx.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, bot.name), func() simulation.WorldState { return simulation.SeedWorldState(bot.c) })
	goal := battleRoyaleSpawnGrid(world.Spawn)
	if !world.MovingAt(fx.clock.NowMs()+100) || battleRoyaleDistance(goal, zone.Target.Center) >= before {
		t.Fatalf("bot not walking in: goal %+v (%.0f from the centre, was %.0f)", goal, battleRoyaleDistance(goal, zone.Target.Center), before)
	}
}

/*
================
TestBattleRoyaleBotsLeaveWithTheLayer

The finished match closes: every bot leaves the tick, the population,
the gid index and the ephemeral roster before the layer is released, and
the next lobby keeps the GM's switch.
================
*/
func TestBattleRoyaleBotsLeaveWithTheLayer(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	player := fx.chars[0]
	m := fx.startBotMatch(t)
	bots := append([]*battleRoyaleBot(nil), m.bots...)
	world := m.world
	_ = m.match.Eliminate(battleroyale.PlayerID(player.Name), "", battleroyale.EliminatedZone, fx.clock.NowMs())
	fx.tick(time.Second)
	fx.tick(time.Duration(battleRoyaleLingerMs) * time.Millisecond)
	// The restored player stands in the lobby and the switch carried
	// over, so the next countdown may already run.
	if state := m.match.State(); state != battleroyale.StateLobby && state != battleroyale.StateCountdown || len(m.bots) != 0 || m.layered ||
		!m.match.BotsEnabled() {
		t.Fatalf("after close: state %v, %d bots, layered %v, switch %v", m.match.State(), len(m.bots), m.layered, m.match.BotsEnabled())
	}
	if _, allocated := fx.rt.Monsters.PopulationLease(testDivision, world); allocated {
		t.Fatal("the arena layer is still allocated")
	}
	if len(fx.rt.BattleRoyaleHeadless()) != 0 || fx.roster.Len() != 0 {
		t.Fatalf("%d headless bots, %d ephemeral records after the close", len(fx.rt.BattleRoyaleHeadless()), fx.roster.Len())
	}
	for _, bot := range bots {
		if _, ok := fx.rt.battleRoyaleBotOf(bot.gid); ok {
			t.Fatalf("bot gid %d still indexed", bot.gid)
		}
		if fx.rt.findCharacter(testDivision, bot.name) != nil {
			t.Fatalf("bot %s still resolves", bot.name)
		}
	}
}

/*
================
hasFeed

Whether a kill-feed line (channel 0x82) with text reached the frames.
================
*/
func hasFeed(frames []wire.Frame, text string) bool {
	want := wire.BattleRoyaleNotificationFrame(clampNotice(text))
	for _, f := range frames {
		if f.Opcode == want.Opcode && string(f.Payload) == string(want.Payload) {
			return true
		}
	}
	return false
}
