/*
===========================================================================

economy_sim_test.go - whole matches of the stage-6 economy, simulated

Port-only, not native (docs/battle-royale.md "Server notes (stage 6:
economy)"). A deterministic, seeded simulation of complete matches on an
injected clock (no sleeps, no wall time) for 2, 5, 10 and 16 real
players plus 20 Normal bots, and (stage 7) for 10 players with 20, 100
and 200 bots on Normal and Hard, driven by the real core functions: ScheduleFor
and PlanZoneFiltered / CircleAt for the zone, DropPointsFiltered for the
drop, MonsterPlan, SupplyPlan and RetireOutside for the wolves,
LootPlan for every kill (stage 9: random across every build, under the
live LiveLootTuning; the player keeps what its build wears,
fullset_sim_test.go), MatchKillExp over the shipped leveldata curve
for the players, BotLevelPaced, BotMaxHP, the difficulty profiles
(ProfileFor), BotGear and BotLocalWolves for the bots, and the
GroundItemLifetimeMs / MaxGroundItems ground policy. The live match
calls the same functions with the same constants
(action/battleroyale_arena.go, battleroyale_loot.go, battleroyale_bots.go).

Agent policies (stated assumptions, not native rules):

  - A player always farms the nearest unclaimed living wolf on the
    zone's side of it (battleroyale.Inward: inside the next circle once
    it is in it, else nearer the next circle's centre), so it farms its
    way into the zone as it shrinks; with none there it takes the
    nearest wolf of the next circle, with none at all it runs to the
    centre. It runs at the native 5 m/s over simDetour x the straight
    distance (paths bend around cliffs), fights simCombatMs(level) (3.5 s
    at level 1 to 2 s from level 16: the starter Sun weapon and the
    build's skills on a 119-HP wolf) and spends simPickupMs on the loot.
    Players keep farming at level 30 (more drops, more kills: the
    economy's worst case) and nobody picks up a drop (the ground cap's
    worst case: a live player picks up what it wears, so the live
    ground holds less). The ground cap is reported, not asserted: the
    sweep evicts the oldest past MaxGroundItems as the live layer does,
    and the row counts how many items the cap (not the expiry) took and
    for how much of the match the ground stood at the cap.
  - A bot follows its live brain (botbrain.go): outside the next circle
    it runs into it; below the level cap it farms the nearest unclaimed
    wolf within BotFarmRangeM, striking every StrikeMs of its profile
    for BotMaxHP(level)/12 x DamageScale x its gear's AttackScale (each
    kill rolls LootPlan at its level into BotGear); otherwise it roams to
    a random point of the next circle. Its level is BotLevelPaced at the
    profile's pace. The sim's bots never fight players or each other
    (the supply's worst case: every bot farms the whole match).
  - Nobody dies: every participant stays alive and keeps eating wolves
    (the supply's worst case).

The supply runs as live: the drop's MonsterPlan, a zone pass at every
phase start (with the supply elite) and every SupplyIntervalMs, a local
pass every LocalSupplyIntervalMs.

===========================================================================
*/
package battleroyale_test

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/game/battleroyale"
)

const (
	simTickMs   = 250
	simRunSpeed = 50.0 // units per second, the native run speed
	simDetour   = 1.25
	simPickupMs = 500
	// simReach is how near a bot strikes from, simBotFarm its wolf search,
	// simBotRoamMs the longest roam step, simBotMargin the roam circle:
	// botbrain.go's numbers.
	simReach     = battleroyale.BotReachM * battleroyale.UnitsPerMeter
	simBotFarm   = battleroyale.BotFarmRangeM * battleroyale.UnitsPerMeter
	simBotRoamMs = 4000
	simBotHits   = 12
	simBotMargin = 0.7
	simSeeds     = 3
)

/*
================
simExpCurve

The shipped leveldata EXP requirements of levels 1-29 (column 1; the
licensed test checks them against leveldata).
================
*/
func simExpCurve() []int64 {
	return []int64{118, 470, 1058, 1880, 2938, 5640, 9048, 13160, 17978, 23500, 34898, 47940, 62628, 78960, 96938,
		127840, 161798, 198810, 238878, 282000, 351231, 427755, 512196, 605232, 707587, 820046, 943453, 1078717, 1226815}
}

/*
================
simCombatMs

A player's fight with one wolf at level: 3.5 s at level 1, 0.1 s less a
level, 2 s from level 16.
================
*/
func simCombatMs(level int64) int64 {
	return max(3500-100*(level-1), 2000)
}

/*
================
simWolf

One wolf: where it stands, whether it lives, its HP (bots strike it
down) and whether an agent is on its way to it.
================
*/
type simWolf struct {
	at      battleroyale.Point
	alive   bool
	hp      int64
	claimed bool
}

/*
================
simAgent

One participant: a real player (EXP by MatchKillExp) or a bot
(BotLevel), where it will stand when its current action ends (readyMs),
the wolf it is on, and what it earned.
================
*/
type simAgent struct {
	bot       bool
	gear      battleroyale.BotGear
	build     battleroyale.Build
	female    bool
	at        battleroyale.Point
	level     int64
	exp       int64
	readyMs   int64
	prey      int
	kills     int
	stacks    int
	reachedMs int64
	// set and setKill (stage 8): what the player holds per slot (it
	// picks up and wears every upgrade of its own drops its build can
	// use) and the kill that completed its degree-4 set (-1 never).
	set     battleroyale.SetState
	setKill int
}

/*
================
simResult

One match's numbers: when each real player reached level 30 (-1 never),
its kills and dropped stacks, the peaks, and the least ratio of living
wolves inside the current circle to participants before phase 5.
================
*/
type simResult struct {
	phase5Ms      int64
	reachedMs     []int64
	kills, stacks []int
	peakGround    int
	peakLive      int
	minPerHead    float64
	botKills      int
	gearWeapon    int
	gearArmor     int
	spawned       int
	retired       int
	sets          []simSet
	invariantFail string
	// capEvicted is how many items the ground cap (not the expiry)
	// removed; capTicks how many ticks ended with the ground at the cap;
	// ticks how many ticks the match ran.
	capEvicted, capTicks, ticks int
}

/*
================
simMatch

The state of one simulated match.
================
*/
type simMatch struct {
	cfg      simConfig
	profile  battleroyale.BotProfile
	mask     *battleroyale.GroundMask
	zone     battleroyale.Zone
	rng      *rand.Rand
	wolves   []simWolf
	agents   []*simAgent
	elites   int
	ground   []int64
	result   simResult
	nextSupp int64
}

/*
================
simConfig

One simulated match's line-up: real players, bots and their difficulty.
================
*/
type simConfig struct {
	players, bots int
	difficulty    battleroyale.BotDifficulty
	// tuning is the loot tuning the players' kills roll (stage 9:
	// LiveLootTuning is the live table, the zero value the stage-9 one).
	tuning battleroyale.LootTuning
}

/*
================
runEconomyMatch

Simulates one match of cfg on mask, seeded, from the drop to the end of
the last shrink.
================
*/
func runEconomyMatch(mask *battleroyale.GroundMask, cfg simConfig, seed uint64) simResult {
	arena := battleroyale.ArenaCircle()
	players := cfg.players
	count := players + cfg.bots
	schedule := battleroyale.ScheduleFor(count, arena.Radius)
	s := &simMatch{cfg: cfg, profile: battleroyale.ProfileFor(cfg.difficulty), mask: mask, rng: rand.New(rand.NewPCG(seed, 0x45636f6e)),
		zone: battleroyale.PlanZoneFiltered(schedule, arena, 0, rand.New(rand.NewPCG(seed, 1)), mask.Contains)}
	s.result.minPerHead = math.Inf(1)
	end := int64(0)
	var starts []int64
	for i, phase := range schedule.Phases {
		starts = append(starts, end)
		if i == 4 {
			s.result.phase5Ms = end
		}
		end += phase.WaitMs + phase.ShrinkMs
	}
	builds := battleroyale.Builds()
	drops := battleroyale.DropPointsFiltered(arena, players, battleroyale.DefaultMinDropSpacing, rand.New(rand.NewPCG(seed, 2)), mask.Contains)
	drops = append(drops, battleroyale.BotRingPoints(drops, cfg.bots, arena, rand.New(rand.NewPCG(seed, 3)), mask.Contains)...)
	for i := range count {
		s.agents = append(s.agents, &simAgent{bot: i >= players, build: builds[i%len(builds)], female: i%2 == 1,
			at: drops[i], level: 1, prey: -1, reachedMs: -1, gear: battleroyale.NewBotGear(), setKill: -1})
	}
	for _, spawn := range battleroyale.MonsterPlan(mask.Within(arena), s.rng) {
		s.addWolf(spawn.At)
	}
	phase := 0
	for now := int64(0); now <= end; now += simTickMs {
		if phase < len(starts) && now >= starts[phase] {
			s.supply(now)
			s.elites++
			phase++
		} else if now >= s.nextSupp {
			s.supply(now)
		}
		if now%battleroyale.LocalSupplyIntervalMs == 0 {
			s.localSupply(now)
		}
		for _, a := range s.agents {
			if now >= a.readyMs {
				s.act(a, now)
			}
		}
		s.sweepGround(now)
		if now < s.result.phase5Ms && now%1000 == 0 {
			s.checkFloor(now)
		}
		s.result.peakLive = max(s.result.peakLive, s.live())
		if now == s.result.phase5Ms {
			s.snapshotSets()
		}
	}
	for _, a := range s.agents {
		if !a.bot {
			s.result.reachedMs = append(s.result.reachedMs, a.reachedMs)
			s.result.kills = append(s.result.kills, a.kills)
			s.result.stacks = append(s.result.stacks, a.stacks)
		} else {
			s.result.botKills += a.kills
		}
	}
	return s.result
}

/*
================
simMatch.addWolf
================
*/
func (s *simMatch) addWolf(at battleroyale.Point) {
	s.wolves = append(s.wolves, simWolf{at: at, alive: true, hp: battleroyale.MatchMonsterHP})
	s.result.spawned++
}

/*
================
simMatch.live

The layer's living monsters as the live cap counts them: wolves, the
supply elites and the bots.
================
*/
func (s *simMatch) live() int {
	n := s.elites + s.cfg.bots
	for i := range s.wolves {
		if s.wolves[i].alive {
			n++
		}
	}
	return n
}

/*
================
simMatch.supply

One supply pass, as supplyBattleRoyaleZone runs it.
================
*/
func (s *simMatch) supply(now int64) {
	s.nextSupp = now + battleroyale.SupplyIntervalMs
	zone := battleroyale.CircleAt(s.zone, now)
	inside := 0
	for i := range s.wolves {
		w := &s.wolves[i]
		if !w.alive {
			continue
		}
		if !w.claimed && w.hp == battleroyale.MatchMonsterHP && battleroyale.RetireOutside(zone.Current, w.at) {
			w.alive = false
			s.result.retired++
			continue
		}
		if !battleroyale.Outside(zone.Target, w.at) {
			inside++
		}
	}
	target := zone.Target
	area := s.mask.Within(battleroyale.Circle{Center: target.Center, Radius: max(target.Radius, battleroyale.SupplyMinRadius)}).Area()
	ground := s.mask.Within(battleroyale.Circle{Center: target.Center, Radius: max(target.Radius*battleroyale.SupplyEdge, battleroyale.SupplyMinRadius)})
	supply := battleroyale.Supply{Participants: s.cfg.players, Bots: s.cfg.bots, Inside: inside, Live: s.live(), GroundM2: area}
	for _, spawn := range battleroyale.SupplyPlan(ground, supply, s.rng) {
		s.addWolf(spawn.At)
	}
}

/*
================
simMatch.localSupply

One local supply pass, as localBattleRoyaleSupply runs it: every
participant below the level cap gets LocalSupplyPlan, never past the
live cap.
================
*/
func (s *simMatch) localSupply(now int64) {
	zone := battleroyale.CircleAt(s.zone, now)
	room := battleroyale.MaxLiveMonsters - s.live()
	for _, a := range s.agents {
		level, want := a.level, battleroyale.LocalWolves
		if a.bot {
			level = int64(battleroyale.BotLevelPaced(now, a.kills, 0, s.profile.LevelPace))
			want = battleroyale.BotLocalWolves(s.cfg.bots)
		}
		if level >= battleroyale.MatchLevelCap || room <= 0 {
			continue
		}
		reach := battleroyale.Circle{Center: a.at, Radius: battleroyale.LocalSupplyRadiusM * battleroyale.UnitsPerMeter}
		nearby := 0
		for i := range s.wolves {
			if s.wolves[i].alive && !battleroyale.Outside(reach, s.wolves[i].at) {
				nearby++
			}
		}
		plan := battleroyale.LocalSupplyPlanFor(s.mask.Within(reach), a.at, zone, want, nearby, s.rng)
		for _, spawn := range plan[:min(len(plan), room)] {
			s.addWolf(spawn.At)
			room--
		}
	}
}

/*
================
simMatch.nearest

The nearest unclaimed living wolf to at inside within (and within reach
when reach is positive), or -1.
================
*/
func (s *simMatch) nearest(at battleroyale.Point, within battleroyale.Circle, reach float64) int {
	best, bestD := -1, math.Inf(1)
	for i := range s.wolves {
		w := &s.wolves[i]
		if !w.alive || w.claimed || battleroyale.Outside(within, w.at) {
			continue
		}
		if d := math.Hypot(w.at.X-at.X, w.at.Z-at.Z); d < bestD && (reach <= 0 || d <= reach) {
			best, bestD = i, d
		}
	}
	return best
}

/*
================
simMatch.nearestInward

The nearest unclaimed living wolf on the zone's side of at
(battleroyale.Inward), or -1.
================
*/
func (s *simMatch) nearestInward(at battleroyale.Point, zone battleroyale.ZoneState) int {
	best, bestD := -1, math.Inf(1)
	for i := range s.wolves {
		w := &s.wolves[i]
		if !w.alive || w.claimed || !battleroyale.Inward(zone, at, w.at) {
			continue
		}
		if d := math.Hypot(w.at.X-at.X, w.at.Z-at.Z); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

/*
================
simMatch.act

An agent is free at now: it settles the kill it was busy with, then
picks its next move.
================
*/
func (s *simMatch) act(a *simAgent, now int64) {
	if a.prey >= 0 {
		s.kill(a, now)
	}
	zone := battleroyale.CircleAt(s.zone, now)
	if a.bot {
		s.actBot(a, zone, now)
		return
	}
	prey := s.nearestInward(a.at, zone)
	if prey < 0 {
		prey = s.nearest(a.at, zone.Target, 0)
	}
	if prey < 0 {
		a.readyMs = now + s.travel(a, zone.Target.Center)
		return
	}
	s.wolves[prey].claimed = true
	a.prey = prey
	a.readyMs = now + s.travel(a, s.wolves[prey].at) + simCombatMs(a.level) + simPickupMs
}

/*
================
simMatch.actBot

The bot brain (battleroyale_bots.go decideBattleRoyaleBot), one decision.
================
*/
func (s *simMatch) actBot(a *simAgent, zone battleroyale.ZoneState, now int64) {
	keep := battleroyale.BotKeepCircle(zone)
	a.level = int64(battleroyale.BotLevelPaced(now, a.kills, 0, s.profile.LevelPace))
	if battleroyale.Outside(keep, a.at) {
		a.readyMs = now + s.travel(a, keep.Center)
		return
	}
	if a.level < battleroyale.MatchLevelCap {
		if prey := s.nearest(a.at, battleroyale.Circle{Center: a.at, Radius: simBotFarm}, simBotFarm); prey >= 0 {
			strike := int64(math.Ceil(float64(battleroyale.BotMaxHP(int(a.level))) / simBotHits * s.profile.DamageScale * a.gear.AttackScale()))
			hits := (s.wolves[prey].hp + strike - 1) / strike
			s.wolves[prey].claimed = true
			a.prey = prey
			reach := max(math.Hypot(s.wolves[prey].at.X-a.at.X, s.wolves[prey].at.Z-a.at.Z)-simReach, 0)
			a.readyMs = now + int64(reach*simDetour/simRunSpeed*1000) + (hits-1)*s.profile.StrikeMs + simTickMs
			return
		}
	}
	roam := battleroyale.NextCircle(battleroyale.Circle{Center: keep.Center, Radius: keep.Radius * simBotMargin}, 0, s.rng).Center
	a.readyMs = now + min(s.travel(a, roam), simBotRoamMs)
}

/*
================
simMatch.travel

Moves the agent to goal and returns the run time in milliseconds.
================
*/
func (s *simMatch) travel(a *simAgent, goal battleroyale.Point) int64 {
	d := math.Hypot(goal.X-a.at.X, goal.Z-a.at.Z)
	a.at = goal
	return max(int64(d*simDetour/simRunSpeed*1000), simTickMs)
}

/*
================
simMatch.kill

The agent's prey dies: a player earns MatchKillExp of its level's
requirement and the wolf drops LootPlan at the player's level, every
invariant checked; a bot counts the wolf for BotLevel.
================
*/
func (s *simMatch) kill(a *simAgent, now int64) {
	s.wolves[a.prey].alive, s.wolves[a.prey].hp = false, 0
	a.prey = -1
	a.kills++
	if a.bot {
		a.gear.Absorb(a.build, battleroyale.LootPlan(battleroyale.Killer{Build: a.build, Level: a.level, Female: a.female}, s.rng))
		s.result.gearWeapon = max(s.result.gearWeapon, a.gear.WeaponDegree())
		s.result.gearArmor = max(s.result.gearArmor, a.gear.ArmorDegree())
		return
	}
	killer := battleroyale.Killer{Build: a.build, Level: a.level, Female: a.female}
	drops := battleroyale.LootPlanTuned(killer, s.cfg.tuning, s.rng)
	s.wear(a, killer, drops)
	if len(drops) < 3 {
		s.fail(fmt.Sprintf("a kill at level %d dropped %d stacks", a.level, len(drops)))
	}
	for _, drop := range drops {
		required, equipment := battleroyale.RequiredLevel(drop.Codename)
		switch {
		case equipment && !battleroyale.InLootWindow(a.level, required) && !simTopDegree(s.cfg.tuning, a.level, required):
			s.fail(fmt.Sprintf("level %d dropped %s (required %d)", a.level, drop.Codename, required))
		case !equipment && !strings.HasPrefix(drop.Codename, "ITEM_ETC_") && !battleroyale.IsSkin(drop.Codename):
			s.fail(fmt.Sprintf("level %d dropped %s", a.level, drop.Codename))
		}
		s.ground = append(s.ground, now)
	}
	a.stacks += len(drops)
	s.result.peakGround = max(s.result.peakGround, len(s.ground))
	curve := simExpCurve()
	if a.level >= battleroyale.MatchLevelCap {
		return
	}
	a.exp += battleroyale.MatchKillExp(curve[a.level-1])
	if a.exp >= curve[a.level-1] {
		a.exp -= curve[a.level-1]
		a.level++
		if a.level == battleroyale.MatchLevelCap {
			a.reachedMs = now
		}
	}
	if a.level > battleroyale.MatchLevelCap {
		s.fail(fmt.Sprintf("level %d past the cap", a.level))
	}
}

/*
================
simTopDegree

Whether a row requiring required is the top degree a killer of level at
the cap may drop under tuning (LootTuning.TopDegreeAtCap).
================
*/
func simTopDegree(tuning battleroyale.LootTuning, level, required int64) bool {
	const topDegreeLevel = 24
	return tuning.TopDegreeAtCap && level >= battleroyale.MatchLevelCap && required >= topDegreeLevel
}

/*
================
simMatch.sweepGround

The layer's ground policy: drops past GroundItemLifetimeMs go, then the
oldest past MaxGroundItems; the cap's evictions and the ticks at the cap
are counted.
================
*/
func (s *simMatch) sweepGround(now int64) {
	cut := 0
	for cut < len(s.ground) && now-s.ground[cut] >= battleroyale.GroundItemLifetimeMs {
		cut++
	}
	if over := len(s.ground) - battleroyale.MaxGroundItems; over > cut {
		s.result.capEvicted += over - cut
		cut = over
	}
	s.ground = s.ground[cut:]
	s.result.ticks++
	if len(s.ground) >= battleroyale.MaxGroundItems {
		s.result.capTicks++
	}
}

/*
================
simMatch.checkFloor

Before phase 5: the living wolves inside the current circle per
participant alive, against MinZoneWolvesPerParticipant.
================
*/
func (s *simMatch) checkFloor(now int64) {
	current := battleroyale.CircleAt(s.zone, now).Current
	inside := 0
	for i := range s.wolves {
		if s.wolves[i].alive && !battleroyale.Outside(current, s.wolves[i].at) {
			inside++
		}
	}
	perHead := float64(inside) / float64(len(s.agents))
	s.result.minPerHead = min(s.result.minPerHead, perHead)
	if floor := battleroyale.MinZoneWolvesPerParticipant*s.cfg.players + battleroyale.MinZoneWolvesPerBot*s.cfg.bots; inside < floor {
		s.fail(fmt.Sprintf("%d wolves in the zone for %d players and %d bots at %d ms", inside, s.cfg.players, s.cfg.bots, now))
	}
}

/*
================
simMatch.fail
================
*/
func (s *simMatch) fail(reason string) {
	if s.result.invariantFail == "" {
		s.result.invariantFail = reason
	}
}

//============================================================================

/*
================
economyRow

The aggregate of every seed of one line-up: the table row the docs
carry.
================
*/
type economyRow struct {
	cfg              simConfig
	reached, total   int
	medianMin, p95   float64
	phase5Min        float64
	kills, stacks    float64
	peakGround, live int
	minPerHead       float64
	botKills         int
	retired          int
	weapon, armor    int
	sets             []simSet
	// capEvicted is the mean items a match lost to the ground cap;
	// capShare the most of a match's ticks spent at the cap.
	capEvicted float64
	capShare   float64
}

/*
================
economyRowFor

Runs simSeeds matches of cfg on mask and aggregates them; fails t on any
broken invariant.
================
*/
func economyRowFor(t *testing.T, mask *battleroyale.GroundMask, cfg simConfig) economyRow {
	t.Helper()
	row := economyRow{cfg: cfg, minPerHead: math.Inf(1)}
	var times []int64
	kills, stacks := 0, 0
	for seed := uint64(1); seed <= simSeeds; seed++ {
		r := runEconomyMatch(mask, cfg, seed*7919+uint64(cfg.players)+uint64(cfg.bots)<<8)
		if r.invariantFail != "" {
			t.Fatalf("%+v, seed %d: %s", cfg, seed, r.invariantFail)
		}
		for i, at := range r.reachedMs {
			row.total++
			if at >= 0 && at < r.phase5Ms {
				row.reached++
			}
			if at < 0 {
				at = math.MaxInt64
			}
			times = append(times, at)
			kills += r.kills[i]
			stacks += r.stacks[i]
		}
		row.phase5Min = float64(r.phase5Ms) / 60000
		row.peakGround = max(row.peakGround, r.peakGround)
		row.live = max(row.live, r.peakLive)
		row.minPerHead = min(row.minPerHead, r.minPerHead)
		row.botKills += r.botKills / simSeeds
		row.retired += r.retired / simSeeds
		row.weapon, row.armor = max(row.weapon, r.gearWeapon), max(row.armor, r.gearArmor)
		row.sets = append(row.sets, r.sets...)
		row.capEvicted += float64(r.capEvicted) / simSeeds
		if r.ticks > 0 {
			row.capShare = max(row.capShare, float64(r.capTicks)/float64(r.ticks))
		}
	}
	slices.SortFunc(times, cmp.Compare[int64])
	row.medianMin = float64(times[len(times)/2]) / 60000
	row.p95 = float64(times[min(len(times)*95/100, len(times)-1)]) / 60000
	row.kills, row.stacks = float64(kills)/float64(row.total), float64(stacks)/float64(row.total)
	return row
}

/*
================
simDifficultyName
================
*/
func simDifficultyName(d battleroyale.BotDifficulty) string {
	return [...]string{"Easy", "Normal", "Hard"}[d]
}

/*
================
logEconomy

The docs table of rows.
================
*/
func logEconomy(t *testing.T, rows []economyRow) {
	t.Helper()
	t.Log("| Line-up | Lvl 30 before phase 5 | Lvl 30 median / p95 (min) | Phase 5 at (min) | Kills / player | Stacks / player | Peak ground items | Peak live monsters | Min wolves in zone / participant | Bot wolf kills | Wolves retired | Best bot weapon / armor degree |")
	for _, r := range rows {
		t.Logf("| %d + %d %s bots | %d/%d | %.1f / %.1f | %.2f | %.0f | %.0f | %d | %d | %.1f | %d | %d | %d / %d |", r.cfg.players, r.cfg.bots,
			simDifficultyName(r.cfg.difficulty), r.reached, r.total, r.medianMin, r.p95, r.phase5Min, r.kills, r.stacks, r.peakGround,
			r.live, r.minPerHead, r.botKills, r.retired, r.weapon, r.armor)
	}
}

/*
================
checkEconomy

Fails t when a row puts fewer than minPercent of its players at level 30
before phase 5, or passes the live cap. The ground cap is the sweep's
(logGroundCap reports how hard it works).
================
*/
func checkEconomy(t *testing.T, rows []economyRow, minPercent int) {
	t.Helper()
	for _, r := range rows {
		if r.reached*100 < r.total*minPercent {
			t.Errorf("%+v: %d of %d reached level 30 before phase 5", r.cfg, r.reached, r.total)
		}
		if r.live > battleroyale.MaxLiveMonsters {
			t.Errorf("%+v: live peak %d", r.cfg, r.live)
		}
	}
}

/*
================
logGroundCap

Per row: the peak ground items between two sweeps, the items the
MaxGroundItems cap evicted per match (mean) and the most of a match spent
at the cap.
================
*/
func logGroundCap(t *testing.T, rows []economyRow) {
	t.Helper()
	for _, r := range rows {
		t.Logf("ground cap %d + %d %s bots: peak %d, %.0f items evicted by the cap a match, at the cap %.1f %% of the match",
			r.cfg.players, r.cfg.bots, simDifficultyName(r.cfg.difficulty), r.peakGround, r.capEvicted, 100*r.capShare)
	}
}

/*
================
runEconomy

The stage-6 table (2, 5, 10, 16 players with 20 Normal bots) on mask:
fails t on any broken invariant or a line-up below 95 % at level 30
before phase 5, and logs the table.
================
*/
func runEconomy(t *testing.T, mask *battleroyale.GroundMask) []economyRow {
	t.Helper()
	var rows []economyRow
	for _, players := range []int{2, 5, 10, 16} {
		rows = append(rows, economyRowFor(t, mask, simConfig{players: players, bots: battleroyale.DefaultBotCount, difficulty: battleroyale.BotNormal,
			tuning: battleroyale.LiveLootTuning()}))
	}
	logEconomy(t, rows)
	checkEconomy(t, rows, 95)
	logGroundCap(t, rows)
	logFullSets(t, rows)
	checkFullSets(t, rows, fullSetFloorPercent)
	runLootProposals(t, mask)
	return rows
}

/*
================
runBotEconomy

The stage-7 table: 10 players with 20, 100 and 200 bots on Normal and
Hard. Every invariant and cap holds in every row; the 95 % rule is
asserted for the 20 Normal bots (runEconomy) and reported here.
================
*/
func runBotEconomy(t *testing.T, mask *battleroyale.GroundMask) []economyRow {
	t.Helper()
	var rows []economyRow
	for _, difficulty := range []battleroyale.BotDifficulty{battleroyale.BotNormal, battleroyale.BotHard} {
		for _, bots := range []int{20, 100, 200} {
			rows = append(rows, economyRowFor(t, mask, simConfig{players: 10, bots: bots, difficulty: difficulty, tuning: battleroyale.LiveLootTuning()}))
		}
	}
	logEconomy(t, rows)
	checkEconomy(t, rows, 0)
	logGroundCap(t, rows)
	return rows
}

/*
================
simRocMask

A stand-in for the licensed Roc ground mask (the licensed test runs the
real one): the Roc square with fixed cliff discs cut out, about 5 km2
in its largest component, as the shipped navmesh gives.
================
*/
func simRocMask() *battleroyale.GroundMask {
	rng := rand.New(rand.NewPCG(11, 13))
	minX, minZ, maxX, maxZ := battleroyale.RocBounds()
	type cliff struct {
		at battleroyale.Point
		r  float64
	}
	var cliffs []cliff
	for range 70 {
		cliffs = append(cliffs, cliff{battleroyale.Point{X: minX + rng.Float64()*(maxX-minX), Z: minZ + rng.Float64()*(maxZ-minZ)},
			600 + rng.Float64()*1400})
	}
	return battleroyale.BuildGroundMask(func(p battleroyale.Point) bool {
		for _, c := range cliffs {
			if math.Hypot(p.X-c.at.X, p.Z-c.at.Z) < c.r {
				return false
			}
		}
		return true
	})
}

/*
================
TestEconomySimulation

Whole matches for 2, 5, 10 and 16 players plus 20 bots, three seeds each,
on the stand-in mask: every kill drops three or more stacks, every
equipment drop is in [level-3, level] (any top-degree row at level 30),
no level passes 30, the living wolves inside the zone never fall below
MinZoneWolvesPerParticipant per participant before phase 5, the live
peak stays under its cap (the ground cap is reported), and at least
95 % of the real players reach level 30 before phase 5 starts.
================
*/
func TestEconomySimulation(t *testing.T) {
	mask := simRocMask()
	if area := mask.Area(); area < 4.5e6 || area > 5.5e6 {
		t.Fatalf("stand-in mask %.2f km2, want about 5", area/1e6)
	}
	runEconomy(t, mask)
	runBotEconomy(t, mask)
	cfg := simConfig{players: 5, bots: battleroyale.DefaultBotCount, difficulty: battleroyale.BotNormal, tuning: battleroyale.LiveLootTuning()}
	again := runEconomyMatch(mask, cfg, 42)
	if first := runEconomyMatch(mask, cfg, 42); !slices.Equal(first.reachedMs, again.reachedMs) || first.peakGround != again.peakGround ||
		first.spawned != again.spawned {
		t.Fatal("the simulation is not deterministic")
	}
}
