/*
===========================================================================

stage7_test.go - smarter bots: difficulty, count, ring drops, brain, paths

Port-only, not native (docs/battle-royale.md "Stage 7"). The difficulty
profiles and the gear formula as data, the bot options and the names of
up to MaxBotCount bots, the ring drop around the players, the hunting
roams and potions of the brain (its rules are botbrain_test.go's), and
the planner over a ground mask with a
wall the mask cannot see (a learned block). Time is injected.

===========================================================================
*/
package battleroyale

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

/*
================
TestBotProfiles

The owner's table: sense 40/70/110 m, damage x0.6/1/1.5, HP x0.8/1/1.3,
strike 1.8/1.3/0.9 s, pace x0.8/1/1.3, potions 30/70/100 %; an unknown
difficulty is Normal; harder never weaker.
================
*/
func TestBotProfiles(t *testing.T) {
	want := map[BotDifficulty]BotProfile{
		BotEasy:   {SenseM: 40, DamageScale: 0.6, HPScale: 0.8, StrikeMs: 1800, LevelPace: 0.8, PotionChance: 30},
		BotNormal: {SenseM: 70, DamageScale: 1.0, HPScale: 1.0, StrikeMs: 1300, LevelPace: 1.0, PotionChance: 70},
		BotHard:   {SenseM: 110, DamageScale: 1.5, HPScale: 1.3, StrikeMs: 900, LevelPace: 1.3, PotionChance: 100},
	}
	for d, w := range want {
		got := ProfileFor(d)
		if got.SenseM != w.SenseM || got.DamageScale != w.DamageScale || got.HPScale != w.HPScale || got.StrikeMs != w.StrikeMs ||
			got.LevelPace != w.LevelPace || got.PotionChance != w.PotionChance || got.SenseRange() != w.SenseM*UnitsPerMeter {
			t.Fatalf("difficulty %d: %+v", d, got)
		}
	}
	if ProfileFor(7) != ProfileFor(BotNormal) || ValidBotDifficulty(3) || !ValidBotDifficulty(BotHard) {
		t.Fatal("an unknown difficulty is not Normal")
	}
	easy, normal, hard := ProfileFor(BotEasy), ProfileFor(BotNormal), ProfileFor(BotHard)
	if !(easy.PlayerHitScale() < normal.PlayerHitScale() && normal.PlayerHitScale() == 1 && normal.PlayerHitScale() < hard.PlayerHitScale()) {
		t.Fatalf("player hit scales %v %v %v", easy.PlayerHitScale(), normal.PlayerHitScale(), hard.PlayerHitScale())
	}
	if BotScaledMaxHP(10, hard) <= BotScaledMaxHP(10, normal) || BotScaledMaxHP(10, normal) != BotMaxHP(10) {
		t.Fatal("HP scale")
	}
	if BotLevelPaced(0, 0, 0, 1.3) != 1 || BotLevelPaced(10*botLevelMs, 0, 0, 1.3) != 14 || BotLevelPaced(10*botLevelMs, 0, 0, 0.8) != 9 ||
		BotLevelPaced(10*botLevelMs, 8, 1, 1) != BotLevel(10*botLevelMs, 8, 1) || BotLevelPaced(1<<40, 0, 0, 0.8) != int(MatchLevelCap) {
		t.Fatal("paced level curve")
	}
	if PotionHeal(1) != 120 || PotionHeal(30) != 820 {
		t.Fatal("potion heals")
	}
}

/*
================
TestBotGear

A bot keeps the best weapon of its build's kinds and the best armor or
shield it looted; the scales follow the documented formula.
================
*/
func TestBotGear(t *testing.T) {
	build := chineseBuild(t)
	g := NewBotGear()
	if g.WeaponDegree() != 1 || g.ArmorDegree() != 0 || g.AttackScale() != 1 || g.DefenceScale() != 1 {
		t.Fatalf("kit gear %+v", g)
	}
	moon2, _ := SealWeaponCodename(build.Race, build.WeaponKinds[0], 2, SealMoon)
	sun1, _ := SealWeaponCodename(build.Race, build.WeaponKinds[0], 1, SealSun)
	other, _ := SealWeaponCodename(RaceEuropean, WeaponDagger, 4, SealSun)
	drops := []Drop{{Codename: sun1}, {Codename: other}, {Codename: "ITEM_ETC_HP_POTION_01"}, {Codename: "ITEM_CH_RING_04_C_RARE"}}
	if g.Absorb(build, drops) {
		t.Fatal("no improvement was taken for one")
	}
	if !g.Absorb(build, []Drop{{Codename: moon2}, {Codename: "ITEM_CH_M_HEAVY_03_HA_C_RARE"}}) || g.WeaponRank != 4 || g.ArmorRank != 7 {
		t.Fatalf("gear after a kill %+v", g)
	}
	if g.Absorb(build, []Drop{{Codename: "ITEM_CH_M_HEAVY_02_HA_C_RARE"}}) || g.ArmorDegree() != 3 {
		t.Fatal("a worse armor replaced a better one")
	}
	if !g.Absorb(build, []Drop{{Codename: "ITEM_CH_SHIELD_04_B_RARE"}}) || g.ArmorRank != 8 {
		t.Fatal("a shield is armor")
	}
	g = BotGear{WeaponRank: 9, ArmorRank: 9}
	if math.Abs(g.AttackScale()-1.48) > 1e-9 || math.Abs(g.DefenceScale()-1.54) > 1e-9 {
		t.Fatalf("degree-4 Sun scales %v %v", g.AttackScale(), g.DefenceScale())
	}
}

/*
================
chineseBuild

The first Chinese build (the CH armor and shield codenames above).
================
*/
func chineseBuild(t *testing.T) Build {
	t.Helper()
	for _, b := range Builds() {
		if b.Race == RaceChinese {
			return b
		}
	}
	t.Fatal("no Chinese build")
	return Build{}
}

/*
================
TestBotOptionsAndNames

The options are lobby/countdown only and validated; the drop adds the
configured count with unique names (200 included), never a member's.
================
*/
func TestBotOptionsAndNames(t *testing.T) {
	m := mustInit(t, testConfig())
	if d, n := m.BotOptions(); d != BotNormal || n != DefaultBotCount {
		t.Fatalf("defaults %d %d", d, n)
	}
	for _, bad := range []struct {
		d BotDifficulty
		n int
	}{{3, 20}, {BotHard, 0}, {BotHard, MaxBotCount + 1}} {
		if m.SetBotOptions(bad.d, bad.n) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	mustNoErr(t, m.SetBots(true))
	mustNoErr(t, m.SetBotOptions(BotHard, MaxBotCount))
	mustEnter(t, m, "Ares", "mira2")
	m.Tick(0)
	starts := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(starts) != 1 {
		t.Fatal("no start")
	}
	names := map[string]bool{}
	bots := 0
	for _, p := range starts[0].Participants {
		lower := strings.ToLower(string(p.ID))
		if names[lower] || len(p.ID) > 64 {
			t.Fatalf("name %q", p.ID)
		}
		names[lower] = true
		if p.Bot {
			bots++
		}
	}
	if bots != MaxBotCount || m.SetBotOptions(BotEasy, 5) == nil {
		t.Fatalf("%d bots", bots)
	}
	if d, n := m.BotOptions(); d != BotHard || n != MaxBotCount {
		t.Fatal("options lost at the drop")
	}
}

/*
================
TestBotRingPoints

Each bot drops at least 150 m from every player and within the grown
ring of its own (spread among them), inside the arena, on accepted
ground, BotRingSpacingM from every other bot; the first dozen bots
around one player stay in the 150-400 m ring (it widens only when full); without players it falls back to the arena.
================
*/
func TestBotRingPoints(t *testing.T) {
	arena := ArenaCircle()
	players := []Point{arena.Center, {X: arena.Center.X + 8000, Z: arena.Center.Z}}
	keep := func(p Point) bool { return math.Mod(math.Abs(p.X), 1000) > 100 }
	points := BotRingPoints(players, 200, arena, rand.New(rand.NewPCG(1, 2)), keep)
	if len(points) != 200 {
		t.Fatalf("%d points", len(points))
	}
	for i, p := range points {
		own := distance(p, players[i%2])
		if own < BotRingMinM*UnitsPerMeter-1e-6 || own > BotRingReach()+1e-6 || nearestDistance(p, players) < BotRingMinM*UnitsPerMeter-1e-6 {
			t.Fatalf("bot %d at %.0f m from its player", i, own/UnitsPerMeter)
		}
		if !keep(p) || Outside(arena, p) {
			t.Fatalf("bot %d off the ground at %+v", i, p)
		}
		if gap := nearestDistance(p, append(append([]Point{}, points[:i]...), points[i+1:]...)); gap < BotRingSpacingM*UnitsPerMeter-1e-6 {
			t.Fatalf("bot %d only %.0f m from another of 200", i, gap/UnitsPerMeter)
		}
	}
	few := BotRingPoints(players[:1], 20, arena, rand.New(rand.NewPCG(3, 4)), nil)
	for i, p := range few {
		if gap := nearestDistance(p, append(append([]Point{}, few[:i]...), few[i+1:]...)); gap < BotRingSpacingM*UnitsPerMeter {
			t.Fatalf("bot %d only %.0f m from another", i, gap/UnitsPerMeter)
		}
		if own := distance(p, players[0]); i < 12 && own > BotRingMaxM*UnitsPerMeter+1e-6 {
			t.Fatalf("bot %d at %.0f m: the ring grew early", i, own/UnitsPerMeter)
		}
	}
	if len(BotRingPoints(nil, 5, arena, rand.New(rand.NewPCG(1, 1)), nil)) != 5 {
		t.Fatal("no fallback without players")
	}
}

//============================================================================

/*
================
brainZone

A one-phase zone: a 300 m circle waiting a long time before shrinking to
its 200 m target around the same centre.
================
*/
func brainZone() Zone {
	c := Point{X: 210000, Z: 175000}
	schedule := Schedule{Phases: []Phase{{WaitMs: 600_000, ShrinkMs: 180_000, EndRadius: 2000, DamagePctPerSec: 1}}}
	return Zone{Schedule: schedule, Start: Circle{Center: c, Radius: 3000}, Targets: []Circle{{Center: c, Radius: 2000}}}
}

/*
================
TestBotBrainHuntsPlayers

A Hard bot's roam goals lead toward the nearest real player more often
than an Easy bot's.
================
*/
func TestBotBrainHuntsPlayers(t *testing.T) {
	zone := brainZone()
	player := BotTarget{GID: 1, At: Point{X: zone.Start.Center.X + 500, Z: zone.Start.Center.Z}, Player: true}
	near := func(d BotDifficulty) int {
		rng := rand.New(rand.NewPCG(9, 9))
		n := 0
		for i := range 1000 {
			var mind BotMind
			s := BotSenses{NowMs: int64(i), At: zone.Start.Center, Level: 30, Zone: zone, Zoned: true, Player: player, HPFrac: 1}
			if got := mind.Decide(s, ProfileFor(d), rng); distance(got.Goal, player.At) <= botRoamPlayerSpreadM*UnitsPerMeter {
				n++
			}
		}
		return n
	}
	easy, hard := near(BotEasy), near(BotHard)
	if easy < 120 || hard < 520 || hard <= easy {
		t.Fatalf("hunting roams: easy %d, hard %d of 1000", easy, hard)
	}
}

/*
================
TestBotBrainPotion

Under 40 % HP a bot drinks an HP potion with its profile's chance, once
per cooldown; under 25 % MP an MP potion the same way; HP comes first.
================
*/
func TestBotBrainPotion(t *testing.T) {
	for _, c := range []struct {
		d        BotDifficulty
		min, max int
	}{{BotEasy, 220, 380}, {BotNormal, 620, 780}, {BotHard, 1000, 1000}} {
		rng := rand.New(rand.NewPCG(1, uint64(c.d)))
		drank := 0
		for i := range 1000 {
			mind := BotMind{}
			if mind.Drink(BotSenses{NowMs: int64(i), HPFrac: 0.3, MPFrac: 1}, ProfileFor(c.d), rng) == DrinkHP {
				drank++
			}
		}
		if drank < c.min || drank > c.max {
			t.Fatalf("difficulty %d drank %d of 1000", c.d, drank)
		}
	}
	var mind BotMind
	rng := rand.New(rand.NewPCG(1, 1))
	hard := ProfileFor(BotHard)
	if mind.Drink(BotSenses{HPFrac: 0.5, MPFrac: 1}, hard, rng) != DrinkNone ||
		mind.Drink(BotSenses{HPFrac: 0.2, MPFrac: 0.1}, hard, rng) != DrinkHP ||
		mind.Drink(BotSenses{NowMs: BotPotionCooldownMs - 1, HPFrac: 0.2, MPFrac: 1}, hard, rng) != DrinkNone {
		t.Fatal("HP potion threshold or cooldown")
	}
	if mind.Drink(BotSenses{NowMs: 1, HPFrac: 1, MPFrac: 0.2}, hard, rng) != DrinkMP ||
		mind.Drink(BotSenses{NowMs: 2, HPFrac: 1, MPFrac: 0.2}, hard, rng) != DrinkNone {
		t.Fatal("MP potion threshold or cooldown")
	}
}

/*
================
wallMask

The Roc mask with a wall of blocked cells down the middle, open only at
its north end (z above gapZ).
================
*/
func wallMask() (*GroundMask, float64, float64) {
	minX, minZ, maxX, maxZ := RocBounds()
	wallX := (minX + maxX) / 2
	gapZ := maxZ - 2000
	mask := BuildGroundMask(func(p Point) bool {
		return !(math.Abs(p.X-wallX) < GroundMaskStep && p.Z < gapZ) && p.Z > minZ+50
	})
	return mask, wallX, gapZ
}

/*
================
walkRoute

Walks a bot from from to goal over nav at 5 m/s on a 0.5 s tick; probe
clips each leg (returns where the walk stops). Returns the arrival time
in ms, or -1 after limitMs.
================
*/
func walkRoute(nav *BotNav, from, goal Point, probe func(a, b Point) Point, limitMs int64) int64 {
	const tickMs, speed = 500, 50.0 / 1000
	route := BotRoute{}
	route.Reset(goal)
	at := from
	for now := int64(0); now <= limitMs; now += tickMs {
		nav.BeginTick(BotNavTickBudget)
		leg, status := nav.Advance(&route, at, now)
		switch status {
		case RouteArrived:
			return now
		case RouteFailed:
			return -1
		case RouteWaiting:
			continue
		}
		stop := probe(at, leg)
		d := distance(at, stop)
		if step := speed * tickMs; d > step {
			stop = Point{X: at.X + (stop.X-at.X)*step/d, Z: at.Z + (stop.Z-at.Z)*step/d}
		}
		at = stop
	}
	return -1
}

/*
================
TestBotNavAroundAWall

A bot crosses a mask wall through its gap (A* plans the detour, every
leg on mask ground), and a wall the mask cannot see (only the probe
knows it) is learned as blocked edges after a stuck and walked around;
arrival within twice the straight time plus the stuck allowance. A goal
off every ground fails.
================
*/
func TestBotNavAroundAWall(t *testing.T) {
	mask, wallX, gapZ := wallMask()
	nav := NewBotNav(mask)
	from := Point{X: wallX - 3000, Z: gapZ - 6000}
	goal := Point{X: wallX + 3000, Z: gapZ - 6000}
	points, status := nav.Plan(from, goal)
	if status != RouteMoving || len(points) < 3 || points[len(points)-1] != goal {
		t.Fatalf("plan %v %d points", status, len(points))
	}
	prev := from
	for i, p := range points {
		if !nav.ClearLine(prev, p) || distance(prev, p) > botNavLegM*UnitsPerMeter+1e-6 {
			t.Fatalf("leg %d %+v -> %+v not a clear leg", i, prev, p)
		}
		prev = p
	}
	open := func(a, b Point) Point { return b }
	if ms := walkRoute(nav, from, goal, open, 600_000); ms < 0 {
		t.Fatal("no arrival through the gap")
	}
	// An invisible wall at x = wall2, z in [z0, z1]: the probe stops there.
	wall2 := wallX + 1500
	z0, z1 := gapZ-7000, gapZ-5000
	probe := func(a, b Point) Point {
		if (a.X-wall2)*(b.X-wall2) >= 0 {
			return b
		}
		t := (wall2 - a.X) / (b.X - a.X)
		z := a.Z + (b.Z-a.Z)*t
		if z < z0 || z > z1 {
			return b
		}
		back := math.Copysign(5, a.X-wall2)
		return Point{X: wall2 + back, Z: z}
	}
	start, end := Point{X: wall2 - 1000, Z: (z0 + z1) / 2}, Point{X: wall2 + 1000, Z: (z0 + z1) / 2}
	nav = NewBotNav(mask)
	ms := walkRoute(nav, start, end, probe, 900_000)
	straight := int64(distance(start, end) / 50 * 1000)
	if ms < 0 || nav.BlockedEdges() == 0 || ms > 3*straight+botRouteMaxStucks*BotStuckMs {
		t.Fatalf("invisible wall: arrival %d ms, %d edges learned", ms, nav.BlockedEdges())
	}
	t.Logf("invisible wall crossed in %.0f s (straight %.0f s), %d edges learned", float64(ms)/1000, float64(straight)/1000, nav.BlockedEdges())
	if _, status := nav.Plan(start, Point{X: 1, Z: 1}); status != RouteFailed {
		t.Fatalf("a goal off the grid planned: %v", status)
	}
}

/*
================
TestBotNavBudget

A tick's budget bounds the searches: once spent, plans wait for the next
tick.
================
*/
func TestBotNavBudget(t *testing.T) {
	mask, wallX, gapZ := wallMask()
	nav := NewBotNav(mask)
	nav.BeginTick(1)
	from, goal := Point{X: wallX - 3000, Z: gapZ - 6000}, Point{X: wallX + 3000, Z: gapZ - 6000}
	if _, status := nav.Plan(from, goal); status != RouteMoving || nav.Budget() >= 0 {
		t.Fatalf("first plan %v, budget %d", status, nav.Budget())
	}
	if _, status := nav.Plan(from, goal); status != RouteWaiting {
		t.Fatalf("plan past the budget: %v", status)
	}
	nav.BeginTick(BotNavTickBudget)
	if _, status := nav.Plan(from, goal); status != RouteMoving {
		t.Fatal("no plan on the next tick")
	}
	t.Logf("a wall detour costs %d expansions", nav.Expansions/2)
}

/*
================
TestBotSupplyShares

A bot weighs ZoneWolvesPerBot in the zone target; the bots share
botLocalBudget local wolves (LocalWolves each up to 20 bots, at least
botLocalMin); and with 200 bots standing the supply never takes the
layer past MaxLiveMonsters (the bots count in Live).
================
*/
func TestBotSupplyShares(t *testing.T) {
	if got := SupplyTarget(Supply{Participants: 10, Bots: 200, GroundM2: 4e6}); got != max(ZoneWolvesPerParticipant*10+ZoneWolvesPerBot*200, 1333) {
		t.Fatalf("zone target %d", got)
	}
	for _, c := range []struct{ bots, want int }{{0, LocalWolves}, {20, LocalWolves}, {40, 5}, {100, 2}, {200, botLocalMin}} {
		if got := BotLocalWolves(c.bots); got != c.want {
			t.Fatalf("%d bots: %d local wolves each, want %d", c.bots, got, c.want)
		}
	}
	for live := MaxLiveMonsters - 700; live <= MaxLiveMonsters; live += 50 {
		s := Supply{Participants: 16, Bots: 200, Inside: 0, Live: live, GroundM2: 4e6}
		if n := SupplyCount(s); live+n > MaxLiveMonsters {
			t.Fatalf("live %d + %d spawned passes the cap", live, n)
		}
	}
}
