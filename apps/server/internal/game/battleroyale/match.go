/*
===========================================================================

match.go - the battle royale match: lobby, countdown, running, result

Port-only, not native (see zone.go). One Match owns one round from the
lobby to its result:

	Lobby --enough players--> Countdown --timer--> Running --<=1 alive--> Finished
	  ^                           |
	  +-----below minimum---------+        (any live state) --Cancel--> Cancelled

The owner calls Init once, feeds player inputs as they happen (EnterLobby,
LeaveLobby, Choose, Eliminate, Disconnect, Reconnect, Cancel) and calls Tick
every frame. Inputs only record facts; every rule that depends on counts or
time (starting or cancelling the countdown, starting the match, expiring a
disconnect, declaring the result) is decided in Tick. Two eliminations
recorded before the same Tick are therefore simultaneous: if they leave
nobody alive the match is a draw.

Time is injected: every call that needs the clock takes nowMs, and nothing
reads a real clock. Randomness comes from Config.Seed through two private
PCG streams (drop points, zone circles), so a seed and an input sequence
replay a match exactly.

A Match is not safe for concurrent use; its owner (the GameWorld tick)
serializes calls.

===========================================================================
*/
package battleroyale

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"

	"opensro.online/server/internal/domain"
)

const (
	// DefaultMinPlayers is the smallest lobby that starts a countdown.
	DefaultMinPlayers = 2
	// DefaultCountdownMs is the lobby countdown (30 s).
	DefaultCountdownMs = 30_000
	// DefaultDisconnectGraceMs is how long a disconnected participant may
	// reconnect before being eliminated (60 s).
	DefaultDisconnectGraceMs = 60_000
	// DefaultMinDropSpacing keeps drop points 50 m apart when the arena
	// has room.
	DefaultMinDropSpacing = 50 * UnitsPerMeter
	// DefaultBuild names the build a participant fights with when it does
	// not choose one; the wiring maps build names to loadouts.
	DefaultBuild = "default"

	// maxBuildLength bounds a chosen build name (player input).
	maxBuildLength = 64
	// dropStream and zoneStream select the two PCG streams of one seed.
	dropStream = 0x44726f70 // "Drop"
	zoneStream = 0x5a6f6e65 // "Zone"
)

var (
	// ErrInvalidConfig is the identity of every Init refusal.
	ErrInvalidConfig = errors.New("battleroyale: invalid match config")
	// ErrWrongState: the input does not apply in the match's state.
	ErrWrongState = errors.New("battleroyale: input not valid in this match state")
	// ErrInvalidPlayer: the player ID is empty.
	ErrInvalidPlayer = errors.New("battleroyale: empty player id")
	// ErrUnknownPlayer: the player is not in the lobby or the match.
	ErrUnknownPlayer = errors.New("battleroyale: player not in this match")
	// ErrOutsideLobby: the player does not stand inside the lobby circle.
	ErrOutsideLobby = errors.New("battleroyale: player is outside the lobby circle")
	// ErrLobbyFull: the lobby already holds MaxPlayers.
	ErrLobbyFull = errors.New("battleroyale: lobby is full")
	// ErrInvalidRace: the race is neither Chinese nor European.
	ErrInvalidRace = errors.New("battleroyale: invalid race")
	// ErrInvalidBuild: the build name is too long.
	ErrInvalidBuild = errors.New("battleroyale: invalid build")
	// ErrInvalidReason: the elimination reason is not one of the four.
	ErrInvalidReason = errors.New("battleroyale: invalid elimination reason")
	// ErrInvalidKiller: the killer is the victim or not a participant.
	ErrInvalidKiller = errors.New("battleroyale: invalid killer")
	// ErrNotAlive: the participant is already eliminated.
	ErrNotAlive = errors.New("battleroyale: participant already eliminated")
	// ErrGraceExpired: the reconnect came after the disconnect grace.
	ErrGraceExpired = errors.New("battleroyale: disconnect grace expired")
)

/*
================
PlayerID

Identifies a player to the match. The wiring chooses the key (the
character name is the natural one); the match only compares and sorts it.
================
*/
type PlayerID string

/*
================
Race

The race a participant fights as. The values are the domain race indices
(domain.RaceEurope 0, domain.RaceChina 1), so the zero value is European.
================
*/
type Race int64

const (
	RaceEuropean Race = Race(domain.RaceEurope)
	RaceChinese  Race = Race(domain.RaceChina)
)

/*
================
Race.String
================
*/
func (r Race) String() string {
	switch r {
	case RaceEuropean:
		return "european"
	case RaceChinese:
		return "chinese"
	}
	return fmt.Sprintf("Race(%d)", int64(r))
}

/*
================
Race.valid
================
*/
func (r Race) valid() bool {
	return r == RaceEuropean || r == RaceChinese
}

/*
================
MatchState

The match's lifecycle state. The zero value, StateNone, is a Match whose
Init has not run or failed; every input refuses it.
================
*/
type MatchState int

const (
	StateNone MatchState = iota
	StateLobby
	StateCountdown
	StateRunning
	StateFinished
	StateCancelled
)

/*
================
MatchState.String
================
*/
func (s MatchState) String() string {
	switch s {
	case StateNone:
		return "none"
	case StateLobby:
		return "lobby"
	case StateCountdown:
		return "countdown"
	case StateRunning:
		return "running"
	case StateFinished:
		return "finished"
	case StateCancelled:
		return "cancelled"
	}
	return fmt.Sprintf("MatchState(%d)", int(s))
}

/*
================
EliminationReason

Why a participant left the match. Killed covers any death (a player, a
monster or the zone's last tick when the wiring cannot tell); Zone is a
death the wiring attributes to outside damage; LeftArena is walking,
teleporting or returning out of the arena; Disconnected is a disconnect
that outlived the grace period.
================
*/
type EliminationReason int

const (
	EliminatedKilled EliminationReason = iota + 1
	EliminatedZone
	EliminatedLeftArena
	EliminatedDisconnected
)

/*
================
EliminationReason.String
================
*/
func (r EliminationReason) String() string {
	switch r {
	case EliminatedKilled:
		return "killed"
	case EliminatedZone:
		return "zone"
	case EliminatedLeftArena:
		return "left-arena"
	case EliminatedDisconnected:
		return "disconnected"
	}
	return fmt.Sprintf("EliminationReason(%d)", int(r))
}

/*
================
EliminationReason.valid
================
*/
func (r EliminationReason) valid() bool {
	return r >= EliminatedKilled && r <= EliminatedDisconnected
}

/*
================
Config

One match's rules. Zero fields take the defaults above, except Lobby and
Arena, which the wiring must place (both need a positive radius), and
MaxPlayers, where 0 means no cap. An empty Schedule means ScheduleFor(the
participant count, Arena.Radius) at start; a given one must pass
ValidateSchedule against Arena.Radius.
================
*/
type Config struct {
	MinPlayers        int
	MaxPlayers        int
	CountdownMs       int64
	DisconnectGraceMs int64
	Lobby             Circle
	Arena             Circle
	Schedule          Schedule
	MinDropSpacing    float64
	DefaultRace       Race
	DefaultBuild      string
	Seed              uint64
	// WalkableTarget, when set, is asked about every target circle's
	// centre the zone plan draws (PlanZoneFiltered): a centre it refuses
	// is redrawn, so the final circle closes on ground a player can stand
	// on; the drop points are drawn under the same veto
	// (DropPointsFiltered). Nil accepts every draw.
	WalkableTarget func(Point) bool
	// NameTaken, when set, is asked about every bot name the drop draws
	// (bots.go): a name it reports taken (an existing character's, since
	// a bot is a real character from stage 8) is skipped. Nil takes
	// every name a lobby member does not hold.
	NameTaken func(string) bool
}

/*
================
Participant

One player of a running match. Placement is 0 while alive (1 for the
winner once the match ends); Reason, Killer and EliminatedAtMs are set on
elimination. DisconnectedAtMs is meaningful only while Connected is false.
Bot marks a server-driven fighter (bots.go): it has no build and never
disconnects. The unexported fields are the zone-damage sampler of
ZoneDamage.
================
*/
type Participant struct {
	ID               PlayerID
	Race             Race
	Build            string
	Drop             Point
	Alive            bool
	Connected        bool
	Kills            int
	Placement        int
	Reason           EliminationReason
	Killer           PlayerID
	EliminatedAtMs   int64
	DisconnectedAtMs int64
	Bot              bool

	outside          bool
	outsideSinceMs   int64
	lastZoneSampleMs int64
}

/*
================
lobbyEntry

A lobby member's current choice.
================
*/
type lobbyEntry struct {
	race  Race
	build string
}

/*
================
Match

One battle royale round. See the file header for the lifecycle.
================
*/
type Match struct {
	cfg   Config
	state MatchState

	// Lobby and Countdown. choices remembers every pick made in this
	// lobby, members or not, so a player who steps out of the circle (or
	// is scanned out for one tick) and back keeps its build.
	lobby            map[PlayerID]lobbyEntry
	choices          map[PlayerID]lobbyEntry
	countdownEndMs   int64
	secondsAnnounced int
	// bots is the GM switch (bots.go): the next drop adds botCount bots
	// of botDifficulty (stage 7).
	bots          bool
	botCount      int
	botDifficulty BotDifficulty

	// Running and after.
	participants map[PlayerID]*Participant
	order        []PlayerID // participants sorted by ID
	alive        int
	eliminated   []PlayerID // elimination order
	zone         Zone
	zoneAnnounce bool
	zonePhase    int
	zoneStage    ZoneStage

	// Events recorded by inputs, returned by the next Tick.
	pending []Event
}

//============================================================================

/*
================
Match.Init

Validates cfg, fills its defaults and opens the lobby. A refused config
leaves the Match in StateNone.
================
*/
func (m *Match) Init(cfg Config) error {
	*m = Match{}
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return err
	}
	m.cfg = cfg
	m.state = StateLobby
	m.botCount, m.botDifficulty = DefaultBotCount, BotNormal
	m.lobby = make(map[PlayerID]lobbyEntry)
	m.choices = make(map[PlayerID]lobbyEntry)
	return nil
}

/*
================
normalizeConfig

Fills the zero fields of cfg with the defaults and refuses what a match
cannot run. With no schedule given, every ScheduleFor result the match
could ask for (player counts are held outside 2..10) is validated here, so
the start of a match can treat a bad schedule as a programmer error.
================
*/
func normalizeConfig(cfg Config) (Config, error) {
	if cfg.MinPlayers == 0 {
		cfg.MinPlayers = DefaultMinPlayers
	}
	if cfg.CountdownMs == 0 {
		cfg.CountdownMs = DefaultCountdownMs
	}
	if cfg.DisconnectGraceMs == 0 {
		cfg.DisconnectGraceMs = DefaultDisconnectGraceMs
	}
	if cfg.MinDropSpacing == 0 {
		cfg.MinDropSpacing = DefaultMinDropSpacing
	}
	if cfg.DefaultBuild == "" {
		cfg.DefaultBuild = DefaultBuild
	}
	switch {
	case cfg.MinPlayers < DefaultMinPlayers:
		return cfg, fmt.Errorf("%w: min players %d below %d", ErrInvalidConfig, cfg.MinPlayers, DefaultMinPlayers)
	case cfg.MaxPlayers < 0 || (cfg.MaxPlayers > 0 && cfg.MaxPlayers < cfg.MinPlayers):
		return cfg, fmt.Errorf("%w: max players %d", ErrInvalidConfig, cfg.MaxPlayers)
	case cfg.CountdownMs < 0:
		return cfg, fmt.Errorf("%w: countdown %d ms", ErrInvalidConfig, cfg.CountdownMs)
	case cfg.DisconnectGraceMs < 0:
		return cfg, fmt.Errorf("%w: disconnect grace %d ms", ErrInvalidConfig, cfg.DisconnectGraceMs)
	case !isFinite(cfg.MinDropSpacing) || cfg.MinDropSpacing < 0:
		return cfg, fmt.Errorf("%w: drop spacing %v", ErrInvalidConfig, cfg.MinDropSpacing)
	case len(cfg.DefaultBuild) > maxBuildLength:
		return cfg, fmt.Errorf("%w: default build too long", ErrInvalidConfig)
	case !cfg.DefaultRace.valid():
		return cfg, fmt.Errorf("%w: default race %v", ErrInvalidConfig, cfg.DefaultRace)
	case !validCircle(cfg.Lobby):
		return cfg, fmt.Errorf("%w: lobby circle %+v", ErrInvalidConfig, cfg.Lobby)
	case !validCircle(cfg.Arena):
		return cfg, fmt.Errorf("%w: arena circle %+v", ErrInvalidConfig, cfg.Arena)
	}
	if len(cfg.Schedule.Phases) > 0 {
		if err := ValidateSchedule(cfg.Schedule, cfg.Arena.Radius); err != nil {
			return cfg, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
		}
		cfg.Schedule = Schedule{Phases: slices.Clone(cfg.Schedule.Phases)}
		return cfg, nil
	}
	for players := fewPlayers; players <= manyPlayers; players++ {
		if err := ValidateSchedule(ScheduleFor(players, cfg.Arena.Radius), cfg.Arena.Radius); err != nil {
			return cfg, fmt.Errorf("%w: arena radius %v: %w", ErrInvalidConfig, cfg.Arena.Radius, err)
		}
	}
	return cfg, nil
}

/*
================
validCircle
================
*/
func validCircle(c Circle) bool {
	return isFinite(c.Center.X) && isFinite(c.Center.Z) && isFinite(c.Radius) && c.Radius > 0
}

//============================================================================

/*
================
Match.Tick

Advances the match to nowMs and returns everything that happened since the
last Tick: first the events inputs recorded, then the ones this tick
decides. A finished, cancelled or uninitialized match returns only what
inputs left pending.
================
*/
func (m *Match) Tick(nowMs int64) []Event {
	events := m.pending
	m.pending = nil
	switch m.state {
	case StateLobby:
		events = m.tickLobby(nowMs, events)
	case StateCountdown:
		events = m.tickCountdown(nowMs, events)
	case StateRunning:
		events = m.tickRunning(nowMs, events)
	}
	return events
}

/*
================
Match.tickLobby

Starts the countdown once the lobby holds its quorum (MinPlayers, or one
member while bots are on).
================
*/
func (m *Match) tickLobby(nowMs int64, events []Event) []Event {
	if len(m.lobby) < m.quorum() {
		return events
	}
	m.state = StateCountdown
	m.countdownEndMs = nowMs + m.cfg.CountdownMs
	m.secondsAnnounced = secondsLeft(m.cfg.CountdownMs)
	return append(events, CountdownStarted{AtMs: nowMs, EndsAtMs: m.countdownEndMs, Players: m.lobbyIDs()})
}

/*
================
Match.tickCountdown

Cancels the countdown when the lobby fell below MinPlayers, starts the
match when it ran out, and otherwise announces each new whole second.
================
*/
func (m *Match) tickCountdown(nowMs int64, events []Event) []Event {
	if len(m.lobby) < m.quorum() {
		m.state = StateLobby
		return append(events, CountdownCancelled{AtMs: nowMs, Players: len(m.lobby)})
	}
	if nowMs >= m.countdownEndMs {
		return m.startMatch(nowMs, events)
	}
	left := secondsLeft(m.countdownEndMs - nowMs)
	if left < m.secondsAnnounced {
		m.secondsAnnounced = left
		events = append(events, CountdownTick{AtMs: nowMs, SecondsLeft: left})
	}
	return events
}

/*
================
Match.startMatch

Turns the lobby into participants (sorted by ID, so the drop assignment
depends on the seed and the set of players, not on join order), adds the
bots when the switch is on (dropped in a ring around the players), plans
the zone from the arena and reports the start and the first zone phase.
================
*/
func (m *Match) startMatch(nowMs int64, events []Event) []Event {
	players := m.lobbyIDs()
	bots := m.botIDs(players)
	slices.Sort(bots)
	ids := slices.Sorted(slices.Values(append(slices.Clone(players), bots...)))
	isBot := make(map[PlayerID]bool, len(bots))
	for _, id := range bots {
		isBot[id] = true
	}
	schedule := m.cfg.Schedule
	if len(schedule.Phases) == 0 {
		schedule = ScheduleFor(len(ids), m.cfg.Arena.Radius)
		if err := ValidateSchedule(schedule, m.cfg.Arena.Radius); err != nil {
			// normalizeConfig validated every count ScheduleFor distinguishes.
			panic(fmt.Sprintf("battleroyale: scaled schedule invalid after Init: %v", err))
		}
	}
	// The players take the points they would have had alone; the bots
	// drop in a ring around them (stage 7, BotRingPoints).
	drops := DropPointsFiltered(m.cfg.Arena, len(players), m.cfg.MinDropSpacing, rand.New(rand.NewPCG(m.cfg.Seed, dropStream)), m.cfg.WalkableTarget)
	drops = append(drops, BotRingPoints(drops, len(bots), m.cfg.Arena, rand.New(rand.NewPCG(m.cfg.Seed, botRingStream)), m.cfg.WalkableTarget)...)
	m.zone = PlanZoneFiltered(schedule, m.cfg.Arena, nowMs, rand.New(rand.NewPCG(m.cfg.Seed, zoneStream)), m.cfg.WalkableTarget)

	m.participants = make(map[PlayerID]*Participant, len(ids))
	m.order = ids
	started := make([]Participant, len(ids))
	// The players keep the drop points they would have had alone; the
	// bots take the ring points after them, in name order.
	order := append(players, bots...)
	for i, id := range order {
		entry := m.lobby[id]
		p := &Participant{ID: id, Race: entry.race, Build: entry.build, Drop: drops[i], Alive: true, Connected: true, Bot: isBot[id]}
		m.participants[id] = p
	}
	for i, id := range ids {
		started[i] = *m.participants[id]
	}
	m.alive = len(ids)
	m.lobby, m.choices = nil, nil
	m.state = StateRunning
	events = append(events, MatchStarted{AtMs: nowMs, Arena: m.cfg.Arena, Participants: started})
	return m.announceZone(nowMs, events)
}

/*
================
Match.tickRunning

Expires disconnects past their grace, ends the match when at most one
participant is alive (or, with bots, when no player is), and otherwise
reports a zone phase change.
================
*/
func (m *Match) tickRunning(nowMs int64, events []Event) []Event {
	events = m.expireDisconnects(nowMs, events)
	if m.alive <= 1 {
		return m.finish(nowMs, events)
	}
	if m.bots && !m.playersAlive() {
		return m.finishAmongBots(nowMs, events)
	}
	return m.announceZone(nowMs, events)
}

/*
================
Match.expireDisconnects

Eliminates every disconnected participant whose grace ran out by nowMs,
earliest deadline first (ties by ID), so placements do not depend on how
coarse the ticks are.
================
*/
func (m *Match) expireDisconnects(nowMs int64, events []Event) []Event {
	var expired []*Participant
	for _, id := range m.order {
		p := m.participants[id]
		if p.Alive && !p.Connected && nowMs >= p.DisconnectedAtMs+m.cfg.DisconnectGraceMs {
			expired = append(expired, p)
		}
	}
	slices.SortStableFunc(expired, func(a, b *Participant) int {
		return cmp.Compare(a.DisconnectedAtMs, b.DisconnectedAtMs)
	})
	for _, p := range expired {
		events = append(events, m.eliminate(p, "", EliminatedDisconnected, nowMs))
	}
	return events
}

/*
================
Match.finish

Records the result: the one participant alive wins (placement 1); none
alive is a draw.
================
*/
func (m *Match) finish(nowMs int64, events []Event) []Event {
	m.state = StateFinished
	ended := MatchEnded{AtMs: nowMs, Draw: m.alive == 0}
	for _, id := range m.order {
		if p := m.participants[id]; p.Alive {
			p.Placement = 1
			ended.Winner = id
			ended.Standings = append(ended.Standings, id)
		}
	}
	for i := len(m.eliminated) - 1; i >= 0; i-- {
		ended.Standings = append(ended.Standings, m.eliminated[i])
	}
	return append(events, ended)
}

/*
================
Match.announceZone

Reports the zone when its phase or stage differs from the last report.
================
*/
func (m *Match) announceZone(nowMs int64, events []Event) []Event {
	state := CircleAt(m.zone, nowMs)
	if m.zoneAnnounce && state.Phase == m.zonePhase && state.Stage == m.zoneStage {
		return events
	}
	m.zoneAnnounce = true
	m.zonePhase = state.Phase
	m.zoneStage = state.Stage
	return append(events, ZonePhase{AtMs: nowMs, State: state, Settings: m.zone.Schedule.Phases[state.Phase]})
}

/*
================
Match.eliminate

Takes p out of the match and returns the event. Its placement is the alive
count before it left; a credited killer gains a kill.
================
*/
func (m *Match) eliminate(p *Participant, killer PlayerID, reason EliminationReason, nowMs int64) PlayerEliminated {
	p.Alive = false
	p.Placement = m.alive
	p.Reason = reason
	p.Killer = killer
	p.EliminatedAtMs = nowMs
	m.alive--
	m.eliminated = append(m.eliminated, p.ID)
	if killer != "" {
		m.participants[killer].Kills++
	}
	return PlayerEliminated{AtMs: nowMs, Player: p.ID, Killer: killer, Reason: reason, Placement: p.Placement, AliveLeft: m.alive}
}

//============================================================================

/*
================
Match.EnterLobby

Adds a player standing at pos to the lobby, during the lobby or the
countdown, with the build it chose earlier in this lobby or else the
default race and build. pos must lie inside the lobby circle. Entering
again is a no-op that keeps the earlier choice.
================
*/
func (m *Match) EnterLobby(id PlayerID, pos Point) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	if id == "" {
		return ErrInvalidPlayer
	}
	if Outside(m.cfg.Lobby, pos) {
		return ErrOutsideLobby
	}
	if _, ok := m.lobby[id]; ok {
		return nil
	}
	if m.cfg.MaxPlayers > 0 && len(m.lobby) >= m.cfg.MaxPlayers {
		return ErrLobbyFull
	}
	entry, chosen := m.choices[id]
	if !chosen {
		entry = lobbyEntry{race: m.cfg.DefaultRace, build: m.cfg.DefaultBuild}
	}
	m.lobby[id] = entry
	return nil
}

/*
================
Match.LeaveLobby

Removes a player from the lobby (walked out, logged out). Leaving when not
a member is a no-op: the player is already out.
================
*/
func (m *Match) LeaveLobby(id PlayerID) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	delete(m.lobby, id)
	return nil
}

/*
================
Match.Choose

Records a lobby member's race and build, during the lobby or the
countdown; the last choice before the start wins. An empty build means
the default build. The choice is also remembered for the rest of this
lobby, so a member who leaves the circle and comes back keeps it.
================
*/
func (m *Match) Choose(id PlayerID, race Race, build string) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	if _, ok := m.lobby[id]; !ok {
		return ErrUnknownPlayer
	}
	if !race.valid() {
		return ErrInvalidRace
	}
	if build == "" {
		build = m.cfg.DefaultBuild
	}
	if len(build) > maxBuildLength {
		return ErrInvalidBuild
	}
	entry := lobbyEntry{race: race, build: build}
	m.lobby[id] = entry
	m.choices[id] = entry
	return nil
}

/*
================
Match.Eliminate

Records that a live participant is out (death, zone, leaving the arena).
killer, when not empty, must be another participant and is credited the
kill. The event comes out of the next Tick, which also decides the result.
================
*/
func (m *Match) Eliminate(id, killer PlayerID, reason EliminationReason, nowMs int64) error {
	if m.state != StateRunning {
		return ErrWrongState
	}
	p, ok := m.participants[id]
	if !ok {
		return ErrUnknownPlayer
	}
	if !p.Alive {
		return ErrNotAlive
	}
	if !reason.valid() {
		return ErrInvalidReason
	}
	if killer != "" {
		if _, ok := m.participants[killer]; !ok || killer == id {
			return ErrInvalidKiller
		}
	}
	m.pending = append(m.pending, m.eliminate(p, killer, reason, nowMs))
	return nil
}

/*
================
Match.Disconnect

Before the start a disconnect is leaving the lobby. While running, a live
participant starts its grace period (a second disconnect keeps the first
time); Tick eliminates it once DisconnectGraceMs has passed. An eliminated
participant's disconnect changes nothing.
================
*/
func (m *Match) Disconnect(id PlayerID, nowMs int64) error {
	switch m.state {
	case StateLobby, StateCountdown:
		return m.LeaveLobby(id)
	case StateRunning:
		p, ok := m.participants[id]
		if !ok {
			return ErrUnknownPlayer
		}
		if p.Alive && p.Connected {
			p.Connected = false
			p.DisconnectedAtMs = nowMs
		}
		return nil
	}
	return ErrWrongState
}

/*
================
Match.Reconnect

A disconnected participant returns before its grace ends (the deadline
itself is too late, matching expireDisconnects). Reconnecting while
connected is a no-op.
================
*/
func (m *Match) Reconnect(id PlayerID, nowMs int64) error {
	if m.state != StateRunning {
		return ErrWrongState
	}
	p, ok := m.participants[id]
	if !ok {
		return ErrUnknownPlayer
	}
	if !p.Alive {
		return ErrNotAlive
	}
	if p.Connected {
		return nil
	}
	if nowMs >= p.DisconnectedAtMs+m.cfg.DisconnectGraceMs {
		return ErrGraceExpired
	}
	p.Connected = true
	p.DisconnectedAtMs = 0
	return nil
}

/*
================
Match.Cancel

Aborts the match from the lobby, the countdown or a running match; the
next Tick reports MatchCancelled.
================
*/
func (m *Match) Cancel(reason string, nowMs int64) error {
	switch m.state {
	case StateLobby, StateCountdown, StateRunning:
		m.state = StateCancelled
		m.pending = append(m.pending, MatchCancelled{AtMs: nowMs, Reason: reason})
		return nil
	}
	return ErrWrongState
}

/*
================
Match.ZoneDamage

Samples a live participant's position against the zone at nowMs and
returns the HP to remove for the time since its previous sample. Only an
interval that starts and ends outside deals damage: the first sample
outside starts the continuous-outside timer, and a sample inside resets
it. The active phase's settings apply to the whole interval. The wiring
calls this once per player per tick, applies the damage and calls
Eliminate(id, "", EliminatedZone, nowMs) when HP reaches zero.
================
*/
func (m *Match) ZoneDamage(id PlayerID, pos Point, maxHP int64, nowMs int64) (int64, error) {
	if m.state != StateRunning {
		return 0, ErrWrongState
	}
	p, ok := m.participants[id]
	if !ok {
		return 0, ErrUnknownPlayer
	}
	if !p.Alive {
		return 0, ErrNotAlive
	}
	state := CircleAt(m.zone, nowMs)
	outside := Outside(state.Current, pos)
	wasOutside := p.outside
	var damage int64
	if outside && wasOutside && nowMs > p.lastZoneSampleMs {
		phase := m.zone.Schedule.Phases[state.Phase]
		damage = OutsideDamage(maxHP, phase, p.lastZoneSampleMs-p.outsideSinceMs, nowMs-p.lastZoneSampleMs)
	}
	if outside && !wasOutside {
		p.outsideSinceMs = nowMs
	}
	p.outside = outside
	// A sample from the past never rewinds the outside timer.
	if !wasOutside || nowMs > p.lastZoneSampleMs {
		p.lastZoneSampleMs = nowMs
	}
	return damage, nil
}

/*
================
Match.lobbyIDs

The lobby members, sorted.
================
*/
func (m *Match) lobbyIDs() []PlayerID {
	return slices.Sorted(maps.Keys(m.lobby))
}

/*
================
secondsLeft

Milliseconds rounded up to whole seconds, the countdown display.
================
*/
func secondsLeft(ms int64) int {
	return int((ms + msPerSecond - 1) / msPerSecond)
}
