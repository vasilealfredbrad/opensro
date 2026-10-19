/*
===========================================================================

battleroyale.go - the battle royale mode on the action runtime

PORT-ONLY, NOT NATIVE: SRO has no battle royale (SRO_BATTLE_ROYALE; unset
or "off" is native and installs nothing). docs/battle-royale.md is the
owner-approved design; internal/game/battleroyale is the pure rule core
(lobby, countdown, zone, drop points, builds, the record swap). This file
and its siblings own what the core cannot: where players stand, the
layer of world 29 a match runs in, the character swap inside the store
door, the frames and notices, and the hooks the combat, loot and
progression lanes ask (battleroyale_combat.go).

One match runs per division. Its state is owned by the division operation
lock: the tick step, every request handler and every in-door hook that
touches it already hold that lock. The runtime's own mutex only guards the
map from a division to its match.

===========================================================================
*/

package action

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvBattleRoyale turns the mode on ("on", "1", "true"); anything else is
// native.
const EnvBattleRoyale = "SRO_BATTLE_ROYALE"

const (
	// The lobby circle (Hotan, beside the storage keeper) and the arena
	// stand are the core's places (battleroyale/places.go); the names stay
	// for the glue and its tests.
	battleRoyaleLobbyRegion  = battleroyale.LobbyRegion
	battleRoyaleLobbyX       = battleroyale.LobbyLocalX
	battleRoyaleLobbyZ       = battleroyale.LobbyLocalZ
	battleRoyaleLobbyRadiusM = battleroyale.LobbyRadiusM
	// The arena world's layers hold 16 players each; battleRoyaleMaxLayer
	// bounds the search for a free one.
	battleRoyaleMaxPlayers = 16
	battleRoyaleMaxLayer   = 64
	// battleRoyaleTickMs is the mode's cadence on the action tick: lobby
	// scans, zone damage and countdown edges all run on it.
	battleRoyaleTickMs = 500
	// battleRoyaleLingerMs keeps a finished match on screen before the
	// winner is restored and the layer released.
	battleRoyaleLingerMs = 8_000
	// battleRoyaleWinnerSilk is the winner's prize, through the operator
	// silk grant path (operator_silk.go).
	battleRoyaleWinnerSilk uint32 = 1000
	// battleRoyaleDropAttempts bounds the walkable-ground search per drop
	// point and per monster (Roc Mountain is about 70 % walkable).
	battleRoyaleDropAttempts = 16
	// battleRoyaleStreakRampage and battleRoyaleStreakUnstoppable are the
	// kill counts the streak banners fire at.
	battleRoyaleStreakRampage     = 3
	battleRoyaleStreakUnstoppable = 5
	// battleRoyaleMonsterStream and battleRoyaleLootStream select PCG
	// streams of the match seed beside the core's own.
	battleRoyaleMonsterStream = 0x4d6f6e73 // "Mons"
	battleRoyaleLootStream    = 0x4c6f6f74 // "Loot"
)

// battleRoyaleWorld is the arena world: INS_EVENT_RANDOMMATCH (instance
// data world 29).
const battleRoyaleWorld instance.DefinitionID = 29

/*
================
BattleRoyaleFromEnv
================
*/
func BattleRoyaleFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBattleRoyale))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
BattleRoyaleSetup

What the wiring hands the mode: the creation seeders the match body is
dressed with, the skill catalogue the automatic skill grant walks (nil
grants no skills), the seed source of each match (nil draws from the
clock) and the movement lane's ground click the bots walk with.
================
*/
type BattleRoyaleSetup struct {
	Seeders battleroyale.Seeders
	Skills  enterworld.SkillRowSource
	Seed    func() uint64
	// Move is the movement lane's 0x7738 handler (movement.Runtime.
	// HandleMove): a bot's ground click (stage 8). Nil leaves the bots
	// standing.
	Move func(division string, c *enterworld.Character, payload []byte)
}

/*
================
BattleRoyaleEvent

One telemetry event of the mode (match_started, zone_phase,
player_eliminated, match_ended), for the history journal.
================
*/
type BattleRoyaleEvent struct {
	Kind, Division, Match, Character string
	Fields                           map[string]string
}

/*
================
battleRoyaleRuntime

The mode's state across divisions.
================
*/
type battleRoyaleRuntime struct {
	setup   BattleRoyaleSetup
	mu      sync.Mutex
	matches map[string]*battleRoyaleMatch
	// skills caches each build's learnable rows in learn order.
	skills map[uint8][]enterworld.SkillRow
	// loot draws every match drop, under lootMu (kills of two divisions
	// may settle at once).
	lootMu sync.Mutex
	loot   *rand.Rand
	// airdrops maps a living airdrop guardian's gid to its airdrop
	// (stage 8b, battleroyale_airdrop.go); the loot hook reads it.
	airdrops sync.Map
	// groundMu guards ground, the walkable Roc mask (stage 4), built on
	// first use from the world's ground test (battleRoyaleGroundMask).
	groundMu sync.Mutex
	ground   *battleroyale.GroundMask
	// hidden is the set of division:name lobby members the beta player
	// map leaves out (BattleRoyaleMapHidden reads it inside the character
	// read door, so it is a lock of its own, never the division lock).
	hidden sync.Map
	// botGIDs maps a standing match bot's gid to its division and name
	// (battleroyale_bots.go); the loot hook, which has no division, reads
	// it.
	botGIDs sync.Map
	// headless lists the standing bots for the tick
	// (BattleRoyaleHeadless) and botProfiles their difficulty for the
	// stat writes, both by WorldKey; read outside the division lock.
	headless    sync.Map
	botProfiles sync.Map
	// botStats maps a standing bot's gid to its counters (stage 8c
	// telemetry): the cast and pickup paths bump them without the
	// division lock.
	botStats sync.Map
}

/*
================
battleRoyaleMatch

One division's match: the core state machine and the world it runs in.
================
*/
type battleRoyaleMatch struct {
	division   string
	id         string
	seed       uint64
	match      battleroyale.Match
	lastTickMs int64
	startedMs  int64
	// members and participants are the players who last received a frame
	// for this match, so a change of view reaches exactly them.
	members      map[string]bool
	participants map[string]bool
	restored     map[string]bool
	// lease and world are set while the layer is allocated.
	lease   instance.Lease
	world   instance.ID
	layered bool
	endedMs int64
	winner  string
	draw    bool
	// streaks is the last streak banner each killer earned (stage 2);
	// equipPending names the participants whose bag the next tick
	// re-checks for a better build weapon (a level gained inside the
	// reward door cannot open the inventory door itself).
	streaks      map[string]int
	equipPending map[string]bool
	// botsEnabled, botDifficulty and botCount are the GM bot switch
	// (stages 5 and 7); they carry over into the next lobby. bots are the
	// running match's bots in name order; botNav plans their paths,
	// botRng draws their decisions and loot, nextBotMapMs is the next
	// 0x3FB5 (battleroyale_bots.go, battleroyale_botbrain.go).
	botsEnabled   bool
	botDifficulty battleroyale.BotDifficulty
	botCount      int
	bots          []*battleRoyaleBot
	botNav        *battleroyale.BotNav
	botRng        *rand.Rand
	nextBotMapMs  int64
	// botCommands are the client commands this tick's decisions queued,
	// run once the division lock is released (stage 8).
	botCommands []battleRoyaleBotCommand
	// nextSupplyMs and nextGroundSweepMs are the economy's cadences
	// (stage 6, battleroyale_arena.go): the zone supply, the local supply
	// and the layer's ground-item lifetime and cap.
	nextSupplyMs      int64
	nextLocalSupplyMs int64
	nextGroundSweepMs int64
	// supply sums the wolf supply for its telemetry (stage 8c,
	// battleroyale_supplylog.go).
	supply battleRoyaleSupplyLog
	// airdrop is the phase's airdrop from its announcement until its
	// guardian dies or the phase ends (stage 8b, stage 9).
	airdrop *battleRoyaleAirdrop
}

/*
================
ConfigureBattleRoyale

Turns the mode on. Called once at wiring, before Register.
================
*/
func (rt *Runtime) ConfigureBattleRoyale(setup BattleRoyaleSetup) {
	seed := uint64(0)
	if setup.Seed != nil {
		seed = setup.Seed()
	} else {
		seed = uint64(rt.Now().UnixNano())
	}
	rt.battleRoyale = &battleRoyaleRuntime{
		setup:   setup,
		matches: map[string]*battleRoyaleMatch{},
		skills:  map[uint8][]enterworld.SkillRow{},
		loot:    rand.New(rand.NewPCG(seed, battleRoyaleLootStream)),
	}
}

/*
================
BattleRoyaleEnabled
================
*/
func (rt *Runtime) BattleRoyaleEnabled() bool {
	return rt.battleRoyale != nil
}

/*
================
BattleRoyaleLocked

Whether a character is locked in a match: its row carries the match
character. The social and trade lanes refuse it (enterworld.Deps.
InteractionLocked). The hold is persisted state, so the answer holds
across a restart and needs no match lookup.
================
*/
func (rt *Runtime) BattleRoyaleLocked(division, name string) bool {
	c := rt.findCharacter(division, name)
	return c != nil && battleroyale.Held(c)
}

/*
================
BattleRoyaleHome

Where a restored character stands: the lobby circle's centre, in the
default world. The entry's terrain lift settles the height.
================
*/
func BattleRoyaleHome() domain.WorldSpawn {
	region, x, y, z, angle := int64(battleRoyaleLobbyRegion), battleRoyaleLobbyX, 0.0, battleRoyaleLobbyZ, int64(0)
	return domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}
}

/*
================
BattleRoyaleMapHidden

Whether a character stays off the beta player map (movement.Runtime.
MapHidden, port-only): a lobby member or a match character. Called inside
the character read door; it reads the record it is handed and the mode's
own hidden set, never the store or the division lock. Off, nobody hides.
================
*/
func (rt *Runtime) BattleRoyaleMapHidden(division string, c *enterworld.Character) bool {
	if rt.battleRoyale == nil || c == nil {
		return false
	}
	if battleroyale.Held(c) {
		return true
	}
	_, member := rt.battleRoyale.hidden.Load(simulation.WorldKey(division, c.Name))
	return member
}

/*
================
battleRoyaleLobby

The lobby circle in world grid units.
================
*/
func battleRoyaleLobby() battleroyale.Circle {
	return battleroyale.LobbyCircle()
}

/*
================
battleRoyaleArena

The arena circle: all of Roc Mountain for every player count
(battleroyale.ArenaCircle, stage 4).
================
*/
func battleRoyaleArena() battleroyale.Circle {
	return battleroyale.ArenaCircle()
}

/*
================
battleRoyaleMatchFor

The division's match, opened in the lobby state on first use. The caller
holds the division lock.
================
*/
func (rt *Runtime) battleRoyaleMatchFor(division string, nowMs int64) *battleRoyaleMatch {
	br := rt.battleRoyale
	br.mu.Lock()
	defer br.mu.Unlock()
	if m := br.matches[division]; m != nil {
		return m
	}
	m := &battleRoyaleMatch{division: division}
	rt.openBattleRoyaleLobby(m, nowMs)
	br.matches[division] = m
	return m
}

/*
================
battleRoyaleMatchOf

The division's match if one was opened, without opening one (hooks).
================
*/
func (rt *Runtime) battleRoyaleMatchOf(division string) *battleRoyaleMatch {
	br := rt.battleRoyale
	if br == nil {
		return nil
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.matches[division]
}

/*
================
openBattleRoyaleLobby

A fresh lobby with a new seed; the arena is all of Roc (stage 4).
================
*/
func (rt *Runtime) openBattleRoyaleLobby(m *battleRoyaleMatch, nowMs int64) {
	seed := uint64(nowMs)
	if rt.battleRoyale.setup.Seed != nil {
		seed = rt.battleRoyale.setup.Seed()
	}
	m.seed = seed
	m.id = fmt.Sprintf("%s-%d", m.division, nowMs)
	rt.setBattleRoyaleHidden(m, nil)
	m.members, m.participants, m.restored = map[string]bool{}, map[string]bool{}, map[string]bool{}
	m.streaks, m.equipPending = map[string]int{}, map[string]bool{}
	m.layered, m.endedMs, m.winner, m.draw, m.startedMs = false, 0, "", false, 0
	cfg := battleroyale.Config{
		MaxPlayers:   battleRoyaleMaxPlayers,
		Lobby:        battleRoyaleLobby(),
		Arena:        battleRoyaleArena(),
		DefaultRace:  battleroyale.DefaultPreset().Race,
		DefaultBuild: battleroyale.BuildName(battleroyale.DefaultBuildID),
		Seed:         seed,
		// The final circle must close on ground a player can stand on.
		WalkableTarget: func(p battleroyale.Point) bool { _, ok := rt.battleRoyaleGround(p); return ok },
		// A bot is a character (stage 8): never a name a character holds.
		NameTaken: func(name string) bool { return rt.findCharacter(m.division, name) != nil },
	}
	if err := m.match.Init(cfg); err != nil {
		// The circles are compiled constants: a refusal is a build defect.
		panic(fmt.Sprintf("battle royale: lobby config refused: %v", err))
	}
	// A fresh lobby is always open, so the switch carries over.
	if m.botCount == 0 {
		m.botDifficulty, m.botCount = battleroyale.BotNormal, battleroyale.DefaultBotCount
	}
	_ = m.match.SetBots(m.botsEnabled)
	_ = m.match.SetBotOptions(m.botDifficulty, m.botCount)
}

/*
================
advanceBattleRoyale

The mode's tick step: every division with admitted players runs its
match on the battleRoyaleTickMs cadence; the commands its bots decided
on run after the division lock is released, as a client's packets do
(runBattleRoyaleBotCommands).
================
*/
func (rt *Runtime) advanceBattleRoyale(nowMs int64) []simulation.DivisionFrames {
	if rt.battleRoyale == nil {
		return nil
	}
	var out []simulation.DivisionFrames
	for _, division := range rt.battleRoyaleDivisions() {
		unlock := rt.lockDivision(division)
		m := rt.battleRoyaleMatchFor(division, nowMs)
		var commands []battleRoyaleBotCommand
		if nowMs-m.lastTickMs >= battleRoyaleTickMs {
			m.lastTickMs = nowMs
			out = append(out, rt.tickBattleRoyale(m, nowMs)...)
			commands, m.botCommands = m.botCommands, nil
		}
		unlock()
		out = append(out, rt.runBattleRoyaleBotCommands(division, commands)...)
	}
	return out
}

/*
================
battleRoyaleDivisions

The divisions with an admitted player or an open match, sorted.
================
*/
func (rt *Runtime) battleRoyaleDivisions() []string {
	seen := map[string]bool{}
	rt.characterAdmissions.Range(func(_, value any) bool {
		seen[value.(populationAdmission).division] = true
		return true
	})
	rt.battleRoyale.mu.Lock()
	for division := range rt.battleRoyale.matches {
		seen[division] = true
	}
	rt.battleRoyale.mu.Unlock()
	out := make([]string, 0, len(seen))
	for division := range seen {
		out = append(out, division)
	}
	sort.Strings(out)
	return out
}

/*
================
tickBattleRoyale

One cadence step of a match: the inputs the world supplies (who stands
in the lobby, who left the arena, the zone's damage), then the core's
Tick and its events. The caller holds the division lock.
================
*/
func (rt *Runtime) tickBattleRoyale(m *battleRoyaleMatch, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	switch m.match.State() {
	case battleroyale.StateLobby, battleroyale.StateCountdown:
		rt.scanBattleRoyaleLobby(m, nowMs)
	case battleroyale.StateRunning:
		out = append(out, rt.advanceBattleRoyaleArena(m, nowMs)...)
		rt.advanceBattleRoyaleBots(m, nowMs)
		out = append(out, rt.advanceBattleRoyaleEconomy(m, nowMs)...)
		rt.landBattleRoyaleAirdrop(m, nowMs)
	case battleroyale.StateFinished, battleroyale.StateCancelled:
		if nowMs >= m.endedMs+battleRoyaleLingerMs {
			rt.closeBattleRoyale(m, nowMs)
			return out
		}
	}
	for _, event := range m.match.Tick(nowMs) {
		rt.handleBattleRoyaleEvent(m, event, nowMs)
	}
	return out
}

/*
================
scanBattleRoyaleLobby

Every admitted, living, unheld player of the division standing in the
lobby circle of the default world is a member; anyone else is not. A
changed membership refreshes every member's frame and tells the leaver;
a newly joined member also gets the bot roster (its switch).
================
*/
func (rt *Runtime) scanBattleRoyaleLobby(m *battleRoyaleMatch, nowMs int64) {
	present := map[string]bool{}
	for _, c := range rt.worldResidents(m.division, instance.ID(domain.DefaultWorldInstance)) {
		if battleroyale.Held(c) || !enterworld.CharacterAlive(c) || c.DeletePending {
			continue
		}
		at := rt.liveSpawn(simulation.WorldKey(m.division, c.Name), c, nowMs)
		if worldgeom.IsDungeonRegion(at.RegionID) {
			continue
		}
		pos := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
		if m.match.EnterLobby(battleroyale.PlayerID(c.Name), pos) == nil {
			present[c.Name] = true
		}
	}
	changed := len(present) != len(m.members)
	for name := range m.members {
		if !present[name] {
			changed = true
			_ = m.match.LeaveLobby(battleroyale.PlayerID(name))
		}
	}
	if !changed {
		return
	}
	rt.setBattleRoyaleHidden(m, present)
	left := m.members
	m.members = present
	for name := range left {
		if !present[name] {
			rt.pushBattleRoyaleState(m, name, nowMs)
		}
	}
	rt.pushBattleRoyaleStates(m, present, nowMs)
	joined := map[string]bool{}
	for name := range present {
		if !left[name] {
			joined[name] = true
		}
	}
	rt.pushBattleRoyaleRoster(m, joined)
}

/*
================
setBattleRoyaleHidden

Replaces the division's hidden lobby members (the beta player map's
exclusions) with the current membership. The match characters hide by
their hold and need no entry.
================
*/
func (rt *Runtime) setBattleRoyaleHidden(m *battleRoyaleMatch, members map[string]bool) {
	hidden := &rt.battleRoyale.hidden
	for name := range m.members {
		if !members[name] {
			hidden.Delete(simulation.WorldKey(m.division, name))
		}
	}
	for name := range members {
		hidden.Store(simulation.WorldKey(m.division, name), true)
	}
}

/*
================
worldResidents

The characters admitted to a world's layer, in name order.
================
*/
func (rt *Runtime) worldResidents(division string, world instance.ID) []*enterworld.Character {
	var out []*enterworld.Character
	rt.characterAdmissions.Range(func(_, value any) bool {
		admission := value.(populationAdmission)
		if admission.division == division && admission.lease.ID == world {
			if c := rt.findCharacter(division, admission.name); c != nil {
				out = append(out, c)
			}
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
