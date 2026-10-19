/*
===========================================================================

schedule.go - default zone pacing and its scaling by players and arena size

Port-only, not native (see zone.go). The numbers are a first cut to be
tuned in play; the RULES that derive a schedule are what this file fixes.

The default schedule (stage 4) is six phases sized for ten players on the
all-Roc arena, ReferenceArenaRadius = ArenaRadiusM (2040 m). Every shrink
lasts at least three minutes (the owner's rule) and closes the radius by
at most 360 m, so the radius never moves faster than MaxShrinkSpeed
(2 m/s) and the worst-case edge (2 x the radius step per shrink, the
target centre at the far side) never faster than 4 m/s, 80 % of the 5 m/s
run speed: a player who starts running when a shrink starts is never
overtaken. It lasts 24:05 at ten players.

	phase  wait  shrink  end radius  damage    ramp
	1      2:00  3:00    1680 m      1 %/s     +0.05 %/s per s outside
	2      1:30  3:00    1320 m      2 %/s     +0.10
	3      1:00  3:00     960 m      3.5 %/s   +0.18
	4      0:45  3:00     600 m      5 %/s     +0.25
	5      0:30  3:00     260 m      8 %/s     +0.40
	6      0:20  3:00       0 m     12 %/s     +0.60

The final circle closes to a point (its centre on walkable ground, see
PlanZoneFiltered), so the match always resolves: the ramp makes standing
outside lethal within seconds by then.

ScheduleFor scales that template:

  - Arena size. Radii and waits scale with arenaRadius /
    ReferenceArenaRadius; shrinks scale the same way but never drop below
    minPhaseShrinkMs (3:00), so a smaller arena (the tests' own) shrinks
    more slowly, never faster.
  - Player count. Waits exist to loot, rotate and fight; with fewer players
    there is less to fight over and a long wait is empty time, so waits
    scale by 0.5 at 2 players, 0.75 at 5 and 1 at 10 (linear between, held
    beyond). The circles do not depend on the player count (stage 4: the
    arena is always all of Roc). On the Roc arena that gives 21:02.5 for 2
    players, 22:33.75 for 5 and 24:05 for 10 or more.
  - Edge speed cap. A shrink's edge can move toward a player at up to
    2*(previous radius - end radius)/shrink time. Every shrink is
    stretched so that speed stays at or below maxZoneEdgeSpeed, 80% of the
    player run speed (the default never needs it).
  - Floors. No wait drops below 10 s, no shrink below 3:00.

===========================================================================
*/
package battleroyale

import "math"

const (
	// ReferenceArenaRadius is the arena the default schedule is sized for:
	// the all-Roc arena, 2040 m.
	ReferenceArenaRadius = ArenaRadiusM * UnitsPerMeter

	// playerRunSpeed is the native run speed in units per second
	// (simulation.RunSpeed; enterworld bootstrap rows carry the same 50).
	playerRunSpeed = 50.0
	// maxZoneEdgeSpeed keeps every shrink outrunnable with a margin.
	maxZoneEdgeSpeed = 0.8 * playerRunSpeed
	// MaxShrinkSpeed is the fastest the radius itself may shrink, in units
	// per second: half the edge bound, 2 m/s (stage 2; stage 4 keeps it:
	// 360 m per three-minute shrink).
	MaxShrinkSpeed = maxZoneEdgeSpeed / 2

	// minPhaseWaitMs floors the size-scaled waits; minPhaseShrinkMs is the
	// owner's three-minute shrink (stage 4: "3 minute pe shrinking").
	minPhaseWaitMs   = 10_000
	minPhaseShrinkMs = 180_000

	// The player-count anchors of the wait scaling. Counts below the first
	// are held at it, counts above the last at it.
	fewPlayers             = 2
	somePlayers            = 5
	manyPlayers            = 10
	waitScaleFew           = 0.5
	waitScaleSome          = 0.75
	waitScaleMany          = 1.0
	defaultPhaseCount      = 6
	defaultSchedulePlayers = manyPlayers

	// The template's end radii in metres at the reference arena.
	phase1EndM = 1680
	phase2EndM = 1320
	phase3EndM = 960
	phase4EndM = 600
	phase5EndM = 260
	phase6EndM = 0
)

/*
================
phaseTemplate

One default phase before scaling: times at the reference arena and ten
players, and the end radius in metres at the reference arena.
================
*/
type phaseTemplate struct {
	waitMs    int64
	shrinkMs  int64
	endM      float64
	damagePct float64
	rampPct   float64
}

/*
================
defaultPhaseTemplates

The default phases (the file header's table) as a fresh array value on
every call, so no package state can be mutated.
================
*/
func defaultPhaseTemplates() [defaultPhaseCount]phaseTemplate {
	const shrink = minPhaseShrinkMs
	return [defaultPhaseCount]phaseTemplate{
		{120_000, shrink, phase1EndM, 1.0, 0.05},
		{90_000, shrink, phase2EndM, 2.0, 0.10},
		{60_000, shrink, phase3EndM, 3.5, 0.18},
		{45_000, shrink, phase4EndM, 5.0, 0.25},
		{30_000, shrink, phase5EndM, 8.0, 0.40},
		{20_000, shrink, phase6EndM, 12.0, 0.60},
	}
}

/*
================
DefaultSchedule

The unscaled default: ten players on the reference arena (see the file
header table).
================
*/
func DefaultSchedule() Schedule {
	return ScheduleFor(defaultSchedulePlayers, ReferenceArenaRadius)
}

/*
================
ScheduleFor

The default schedule scaled for a player count and an arena radius in
world units (see the file header for the rules). arenaRadius must be
positive; the match validates the result for every player count when it
is configured.
================
*/
func ScheduleFor(players int, arenaRadius float64) Schedule {
	waitScale := playerScale(players)
	sizeScale := arenaRadius / ReferenceArenaRadius
	templates := defaultPhaseTemplates()
	schedule := Schedule{Phases: make([]Phase, 0, len(templates))}
	previous := arenaRadius
	for _, template := range templates {
		end := template.endM * UnitsPerMeter * sizeScale
		wait := max(scaleMs(template.waitMs, waitScale*sizeScale), minPhaseWaitMs)
		shrink := max(scaleMs(template.shrinkMs, sizeScale), minPhaseShrinkMs, edgeLimitedShrinkMs(previous, end))
		schedule.Phases = append(schedule.Phases, Phase{
			WaitMs:           wait,
			ShrinkMs:         shrink,
			EndRadius:        end,
			DamagePctPerSec:  template.damagePct,
			DamageRampPerSec: template.rampPct,
		})
		previous = end
	}
	return schedule
}

/*
================
playerScale

The wait scale for a player count: linear between the anchors (2, 5,
10), held outside them.
================
*/
func playerScale(players int) float64 {
	switch {
	case players <= fewPlayers:
		return waitScaleFew
	case players >= manyPlayers:
		return waitScaleMany
	case players <= somePlayers:
		return lerp(waitScaleFew, waitScaleSome, float64(players-fewPlayers)/float64(somePlayers-fewPlayers))
	}
	return lerp(waitScaleSome, waitScaleMany, float64(players-somePlayers)/float64(manyPlayers-somePlayers))
}

/*
================
edgeLimitedShrinkMs

The shortest shrink, in whole milliseconds, that keeps the worst-case edge
speed 2*(previous-end)/time at or below maxZoneEdgeSpeed.
================
*/
func edgeLimitedShrinkMs(previous, end float64) int64 {
	return int64(math.Ceil(2 * (previous - end) / maxZoneEdgeSpeed * msPerSecond))
}

/*
================
scaleMs
================
*/
func scaleMs(ms int64, scale float64) int64 {
	return int64(math.Round(float64(ms) * scale))
}

/*
================
lerp
================
*/
func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}
