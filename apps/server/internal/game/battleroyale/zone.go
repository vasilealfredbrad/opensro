/*
===========================================================================

zone.go - the battle royale safe zone: circles, phases and outside damage

Package battleroyale is the rule core of the battle royale mode: the
shrinking safe zone (this file), its default pacing (schedule.go) and the
match state machine that drives a lobby into a match and a match into a
winner (match.go, events.go). It is pure: no clock, no global random
source, no world or network access. Time comes in as milliseconds on every
call and randomness comes from a caller-owned *rand.Rand, so a seed and an
input sequence replay a match exactly.

PORT-ONLY, NOT NATIVE. SRO has no battle royale; nothing here exists in the
v1.150 client or the v1.188 SR_GameServer. The later wiring stage owns the
GameWorld hooks and must gate them behind an SRO_<FEATURE> environment flag
whose off value is native (no battle royale anywhere).

Coordinates: every point is in the continuous outdoor sector grid, in world
units, the frame world.GridXZ already defines (1 m = 10 units, one region
edge = world.OutdoorRegionSize = 1920 units). The wiring converts a live
(regionID, x, z) position with world.ExpandGrid and a drop point back with
world.LocalFromGrid followed by world.NormalizeOutdoor. Arenas are outdoor
only: the dungeon plane has no shared grid.

Zone model: a Schedule is a list of phases. Each phase first WAITS (the
current circle holds still while the next one is already known, so players
can rotate), then SHRINKS (centre and radius move linearly from the current
circle to the phase's target). After the last phase the final circle holds
forever. Every target lies fully inside the circle before it (NextCircle),
so a linear blend between the two always contains the target and stays
inside the previous circle: a player standing in the next circle is never
caught by the shrink.

Outside damage follows the PUBG 2026 rule: damage grows with the time spent
continuously outside, not with the distance to the edge. A phase's rate is
DamagePctPerSec of max HP per second, plus DamageRampPerSec more for every
second already spent outside. OutsideDamage integrates that rate exactly,
so the damage dealt never depends on the tick length.

===========================================================================
*/
package battleroyale

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"

	"opensro.online/server/internal/game/world"
)

const (
	// UnitsPerMeter converts the design numbers (metres) into world units.
	UnitsPerMeter = 10.0

	// msPerSecond turns the schedule's millisecond clock into the damage
	// rates' seconds.
	msPerSecond = 1000.0

	// percentScale turns a percentage of max HP into a fraction.
	percentScale = 100.0

	// maxPhaseMs bounds one phase's wait or shrink (one day). It keeps the
	// summed schedule far from int64 overflow; no real phase comes close.
	maxPhaseMs = 24 * 60 * 60 * 1000

	// maxCumulativeOutsidePct caps the integrated outside damage (10 000
	// times max HP). A player dies long before; the cap only keeps the
	// float-to-int conversion in range for absurd outside times.
	maxCumulativeOutsidePct = 1_000_000.0

	// maxDamageTotal bounds the integrated damage in HP for an absurd max
	// HP, keeping the float-to-int conversion defined.
	maxDamageTotal = 1 << 62
)

// ErrInvalidSchedule is the identity of every ValidateSchedule refusal.
var ErrInvalidSchedule = errors.New("battleroyale: invalid zone schedule")

/*
================
Point

A planar point in the continuous outdoor sector grid, in world units. It
is world.GridXZ itself, not a copy, so the coordinate contract has one
owner (see the file header for the conversion).
================
*/
type Point = world.GridXZ

/*
================
Circle

A zone circle in world units. A point exactly on the edge is inside.
================
*/
type Circle struct {
	Center Point
	Radius float64
}

/*
================
Phase

One zone phase. WaitMs is how long the current circle holds still while
the target is announced; ShrinkMs is how long the move to the target takes.
EndRadius is the target's radius in world units. Damage outside the circle
while this phase is active is DamagePctPerSec percent of max HP per second,
plus DamageRampPerSec more percent for every second already spent
continuously outside (0 disables the ramp).
================
*/
type Phase struct {
	WaitMs           int64
	ShrinkMs         int64
	EndRadius        float64
	DamagePctPerSec  float64
	DamageRampPerSec float64
}

/*
================
Schedule

The ordered zone phases of one match. It must pass ValidateSchedule against
the arena radius before PlanZone uses it.
================
*/
type Schedule struct {
	Phases []Phase
}

/*
================
ZoneStage

Where the zone is inside its active phase.
================
*/
type ZoneStage int

const (
	// ZoneWaiting: the current circle holds still, the target is known.
	ZoneWaiting ZoneStage = iota
	// ZoneShrinking: the circle moves linearly toward the target.
	ZoneShrinking
	// ZoneFinal: the last target is reached and holds forever.
	ZoneFinal
)

/*
================
ZoneStage.String
================
*/
func (s ZoneStage) String() string {
	switch s {
	case ZoneWaiting:
		return "waiting"
	case ZoneShrinking:
		return "shrinking"
	case ZoneFinal:
		return "final"
	}
	return fmt.Sprintf("ZoneStage(%d)", int(s))
}

/*
================
Zone

One match's planned zone: the schedule, the arena circle it starts from,
the clock time the first phase starts, and every target circle drawn up
front (Targets[i] is phase i's target). Drawing the targets once makes
every later query a pure function of the clock and lets the wiring show
the next circle while the current one still waits.
================
*/
type Zone struct {
	Schedule Schedule
	Start    Circle
	StartMs  int64
	Targets  []Circle
}

/*
================
ZoneState

The zone at one instant. Phase indexes Schedule.Phases and selects the
damage settings; Current is the live safe circle and Target the circle the
phase shrinks to (equal to Current once final). RemainingMs is the time left
in this stage, 0 when final.
================
*/
type ZoneState struct {
	Phase       int
	Stage       ZoneStage
	Current     Circle
	Target      Circle
	RemainingMs int64
}

//============================================================================

/*
================
Outside

Reports whether p lies strictly outside c. Squared distances avoid a square
root and keep the edge itself inside.
================
*/
func Outside(c Circle, p Point) bool {
	dx := p.X - c.Center.X
	dz := p.Z - c.Center.Z
	return dx*dx+dz*dz > c.Radius*c.Radius
}

/*
================
NextCircle

Draws the next target circle of radius endRadius so it lies fully inside
prev: its centre is uniform over the disc of radius prev.Radius - endRadius
around prev's centre (the square root makes the draw uniform by area, not
bunched at the middle). Exactly two draws are taken from rng whenever the
circle shrinks, so the stream stays aligned across schedules. A radius that
does not shrink returns prev unchanged; ValidateSchedule refuses such
phases, so a planned zone never reaches that branch.
================
*/
func NextCircle(prev Circle, endRadius float64, rng *rand.Rand) Circle {
	endRadius = max(endRadius, 0)
	if endRadius >= prev.Radius {
		return prev
	}
	slack := prev.Radius - endRadius
	distance := slack * math.Sqrt(rng.Float64())
	angle := 2 * math.Pi * rng.Float64()
	return Circle{
		Center: Point{
			X: prev.Center.X + distance*math.Cos(angle),
			Z: prev.Center.Z + distance*math.Sin(angle),
		},
		Radius: endRadius,
	}
}

/*
================
ValidateSchedule

Refuses a schedule a zone cannot run: no phases, a phase that does not
strictly shrink (its EndRadius must be below the previous circle's radius,
the arena radius for the first phase), a negative wait, a shrink that takes
no time (the circle would jump over players), a wait or shrink beyond
maxPhaseMs, or a negative or non-finite radius or damage setting.
================
*/
func ValidateSchedule(schedule Schedule, startRadius float64) error {
	if !isFinite(startRadius) || startRadius <= 0 {
		return fmt.Errorf("%w: start radius %v must be positive", ErrInvalidSchedule, startRadius)
	}
	if len(schedule.Phases) == 0 {
		return fmt.Errorf("%w: no phases", ErrInvalidSchedule)
	}
	previous := startRadius
	for i, phase := range schedule.Phases {
		if phase.WaitMs < 0 || phase.WaitMs > maxPhaseMs {
			return fmt.Errorf("%w: phase %d: wait %d ms out of range", ErrInvalidSchedule, i, phase.WaitMs)
		}
		if phase.ShrinkMs <= 0 || phase.ShrinkMs > maxPhaseMs {
			return fmt.Errorf("%w: phase %d: shrink %d ms out of range", ErrInvalidSchedule, i, phase.ShrinkMs)
		}
		if !isFinite(phase.EndRadius) || phase.EndRadius < 0 {
			return fmt.Errorf("%w: phase %d: end radius %v invalid", ErrInvalidSchedule, i, phase.EndRadius)
		}
		if phase.EndRadius >= previous {
			return fmt.Errorf("%w: phase %d: end radius %v does not shrink below %v", ErrInvalidSchedule, i, phase.EndRadius, previous)
		}
		if !isFinite(phase.DamagePctPerSec) || phase.DamagePctPerSec < 0 {
			return fmt.Errorf("%w: phase %d: damage %v invalid", ErrInvalidSchedule, i, phase.DamagePctPerSec)
		}
		if !isFinite(phase.DamageRampPerSec) || phase.DamageRampPerSec < 0 {
			return fmt.Errorf("%w: phase %d: damage ramp %v invalid", ErrInvalidSchedule, i, phase.DamageRampPerSec)
		}
		previous = phase.EndRadius
	}
	return nil
}

/*
================
PlanZone

Draws every target circle of a validated schedule, each inside the one
before, starting from the arena circle. The first phase begins at startMs.
The schedule must already have passed ValidateSchedule against
start.Radius; the slice is copied so the zone owns its phases.
================
*/
func PlanZone(schedule Schedule, start Circle, startMs int64, rng *rand.Rand) Zone {
	return PlanZoneFiltered(schedule, start, startMs, rng, nil)
}

// targetDrawAttempts bounds how often PlanZoneFiltered redraws a target
// the filter refuses before it keeps the last draw.
const targetDrawAttempts = 32

/*
================
PlanZoneFiltered

PlanZone with a veto on target centres: a target whose centre walkable
refuses is redrawn, up to targetDrawAttempts times, after which the last
draw stands (a filter that refuses everything cannot stall a match). A nil
filter is PlanZone. Every refused draw still consumes its two numbers, so
a seed replays the same plan under the same filter.
================
*/
func PlanZoneFiltered(schedule Schedule, start Circle, startMs int64, rng *rand.Rand, walkable func(Point) bool) Zone {
	zone := Zone{
		Schedule: Schedule{Phases: append([]Phase(nil), schedule.Phases...)},
		Start:    start,
		StartMs:  startMs,
		Targets:  make([]Circle, len(schedule.Phases)),
	}
	previous := start
	for i, phase := range schedule.Phases {
		target := NextCircle(previous, phase.EndRadius, rng)
		for attempt := 1; walkable != nil && !walkable(target.Center) && attempt < targetDrawAttempts; attempt++ {
			target = NextCircle(previous, phase.EndRadius, rng)
		}
		previous = target
		zone.Targets[i] = previous
	}
	return zone
}

/*
================
CircleAt

The zone at clock time nowMs. A time before the zone starts reads as the
start of the first wait. Within a shrink the centre and radius blend
linearly from the previous circle to the target; because the target lies
inside the previous circle, every blend contains the target and stays
inside the previous circle. After the last phase the final circle holds.
An empty schedule (never produced by PlanZone from a validated schedule)
reads as final with Phase -1.
================
*/
func CircleAt(zone Zone, nowMs int64) ZoneState {
	elapsed := max(nowMs-zone.StartMs, 0)
	previous := zone.Start
	for i, phase := range zone.Schedule.Phases {
		target := zone.Targets[i]
		if elapsed < phase.WaitMs {
			return ZoneState{Phase: i, Stage: ZoneWaiting, Current: previous, Target: target, RemainingMs: phase.WaitMs - elapsed}
		}
		elapsed -= phase.WaitMs
		if elapsed < phase.ShrinkMs {
			t := float64(elapsed) / float64(phase.ShrinkMs)
			return ZoneState{Phase: i, Stage: ZoneShrinking, Current: blendCircle(previous, target, t), Target: target, RemainingMs: phase.ShrinkMs - elapsed}
		}
		elapsed -= phase.ShrinkMs
		previous = target
	}
	return ZoneState{Phase: len(zone.Schedule.Phases) - 1, Stage: ZoneFinal, Current: previous, Target: previous}
}

/*
================
OutsideDamage

HP to remove for one tick of dtMs spent outside the circle, after msOutside
milliseconds already spent continuously outside. The damage percentage
accumulated after t seconds outside is

	D(t) = DamagePctPerSec*t + DamageRampPerSec*t*t/2

(the integral of a rate that grows linearly with time outside). One tick
deals floor(D(t1)*maxHP) - floor(D(t0)*maxHP), so consecutive ticks
telescope to exactly the damage of one long tick: the tick length never
changes the total and no remainder needs carrying. The result is clamped
to [0, maxHP]; the caller clamps to current HP.
================
*/
func OutsideDamage(maxHP int64, phase Phase, msOutside, dtMs int64) int64 {
	if maxHP <= 0 || dtMs <= 0 {
		return 0
	}
	msOutside = max(msOutside, 0)
	before := outsideDamageTotal(maxHP, phase, msOutside)
	after := outsideDamageTotal(maxHP, phase, msOutside+dtMs)
	return min(max(after-before, 0), maxHP)
}

/*
================
outsideDamageTotal

floor(D(ms) * maxHP) for OutsideDamage, with D and the product capped so
the conversion to int64 stays in range.
================
*/
func outsideDamageTotal(maxHP int64, phase Phase, ms int64) int64 {
	seconds := float64(ms) / msPerSecond
	pct := phase.DamagePctPerSec*seconds + phase.DamageRampPerSec*seconds*seconds/2
	pct = min(max(pct, 0), maxCumulativeOutsidePct)
	total := math.Floor(pct / percentScale * float64(maxHP))
	if total >= maxDamageTotal {
		return maxDamageTotal
	}
	return int64(total)
}

/*
================
blendCircle

Linear blend from a (t = 0) to b (t = 1) of both centre and radius.
================
*/
func blendCircle(a, b Circle, t float64) Circle {
	return Circle{
		Center: Point{
			X: a.Center.X + (b.Center.X-a.Center.X)*t,
			Z: a.Center.Z + (b.Center.Z-a.Center.Z)*t,
		},
		Radius: a.Radius + (b.Radius-a.Radius)*t,
	}
}

/*
================
isFinite
================
*/
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
