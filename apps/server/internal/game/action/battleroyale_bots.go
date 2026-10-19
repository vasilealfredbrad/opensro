/*
===========================================================================

battleroyale_bots.go - the bots of a match: switch, roster, map, bodies

Port-only, not native (battleroyale.go; docs/battle-royale.md "Stage 5:
bots", "Stage 7" and "Stage 8"). The core makes a bot a participant
(battleroyale/bots.go); this file gives it a body and tells the players
about it; battleroyale_botbrain.go drives it:

  - Switch. 0x7FB4 from a GM lobby member sets the division's bot switch,
    difficulty and count (they stay set for the following lobbies until
    a GM changes them); every lobby member hears the 0x3FB4 roster.
  - Body (stage 8). Each bot is a real character no account owns, made
    exactly as a player's match character is (battleroyale.Hold over a
    blank record: the build's race, gender, model, masteries, base
    skills, starter inventory and kit, the automatic skill grant), and
    held by the store as an ephemeral record (enterworld.Deps.Ephemeral,
    data/store/ephemeral.go): it resolves by name and id and goes
    through every commit door, yet never becomes a row, never lists in a
    character select and can never log in. It is admitted to the match
    layer under a synthetic session id (a population membership, the
    natural and potion recovery), and the tick sees it through the same
    snapshot a bound session gets (BattleRoyaleHeadless,
    simulation.Ticker.Headless): every observer spawns it with the
    ordinary player row and its worn equipment, follows its moves, and
    every combat, PvP, loot, growth and death rule is the players' own.
  - Map. Every 2 s every participant of the running match (alive or
    spectating) gets the living bots' positions (0x3FB5).
  - Death. A bot dies like any match character (the elimination is the
    death hook's, battleroyale_combat.go); its corpse lies
    battleRoyaleBotCorpseMs, then the body leaves the world and the
    store (retireBattleRoyaleBot). The close retires every bot left.

The caller of every function holds the division lock.

===========================================================================
*/

package action

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleBotStream seeds the bots' draws (builds, roam goals,
	// potions).
	battleRoyaleBotStream = 0x426f7452 // "BotR"
	// battleRoyaleBotMapMs is the cadence of the 0x3FB5 bot positions.
	battleRoyaleBotMapMs = 2000
	// battleRoyaleBotRedrawM is how far around its ring point a bot whose
	// exact point is not walkable is redrawn first.
	battleRoyaleBotRedrawM = 60
	// battleRoyaleBotIDBase and battleRoyaleBotIDSpan are the character
	// ids a bot takes: the top of the player object band (gid 100000 +
	// id stays below the NPC band at 200000), far above any real
	// allocation; an id a real or ephemeral record holds is skipped.
	battleRoyaleBotIDBase int64 = 99000
	battleRoyaleBotIDSpan int64 = 1000
	// battleRoyaleBotSessionBase marks a bot's synthetic admission
	// session: no transport session id is ever this large.
	battleRoyaleBotSessionBase uint64 = 1 << 62
	// battleRoyaleBotCorpseMs is how long a dead bot lies before its body
	// leaves the world.
	battleRoyaleBotCorpseMs = 5000
	// battleRoyaleBotSessionPrefix names a bot's tick snapshot (never a
	// transport session: the bridge drops pushes to it).
	battleRoyaleBotSessionPrefix = "~bot:"
)

/*
================
battleRoyaleBot

One bot of a match: its participant name, its character and gid (0 until
it stood), its synthetic session, the build it was dealt, and the
brain's state (battleroyale_botbrain.go).
================
*/
type battleRoyaleBot struct {
	name         string
	gid          uint32
	alive        bool
	build        battleroyale.Build
	female       bool
	c            *enterworld.Character
	session      uint64
	retireAtMs   int64
	monsterKills int
	potions      int
	nextThinkMs  int64
	mind         battleroyale.BotMind
	route        battleroyale.BotRoute
	routing      bool
	leg          battleroyale.Point
	at           battleroyale.Point
	// stats are the counters the cast and pickup paths bump (stage 8c);
	// relocations counts the stuck relocations (battleroyale_botstuck.go)
	// and progressAt / progressMs / offGroundMs the stuck watch.
	stats       *battleRoyaleBotStats
	relocations int
	progressAt  battleroyale.Point
	progressMs  int64
	offGroundMs int64
}

/*
================
battleRoyaleBotStats

A bot's counters bumped outside the division lock (stage 8c telemetry):
the skills it cast (attack, heal and other non-buff skills, never the
basic attack), the buffs it cast, the drops it picked up, and the last
skill it cast (tests and debugging).
================
*/
type battleRoyaleBotStats struct {
	skills    atomic.Int64
	buffs     atomic.Int64
	picked    atomic.Int64
	lastSkill atomic.Uint32
}

/*
================
battleRoyaleBotKey

Where a bot's gid leads (the runtime's gid index).
================
*/
type battleRoyaleBotKey struct {
	division, name string
}

/*
================
BattleRoyaleHeadlessCharacter

One standing bot as the tick snapshots it (simulation.Ticker.Headless):
its character, the snapshot's session id, and the population it lives
in.
================
*/
type BattleRoyaleHeadlessCharacter struct {
	Division  string
	SessionID string
	Character *enterworld.Character
	Lease     instance.Lease
}

/*
================
battleRoyaleEphemeralDoor

The composition's door for never-persisted characters
(enterworld.Deps.RegisterEphemeralCharacter).
================
*/
type battleRoyaleEphemeralDoor interface {
	RegisterEphemeralCharacter(division string, c *enterworld.Character) error
	ForgetEphemeralCharacter(c *enterworld.Character)
}

//============================================================================

/*
================
HandleBattleRoyaleBots

0x7FB4 (wire.DecodeBattleRoyaleBots: [u8 enabled][u8 difficulty][u16
count], or the stage-5 one byte): a GM lobby member, during the lobby or
the countdown, sets the next match's bots. A sender the last scan did
not admit is scanned for at once, as for the pick. Every lobby member
hears the 0x3FB4 roster of an accepted switch; a refused one is answered
with the roster the server holds, so the button shows the truth.
================
*/
func (rt *Runtime) HandleBattleRoyaleBots(division string, c *enterworld.Character, payload []byte) OpResult {
	if rt.battleRoyale == nil {
		return OpResult{DiagnosticRefusal: "battle-royale-bots: the mode is off"}
	}
	request, err := wire.DecodeBattleRoyaleBots(payload)
	if err != nil || c == nil {
		return OpResult{DiagnosticRefusal: "battle-royale-bots: malformed"}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	nowMs := rt.Now().UnixMilli()
	m := rt.battleRoyaleMatchFor(division, nowMs)
	answer := func(refusal string) OpResult {
		return OpResult{Frames: []wire.Frame{rt.battleRoyaleRosterFrame(m)}, DiagnosticRefusal: refusal}
	}
	if !c.GMPrivilege {
		return answer("battle-royale-bots: not a GM")
	}
	state := m.match.State()
	if state != battleroyale.StateLobby && state != battleroyale.StateCountdown {
		return answer("battle-royale-bots: no lobby open")
	}
	if _, _, member := m.match.LobbyBuild(battleroyale.PlayerID(c.Name)); !member {
		rt.scanBattleRoyaleLobby(m, nowMs)
	}
	if _, _, member := m.match.LobbyBuild(battleroyale.PlayerID(c.Name)); !member {
		return answer("battle-royale-bots: not a lobby member")
	}
	difficulty := battleroyale.BotDifficulty(request.Difficulty)
	if err := m.match.SetBotOptions(difficulty, request.Count); err != nil {
		return answer("battle-royale-bots: " + err.Error())
	}
	if err := m.match.SetBots(request.Enabled); err != nil {
		return answer("battle-royale-bots: " + err.Error())
	}
	m.botsEnabled, m.botDifficulty, m.botCount = request.Enabled, difficulty, request.Count
	others := map[string]bool{}
	for name := range m.members {
		if name != c.Name {
			others[name] = true
		}
	}
	rt.pushBattleRoyaleRoster(m, others)
	return answer("")
}

/*
================
battleRoyaleRosterFrame

The 0x3FB4 roster of a match: the switch, difficulty and count, and every
bot that stood in it, dead ones included, so the scoreboard can still
mark their rows. The encoder always sends a valid difficulty and count
(the client ends the session on any other).
================
*/
func (rt *Runtime) battleRoyaleRosterFrame(m *battleRoyaleMatch) wire.Frame {
	var bots []wire.BattleRoyaleBot
	for _, bot := range m.bots {
		if bot.gid != 0 {
			bots = append(bots, wire.BattleRoyaleBot{GID: bot.gid, Name: bot.name})
		}
	}
	return wire.BattleRoyaleBotRosterFrame(wire.BattleRoyaleBotRoster{Enabled: m.botsEnabled, Difficulty: uint8(m.botDifficulty),
		Configured: m.botCount}, bots)
}

/*
================
pushBattleRoyaleRoster

The roster to each named player, in name order.
================
*/
func (rt *Runtime) pushBattleRoyaleRoster(m *battleRoyaleMatch, names map[string]bool) {
	if rt.PushCharacterFrames == nil || len(names) == 0 {
		return
	}
	frames := []wire.Frame{rt.battleRoyaleRosterFrame(m)}
	for _, name := range sortedNames(names) {
		rt.PushCharacterFrames(m.division, name, frames)
	}
}

/*
================
BattleRoyaleEntryRoster

The roster a world entry appends after the state frame when the match
concerns the character (a lobby member, or a participant rejoining).
================
*/
func (rt *Runtime) BattleRoyaleEntryRoster(division string, c *enterworld.Character) (wire.Frame, bool) {
	if rt.battleRoyale == nil || c == nil {
		return wire.Frame{}, false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	m := rt.battleRoyaleMatchOf(division)
	if m == nil || rt.battleRoyaleStateFor(m, c.Name, rt.Now().UnixMilli()).State == wire.BattleRoyaleStateNone {
		return wire.Frame{}, false
	}
	return rt.battleRoyaleRosterFrame(m), true
}

/*
================
pushBattleRoyaleBotMap

Every battleRoyaleBotMapMs while a bot lives: the 0x3FB5 positions of
the living bots (where the brain last saw them, grid units, and their
level) to every participant of the match, alive or spectating.
================
*/
func (rt *Runtime) pushBattleRoyaleBotMap(m *battleRoyaleMatch, nowMs int64) {
	if nowMs < m.nextBotMapMs || rt.PushCharacterFrames == nil {
		return
	}
	m.nextBotMapMs = nowMs + battleRoyaleBotMapMs
	var bots []wire.BattleRoyaleBotPosition
	for _, bot := range m.bots {
		if bot.alive && bot.c != nil {
			bots = append(bots, wire.BattleRoyaleBotPosition{GID: bot.gid, X: float32(bot.at.X), Z: float32(bot.at.Z),
				Level: uint8(min(max(rewardLevel(bot.c), 1), 0xff))})
		}
	}
	if len(bots) == 0 {
		return
	}
	frames := []wire.Frame{wire.BattleRoyaleBotPositionsFrame(bots)}
	for _, name := range sortedNames(m.participants) {
		rt.PushCharacterFrames(m.division, name, frames)
	}
}

//============================================================================

/*
================
spawnBattleRoyaleBots

The drop's bots, once the players stand: each bot participant stands as
a character of a build drawn from the match seed, on walkable ground at
its ring point around the players (redrawn within battleRoyaleBotRedrawM
of it, then anywhere in the arena, when the exact point is not
walkable). A bot that cannot stand is out before it starts. Returns how
many stood.
================
*/
func (rt *Runtime) spawnBattleRoyaleBots(m *battleRoyaleMatch, started battleroyale.MatchStarted, nowMs int64) int {
	m.bots = nil
	m.botRng = rand.New(rand.NewPCG(m.seed, battleRoyaleBotStream))
	m.botNav = battleroyale.NewBotNav(rt.battleRoyaleGroundMask())
	m.botNav.SetWalkTest(rt.battleRoyaleBotWalk())
	m.nextBotMapMs = nowMs
	profile := battleroyale.ProfileFor(m.botDifficulty)
	mask := rt.battleRoyaleGroundMask()
	arena := mask.Within(started.Arena)
	builds := battleroyale.Builds()
	stood := 0
	for _, p := range started.Participants {
		if !p.Bot {
			continue
		}
		i := len(m.bots)
		bot := &battleRoyaleBot{name: string(p.ID), build: builds[m.botRng.IntN(len(builds))], female: m.botRng.IntN(2) == 1, at: p.Drop,
			// Stagger the decisions across the ticks of one think period.
			nextThinkMs: nowMs + int64(i%max(int(profile.ThinkMs/battleRoyaleTickMs), 1))*battleRoyaleTickMs}
		m.bots = append(m.bots, bot)
		at, ok := rt.battleRoyaleGround(p.Drop)
		if !ok {
			at, ok = rt.battleRoyaleRedraw(mask.Within(battleroyale.Circle{Center: p.Drop, Radius: battleRoyaleBotRedrawM * battleroyale.UnitsPerMeter}), m.botRng)
		}
		if !ok {
			at, ok = rt.battleRoyaleRedraw(arena, m.botRng)
		}
		if !ok || !rt.standBattleRoyaleBot(m, bot, at, nowMs) {
			_ = m.match.Eliminate(p.ID, "", battleroyale.EliminatedLeftArena, nowMs)
			continue
		}
		stood++
	}
	return stood
}

/*
================
standBattleRoyaleBot

Makes the bot's character and puts it in the world at at: a blank
record of a free bot id dressed by the hold exactly as a player's match
character (battleroyale.Hold with the store's creation seeds, the build's
skills and kit), standing at at in the match layer, held as an ephemeral
record, admitted to the layer's population under its synthetic session,
given the natural and potion recovery of a world resident, and listed
for the tick (BattleRoyaleHeadless). Anything refused undoes what stood.
================
*/
func (rt *Runtime) standBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot, at simulation.Spawn, nowMs int64) bool {
	door, ok := rt.deps.(battleRoyaleEphemeralDoor)
	if !ok {
		return false
	}
	id, ok := rt.freeBattleRoyaleBotID(m.division)
	if !ok {
		return false
	}
	gender := int64(domain.GenderMale)
	if bot.female {
		gender = int64(domain.GenderFemale)
	}
	c := &enterworld.Character{ID: id, Name: bot.name, Gender: &gender}
	if err := battleroyale.Hold(c, bot.build, uint32(m.world), nowMs, rt.battleRoyale.setup.Seeders); err != nil {
		return false
	}
	rt.learnBattleRoyaleSkills(c, bot.build)
	rt.dressBattleRoyaleKit(c, bot.build)
	region, x, y, z, angle := int64(at.RegionID), at.X, at.Y, at.Z, int64(0)
	c.World.Spawn = &domain.WorldSpawn{RegionID: &region, X: &x, Y: &y, Z: &z, Angle: &angle}
	if err := door.RegisterEphemeralCharacter(m.division, c); err != nil {
		return false
	}
	key := simulation.WorldKey(m.division, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn, w.MoveSegment, w.SpawnSet, w.MovementSourceSeeded = at, nil, true, true
	})
	session := battleRoyaleBotSessionBase + uint64(id)
	if !rt.admitBattleRoyaleBot(m, c, session) {
		rt.Worlds.Forget(key)
		door.ForgetEphemeralCharacter(c)
		return false
	}
	rt.bindRecoverySessionLocked(m.division, c, session)
	bot.c, bot.session, bot.gid, bot.alive = c, session, enterworld.ObjectIDForCharacter(c), true
	bot.at = battleRoyaleSpawnGrid(at)
	bot.progressAt, bot.progressMs, bot.stats = bot.at, nowMs, &battleRoyaleBotStats{}
	br := rt.battleRoyale
	br.botGIDs.Store(bot.gid, battleRoyaleBotKey{division: m.division, name: bot.name})
	br.botStats.Store(bot.gid, bot.stats)
	br.botProfiles.Store(key, battleroyale.ProfileFor(m.botDifficulty))
	br.headless.Store(key, BattleRoyaleHeadlessCharacter{Division: m.division, Character: c, Lease: m.lease,
		SessionID: fmt.Sprintf("%s%s:%d", battleRoyaleBotSessionPrefix, m.division, id)})
	return true
}

/*
================
admitBattleRoyaleBot

The bot's membership of the match layer under its synthetic session, as
admitPopulationSession takes a player's: past the layer's player
capacity (the arena's 16), since the bots are the match's own fill.
================
*/
func (rt *Runtime) admitBattleRoyaleBot(m *battleRoyaleMatch, c *enterworld.Character, session uint64) bool {
	gid := enterworld.ObjectIDForCharacter(c)
	if rt.Monsters.AdmitPopulationPC(m.division, m.lease, gid, true) != instance.Success {
		return false
	}
	key := simulation.WorldKey(m.division, c.Name)
	rt.characterAdmissions.Store(key, populationAdmission{session: session, gid: gid, lease: m.lease, division: m.division, name: c.Name})
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	return true
}

/*
================
freeBattleRoyaleBotID

The first bot id of the band no record of the division holds.
================
*/
func (rt *Runtime) freeBattleRoyaleBotID(division string) (int64, bool) {
	lookup, ok := rt.deps.(domain.CharacterLookup)
	if !ok {
		return 0, false
	}
	for id := battleRoyaleBotIDBase; id < battleRoyaleBotIDBase+battleRoyaleBotIDSpan; id++ {
		if lookup.CharacterByID(division, id) == nil {
			return id, true
		}
	}
	return 0, false
}

/*
================
retireBattleRoyaleBot

The body leaves: off the tick (every observer gets the despawn), out of
the population and every runtime plane (forgetCharacterLocked, the
session close's path), then out of the store. Idempotent.
================
*/
func (rt *Runtime) retireBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot) {
	if bot.c == nil {
		return
	}
	key := simulation.WorldKey(m.division, bot.name)
	br := rt.battleRoyale
	br.headless.Delete(key)
	rt.forgetCharacterLocked(m.division, bot.name)
	if door, ok := rt.deps.(battleRoyaleEphemeralDoor); ok {
		door.ForgetEphemeralCharacter(bot.c)
	}
	br.botGIDs.Delete(bot.gid)
	br.botStats.Delete(bot.gid)
	br.botProfiles.Delete(key)
	bot.c, bot.alive, bot.routing = nil, false, false
}

/*
================
retireDeadBattleRoyaleBots

Every dead bot whose corpse has lain battleRoyaleBotCorpseMs leaves.
================
*/
func (rt *Runtime) retireDeadBattleRoyaleBots(m *battleRoyaleMatch, nowMs int64) {
	for _, bot := range m.bots {
		if bot.c != nil && !bot.alive && bot.retireAtMs != 0 && nowMs >= bot.retireAtMs {
			rt.retireBattleRoyaleBot(m, bot)
		}
	}
}

/*
================
BattleRoyaleHeadless

The standing bots of every division, in session id order, for the tick
(simulation.Ticker.Headless). It reads the mode's own index only, never
the division lock or the store.
================
*/
func (rt *Runtime) BattleRoyaleHeadless() []BattleRoyaleHeadlessCharacter {
	if rt.battleRoyale == nil {
		return nil
	}
	var out []BattleRoyaleHeadlessCharacter
	rt.battleRoyale.headless.Range(func(_, value any) bool {
		out = append(out, value.(BattleRoyaleHeadlessCharacter))
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}

/*
================
battleRoyaleBotCharacters

The standing bots' characters of a division (the reward roster lists
them beside the stored characters). Reads the mode's own index only.
================
*/
func (rt *Runtime) battleRoyaleBotCharacters(division string) []*enterworld.Character {
	var out []*enterworld.Character
	for _, h := range rt.BattleRoyaleHeadless() {
		if h.Division == division {
			out = append(out, h.Character)
		}
	}
	return out
}

/*
================
BattleRoyaleHeadlessGID

The object id of a standing bot of division named name: the peer
delivery of a character without a session (its frames go to the
sessions observing that gid). Reads the mode's own index only.
================
*/
func (rt *Runtime) BattleRoyaleHeadlessGID(division, name string) (uint32, bool) {
	if rt.battleRoyale == nil {
		return 0, false
	}
	value, ok := rt.battleRoyale.headless.Load(simulation.WorldKey(division, name))
	if !ok {
		return 0, false
	}
	return enterworld.ObjectIDForCharacter(value.(BattleRoyaleHeadlessCharacter).Character), true
}

/*
================
battleRoyaleBotProfile

The difficulty profile of a standing bot's character, or false for every
other character (the stat writes ask it, battleroyale_combat.go).
================
*/
func (rt *Runtime) battleRoyaleBotProfile(division string, c *enterworld.Character) (battleroyale.BotProfile, bool) {
	if rt.battleRoyale == nil || c == nil {
		return battleroyale.BotProfile{}, false
	}
	value, ok := rt.battleRoyale.botProfiles.Load(simulation.WorldKey(division, c.Name))
	if !ok {
		return battleroyale.BotProfile{}, false
	}
	return value.(battleroyale.BotProfile), true
}

/*
================
battleRoyaleBotWalk

The planner's walk test (battleroyale.BotNav.SetWalkTest): whether the
ordinary movement clip (the steering owner's PlanPath) walks straight
from a to b, unclipped. Nil without a planner (the mask alone).
================
*/
func (rt *Runtime) battleRoyaleBotWalk() func(a, b battleroyale.Point) bool {
	if rt.MonsterSteer == nil || rt.MonsterSteer.PlanPath == nil {
		return nil
	}
	plan := rt.MonsterSteer.PlanPath
	return func(a, b battleroyale.Point) bool {
		from, _ := rt.battleRoyaleSurface(a)
		to, _ := rt.battleRoyaleSurface(b)
		path := plan(battleRoyaleMonsterPose(from), battleRoyaleMonsterPose(to))
		if path == nil || path.Result()&(battleRoyaleNavBlocked) != 0 {
			return false
		}
		rest := battleRoyalePoseGrid(path.Rest())
		return battleRoyaleDistance(rest, b) < battleRoyaleBotWalkReach
	}
}

/*
================
battleRoyaleBotOf

The bot of m whose character has gid, or nil.
================
*/
func (m *battleRoyaleMatch) battleRoyaleBotOf(gid uint32) *battleRoyaleBot {
	if gid == 0 {
		return nil
	}
	for _, bot := range m.bots {
		if bot.gid == gid {
			return bot
		}
	}
	return nil
}

/*
================
battleRoyaleBotNamed

The bot of m named name, or nil.
================
*/
func (m *battleRoyaleMatch) battleRoyaleBotNamed(name string) *battleRoyaleBot {
	for _, bot := range m.bots {
		if bot.name == name {
			return bot
		}
	}
	return nil
}

/*
================
battleRoyaleBotOf

The bot behind a gid, in any division (the loot hook has no division),
or false. From stage 8 a bot's gid is a player object id, so no monster
gid ever names one.
================
*/
func (rt *Runtime) battleRoyaleBotOf(gid uint32) (battleRoyaleBotKey, bool) {
	if rt.battleRoyale == nil {
		return battleRoyaleBotKey{}, false
	}
	key, ok := rt.battleRoyale.botGIDs.Load(gid)
	if !ok {
		return battleRoyaleBotKey{}, false
	}
	return key.(battleRoyaleBotKey), true
}

/*
================
forgetBattleRoyaleBots

The match closes: every bot still standing (alive or a corpse) leaves.
================
*/
func (rt *Runtime) forgetBattleRoyaleBots(m *battleRoyaleMatch) {
	for _, bot := range m.bots {
		rt.retireBattleRoyaleBot(m, bot)
	}
	m.bots, m.botNav, m.botCommands = nil, nil, nil
}

//============================================================================

/*
================
battleRoyaleBotDied

A monster died through a settled kill (settleMonsterInsideDoor). From
stage 8 no monster is a bot, so nothing happens; the hook stays for the
kill lanes that call it.
================
*/
func (rt *Runtime) battleRoyaleBotDied(string, *enterworld.Character, uint32, int64) {}

/*
================
battleRoyaleBotEliminated

A bot participant is out (any reason): the brain stops and its corpse
leaves battleRoyaleBotCorpseMs later.
================
*/
func (m *battleRoyaleMatch) battleRoyaleBotEliminated(name string, nowMs int64) {
	if bot := m.battleRoyaleBotNamed(name); bot != nil && bot.alive {
		bot.alive, bot.routing, bot.retireAtMs = false, false, nowMs+battleRoyaleBotCorpseMs
	}
}

/*
================
battleRoyaleBotFields

The telemetry of a bot's elimination: its level, the degrees of its worn
weapon and best worn armor and their sum over every worn piece, its
participant kills, monster kills (wolfKills), potions drunk, and from
stage 8c the skills and buffs it cast, the drops it picked up and the
times it was relocated when stuck.
================
*/
func battleRoyaleBotFields(bot *battleRoyaleBot, kills int, fields map[string]string) {
	fields["bot"] = "1"
	fields["kills"] = strconv.Itoa(kills)
	fields["relocations"] = strconv.Itoa(bot.relocations)
	if bot.stats != nil {
		fields["skillsCast"] = strconv.FormatInt(bot.stats.skills.Load(), 10)
		fields["buffsCast"] = strconv.FormatInt(bot.stats.buffs.Load(), 10)
		fields["itemsPicked"] = strconv.FormatInt(bot.stats.picked.Load(), 10)
	}
	if bot.c == nil {
		return
	}
	weapon, armor := battleRoyaleWornDegrees(bot.c)
	fields["botLevel"] = strconv.FormatInt(rewardLevel(bot.c), 10)
	fields["build"] = battleroyale.BuildName(bot.build.ID)
	fields["weaponDegree"] = strconv.Itoa(weapon)
	fields["armorDegree"] = strconv.Itoa(armor)
	fields["gearDegrees"] = strconv.Itoa(battleRoyaleWornDegreeSum(bot.c))
	fields["wolfKills"] = strconv.Itoa(bot.monsterKills)
	fields["potions"] = strconv.Itoa(bot.potions)
}

/*
================
battleRoyaleWornDegrees

The degree of the worn weapon and of the best worn armor piece (0
none), read off the Moon/Sun codenames (..._0d_...).
================
*/
func battleRoyaleWornDegrees(c *enterworld.Character) (weapon, armor int) {
	for _, row := range c.MissionInventory {
		degree := battleRoyaleItemDegree(row.Codename)
		switch {
		case row.Slot == int64(inventory.SocketWeapon):
			weapon = degree
		case row.Slot >= int64(inventory.SocketHead) && row.Slot <= int64(inventory.SocketFoot):
			armor = max(armor, degree)
		}
	}
	return weapon, armor
}

/*
================
battleRoyaleItemDegree

The degree word (01-04) of a match item codename, 0 for none.
================
*/
func battleRoyaleItemDegree(codename string) int {
	for _, word := range strings.Split(codename, "_") {
		if degree, err := strconv.Atoi(word); err == nil && len(word) == 2 && degree >= 1 && degree <= 4 {
			return degree
		}
	}
	return 0
}

/*
================
battleRoyaleDistance

The planar distance between two grid points.
================
*/
func battleRoyaleDistance(a, b battleroyale.Point) float64 {
	return math.Hypot(a.X-b.X, a.Z-b.Z)
}
