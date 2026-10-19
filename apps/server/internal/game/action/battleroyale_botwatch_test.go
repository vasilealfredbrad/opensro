/*
===========================================================================

battleroyale_botwatch_test.go - a bot's counters and stuck relocation

Port-only, not native (docs/battle-royale.md "Stage 8c"). A bot whose
ground clicks never move it is relocated after battleRoyaleBotStuckMs
of walking without progress, onto walkable ground inside the circle the
zone closes to, with the relocation counted and recorded; a bot off the
walkable ground goes after battleRoyaleBotOffGroundMs; a bot fighting
in place is never relocated; the elimination telemetry carries the
counters.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
stepBots

n brain steps of the match's bots, the clock advancing a match tick
before each.
================
*/
func (fx *battleRoyaleFixture) stepBots(m *battleRoyaleMatch, n int) {
	for range n {
		fx.clock.Advance(battleRoyaleTickMs * time.Millisecond)
		fx.botStep(m)
	}
}

/*
================
TestBattleRoyaleStuckBotIsRelocated

The movement lane ignores the bot's clicks (a bot in a navmesh pocket):
it walks for battleRoyaleBotStuckMs without progress and is relocated
once, onto the ground mask inside the keep circle, the relocation
counted, recorded and reported at its elimination.
================
*/
func TestBattleRoyaleStuckBotIsRelocated(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	var events []BattleRoyaleEvent
	fx.rt.RecordBattleRoyale = func(event BattleRoyaleEvent) { events = append(events, event) }
	m := fx.startBotMatch(t)
	bot := m.bots[0]
	fx.idleBotsBut(m, bot)
	// No wolf to farm: the bot roams, a walk.
	wolves, _ := fx.rt.battleRoyaleCensus(m, fx.clock.NowMs())
	for _, wolf := range wolves {
		fx.rt.Monsters.Defeat(testDivision, wolf.gid, fx.clock.Now())
	}
	m.nextLocalSupplyMs, m.nextSupplyMs = 1<<62, 1<<62
	fx.rt.battleRoyale.setup.Move = func(string, *enterworld.Character, []byte) {}
	stuckAt := fx.playerAt(bot.c)
	fx.standBot(t, bot, stuckAt)
	bot.progressAt, bot.progressMs = stuckAt, fx.clock.NowMs()
	fx.stepBots(m, battleRoyaleBotStuckMs/battleRoyaleTickMs-2)
	if bot.relocations != 0 {
		t.Fatalf("relocated after %d ms", battleRoyaleBotStuckMs-2*battleRoyaleTickMs)
	}
	fx.stepBots(m, 4)
	if bot.relocations != 1 {
		t.Fatalf("%d relocations after %d ms stuck", bot.relocations, battleRoyaleBotStuckMs+2*battleRoyaleTickMs)
	}
	at := fx.playerAt(bot.c)
	zone, _ := m.match.ZoneAt(fx.clock.NowMs())
	if battleRoyaleDistance(at, stuckAt) < battleRoyaleBotProgressM*battleroyale.UnitsPerMeter || battleroyale.Outside(battleroyale.BotKeepCircle(zone), at) ||
		!fx.rt.battleRoyaleGroundMask().Contains(at) || bot.at != at {
		t.Fatalf("relocated from %+v to %+v (bot sees %+v)", stuckAt, at, bot.at)
	}
	recorded := 0
	for _, event := range events {
		if event.Kind == "bot_relocated" && event.Character == bot.name && event.Fields["reason"] == "no-progress" {
			recorded++
		}
	}
	if recorded != 1 {
		t.Fatalf("%d relocation records", recorded)
	}
	fields := map[string]string{}
	battleRoyaleBotFields(bot, 2, fields)
	if fields["relocations"] != "1" || fields["kills"] != "2" || fields["skillsCast"] != "0" || fields["itemsPicked"] != "0" {
		t.Fatalf("telemetry %v", fields)
	}
}

/*
================
TestBattleRoyaleOffGroundBotIsRelocated

A bot standing off the walkable ground mask is relocated after
battleRoyaleBotOffGroundMs, sooner than the no-progress wait.
================
*/
func TestBattleRoyaleOffGroundBotIsRelocated(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatch(t)
	bot := m.bots[0]
	fx.idleBotsBut(m, bot)
	fx.rt.battleRoyale.setup.Move = func(string, *enterworld.Character, []byte) {}
	mask := fx.rt.battleRoyaleGroundMask()
	off := battleroyale.Point{X: battleroyale.ArenaCircle().Center.X, Z: battleroyale.ArenaCircle().Center.Z}
	for mask.Contains(off) {
		off.X += battleroyale.GroundMaskStep
	}
	off.X += 20 * battleroyale.GroundMaskStep
	fx.standBot(t, bot, off)
	fx.stepBots(m, battleRoyaleBotOffGroundMs/battleRoyaleTickMs+2)
	if bot.relocations != 1 || !mask.Contains(fx.playerAt(bot.c)) {
		t.Fatalf("off-ground bot: %d relocations, at %+v", bot.relocations, fx.playerAt(bot.c))
	}
}

/*
================
TestBattleRoyaleFightingBotIsNotRelocated

A bot standing still while it fights (its decision is a press or a
hold, not a walk) is never relocated, however long the fight.
================
*/
func TestBattleRoyaleFightingBotIsNotRelocated(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatch(t)
	bot := fx.bladeBot(t, m)
	fx.idleBotsBut(m, bot)
	at := fx.playerAt(bot.c)
	for range 2 * battleRoyaleBotStuckMs / battleRoyaleTickMs {
		fx.clock.Advance(battleRoyaleTickMs * time.Millisecond)
		unlock := fx.rt.lockDivision(testDivision)
		intent := battleroyale.BotIntent{Action: battleroyale.BotAttack, Goal: at, Target: 1}
		relocated := fx.rt.watchBattleRoyaleBot(m, bot, intent, nil, fx.clock.NowMs())
		unlock()
		if relocated {
			t.Fatal("a fighting bot was relocated")
		}
	}
	world := fx.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, bot.name), func() simulation.WorldState { return simulation.SeedWorldState(bot.c) })
	if battleRoyaleSpawnGrid(world.Spawn) != at {
		t.Fatal("the fighting bot moved")
	}
}
