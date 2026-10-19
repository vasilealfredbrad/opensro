/*
===========================================================================

match_test.go - the match state machine against an injected clock

Every test drives time by passing nowMs to Tick and the inputs; nothing
sleeps or reads a real clock. Seeds are fixed, so drop points and zone
circles are the same on every run.

===========================================================================
*/
package battleroyale

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

const (
	testSeed      = 42
	testCountdown = DefaultCountdownMs
	// testStartMs is when a match whose countdown started at 0 begins.
	testStartMs = testCountdown
)

/*
================
testConfig

A lobby by the arena and an arena of the reference size; every other
field takes its default.
================
*/
func testConfig() Config {
	return Config{
		Lobby: Circle{Center: Point{X: 100, Z: 100}, Radius: 50},
		Arena: Circle{Center: Point{X: 5000, Z: 5000}, Radius: ReferenceArenaRadius},
		Seed:  testSeed,
	}
}

/*
================
lobbySpot

A point inside testConfig's lobby circle.
================
*/
func lobbySpot() Point {
	return Point{X: 110, Z: 90}
}

/*
================
mustInit
================
*/
func mustInit(t *testing.T, cfg Config) *Match {
	t.Helper()
	var m Match
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return &m
}

/*
================
mustEnter
================
*/
func mustEnter(t *testing.T, m *Match, ids ...PlayerID) {
	t.Helper()
	for _, id := range ids {
		if err := m.EnterLobby(id, lobbySpot()); err != nil {
			t.Fatalf("EnterLobby(%s): %v", id, err)
		}
	}
}

/*
================
mustNoErr
================
*/
func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

/*
================
startMatch

Fills the lobby with ids, starts the countdown at 0 and the match at
testStartMs. Returns the match and its start event.
================
*/
func startMatch(t *testing.T, cfg Config, ids ...PlayerID) (*Match, MatchStarted) {
	t.Helper()
	m := mustInit(t, cfg)
	mustEnter(t, m, ids...)
	if started := eventsOf[CountdownStarted](m.Tick(0)); len(started) != 1 {
		t.Fatalf("countdown did not start: %+v", started)
	}
	starts := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(starts) != 1 || m.State() != StateRunning {
		t.Fatalf("match did not start: state %v", m.State())
	}
	return m, starts[0]
}

/*
================
eventsOf

The events of one concrete type, in order.
================
*/
func eventsOf[T Event](events []Event) []T {
	var out []T
	for _, event := range events {
		if typed, ok := event.(T); ok {
			out = append(out, typed)
		}
	}
	return out
}

//============================================================================

/*
================
TestInitRefusals
================
*/
func TestInitRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"one player minimum", func(c *Config) { c.MinPlayers = 1 }},
		{"negative max players", func(c *Config) { c.MaxPlayers = -1 }},
		{"max below min", func(c *Config) { c.MinPlayers = 3; c.MaxPlayers = 2 }},
		{"negative countdown", func(c *Config) { c.CountdownMs = -1 }},
		{"negative grace", func(c *Config) { c.DisconnectGraceMs = -1 }},
		{"NaN drop spacing", func(c *Config) { c.MinDropSpacing = math.NaN() }},
		{"negative drop spacing", func(c *Config) { c.MinDropSpacing = -1 }},
		{"unknown race", func(c *Config) { c.DefaultRace = 9 }},
		{"long build", func(c *Config) { c.DefaultBuild = strings.Repeat("b", maxBuildLength+1) }},
		{"no lobby", func(c *Config) { c.Lobby = Circle{} }},
		{"NaN arena", func(c *Config) { c.Arena.Radius = math.NaN() }},
		{"infinite arena centre", func(c *Config) { c.Arena.Center.X = math.Inf(1) }},
		{"non-shrinking schedule", func(c *Config) {
			c.Schedule = Schedule{Phases: []Phase{{WaitMs: 1000, ShrinkMs: 1000, EndRadius: c.Arena.Radius}}}
		}},
	}
	for _, tc := range cases {
		cfg := testConfig()
		tc.mutate(&cfg)
		var m Match
		err := m.Init(cfg)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: error %v, want ErrInvalidConfig", tc.name, err)
		}
		if m.State() != StateNone {
			t.Errorf("%s: state %v after a refused Init", tc.name, m.State())
		}
		if err := m.EnterLobby("alice", lobbySpot()); !errors.Is(err, ErrWrongState) {
			t.Errorf("%s: EnterLobby on a refused match: %v", tc.name, err)
		}
		if events := m.Tick(0); len(events) != 0 {
			t.Errorf("%s: Tick on a refused match: %+v", tc.name, events)
		}
	}
}

/*
================
TestCountdownStartsAtMinPlayers
================
*/
func TestCountdownStartsAtMinPlayers(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "bob")
	if events := m.Tick(0); len(events) != 0 || m.State() != StateLobby {
		t.Fatalf("one player: events %+v state %v", events, m.State())
	}
	mustEnter(t, m, "alice")
	events := m.Tick(500)
	want := []Event{CountdownStarted{AtMs: 500, EndsAtMs: 500 + DefaultCountdownMs, Players: []PlayerID{"alice", "bob"}}}
	if !reflect.DeepEqual(events, want) || m.State() != StateCountdown {
		t.Fatalf("two players: events %+v state %v, want %+v", events, m.State(), want)
	}
}

/*
================
TestCountdownHonoursMinPlayers
================
*/
func TestCountdownHonoursMinPlayers(t *testing.T) {
	cfg := testConfig()
	cfg.MinPlayers = 3
	m := mustInit(t, cfg)
	mustEnter(t, m, "a", "b")
	if events := m.Tick(0); len(events) != 0 {
		t.Fatalf("two of three: %+v", events)
	}
	mustEnter(t, m, "c")
	if started := eventsOf[CountdownStarted](m.Tick(1)); len(started) != 1 {
		t.Fatalf("three of three did not start the countdown")
	}
}

/*
================
TestEnterLobbyRefusals
================
*/
func TestEnterLobbyRefusals(t *testing.T) {
	cfg := testConfig()
	cfg.MaxPlayers = 2
	m := mustInit(t, cfg)
	mustEnter(t, m, "alice", "bob")
	cases := []struct {
		name string
		id   PlayerID
		pos  Point
		want error
	}{
		{"outside the lobby circle", "carol", Point{X: 500, Z: 500}, ErrOutsideLobby},
		{"empty id", "", lobbySpot(), ErrInvalidPlayer},
		{"full", "carol", lobbySpot(), ErrLobbyFull},
		{"member again", "alice", lobbySpot(), nil},
	}
	for _, tc := range cases {
		if err := m.EnterLobby(tc.id, tc.pos); !errors.Is(err, tc.want) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.want)
		}
	}
	if m.LobbyCount() != 2 {
		t.Errorf("lobby holds %d, want 2", m.LobbyCount())
	}
	if err := m.LeaveLobby("nobody"); err != nil {
		t.Errorf("leaving when not a member: %v", err)
	}
}

/*
================
TestCountdownCancelsBelowMinimum

Leaving or disconnecting during the countdown below the minimum cancels
it; a later countdown starts over with the full time.
================
*/
func TestCountdownCancelsBelowMinimum(t *testing.T) {
	drops := []struct {
		name string
		drop func(m *Match, id PlayerID, nowMs int64) error
	}{
		{"leave", func(m *Match, id PlayerID, _ int64) error { return m.LeaveLobby(id) }},
		{"disconnect", func(m *Match, id PlayerID, nowMs int64) error { return m.Disconnect(id, nowMs) }},
	}
	for _, tc := range drops {
		name := tc.name
		m := mustInit(t, testConfig())
		mustEnter(t, m, "alice", "bob")
		m.Tick(0)
		mustNoErr(t, tc.drop(m, "bob", 900))
		events := m.Tick(1000)
		if want := []Event{CountdownCancelled{AtMs: 1000, Players: 1}}; !reflect.DeepEqual(events, want) {
			t.Fatalf("%s: events %+v, want %+v", name, events, want)
		}
		if m.State() != StateLobby {
			t.Fatalf("%s: state %v after cancel", name, m.State())
		}
		mustEnter(t, m, "bob")
		started := eventsOf[CountdownStarted](m.Tick(5000))
		if len(started) != 1 || started[0].EndsAtMs != 5000+DefaultCountdownMs {
			t.Fatalf("%s: restart %+v", name, started)
		}
		if starts := eventsOf[MatchStarted](m.Tick(5000 + DefaultCountdownMs - 1)); len(starts) != 0 {
			t.Fatalf("%s: started a millisecond early", name)
		}
		if starts := eventsOf[MatchStarted](m.Tick(5000 + DefaultCountdownMs)); len(starts) != 1 {
			t.Fatalf("%s: did not start on time", name)
		}
	}
}

/*
================
TestCountdownSurvivesLeaveAndJoinBetweenTicks

A swap between two ticks never drops the count below the minimum, so the
countdown keeps running.
================
*/
func TestCountdownSurvivesLeaveAndJoinBetweenTicks(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "alice", "bob")
	m.Tick(0)
	mustNoErr(t, m.LeaveLobby("bob"))
	mustEnter(t, m, "carol")
	if cancelled := eventsOf[CountdownCancelled](m.Tick(1000)); len(cancelled) != 0 {
		t.Fatalf("swap cancelled the countdown")
	}
	starts := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(starts) != 1 || starts[0].Participants[1].ID != "carol" {
		t.Fatalf("start %+v, want alice and carol", starts)
	}
}

/*
================
TestJoinDuringCountdown
================
*/
func TestJoinDuringCountdown(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "alice", "bob")
	m.Tick(0)
	mustEnter(t, m, "carol")
	events := m.Tick(10_000)
	if started := eventsOf[CountdownStarted](events); len(started) != 0 {
		t.Fatalf("a join restarted the countdown: %+v", started)
	}
	starts := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(starts) != 1 || len(starts[0].Participants) != 3 || m.AliveCount() != 3 {
		t.Fatalf("start %+v, want three participants", starts)
	}
	if err := m.EnterLobby("dave", lobbySpot()); !errors.Is(err, ErrWrongState) {
		t.Errorf("join after the start: %v", err)
	}
}

/*
================
TestCountdownTickSeconds

One-second ticks announce 29 down to 1; coarse ticks announce only the
latest whole second.
================
*/
func TestCountdownTickSeconds(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "alice", "bob")
	m.Tick(0)
	var seconds []int
	for nowMs := int64(1000); nowMs < testStartMs; nowMs += 1000 {
		for _, tick := range eventsOf[CountdownTick](m.Tick(nowMs)) {
			seconds = append(seconds, tick.SecondsLeft)
		}
	}
	if len(seconds) != 29 || seconds[0] != 29 || seconds[28] != 1 {
		t.Fatalf("fine ticks announced %v, want 29..1", seconds)
	}

	coarse := mustInit(t, testConfig())
	mustEnter(t, coarse, "alice", "bob")
	coarse.Tick(0)
	cases := []struct {
		nowMs int64
		want  []Event
	}{
		{500, nil},
		{2500, []Event{CountdownTick{AtMs: 2500, SecondsLeft: 28}}},
		{2900, nil},
		{10_000, []Event{CountdownTick{AtMs: 10_000, SecondsLeft: 20}}},
		{29_999, []Event{CountdownTick{AtMs: 29_999, SecondsLeft: 1}}},
	}
	for _, tc := range cases {
		if got := coarse.Tick(tc.nowMs); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("at %d: %+v, want %+v", tc.nowMs, got, tc.want)
		}
	}
}

/*
================
TestMatchStartedEvent

The start lists the lobby sorted by ID with each choice (or the default),
drops everyone inside the arena and spaced apart, and opens zone phase 1.
================
*/
func TestMatchStartedEvent(t *testing.T) {
	cfg := testConfig()
	m := mustInit(t, cfg)
	mustEnter(t, m, "carol", "alice", "bob")
	mustNoErr(t, m.Choose("alice", RaceChinese, "glaive"))
	mustNoErr(t, m.Choose("bob", RaceEuropean, ""))
	mustNoErr(t, m.Choose("bob", RaceEuropean, "warlock"))
	m.Tick(0)
	events := m.Tick(testStartMs)
	if len(events) != 2 {
		t.Fatalf("start produced %+v, want MatchStarted and ZonePhase", events)
	}
	started, ok := events[0].(MatchStarted)
	if !ok || started.AtMs != testStartMs || started.Arena != cfg.Arena {
		t.Fatalf("first event %+v", events[0])
	}
	want := []struct {
		id    PlayerID
		race  Race
		build string
	}{
		{"alice", RaceChinese, "glaive"},
		{"bob", RaceEuropean, "warlock"},
		{"carol", RaceEuropean, DefaultBuild},
	}
	if len(started.Participants) != len(want) {
		t.Fatalf("%d participants, want %d", len(started.Participants), len(want))
	}
	for i, p := range started.Participants {
		if p.ID != want[i].id || p.Race != want[i].race || p.Build != want[i].build || !p.Alive || !p.Connected {
			t.Errorf("participant %d: %+v, want %+v", i, p, want[i])
		}
		if Outside(Circle{Center: cfg.Arena.Center, Radius: cfg.Arena.Radius * dropEdgeFraction}, p.Drop) {
			t.Errorf("%s drops at %+v, outside the drop area", p.ID, p.Drop)
		}
		for _, other := range started.Participants[:i] {
			if gap := math.Hypot(p.Drop.X-other.Drop.X, p.Drop.Z-other.Drop.Z); gap < DefaultMinDropSpacing {
				t.Errorf("%s and %s drop %v apart, want at least %v", p.ID, other.ID, gap, DefaultMinDropSpacing)
			}
		}
	}
	zone, ok := events[1].(ZonePhase)
	firstWait := ScheduleFor(3, cfg.Arena.Radius).Phases[0].WaitMs
	if !ok || zone.State.Phase != 0 || zone.State.Stage != ZoneWaiting || zone.State.Current != cfg.Arena || zone.State.RemainingMs != firstWait {
		t.Fatalf("second event %+v, want phase 1 waiting %d ms on the arena", events[1], firstWait)
	}
	if m.LobbyCount() != 0 || m.AliveCount() != 3 || len(m.Participants()) != 3 {
		t.Errorf("after start: lobby %d alive %d", m.LobbyCount(), m.AliveCount())
	}
	if p, ok := m.Participant("bob"); !ok || p.Build != "warlock" {
		t.Errorf("Participant(bob) = %+v, %v", p, ok)
	}
}

/*
================
TestMatchIsDeterministic

The same seed and inputs give the same events, whatever the join order;
another seed moves the drop points.
================
*/
func TestMatchIsDeterministic(t *testing.T) {
	run := func(seed uint64, ids ...PlayerID) []Event {
		cfg := testConfig()
		cfg.Seed = seed
		m := mustInit(t, cfg)
		mustEnter(t, m, ids...)
		events := m.Tick(0)
		events = append(events, m.Tick(testStartMs)...)
		for nowMs := int64(testStartMs); nowMs <= testStartMs+15*msPerMinute; nowMs += 5000 {
			events = append(events, m.Tick(nowMs)...)
		}
		return events
	}
	first := run(testSeed, "alice", "bob", "carol")
	if again := run(testSeed, "carol", "alice", "bob"); !reflect.DeepEqual(first, again) {
		t.Fatalf("same seed, different events")
	}
	a := eventsOf[MatchStarted](first)[0]
	b := eventsOf[MatchStarted](run(testSeed+1, "alice", "bob", "carol"))[0]
	if a.Participants[0].Drop == b.Participants[0].Drop {
		t.Errorf("seeds %d and %d drop alice at the same point", testSeed, testSeed+1)
	}
}

/*
================
TestChooseRefusals
================
*/
func TestChooseRefusals(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "alice", "bob")
	cases := []struct {
		name  string
		id    PlayerID
		race  Race
		build string
		want  error
	}{
		{"not a member", "carol", RaceChinese, "x", ErrUnknownPlayer},
		{"unknown race", "alice", Race(7), "x", ErrInvalidRace},
		{"long build", "alice", RaceChinese, strings.Repeat("b", maxBuildLength+1), ErrInvalidBuild},
		{"valid", "alice", RaceChinese, "x", nil},
	}
	for _, tc := range cases {
		if err := m.Choose(tc.id, tc.race, tc.build); !errors.Is(err, tc.want) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.want)
		}
	}
	m.Tick(0)
	m.Tick(testStartMs)
	if err := m.Choose("alice", RaceEuropean, "y"); !errors.Is(err, ErrWrongState) {
		t.Errorf("choice after the start: %v", err)
	}
}

/*
================
TestZonePhaseProgression

With one-second ticks every stage change is reported once, at the tick it
happens: wait, shrink, next wait, next shrink, final.
================
*/
func TestZonePhaseProgression(t *testing.T) {
	cfg := testConfig()
	cfg.Schedule = Schedule{Phases: []Phase{
		{WaitMs: 10_000, ShrinkMs: 5000, EndRadius: 2000, DamagePctPerSec: 1},
		{WaitMs: 4000, ShrinkMs: 3000, EndRadius: 0, DamagePctPerSec: 5, DamageRampPerSec: 1},
	}}
	m, _ := startMatch(t, cfg, "alice", "bob")
	type report struct {
		atMs  int64
		phase int
		stage ZoneStage
	}
	var got []report
	for nowMs := int64(testStartMs + 1000); nowMs <= testStartMs+60_000; nowMs += 1000 {
		for _, zone := range eventsOf[ZonePhase](m.Tick(nowMs)) {
			if zone.Settings != cfg.Schedule.Phases[zone.State.Phase] {
				t.Errorf("at %d: settings %+v for phase %d", nowMs, zone.Settings, zone.State.Phase)
			}
			got = append(got, report{zone.AtMs, zone.State.Phase, zone.State.Stage})
		}
	}
	want := []report{
		{testStartMs + 10_000, 0, ZoneShrinking},
		{testStartMs + 15_000, 1, ZoneWaiting},
		{testStartMs + 19_000, 1, ZoneShrinking},
		{testStartMs + 22_000, 1, ZoneFinal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("zone reports %+v, want %+v", got, want)
	}
	state, ok := m.ZoneAt(testStartMs + 22_000)
	if !ok || state.Current.Radius != 0 {
		t.Errorf("final zone %+v", state)
	}
}

/*
================
TestEliminationOrderAndWinner
================
*/
func TestEliminationOrderAndWinner(t *testing.T) {
	m, _ := startMatch(t, testConfig(), "a", "b", "c", "d")
	steps := []struct {
		nowMs  int64
		id     PlayerID
		killer PlayerID
		reason EliminationReason
		want   PlayerEliminated
	}{
		{40_000, "b", "a", EliminatedKilled, PlayerEliminated{AtMs: 40_000, Player: "b", Killer: "a", Reason: EliminatedKilled, Placement: 4, AliveLeft: 3}},
		{50_000, "d", "", EliminatedZone, PlayerEliminated{AtMs: 50_000, Player: "d", Reason: EliminatedZone, Placement: 3, AliveLeft: 2}},
		{60_000, "c", "a", EliminatedLeftArena, PlayerEliminated{AtMs: 60_000, Player: "c", Killer: "a", Reason: EliminatedLeftArena, Placement: 2, AliveLeft: 1}},
	}
	var last []Event
	for _, step := range steps {
		mustNoErr(t, m.Eliminate(step.id, step.killer, step.reason, step.nowMs))
		last = m.Tick(step.nowMs)
		if got := eventsOf[PlayerEliminated](last); len(got) != 1 || got[0] != step.want {
			t.Fatalf("eliminating %s: %+v, want %+v", step.id, got, step.want)
		}
	}
	ended := eventsOf[MatchEnded](last)
	want := MatchEnded{AtMs: 60_000, Winner: "a", Standings: []PlayerID{"a", "c", "d", "b"}}
	if len(ended) != 1 || !reflect.DeepEqual(ended[0], want) {
		t.Fatalf("end %+v, want %+v", ended, want)
	}
	if winner, _ := m.Participant("a"); winner.Placement != 1 || winner.Kills != 2 || !winner.Alive {
		t.Errorf("winner %+v", winner)
	}
	if m.State() != StateFinished {
		t.Errorf("state %v", m.State())
	}
	if err := m.Eliminate("a", "", EliminatedKilled, 70_000); !errors.Is(err, ErrWrongState) {
		t.Errorf("elimination after the end: %v", err)
	}
	if events := m.Tick(70_000); len(events) != 0 {
		t.Errorf("tick after the end: %+v", events)
	}
}

/*
================
TestSimultaneousLastEliminationsDraw

Two eliminations recorded before the same tick leave nobody alive: a draw.
================
*/
func TestSimultaneousLastEliminationsDraw(t *testing.T) {
	m, _ := startMatch(t, testConfig(), "a", "b")
	mustNoErr(t, m.Eliminate("a", "", EliminatedZone, 45_000))
	mustNoErr(t, m.Eliminate("b", "", EliminatedZone, 45_000))
	events := m.Tick(45_000)
	want := []Event{
		PlayerEliminated{AtMs: 45_000, Player: "a", Reason: EliminatedZone, Placement: 2, AliveLeft: 1},
		PlayerEliminated{AtMs: 45_000, Player: "b", Reason: EliminatedZone, Placement: 1, AliveLeft: 0},
		MatchEnded{AtMs: 45_000, Draw: true, Standings: []PlayerID{"b", "a"}},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events %+v, want %+v", events, want)
	}
}

/*
================
TestEliminateRefusals
================
*/
func TestEliminateRefusals(t *testing.T) {
	lobby := mustInit(t, testConfig())
	if err := lobby.Eliminate("a", "", EliminatedKilled, 0); !errors.Is(err, ErrWrongState) {
		t.Errorf("before the start: %v", err)
	}
	m, _ := startMatch(t, testConfig(), "a", "b", "c")
	mustNoErr(t, m.Eliminate("c", "", EliminatedKilled, 40_000))
	cases := []struct {
		name   string
		id     PlayerID
		killer PlayerID
		reason EliminationReason
		want   error
	}{
		{"unknown", "zed", "", EliminatedKilled, ErrUnknownPlayer},
		{"already out", "c", "", EliminatedKilled, ErrNotAlive},
		{"no reason", "a", "", 0, ErrInvalidReason},
		{"self kill credit", "a", "a", EliminatedKilled, ErrInvalidKiller},
		{"outsider killer", "a", "zed", EliminatedKilled, ErrInvalidKiller},
	}
	for _, tc := range cases {
		if err := m.Eliminate(tc.id, tc.killer, tc.reason, 41_000); !errors.Is(err, tc.want) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.want)
		}
	}
	if m.AliveCount() != 2 {
		t.Errorf("refusals changed the alive count to %d", m.AliveCount())
	}
}

/*
================
TestDisconnectGrace
================
*/
func TestDisconnectGrace(t *testing.T) {
	const disconnectMs = 40_000
	const deadlineMs = disconnectMs + DefaultDisconnectGraceMs

	t.Run("reconnect within grace", func(t *testing.T) {
		m, _ := startMatch(t, testConfig(), "a", "b", "c")
		mustNoErr(t, m.Disconnect("a", disconnectMs))
		mustNoErr(t, m.Disconnect("a", disconnectMs+5000))
		m.Tick(deadlineMs - 1)
		mustNoErr(t, m.Reconnect("a", deadlineMs-1))
		if out := eventsOf[PlayerEliminated](m.Tick(deadlineMs + 10_000)); len(out) != 0 {
			t.Fatalf("reconnected player eliminated: %+v", out)
		}
		if p, _ := m.Participant("a"); !p.Alive || !p.Connected {
			t.Errorf("after reconnect %+v", p)
		}
	})

	t.Run("grace expiry eliminates", func(t *testing.T) {
		m, _ := startMatch(t, testConfig(), "a", "b", "c")
		mustNoErr(t, m.Disconnect("a", disconnectMs))
		if out := eventsOf[PlayerEliminated](m.Tick(deadlineMs - 1)); len(out) != 0 {
			t.Fatalf("eliminated before the deadline: %+v", out)
		}
		out := eventsOf[PlayerEliminated](m.Tick(deadlineMs))
		want := PlayerEliminated{AtMs: deadlineMs, Player: "a", Reason: EliminatedDisconnected, Placement: 3, AliveLeft: 2}
		if len(out) != 1 || out[0] != want {
			t.Fatalf("at the deadline %+v, want %+v", out, want)
		}
	})

	t.Run("reconnect at the deadline is too late", func(t *testing.T) {
		m, _ := startMatch(t, testConfig(), "a", "b", "c")
		mustNoErr(t, m.Disconnect("a", disconnectMs))
		if err := m.Reconnect("a", deadlineMs); !errors.Is(err, ErrGraceExpired) {
			t.Fatalf("late reconnect: %v", err)
		}
		if out := eventsOf[PlayerEliminated](m.Tick(deadlineMs)); len(out) != 1 {
			t.Fatalf("late reconnect not eliminated: %+v", out)
		}
		if err := m.Reconnect("a", deadlineMs+1); !errors.Is(err, ErrNotAlive) {
			t.Errorf("reconnect after elimination: %v", err)
		}
	})

	t.Run("expiries in deadline order", func(t *testing.T) {
		m, _ := startMatch(t, testConfig(), "a", "b", "c")
		mustNoErr(t, m.Disconnect("b", disconnectMs))
		mustNoErr(t, m.Disconnect("a", disconnectMs+1000))
		events := m.Tick(deadlineMs + 30_000)
		out := eventsOf[PlayerEliminated](events)
		if len(out) != 2 || out[0].Player != "b" || out[1].Player != "a" || out[1].Placement != 2 {
			t.Fatalf("expiries %+v, want b then a", out)
		}
		if ended := eventsOf[MatchEnded](events); len(ended) != 1 || ended[0].Winner != "c" {
			t.Fatalf("end %+v, want c to win", ended)
		}
	})
}

/*
================
TestZoneDamageSampling

Damage needs two samples outside in a row, ramps with the continuous time
outside, and restarts from zero after stepping back in.
================
*/
func TestZoneDamageSampling(t *testing.T) {
	cfg := testConfig()
	cfg.Schedule = Schedule{Phases: []Phase{
		{WaitMs: 600_000, ShrinkMs: 1000, EndRadius: 100, DamagePctPerSec: 1, DamageRampPerSec: 0.5},
	}}
	m, _ := startMatch(t, cfg, "alice", "bob")
	outside := Point{X: cfg.Arena.Center.X + cfg.Arena.Radius + 500, Z: cfg.Arena.Center.Z}
	inside := cfg.Arena.Center
	const maxHP = 1000
	steps := []struct {
		name string
		at   int64
		pos  Point
		want int64
	}{
		{"inside", testStartMs, inside, 0},
		{"first sample outside", testStartMs + 1000, outside, 0},
		// D(2 s) = 1*2 + 0.5*4/2 = 3%.
		{"two seconds outside", testStartMs + 3000, outside, 30},
		// D(4 s) - D(2 s) = 5%.
		{"four seconds outside", testStartMs + 5000, outside, 50},
		{"stale sample", testStartMs + 4000, outside, 0},
		{"back inside", testStartMs + 6000, inside, 0},
		{"out again", testStartMs + 7000, outside, 0},
		// The timer restarted: D(1 s) = 1.25% -> 12.
		{"one second outside again", testStartMs + 8000, outside, 12},
	}
	for _, step := range steps {
		got, err := m.ZoneDamage("alice", step.pos, maxHP, step.at)
		if err != nil || got != step.want {
			t.Fatalf("%s: damage %d, %v; want %d", step.name, got, err, step.want)
		}
	}
	if _, err := m.ZoneDamage("zed", outside, maxHP, testStartMs+9000); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("unknown player: %v", err)
	}
	mustNoErr(t, m.Eliminate("bob", "", EliminatedZone, testStartMs+9000))
	if _, err := m.ZoneDamage("bob", outside, maxHP, testStartMs+9000); !errors.Is(err, ErrNotAlive) {
		t.Errorf("eliminated player: %v", err)
	}
	lobby := mustInit(t, cfg)
	if _, err := lobby.ZoneDamage("alice", outside, maxHP, 0); !errors.Is(err, ErrWrongState) {
		t.Errorf("before the start: %v", err)
	}
}

/*
================
TestCancel
================
*/
func TestCancel(t *testing.T) {
	m := mustInit(t, testConfig())
	mustEnter(t, m, "alice")
	mustNoErr(t, m.Cancel("server restart", 700))
	if events := m.Tick(800); !reflect.DeepEqual(events, []Event{MatchCancelled{AtMs: 700, Reason: "server restart"}}) {
		t.Fatalf("lobby cancel: %+v", events)
	}
	if m.State() != StateCancelled {
		t.Fatalf("state %v", m.State())
	}
	if err := m.Cancel("again", 900); !errors.Is(err, ErrWrongState) {
		t.Errorf("second cancel: %v", err)
	}
	if err := m.EnterLobby("bob", lobbySpot()); !errors.Is(err, ErrWrongState) {
		t.Errorf("join after cancel: %v", err)
	}

	running, _ := startMatch(t, testConfig(), "alice", "bob")
	mustNoErr(t, running.Cancel("gm abort", 50_000))
	events := running.Tick(50_000)
	if len(events) != 1 || len(eventsOf[MatchCancelled](events)) != 1 {
		t.Fatalf("running cancel: %+v", events)
	}
	if err := running.Eliminate("alice", "", EliminatedKilled, 51_000); !errors.Is(err, ErrWrongState) {
		t.Errorf("elimination after cancel: %v", err)
	}
}

/*
================
TestDropPoints

With room every point keeps the spacing; without room the points are
still inside the arena, one per participant. Both are seed-determined.
================
*/
func TestDropPoints(t *testing.T) {
	arena := Circle{Center: Point{X: -3000, Z: 7000}, Radius: ReferenceArenaRadius}
	area := Circle{Center: arena.Center, Radius: arena.Radius * dropEdgeFraction}
	cases := []struct {
		name    string
		count   int
		spacing float64
		spaced  bool
	}{
		{"room for all", 10, 500, true},
		{"crowded", 10, 1_000_000, false},
		{"nobody", 0, 500, true},
	}
	for _, tc := range cases {
		for seed := range uint64(100) {
			points := DropPoints(arena, tc.count, tc.spacing, newTestRng(seed))
			if !reflect.DeepEqual(points, DropPoints(arena, tc.count, tc.spacing, newTestRng(seed))) {
				t.Fatalf("%s seed %d: not deterministic", tc.name, seed)
			}
			if len(points) != tc.count {
				t.Fatalf("%s seed %d: %d points, want %d", tc.name, seed, len(points), tc.count)
			}
			for i, p := range points {
				if Outside(area, p) {
					t.Fatalf("%s seed %d: point %+v outside the drop area", tc.name, seed, p)
				}
				if tc.spaced && nearestDistance(p, points[:i]) < tc.spacing {
					t.Fatalf("%s seed %d: point %d closer than %v", tc.name, seed, i, tc.spacing)
				}
			}
		}
	}
}
