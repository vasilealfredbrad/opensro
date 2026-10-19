/*
===========================================================================

bots_test.go - the stage-5 bots in the match state machine

The GM switch, the one-player quorum, the bot roster of a drop, bot
eliminations and a bot winner, and the level and HP curves the wiring's
brain reads. Time is injected as in match_test.go.

===========================================================================
*/
package battleroyale

import (
	"errors"
	"strings"
	"testing"
)

/*
================
startBotMatch

A lobby of ids with the switch on, started at testStartMs.
================
*/
func startBotMatch(t *testing.T, ids ...PlayerID) (*Match, MatchStarted) {
	t.Helper()
	m := mustInit(t, testConfig())
	mustNoErr(t, m.SetBots(true))
	mustEnter(t, m, ids...)
	if started := eventsOf[CountdownStarted](m.Tick(0)); len(started) != 1 {
		t.Fatalf("countdown did not start with %d players and bots", len(ids))
	}
	starts := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(starts) != 1 || m.State() != StateRunning {
		t.Fatalf("match did not start: state %v", m.State())
	}
	return m, starts[0]
}

/*
================
TestBotSwitchQuorum

Off (native), one lobby member waits; on, one member starts the
countdown, and switching off again cancels it. The switch is refused
once the match runs.
================
*/
func TestBotSwitchQuorum(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "solo")
	if events := m.Tick(0); len(eventsOf[CountdownStarted](events)) != 0 || m.State() != StateLobby || m.BotsEnabled() {
		t.Fatalf("one player without bots left the lobby: %v", m.State())
	}
	mustNoErr(t, m.SetBots(true))
	if started := eventsOf[CountdownStarted](m.Tick(1000)); len(started) != 1 || m.State() != StateCountdown {
		t.Fatalf("one player with bots: state %v", m.State())
	}
	mustNoErr(t, m.SetBots(false))
	if cancelled := eventsOf[CountdownCancelled](m.Tick(2000)); len(cancelled) != 1 || m.State() != StateLobby {
		t.Fatalf("switch off did not cancel: state %v", m.State())
	}
	mustNoErr(t, m.SetBots(true))
	m.Tick(3000)
	if starts := eventsOf[MatchStarted](m.Tick(3000 + testCountdown)); len(starts) != 1 {
		t.Fatal("the bot match did not start")
	}
	if err := m.SetBots(false); !errors.Is(err, ErrWrongState) {
		t.Fatalf("switch while running: %v", err)
	}
}

/*
================
TestBotRosterOfADrop

The drop adds DefaultBotCount bots with unique player-like names that never
repeat a member's (case-insensitively), each with its own drop point; the
players keep the points they would have had alone; everyone is alive.
================
*/
func TestBotRosterOfADrop(t *testing.T) {
	m, started := startBotMatch(t, "ares", "test")
	if len(started.Participants) != 2+DefaultBotCount || m.AliveCount() != 2+DefaultBotCount {
		t.Fatalf("%d participants, %d alive", len(started.Participants), m.AliveCount())
	}
	names := map[string]bool{}
	drops := map[Point]bool{}
	bots := 0
	for _, p := range started.Participants {
		lower := strings.ToLower(string(p.ID))
		if names[lower] {
			t.Fatalf("name %s twice", p.ID)
		}
		names[lower] = true
		drops[p.Drop] = true
		if p.Bot {
			bots++
			if !m.IsBot(p.ID) || p.ID == "" || !p.Alive || !p.Connected {
				t.Fatalf("bot %+v", p)
			}
		} else if m.IsBot(p.ID) {
			t.Fatalf("player %s marked a bot", p.ID)
		}
	}
	if bots != DefaultBotCount || len(drops) != len(started.Participants) {
		t.Fatalf("%d bots, %d distinct drops", bots, len(drops))
	}
	_, aloneStart := startMatch(t, testConfig(), "ares", "test")
	for _, p := range aloneStart.Participants {
		if got, _ := m.Participant(p.ID); got.Drop != p.Drop {
			t.Fatalf("%s drops at %+v with bots, %+v without", p.ID, got.Drop, p.Drop)
		}
	}
}

/*
================
TestBotEliminationsAndBotWinner

A player's kill of a bot and a bot's kill of a player are credited
eliminations; once only a bot stands, the bot wins.
================
*/
func TestBotEliminationsAndBotWinner(t *testing.T) {
	m, started := startBotMatch(t, "gina", "test")
	bots := botsOf(started)
	at := int64(testStartMs + 1000)
	mustNoErr(t, m.Eliminate(bots[0], "test", EliminatedKilled, at))
	out := eventsOf[PlayerEliminated](m.Tick(at))
	if len(out) != 1 || out[0].Player != bots[0] || out[0].Killer != "test" || out[0].AliveLeft != 1+DefaultBotCount {
		t.Fatalf("bot eliminated: %+v", out)
	}
	for _, id := range bots[2:] {
		mustNoErr(t, m.Eliminate(id, bots[1], EliminatedKilled, at+1))
	}
	mustNoErr(t, m.Eliminate("gina", bots[1], EliminatedKilled, at+1))
	m.Tick(at + 1)
	if m.State() != StateRunning || m.AliveCount() != 2 {
		t.Fatalf("state %v, %d alive", m.State(), m.AliveCount())
	}
	mustNoErr(t, m.Eliminate("test", bots[1], EliminatedKilled, at+2))
	ended := eventsOf[MatchEnded](m.Tick(at + 2))
	if len(ended) != 1 || ended[0].Winner != bots[1] || ended[0].Draw || m.State() != StateFinished {
		t.Fatalf("result %+v", ended)
	}
	if p, _ := m.Participant(bots[1]); p.Placement != 1 || p.Kills != DefaultBotCount {
		t.Fatalf("winner %+v", p)
	}
}

/*
================
TestBotsAloneEndTheMatch

When the last player is out the standing bots do not play on: the match
ends at once, the bot with the most kills first.
================
*/
func TestBotsAloneEndTheMatch(t *testing.T) {
	m, started := startBotMatch(t, "test")
	bots := botsOf(started)
	at := int64(testStartMs + 1000)
	mustNoErr(t, m.Eliminate(bots[4], bots[7], EliminatedKilled, at))
	m.Tick(at)
	mustNoErr(t, m.Eliminate("test", bots[3], EliminatedZone, at+1))
	mustNoErr(t, m.Eliminate(bots[5], bots[7], EliminatedKilled, at+1))
	ended := eventsOf[MatchEnded](m.Tick(at + 1))
	if len(ended) != 1 || ended[0].Winner != bots[7] || m.State() != StateFinished || len(ended[0].Standings) != 1+DefaultBotCount {
		t.Fatalf("result %+v", ended)
	}
	places := map[int]bool{}
	for _, p := range m.Participants() {
		if places[p.Placement] || p.Placement < 1 {
			t.Fatalf("placement %d of %s", p.Placement, p.ID)
		}
		places[p.Placement] = true
	}
}

/*
================
botsOf

The bot participants of a start, in ID order.
================
*/
func botsOf(started MatchStarted) []PlayerID {
	var bots []PlayerID
	for _, p := range started.Participants {
		if p.Bot {
			bots = append(bots, p.ID)
		}
	}
	return bots
}

/*
================
TestBotCurves

BotLevel climbs by time and kills and stops at 30; BotMaxHP is a match
character's HP (x2.5 the native curve over 2 STR a level); the rows a bot
fights and shows as are the level's first thief row and the ten level-1
rows.
================
*/
func TestBotCurves(t *testing.T) {
	if BotLevel(0, 0, 0) != 1 || BotLevel(botLevelMs, 0, 0) != 2 || BotLevel(0, botMonsterKillsPerLevel, 0) != 2 ||
		BotLevel(0, 0, 1) != 1+botPlayerKillLevels || BotLevel(1<<40, 0, 0) != int(MatchLevelCap) {
		t.Fatal("level curve")
	}
	if BotMaxHP(1) != 500 || BotMaxHP(30) != 3462 || BotMaxHP(0) != BotMaxHP(1) || BotMaxHP(99) != BotMaxHP(30) {
		t.Fatalf("hp curve %d %d", BotMaxHP(1), BotMaxHP(30))
	}
	for level := 2; level <= int(MatchLevelCap); level++ {
		if BotMaxHP(level) <= BotMaxHP(level-1) {
			t.Fatalf("hp falls at %d", level)
		}
	}
	if BotCodename(1) != "MOB_THIEF_NPC_0001" || BotCodename(30) != "MOB_THIEF_NPC_0291" {
		t.Fatalf("rows %s %s", BotCodename(1), BotCodename(30))
	}
	if BotModel(0) != "MOB_THIEF_NPC_0001" || BotModel(9) != "MOB_THIEF_NPC_0010" || BotModel(10) != BotModel(0) {
		t.Fatal("models")
	}
}
