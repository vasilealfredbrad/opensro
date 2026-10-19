/*
===========================================================================

botnav_licensed_test.go - bots crossing the real Roc navmesh

Port-only, not native (docs/battle-royale.md "Stage 7"). Bots walk
between random far points of the shipped Roc ground mask, each leg
clipped by the server's own monster movement probe
(movement.WaterValidator.PlanMonsterPath, what SteerPortActor's leg
uses), at the native run speed on the match's 0.5 s tick, the planner
probing each cell edge with the same clip (SetWalkTest). The stage-7
planner (BotNav, BotRoute) must bring every bot to its goal within twice
its planned route's run time plus a minute; the stage-5 rule (one straight 40 m leg toward the goal per
tick) is run on the same pairs and logged for comparison. Skips without
the licensed data.

===========================================================================
*/
package battleroyale_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"opensro.online/server/internal/game/battleroyale"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	navTrials     = 40
	navMinApartM  = 800
	navTickMs     = 500
	navRunPerMs   = 50.0 / 1000 // the native run speed, units per ms
	navStage5LegM = 40
	// navBoundSlackMs is the time a route may add to twice its planned
	// run (stucks, the 5 s detection, re-plans).
	navBoundSlackMs = 60_000
	// navEdgeReach is how near the probe must bring a walk to the next
	// cell centre for the edge to count as open (0.5 m).
	navEdgeReach = 5.0
	// navClipBack is how far short of a clipped walk's rest the test's bot
	// stands (0.1 m).
	navClipBack = 1.0
)

/*
================
navProbe

Where a straight walk from a to b stops on the shipped navmesh: the
monster movement clip (PlanMonsterPath); a refused plan stops at a.
================
*/
func navProbe(v *movement.WaterValidator) func(a, b battleroyale.Point) battleroyale.Point {
	pose := func(p battleroyale.Point) monster.Pose {
		region := worldgeom.RegionIDForSectors(int(p.X/worldgeom.OutdoorRegionSize), int(p.Z/worldgeom.OutdoorRegionSize))
		local := worldgeom.LocalFromGrid(region, p)
		y, _ := v.TerrainHeightAt(local.RegionID, local.X, local.Z)
		return monster.Pose{RegionID: local.RegionID, X: local.X, Y: y, Z: local.Z}
	}
	return func(a, b battleroyale.Point) battleroyale.Point {
		path := v.PlanMonsterPath(pose(a), pose(b))
		if path == nil {
			return a
		}
		rest := path.Rest()
		return worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: rest.RegionID, X: rest.X, Z: rest.Z})
	}
}

/*
================
navWalk

One bot's walk from from to goal: each tick next(at, now) names the
point it heads for (false: given up), probe clips the leg and the bot
runs along it at most one tick's distance. Returns the arrival time or
-1 past limitMs.
================
*/
func navWalk(from, goal battleroyale.Point, next func(at battleroyale.Point, now int64) (battleroyale.Point, bool),
	probe func(a, b battleroyale.Point) battleroyale.Point, limitMs int64) int64 {
	at := from
	for now := int64(0); now <= limitMs; now += navTickMs {
		if math.Hypot(at.X-goal.X, at.Z-goal.Z) <= 3*battleroyale.UnitsPerMeter {
			return now
		}
		leg, ok := next(at, now)
		if !ok {
			return -1
		}
		stop := probe(at, leg)
		d := math.Hypot(stop.X-at.X, stop.Z-at.Z)
		if step := navRunPerMs * navTickMs; d > step {
			stop = battleroyale.Point{X: at.X + (stop.X-at.X)*step/d, Z: at.Z + (stop.Z-at.Z)*step/d}
		} else if stop != leg && d > navClipBack {
			// A clipped walk rests on the blocked tile's edge, where an
			// owner-less probe (this test's) finds its start blocked; the
			// live mover keeps the walked cell as its navigation owner
			// (PlanPathFrom). Stand just short of the edge instead.
			stop = battleroyale.Point{X: stop.X - (stop.X-at.X)*navClipBack/d, Z: stop.Z - (stop.Z-at.Z)*navClipBack/d}
		}
		at = stop
	}
	return -1
}

/*
================
TestBotPathsCrossRoc
================
*/
func TestBotPathsCrossRoc(t *testing.T) {
	v := movement.NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	walkable := rocWalkable(v)
	mask := battleroyale.BuildGroundMask(walkable)
	ground := mask.Within(battleroyale.ArenaCircle())
	probe := navProbe(v)
	rng := rand.New(rand.NewPCG(7, 7))
	nav := battleroyale.NewBotNav(mask)
	var probeTime time.Duration
	nav.SetWalkTest(func(a, b battleroyale.Point) bool {
		began := time.Now()
		stop := probe(a, b)
		probeTime += time.Since(began)
		return math.Hypot(stop.X-b.X, stop.Z-b.Z) < navEdgeReach
	})
	arrived, straightArrived := 0, 0
	worst, total, detour := 0.0, 0.0, 0.0
	for trial := 0; trial < navTrials; trial++ {
		var from, goal battleroyale.Point
		for {
			from, _ = ground.Sample(rng)
			goal, _ = ground.Sample(rng)
			if walkable(from) && walkable(goal) && math.Hypot(from.X-goal.X, from.Z-goal.Z) >= navMinApartM*battleroyale.UnitsPerMeter {
				break
			}
		}
		// The bound is the planned route's own length (Roc's detours can
		// double a straight line): twice its run time plus the slack.
		pathMs := int64(0)
		for {
			nav.BeginTick(battleroyale.BotNavTickBudget)
			points, status := nav.Plan(from, goal)
			if status == battleroyale.RouteWaiting {
				continue
			}
			if status != battleroyale.RouteMoving {
				t.Fatalf("trial %d: no plan %+v -> %+v", trial, from, goal)
			}
			prev := from
			for _, p := range points {
				pathMs += int64(math.Hypot(p.X-prev.X, p.Z-prev.Z) / navRunPerMs)
				prev = p
			}
			break
		}
		straightMs := int64(math.Hypot(from.X-goal.X, from.Z-goal.Z) / navRunPerMs)
		limit := 2*pathMs + navBoundSlackMs
		route := battleroyale.BotRoute{}
		route.Reset(goal)
		ms := navWalk(from, goal, func(at battleroyale.Point, now int64) (battleroyale.Point, bool) {
			nav.BeginTick(battleroyale.BotNavTickBudget)
			leg, status := nav.Advance(&route, at, now)
			return leg, status != battleroyale.RouteFailed
		}, probe, limit)
		if ms >= 0 {
			arrived++
			ratio := float64(ms) / float64(pathMs)
			worst, total = max(worst, ratio), total+ratio
			detour = max(detour, float64(pathMs)/float64(straightMs))
		} else {
			t.Errorf("trial %d: %+v -> %+v (%.0f m) not reached in %d s (%d stucks)", trial, from, goal,
				math.Hypot(from.X-goal.X, from.Z-goal.Z)/battleroyale.UnitsPerMeter, limit/1000, route.Stucks)
		}
		stage5 := navWalk(from, goal, func(at battleroyale.Point, _ int64) (battleroyale.Point, bool) {
			d := math.Hypot(goal.X-at.X, goal.Z-at.Z)
			if d <= navStage5LegM*battleroyale.UnitsPerMeter {
				return goal, true
			}
			s := navStage5LegM * battleroyale.UnitsPerMeter / d
			return battleroyale.Point{X: at.X + (goal.X-at.X)*s, Z: at.Z + (goal.Z-at.Z)*s}, true
		}, probe, limit)
		if stage5 >= 0 {
			straightArrived++
		}
	}
	t.Logf("stage-7 paths: %d/%d arrived within 2x the planned route's run + %d s (mean %.2fx, worst %.2fx the planned run; routes up to %.2fx the straight line), %d edges blocked, %d probes, %d expansions over %d plans",
		arrived, navTrials, navBoundSlackMs/1000, total/float64(max(arrived, 1)), worst, detour, nav.BlockedEdges(), nav.Probes, nav.Expansions, nav.Plans)
	t.Logf("one edge probe costs %.1f us on average: a tick's BotNavTickProbes (%d) at most %.1f ms",
		float64(probeTime.Microseconds())/float64(max(nav.Probes, 1)), battleroyale.BotNavTickProbes,
		float64(probeTime.Microseconds())/float64(max(nav.Probes, 1))*battleroyale.BotNavTickProbes/1000)
	t.Logf("stage-5 straight legs: %d/%d arrived in the same bound", straightArrived, navTrials)
}
