/*
===========================================================================

battleroyale_test.go - the battle royale mode on the action runtime

Port-only, not native. The fixture is the combat runtime with N level-1
characters admitted to the default world and standing in the Wind Town
lobby circle; the clock is injected and every match seed fixed, so a
scenario replays exactly. Frames are captured per character.

===========================================================================
*/

package action

import (
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

// battleRoyaleTestSeed fixes every match of a fixture.
const battleRoyaleTestSeed = 7

/*
================
battleRoyaleFixture
================
*/
type battleRoyaleFixture struct {
	rt     *Runtime
	clock  *fakeClock
	chars  []*enterworld.Character
	frames map[string][]wire.Frame
	silk   map[string]uint32
	sword  enterworld.InventoryRow
	// roster and movement are the bot fixture's (newBattleRoyaleBotFixture):
	// the ephemeral door and the ground click.
	roster   *enterworld.EphemeralRoster
	movement *movement.Runtime
}

/*
================
battleRoyaleWallet
================
*/
type battleRoyaleWallet struct{ granted map[string]uint32 }

/*
================
GrantSilk
================
*/
func (w *battleRoyaleWallet) GrantSilk(accountID string, amount uint32) (domain.MallBalance, error) {
	w.granted[accountID] += amount
	return domain.MallBalance{Silk: w.granted[accountID]}, nil
}

/*
================
battleRoyaleTestMonsters

A population that knows the match's only monster, the level-5 Baroi
wolf.
================
*/
func battleRoyaleTestMonsters(clock *fakeClock) *simulation.MonsterState {
	wolf := monster.MonsterRef{RefObjID: 5854, TidWord: 0x00c6, Codename: battleroyale.MatchMonsterCodename, Level: 5, MaxHP: 300,
		BodyRadius: 6, ExpToGive: 30, RewardActionPinned: true, CombatPinned: true}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{wolf.RefObjID: wolf}, nil))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	return monsters
}

/*
================
newBattleRoyaleFixture

n characters in the lobby circle, the mode on with a fixed seed.
================
*/
func newBattleRoyaleFixture(t *testing.T, n int) *battleRoyaleFixture {
	t.Helper()
	rt, clock, first, _ := newCombatTestRuntime(t, 100000)
	fx := &battleRoyaleFixture{rt: rt, clock: clock, frames: map[string][]wire.Frame{}, silk: map[string]uint32{}}
	fx.sword = first.MissionInventory[0]
	deps := rt.deps.(*enterworld.Deps)
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	rt.Monsters = battleRoyaleTestMonsters(clock)
	rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	first.AccountID = "account-1"
	fx.chars = []*enterworld.Character{first}
	for i := 1; i < n; i++ {
		c := domain.CloneCharacter(first)
		c.ID, c.Name, c.AccountID = int64(10+i), fmt.Sprintf("bot%d", i), fmt.Sprintf("account-%d", 10+i)
		c.BattleUntilMs = math.MaxInt64
		fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], c)
		fx.chars = append(fx.chars, c)
	}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { fx.frames[name] = append(fx.frames[name], frames...) }
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	rt.PushShardPeerFrames = func(_, _ string, frames []wire.Frame) { fx.frames["*"] = append(fx.frames["*"], frames...) }
	rt.SilkWallet = &battleRoyaleWallet{granted: fx.silk}
	rt.ConfigureBattleRoyale(BattleRoyaleSetup{Seeders: fx.seeders(), Seed: func() uint64 { return battleRoyaleTestSeed }})
	for _, c := range fx.chars {
		if err := rt.AdmitCharacterSession(testDivision, c.Name, uint64(c.ID)); err != nil {
			t.Fatal(err)
		}
		fx.place(c, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	}
	return fx
}

/*
================
seeders

The match body of the fixture's roster: the CH adventurer or the EU
noble, the fixture sword, the base sword skill.
================
*/
func (fx *battleRoyaleFixture) seeders() battleroyale.Seeders {
	sword := fx.sword
	return battleroyale.Seeders{
		Model: func(race battleroyale.Race, _ int64) (string, uint32, bool) {
			if race == battleroyale.RaceChinese {
				return "CHAR_CH_MAN_ADVENTURER", 1907, true
			}
			return "CHAR_EU_MAN_NOBLE", 14717, true
		},
		Skills:    func(string, []uint32) ([]uint32, error) { return []uint32{2}, nil },
		Inventory: func(c *domain.Character) { c.MissionInventory = []domain.InventoryRow{sword} },
	}
}

/*
================
place
================
*/
func (fx *battleRoyaleFixture) place(c *enterworld.Character, at simulation.Spawn) {
	fx.rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn, w.MoveSegment, w.SpawnSet, w.MovementSourceSeeded = at, nil, true, true
		})
}

/*
================
tick

Runs the mode's step every half second for d, draining the skill
finalizes a strike left open, as the runtime tick does.
================
*/
func (fx *battleRoyaleFixture) tick(d time.Duration) {
	for elapsed := time.Duration(0); elapsed < d; elapsed += battleRoyaleTickMs * time.Millisecond {
		fx.clock.Advance(battleRoyaleTickMs * time.Millisecond)
		fx.rt.drainSkillFinalizes(fx.clock.NowMs())
		fx.rt.advanceBattleRoyale(fx.clock.NowMs())
	}
}

/*
================
match
================
*/
func (fx *battleRoyaleFixture) match() *battleRoyaleMatch {
	return fx.rt.battleRoyaleMatchOf(testDivision)
}

/*
================
lastState

The last 0x3FB2 frame a character received.
================
*/
func (fx *battleRoyaleFixture) lastState(t *testing.T, name string) wire.BattleRoyaleState {
	t.Helper()
	for i := len(fx.frames[name]) - 1; i >= 0; i-- {
		if f := fx.frames[name][i]; f.Opcode == wire.OpBattleRoyaleState {
			return decodeBattleRoyaleState(t, f.Payload)
		}
	}
	t.Fatalf("%s received no state frame", name)
	return wire.BattleRoyaleState{}
}

/*
================
decodeBattleRoyaleState

The frame's fixed head, read back for the assertions.
================
*/
func decodeBattleRoyaleState(t *testing.T, payload []byte) wire.BattleRoyaleState {
	t.Helper()
	r := wire.NewReader(payload)
	var s wire.BattleRoyaleState
	version, _ := r.U8()
	if version != wire.BattleRoyaleStateVersion {
		t.Fatalf("state version %d", version)
	}
	s.State, _ = r.U8()
	s.Role, _ = r.U8()
	s.Build, _ = r.U8()
	circle := func() wire.BattleRoyaleCircle {
		x, _ := r.U32()
		z, _ := r.U32()
		radius, _ := r.U32()
		return wire.BattleRoyaleCircle{X: math.Float32frombits(x), Z: math.Float32frombits(z), Radius: math.Float32frombits(radius)}
	}
	s.Lobby = circle()
	s.Members, _ = r.U8()
	s.Alive, _ = r.U8()
	s.CountdownMs, _ = r.U32()
	s.Phase, _ = r.U8()
	s.Phases, _ = r.U8()
	s.Stage, _ = r.U8()
	s.StageMs, _ = r.U32()
	s.Current, s.Next = circle(), circle()
	damage, _ := r.U32()
	s.DamagePerSec = math.Float32frombits(damage)
	n, _ := r.U16()
	name := make([]byte, n)
	for i := range name {
		name[i], _ = r.U8()
	}
	s.Winner = string(name)
	if err := r.Done(); err != nil {
		t.Fatalf("state frame tail: %v", err)
	}
	return s
}

/*
================
recordBytes

The persisted record without its stand: the restore returns a character
to the lobby, so the world is the one field a match may change.
================
*/
func recordBytes(t *testing.T, c *enterworld.Character) []byte {
	t.Helper()
	clone := domain.CloneCharacter(c)
	clone.World = nil
	raw, err := json.Marshal(clone)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

/*
================
startMatch

The lobby, the countdown and the drop for every fixture character.
================
*/
func (fx *battleRoyaleFixture) startMatch(t *testing.T) {
	t.Helper()
	fx.tick(500 * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateCountdown {
		t.Fatalf("state %v after the lobby scan", fx.match().match.State())
	}
	fx.tick(time.Duration(battleroyale.DefaultCountdownMs) * time.Millisecond)
	if fx.match().match.State() != battleroyale.StateRunning {
		t.Fatalf("state %v after the countdown", fx.match().match.State())
	}
}

//============================================================================

/*
================
TestBattleRoyaleOffInstallsNothing

Native: no mode, no match, no frames, no handler, no lock.
================
*/
func TestBattleRoyaleOffInstallsNothing(t *testing.T) {
	t.Setenv(EnvBattleRoyale, "")
	if BattleRoyaleFromEnv() {
		t.Fatal("unset flag reads on")
	}
	t.Setenv(EnvBattleRoyale, "on")
	if !BattleRoyaleFromEnv() {
		t.Fatal("on reads off")
	}
	rt, clock := newTestRuntime(testCharacter(), testItems())
	sent := false
	rt.PushCharacterFrames = func(string, string, []wire.Frame) { sent = true }
	if out := rt.advanceBattleRoyale(clock.NowMs()); out != nil || sent || rt.battleRoyaleMatchOf(testDivision) != nil {
		t.Fatal("the mode ran while off")
	}
	if rt.BattleRoyaleLocked(testDivision, "asd2") {
		t.Fatal("an ordinary character is locked")
	}
	// Off, the handler is never registered; called anyway it refuses.
	if out := rt.HandleBattleRoyalePick(testDivision, testCharacter(), []byte{1}); len(out.Frames) != 0 || out.DiagnosticRefusal == "" || rt.battleRoyaleMatchOf(testDivision) != nil {
		t.Fatalf("pick while off: %+v", out)
	}
}

/*
================
TestBattleRoyaleLobbyCountdownAndCancel
================
*/
func TestBattleRoyaleLobbyCountdownAndCancel(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	a, b := fx.chars[0], fx.chars[1]
	// A pick from outside the circle is refused but still answered with
	// the sender's (empty) view, so the client shows no pick.
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	if out := fx.rt.HandleBattleRoyalePick(testDivision, b, []byte{3}); out.DiagnosticRefusal == "" || len(out.Frames) != 1 ||
		decodeBattleRoyaleState(t, out.Frames[0].Payload).State != wire.BattleRoyaleStateNone {
		t.Fatalf("outsider's pick: %+v", out)
	}
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	// One scan: both stand in the circle, so the same tick opens the lobby
	// and starts the countdown; the frames arrive in that order.
	fx.tick(500 * time.Millisecond)
	lobbyFrames := 0
	for _, f := range fx.frames[a.Name] {
		if f.Opcode == wire.OpBattleRoyaleState && decodeBattleRoyaleState(t, f.Payload).State == wire.BattleRoyaleStateLobby {
			lobbyFrames++
		}
	}
	if lobbyFrames == 0 {
		t.Fatal("no lobby frame before the countdown")
	}
	if got := fx.lastState(t, a.Name); got.State != wire.BattleRoyaleStateCountdown || got.Role != wire.BattleRoyaleRoleMember || got.Members != 2 ||
		got.Build != battleroyale.DefaultBuildID || got.Lobby.Radius != battleRoyaleLobbyRadiusM*battleroyale.UnitsPerMeter {
		t.Fatalf("countdown frame %+v", got)
	}
	// The pick answers with the stored build.
	out := fx.rt.HandleBattleRoyalePick(testDivision, a, []byte{3})
	if len(out.Frames) != 1 || decodeBattleRoyaleState(t, out.Frames[0].Payload).Build != 3 {
		t.Fatalf("pick answer %+v", out)
	}
	if out := fx.rt.HandleBattleRoyalePick(testDivision, a, []byte{99}); out.DiagnosticRefusal == "" {
		t.Fatal("unknown build accepted")
	}
	fx.tick(500 * time.Millisecond)
	if got := fx.lastState(t, b.Name); got.State != wire.BattleRoyaleStateCountdown || got.CountdownMs == 0 || got.CountdownMs > uint32(battleroyale.DefaultCountdownMs) {
		t.Fatalf("countdown frame %+v", got)
	}
	// b walks out: the countdown cancels, b sees nothing, a is alone.
	fx.place(b, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: 100, Y: 20, Z: 100})
	fx.tick(time.Second)
	if fx.match().match.State() != battleroyale.StateLobby {
		t.Fatalf("state %v after a leaver", fx.match().match.State())
	}
	if got := fx.lastState(t, b.Name); got.State != wire.BattleRoyaleStateNone {
		t.Fatalf("leaver frame %+v", got)
	}
	if got := fx.lastState(t, a.Name); got.State != wire.BattleRoyaleStateLobby || got.Members != 1 {
		t.Fatalf("remaining frame %+v", got)
	}
	// Nobody was held.
	for _, c := range fx.chars {
		if battleroyale.Held(c) {
			t.Fatalf("%s held without a match", c.Name)
		}
	}
}

/*
================
TestBattleRoyaleDropHoldsAndSpreadsEveryone

The drop moves every participant into one layer of world 29, at points
at least 50 m apart, as a level-1 match character of its build, with the
real record held byte for byte; the layer holds the match wolves.
================
*/
func TestBattleRoyaleDropHoldsAndSpreadsEveryone(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 3)
	fx.tick(500 * time.Millisecond)
	if out := fx.rt.HandleBattleRoyalePick(testDivision, fx.chars[1], []byte{11}); out.DiagnosticRefusal != "" {
		t.Fatal(out.DiagnosticRefusal)
	}
	if out := fx.rt.HandleBattleRoyalePick(testDivision, fx.chars[2], []byte{1}); out.DiagnosticRefusal != "" {
		t.Fatal(out.DiagnosticRefusal)
	}
	before := map[string][]byte{}
	for _, c := range fx.chars {
		before[c.Name] = recordBytes(t, c)
	}
	fx.startMatch(t)
	m := fx.match()
	if !m.layered || m.world.Definition() != battleRoyaleWorld {
		t.Fatalf("no arena layer: %+v", m.world)
	}
	var points []worldgeom.GridXZ
	for _, c := range fx.chars {
		if !battleroyale.Held(c) || domain.CharacterWorldInstance(c) != uint32(m.world) {
			t.Fatalf("%s not held in the arena: held %v world %08x", c.Name, battleroyale.Held(c), domain.CharacterWorldInstance(c))
		}
		if lease, ok := fx.rt.EntryPopulationLease(testDivision, c.Name); !ok || lease != m.lease {
			t.Fatalf("%s membership did not follow: %+v", c.Name, lease)
		}
		if *c.Level != 1 || *c.Gold != 0 || c.PK != nil || len(c.MissionInventory) != 1 {
			t.Fatalf("%s is not a fresh match character: level %d gold %d", c.Name, *c.Level, *c.Gold)
		}
		if got := recordBytes(t, c.BattleRoyaleHeld.Real); string(got) != string(before[c.Name]) {
			t.Fatalf("%s held record differs from the real one", c.Name)
		}
		at := fx.rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, fx.clock.NowMs())
		point := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
		if battleroyale.Outside(m.match.Arena(), point) {
			t.Fatalf("%s dropped outside the arena", c.Name)
		}
		points = append(points, point)
		if got := fx.lastState(t, c.Name); got.State != wire.BattleRoyaleStateRunning || got.Role != wire.BattleRoyaleRoleAlive || got.Alive != 3 || got.Phases != 6 {
			t.Fatalf("%s running frame %+v", c.Name, got)
		}
	}
	for i := range points {
		for j := i + 1; j < len(points); j++ {
			if d := math.Hypot(points[i].X-points[j].X, points[i].Z-points[j].Z); d < battleroyale.DefaultMinDropSpacing {
				t.Fatalf("drops %d and %d only %.0f units apart", i, j, d)
			}
		}
	}
	// Each pick dressed its race: build 11 an EU body, build 1 a CH body.
	if fx.chars[1].ModelCodename != "CHAR_EU_MAN_NOBLE" || fx.chars[1].BattleRoyaleHeld.Build != 11 || *fx.chars[1].RaceIndex != domain.RaceEurope {
		t.Fatalf("build 11 body: %s build %d", fx.chars[1].ModelCodename, fx.chars[1].BattleRoyaleHeld.Build)
	}
	if fx.chars[2].ModelCodename != "CHAR_CH_MAN_ADVENTURER" || fx.chars[2].BattleRoyaleHeld.Build != 1 || *fx.chars[2].RaceIndex != domain.RaceChina {
		t.Fatalf("build 1 body: %s build %d", fx.chars[2].ModelCodename, fx.chars[2].BattleRoyaleHeld.Build)
	}
	region := battleRoyaleSpawnAt(m.match.Arena().Center, 0).RegionID
	regions := []uint16{}
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			regions = append(regions, worldgeom.RegionIDForSectors(worldgeom.SectorX(region)+dx, worldgeom.SectorY(region)+dz))
		}
	}
	if wolves := fx.rt.Monsters.PopulationInstances(testDivision, m.lease, regions); len(wolves) == 0 {
		t.Fatal("no match wolves in the layer")
	}
	for _, c := range fx.chars {
		if !fx.rt.BattleRoyaleLocked(testDivision, c.Name) {
			t.Fatalf("%s not locked", c.Name)
		}
		if out := fx.rt.HandlePortal(testDivision, c, wire.NewWriter(5).U32(1).U8(0).Payload()); len(out.Frames) == 0 {
			t.Fatal("portal not refused")
		}
	}
}

/*
================
TestBattleRoyaleZoneKillsAndRestores

A participant outside the zone loses HP every tick and more over time;
one inside loses none. The death eliminates, restores the real record
byte for byte and returns the player to the lobby in the default world;
the last one alive wins, is paid and restored once the arena lingered.
================
*/
func TestBattleRoyaleZoneKillsAndRestores(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	a, b := fx.chars[0], fx.chars[1]
	before := map[string][]byte{a.Name: recordBytes(t, a), b.Name: recordBytes(t, b)}
	fx.startMatch(t)
	m := fx.match()
	// b stands at the arena's centre, a far outside it (the lobby's spot,
	// still in the arena world).
	fx.place(b, battleRoyaleSpawnAt(m.match.Arena().Center, 20))
	fx.place(a, simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ})
	maxHP, _, hp0, _ := fx.rt.playerKeeperVitals(testDivision, a)
	if maxHP < 2*domain.BaseVitals {
		t.Fatalf("match max HP %d, want x2.5 of %d", maxHP, domain.BaseVitals)
	}
	fx.tick(time.Second)
	hp1 := *a.CurrentHP
	if hp1 >= hp0 {
		t.Fatalf("outside the zone HP %d -> %d", hp0, hp1)
	}
	fx.tick(10 * time.Second)
	hp2 := *a.CurrentHP
	if (hp1 - hp2) <= (hp0-hp1)*10 {
		t.Fatalf("damage did not ramp: first second %d, next ten %d", hp0-hp1, hp1-hp2)
	}
	if _, _, bhp, _ := fx.rt.playerKeeperVitals(testDivision, b); bhp != maxHP {
		t.Fatalf("inside the zone HP %d of %d", bhp, maxHP)
	}
	for i := 0; i < 240 && battleroyale.Held(a); i++ {
		fx.tick(time.Second)
	}
	if battleroyale.Held(a) {
		t.Fatal("a never died outside the zone")
	}
	if got := recordBytes(t, a); string(got) != string(before[a.Name]) {
		t.Fatalf("a restored differently:\n%s\n%s", got, before[a.Name])
	}
	if domain.CharacterWorldInstance(a) != domain.DefaultWorldInstance || *a.World.Spawn.RegionID != int64(battleRoyaleLobbyRegion) {
		t.Fatalf("a not back at the lobby: world %08x region %d", domain.CharacterWorldInstance(a), *a.World.Spawn.RegionID)
	}
	if p, _ := m.match.Participant(battleroyale.PlayerID(a.Name)); p.Alive || p.Reason != battleroyale.EliminatedKilled {
		t.Fatalf("a's elimination %+v", p)
	}
	if m.match.State() != battleroyale.StateFinished || m.winner != b.Name {
		t.Fatalf("state %v winner %q", m.match.State(), m.winner)
	}
	if fx.silk[b.AccountID] != battleRoyaleWinnerSilk {
		t.Fatalf("winner silk %d", fx.silk[b.AccountID])
	}
	if got := fx.lastState(t, b.Name); got.State != wire.BattleRoyaleStateFinished || got.Winner != b.Name {
		t.Fatalf("winner frame %+v", got)
	}
	if len(fx.frames["*"]) == 0 {
		t.Fatal("no shard announcement")
	}
	fx.tick(battleRoyaleLingerMs * time.Millisecond)
	if battleroyale.Held(b) {
		t.Fatal("winner still held after the linger")
	}
	if got := recordBytes(t, b); string(got) != string(before[b.Name]) {
		t.Fatal("winner restored differently")
	}
	if _, allocated := fx.rt.Monsters.PopulationLease(testDivision, m.world); allocated {
		t.Fatal("arena layer not released")
	}
	if fx.match().match.State() != battleroyale.StateLobby {
		t.Fatalf("lobby not reopened: %v", fx.match().match.State())
	}
	if got := fx.lastState(t, b.Name); got.State != wire.BattleRoyaleStateNone {
		t.Fatalf("final frame %+v", got)
	}
}

/*
================
TestBattleRoyaleKillIsFreeForAll

A participant kills another inside the arena: no murder, no penalty, the
kill feed names both, the killer is credited and the victim restored.
================
*/
func TestBattleRoyaleKillIsFreeForAll(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 3)
	fx.tick(500 * time.Millisecond)
	for _, c := range fx.chars {
		if out := fx.rt.HandleBattleRoyalePick(testDivision, c, []byte{1}); out.DiagnosticRefusal != "" {
			t.Fatal(out.DiagnosticRefusal)
		}
	}
	fx.startMatch(t)
	m := fx.match()
	a, v := fx.chars[0], fx.chars[1]
	center := battleRoyaleSpawnAt(m.match.Arena().Center, 20)
	fx.place(a, center)
	fx.place(v, simulation.Spawn{RegionID: center.RegionID, X: center.X + 3, Y: 20, Z: center.Z})
	fx.place(fx.chars[2], simulation.Spawn{RegionID: center.RegionID, X: center.X - 30, Y: 20, Z: center.Z})
	one := int64(1)
	v.CurrentHP = &one
	out := fx.rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("strike refused: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if enterworld.CharacterAlive(v) {
		t.Fatal("victim survived")
	}
	if a.PK != nil || a.PVPState() != 0 || v.DeathKind != uint8(2) {
		t.Fatalf("murder booked: pk %+v state %d victim kind %d", a.PK, a.PVPState(), v.DeathKind)
	}
	fx.tick(time.Second)
	p, _ := m.match.Participant(battleroyale.PlayerID(v.Name))
	if p.Alive || p.Killer != battleroyale.PlayerID(a.Name) {
		t.Fatalf("victim's record %+v", p)
	}
	if k, _ := m.match.Participant(battleroyale.PlayerID(a.Name)); k.Kills != 1 {
		t.Fatalf("killer kills %d", k.Kills)
	}
	if battleroyale.Held(v) || !enterworld.CharacterAlive(v) {
		t.Fatal("victim not restored alive")
	}
	fed := false
	for _, f := range fx.frames[a.Name] {
		if f.Opcode == 0x3667 && len(f.Payload) > 0 && f.Payload[0] == wire.BattleRoyaleNoticeChannel {
			fed = true
		}
	}
	if !fed {
		t.Fatal("no kill feed line")
	}
	if got := fx.lastState(t, v.Name); got.Role != wire.BattleRoyaleRoleEliminated || got.Alive != 2 {
		t.Fatalf("victim frame %+v", got)
	}
}

/*
================
TestBattleRoyaleBotMatchesEndWithOneWinner

Matches of 2 to 8 players, eliminations in a seeded random order through
the zone: alive never rises, exactly one winner, everyone restored, the
layer released.
================
*/
func TestBattleRoyaleBotMatchesEndWithOneWinner(t *testing.T) {
	for n := 2; n <= 8; n++ {
		t.Run(fmt.Sprintf("%d-players", n), func(t *testing.T) {
			fx := newBattleRoyaleFixture(t, n)
			before := map[string][]byte{}
			for _, c := range fx.chars {
				before[c.Name] = recordBytes(t, c)
			}
			fx.startMatch(t)
			m := fx.match()
			rng := rand.New(rand.NewPCG(uint64(n), 1))
			alive := m.match.AliveCount()
			outside := simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ}
			center := battleRoyaleSpawnAt(m.match.Arena().Center, 20)
			for _, c := range fx.chars {
				fx.place(c, center)
			}
			for round := 0; m.match.State() == battleroyale.StateRunning && round < 64; round++ {
				var candidates []*enterworld.Character
				for _, c := range fx.chars {
					if p, ok := m.match.Participant(battleroyale.PlayerID(c.Name)); ok && p.Alive {
						candidates = append(candidates, c)
					}
				}
				victim := candidates[rng.IntN(len(candidates))]
				fx.place(victim, outside)
				for i := 0; i < 240 && battleroyale.Held(victim) && m.match.State() == battleroyale.StateRunning; i++ {
					fx.tick(time.Second)
					if now := m.match.AliveCount(); now > alive {
						t.Fatalf("alive rose %d -> %d", alive, now)
					} else {
						alive = now
					}
				}
			}
			if m.match.State() != battleroyale.StateFinished || m.winner == "" || m.match.AliveCount() != 1 {
				t.Fatalf("state %v winner %q alive %d", m.match.State(), m.winner, m.match.AliveCount())
			}
			fx.tick(battleRoyaleLingerMs * time.Millisecond)
			for _, c := range fx.chars {
				if battleroyale.Held(c) {
					t.Fatalf("%s still held", c.Name)
				}
				if got := recordBytes(t, c); string(got) != string(before[c.Name]) {
					t.Fatalf("%s restored differently", c.Name)
				}
			}
			if _, allocated := fx.rt.Monsters.PopulationLease(testDivision, m.world); allocated {
				t.Fatal("layer not released")
			}
		})
	}
}

/*
================
TestBattleRoyaleDisconnectGraceAndEntryRestore

A participant who disconnects keeps its match character for the grace
and is restored by the world entry once it ran out; one back in time
rejoins without a restore.
================
*/
func TestBattleRoyaleDisconnectGraceAndEntryRestore(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 3)
	fx.startMatch(t)
	m := fx.match()
	a := fx.chars[0]
	fx.rt.ForgetCharacterSession(testDivision, a.Name, uint64(a.ID))
	fx.tick(time.Second)
	if p, _ := m.match.Participant(battleroyale.PlayerID(a.Name)); !p.Alive || p.Connected {
		t.Fatalf("disconnect not recorded: %+v", p)
	}
	// Back in time: the entry keeps the match character.
	if err := fx.rt.BattleRoyaleEntryRestore(testDivision, a); err != nil || !battleroyale.Held(a) {
		t.Fatalf("early entry restored: %v held %v", err, battleroyale.Held(a))
	}
	if err := fx.rt.AdmitCharacterSession(testDivision, a.Name, uint64(a.ID)+1); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.match.Participant(battleroyale.PlayerID(a.Name)); !p.Connected {
		t.Fatal("reconnect not recorded")
	}
	// Gone past the grace: the tick eliminates and restores the record.
	fx.rt.ForgetCharacterSession(testDivision, a.Name, uint64(a.ID)+1)
	fx.tick(time.Duration(battleroyale.DefaultDisconnectGraceMs+1000) * time.Millisecond)
	if p, _ := m.match.Participant(battleroyale.PlayerID(a.Name)); p.Alive || p.Reason != battleroyale.EliminatedDisconnected {
		t.Fatalf("grace did not eliminate: %+v", p)
	}
	if battleroyale.Held(a) || domain.CharacterWorldInstance(a) != domain.DefaultWorldInstance {
		t.Fatal("offline participant not restored to the default world")
	}
	// A held record of a match that is gone restores at entry.
	b := fx.chars[1]
	fx.rt.ForgetCharacterSession(testDivision, b.Name, uint64(b.ID))
	m.match = battleroyale.Match{}
	if err := fx.rt.BattleRoyaleEntryRestore(testDivision, b); err != nil || battleroyale.Held(b) {
		t.Fatalf("entry restore: %v held %v", err, battleroyale.Held(b))
	}
}

/*
================
TestBattleRoyaleLootAndExperience

A match character's kill drops the match table at its own level; a
credited kill pays MatchKillExp of its level's requirement whatever the
formula offered (no level-gap cut, no growth rate), nothing at the cap,
and a level trains the build's masteries.
================
*/
func TestBattleRoyaleLootAndExperience(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.startMatch(t)
	a := fx.chars[0]
	wolf := monster.Instance{Gid: 123456, Ref: monster.MonsterRef{RefObjID: 5854, Codename: battleroyale.MatchMonsterCodename, Level: 5}}
	items := fx.rt.deps.(*enterworld.Deps).Items.(staticItemSource)
	items["ITEM_ETC_HP_POTION_01"] = &enterworld.ItemRef{RefObjID: 4, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1}}
	items["ITEM_ETC_MP_POTION_01"] = &enterworld.ItemRef{RefObjID: 11, Codename: "ITEM_ETC_MP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 2}}
	potions := 0
	for i := 0; i < 20; i++ {
		drops, match := fx.rt.battleRoyaleLoot(a, wolf, simulation.Spawn{RegionID: 0x5a6f, X: 1, Z: 1}, fx.clock.Now())
		if !match || len(drops) > 4 {
			t.Fatalf("match loot %+v", drops)
		}
		for _, drop := range drops {
			if drop.Codename != "ITEM_ETC_HP_POTION_01" && drop.Codename != "ITEM_ETC_MP_POTION_01" {
				t.Fatalf("a level-1 killer's drop %s (only the potions are in the fixture itemdata)", drop.Codename)
			}
			potions++
		}
	}
	if potions == 0 {
		t.Fatal("no potions in 20 kills")
	}
	if _, match := fx.rt.battleRoyaleLoot(domain.CloneCharacter(a.BattleRoyaleHeld.Real), wolf, simulation.Spawn{}, fx.clock.Now()); match {
		t.Fatal("an unheld character got match loot")
	}
	var granted int64
	fx.rt.CommitExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		granted += exp
		level := *c.Level + 1
		c.Level = &level
		return nil, true
	}
	fx.rt.UpdateExperience = func(_ *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
		granted += exp
		return nil, true
	}
	frames := fx.rt.grantMonsterExperience(testDivision, a, 1, 0, wolf.Gid)
	if want := battleroyale.MatchKillExp(118); granted != want || want != 34 {
		t.Fatalf("match pace granted %d, want %d (level 1 needs 118)", granted, want)
	}
	if level, _ := enterworld.MasteryLevel(a, 513); level != 2 {
		t.Fatalf("mastery 513 level %d after the level-up", level)
	}
	acks := 0
	for _, f := range frames {
		if f.Opcode == wire.OpMasteryLevelUpResponse {
			acks++
		}
	}
	if acks != 2 {
		t.Fatalf("%d mastery acks", acks)
	}
	cap := battleroyale.MatchLevelCap
	a.Level = &cap
	granted = 0
	fx.rt.grantMonsterExperience(testDivision, a, 100, 0, wolf.Gid)
	if granted != 0 {
		t.Fatalf("EXP past the cap: %d", granted)
	}
	// An ordinary character's EXP goes through untouched.
	plain := domain.CloneCharacter(a.BattleRoyaleHeld.Real)
	fx.rt.grantMonsterExperience(testDivision, plain, 100, 0, wolf.Gid)
	if granted != 100 {
		t.Fatalf("plain grant %d", granted)
	}
}

/*
================
TestBattleRoyaleAutoLearnsBuildSkills

The automatic skill grant learns the build's roots in rank order as the
masteries allow, replacing a group's previous rank.
================
*/
func TestBattleRoyaleAutoLearnsBuildSkills(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rows := staticSkillRowSource{
		2:   {ID: 2, Codename: "SKILL_CH_SWORD_BASE_01", Group: 1, Level: 1, RequiredRace: enterworld.SkillRaceAny},
		100: {ID: 100, Group: 10, Level: 1, RequiredRace: 0, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 1}}},
		101: {ID: 101, Group: 10, Level: 2, RequiredRace: 0, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 3}}},
		102: {ID: 102, Group: 11, Level: 1, RequiredRace: 0, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 1}},
			Prerequisites: [3]enterworld.SkillRequirement{{ID: 10, Level: 2}}},
		200: {ID: 200, Group: 20, Level: 1, RequiredRace: 0, Masteries: [2]enterworld.SkillRequirement{{ID: 258, Level: 1}}},
		300: {ID: 300, Group: 30, Level: 1, RequiredRace: 1, Masteries: [2]enterworld.SkillRequirement{{ID: 513, Level: 1}}},
		301: {ID: 301, Group: 31, Level: 1, RequiredRace: 0, Masteries: [2]enterworld.SkillRequirement{{ID: 257, Level: 1}}, ChainSub: true},
	}
	rt.deps.(*enterworld.Deps).Skills = rows
	rt.ConfigureBattleRoyale(BattleRoyaleSetup{Skills: rows, Seed: func() uint64 { return 1 }})
	build, _ := battleroyale.BuildByID(1)
	c := &enterworld.Character{Skills: []uint32{2}, Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 1}, {ID: 275, Level: 1}}}
	if got := rt.learnBattleRoyaleSkills(c, build); len(got) != 1 || got[0] != 100 {
		t.Fatalf("level-1 learn %v", got)
	}
	c.Masteries[0].Level = 3
	if got := rt.learnBattleRoyaleSkills(c, build); len(got) != 2 || got[0] != 101 || got[1] != 102 {
		t.Fatalf("level-3 learn %v", got)
	}
	if len(c.Skills) != 3 || c.Skills[1] != 101 {
		t.Fatalf("learned list %v, want the rank replaced in place", c.Skills)
	}
}

/*
================
staticSkillRowSource
================
*/
type staticSkillRowSource map[uint32]enterworld.SkillRow

/*
================
SkillByID
================
*/
func (s staticSkillRowSource) SkillByID(id uint32) (enterworld.SkillRow, bool) {
	row, ok := s[id]
	return row, ok
}

/*
================
Rows
================
*/
func (s staticSkillRowSource) Rows() iter.Seq2[uint32, enterworld.SkillRow] {
	return func(yield func(uint32, enterworld.SkillRow) bool) {
		for id, row := range s {
			if !yield(id, row) {
				return
			}
		}
	}
}

/*
================
TestBattleRoyaleStatWritesFollowTheBuild
================
*/
func TestBattleRoyaleStatWritesFollowTheBuild(t *testing.T) {
	plain := &enterworld.Character{}
	if writes := battleRoyaleStatWrites(plain); len(writes) != 0 {
		t.Fatalf("plain character writes %+v", writes)
	}
	chinese := &enterworld.Character{BattleRoyaleHeld: &domain.BattleRoyaleHold{Build: 1}}
	if writes := battleRoyaleStatWrites(chinese); len(writes) != 3 || writes[0].Parameter != keeperParamMaxHP || writes[0].Value != battleroyale.MatchMaxHPPercent {
		t.Fatalf("CH writes %+v", writes)
	}
	wizard := &enterworld.Character{BattleRoyaleHeld: &domain.BattleRoyaleHold{Build: 15}}
	if writes := battleRoyaleStatWrites(wizard); len(writes) != 3 || writes[1].Parameter != keeperParamMagicalAttackMin || writes[1].Value != battleroyale.WizardMagicalPercent {
		t.Fatalf("wizard writes %+v", writes)
	}
	knight := &enterworld.Character{BattleRoyaleHeld: &domain.BattleRoyaleHold{Build: 12}}
	if writes := battleRoyaleStatWrites(knight); len(writes) != 1 {
		t.Fatalf("knight writes %+v", writes)
	}
	_ = instance.Pack
}
