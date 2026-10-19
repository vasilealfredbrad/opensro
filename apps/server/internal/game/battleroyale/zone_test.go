/*
===========================================================================

zone_test.go - zone circles, phase timing, schedule validation, damage

Every random draw uses a fixed seed, and the property tests loop over many
seeds: a target circle must lie inside the one before it for every seed,
not for a lucky one.

===========================================================================
*/
package battleroyale

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

const (
	// containmentSlack absorbs one rounding step in the containment checks.
	containmentSlack = 1e-9
	// propertySeeds is how many seeds the property loops try.
	propertySeeds = 2000
)

/*
================
newTestRng
================
*/
func newTestRng(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, 0))
}

/*
================
circleInside

Reports whether inner lies fully inside outer, within rounding.
================
*/
func circleInside(inner, outer Circle) bool {
	gap := math.Hypot(inner.Center.X-outer.Center.X, inner.Center.Z-outer.Center.Z)
	return gap+inner.Radius <= outer.Radius+containmentSlack*max(outer.Radius, 1)
}

/*
================
TestOutside
================
*/
func TestOutside(t *testing.T) {
	circle := Circle{Center: Point{X: 100, Z: -50}, Radius: 10}
	cases := []struct {
		name string
		p    Point
		want bool
	}{
		{"centre", Point{X: 100, Z: -50}, false},
		{"inside", Point{X: 105, Z: -45}, false},
		{"on edge", Point{X: 110, Z: -50}, false},
		{"just outside", Point{X: 110.001, Z: -50}, true},
		{"far", Point{X: -100, Z: 900}, true},
	}
	for _, tc := range cases {
		if got := Outside(circle, tc.p); got != tc.want {
			t.Errorf("%s: Outside = %v, want %v", tc.name, got, tc.want)
		}
	}
	point := Circle{Center: Point{X: 1, Z: 1}, Radius: 0}
	if Outside(point, Point{X: 1, Z: 1}) || !Outside(point, Point{X: 1, Z: 1.0001}) {
		t.Errorf("a zero-radius circle holds only its centre")
	}
}

/*
================
TestNextCircleStaysInsidePrevious

The property behind the whole zone: for any seed and any shrink, the next
circle has the asked radius and lies fully inside the previous one, and
the draws reach both the middle and the rim of the allowed disc.
================
*/
func TestNextCircleStaysInsidePrevious(t *testing.T) {
	prev := Circle{Center: Point{X: 12345.5, Z: -6789.25}, Radius: 3000}
	for _, fraction := range []float64{0, 0.01, 0.32, 0.55, 0.99, 0.999999} {
		end := prev.Radius * fraction
		slack := prev.Radius - end
		nearest, farthest := math.Inf(1), 0.0
		for seed := range uint64(propertySeeds) {
			next := NextCircle(prev, end, newTestRng(seed))
			if next.Radius != end {
				t.Fatalf("fraction %v seed %d: radius %v, want %v", fraction, seed, next.Radius, end)
			}
			if !circleInside(next, prev) {
				t.Fatalf("fraction %v seed %d: %+v not inside %+v", fraction, seed, next, prev)
			}
			offset := math.Hypot(next.Center.X-prev.Center.X, next.Center.Z-prev.Center.Z)
			nearest = min(nearest, offset/slack)
			farthest = max(farthest, offset/slack)
		}
		if nearest > 0.1 || farthest < 0.9 {
			t.Errorf("fraction %v: offsets span %.3f..%.3f of the slack, want most of 0..1", fraction, nearest, farthest)
		}
	}
}

/*
================
TestNextCircleDeterministic
================
*/
func TestNextCircleDeterministic(t *testing.T) {
	prev := Circle{Center: Point{X: 0, Z: 0}, Radius: 1000}
	a := NextCircle(prev, 400, newTestRng(7))
	b := NextCircle(prev, 400, newTestRng(7))
	c := NextCircle(prev, 400, newTestRng(8))
	if a != b {
		t.Errorf("same seed drew %+v and %+v", a, b)
	}
	if a == c {
		t.Errorf("seeds 7 and 8 drew the same circle %+v", a)
	}
}

/*
================
TestNextCircleWithoutShrinkReturnsPrevious
================
*/
func TestNextCircleWithoutShrinkReturnsPrevious(t *testing.T) {
	prev := Circle{Center: Point{X: 5, Z: 6}, Radius: 100}
	for _, end := range []float64{100, 150} {
		if got := NextCircle(prev, end, newTestRng(1)); got != prev {
			t.Errorf("end %v: got %+v, want the previous circle", end, got)
		}
	}
}

/*
================
TestPlanZoneNestsEveryTarget
================
*/
func TestPlanZoneNestsEveryTarget(t *testing.T) {
	arena := Circle{Center: Point{X: 40000, Z: 20000}, Radius: ReferenceArenaRadius}
	schedule := DefaultSchedule()
	for seed := range uint64(propertySeeds) {
		zone := PlanZone(schedule, arena, 0, newTestRng(seed))
		if len(zone.Targets) != len(schedule.Phases) {
			t.Fatalf("seed %d: %d targets for %d phases", seed, len(zone.Targets), len(schedule.Phases))
		}
		previous := arena
		for i, target := range zone.Targets {
			if target.Radius != schedule.Phases[i].EndRadius {
				t.Fatalf("seed %d phase %d: radius %v, want %v", seed, i, target.Radius, schedule.Phases[i].EndRadius)
			}
			if !circleInside(target, previous) {
				t.Fatalf("seed %d phase %d: %+v not inside %+v", seed, i, target, previous)
			}
			previous = target
		}
	}
}

/*
================
TestPlanZoneOwnsItsPhases
================
*/
func TestPlanZoneOwnsItsPhases(t *testing.T) {
	schedule := DefaultSchedule()
	zone := PlanZone(schedule, Circle{Radius: ReferenceArenaRadius}, 0, newTestRng(1))
	schedule.Phases[0].WaitMs = 1
	if zone.Schedule.Phases[0].WaitMs == 1 {
		t.Errorf("the zone shares the caller's phase slice")
	}
}

/*
================
handZone

A zone with fixed targets, so CircleAt's timings and blends are checked
against hand-computed values.
================
*/
func handZone() Zone {
	return Zone{
		Schedule: Schedule{Phases: []Phase{
			{WaitMs: 1000, ShrinkMs: 2000, EndRadius: 500},
			{WaitMs: 500, ShrinkMs: 1000, EndRadius: 100},
		}},
		Start:   Circle{Center: Point{X: 0, Z: 0}, Radius: 1000},
		StartMs: 10_000,
		Targets: []Circle{
			{Center: Point{X: 200, Z: 0}, Radius: 500},
			{Center: Point{X: 300, Z: 100}, Radius: 100},
		},
	}
}

/*
================
TestCircleAtTimings
================
*/
func TestCircleAtTimings(t *testing.T) {
	zone := handZone()
	start, first, second := zone.Start, zone.Targets[0], zone.Targets[1]
	cases := []struct {
		name  string
		nowMs int64
		want  ZoneState
	}{
		{"before start", 0, ZoneState{Phase: 0, Stage: ZoneWaiting, Current: start, Target: first, RemainingMs: 1000}},
		{"start", 10_000, ZoneState{Phase: 0, Stage: ZoneWaiting, Current: start, Target: first, RemainingMs: 1000}},
		{"last wait ms", 10_999, ZoneState{Phase: 0, Stage: ZoneWaiting, Current: start, Target: first, RemainingMs: 1}},
		{"shrink begins", 11_000, ZoneState{Phase: 0, Stage: ZoneShrinking, Current: start, Target: first, RemainingMs: 2000}},
		{"half shrunk", 12_000, ZoneState{Phase: 0, Stage: ZoneShrinking, Current: Circle{Center: Point{X: 100, Z: 0}, Radius: 750}, Target: first, RemainingMs: 1000}},
		{"phase 2 waits", 13_000, ZoneState{Phase: 1, Stage: ZoneWaiting, Current: first, Target: second, RemainingMs: 500}},
		{"phase 2 shrinks", 13_500, ZoneState{Phase: 1, Stage: ZoneShrinking, Current: first, Target: second, RemainingMs: 1000}},
		{"phase 2 half", 14_000, ZoneState{Phase: 1, Stage: ZoneShrinking, Current: Circle{Center: Point{X: 250, Z: 50}, Radius: 300}, Target: second, RemainingMs: 500}},
		{"final", 14_500, ZoneState{Phase: 1, Stage: ZoneFinal, Current: second, Target: second}},
		{"long after", 1 << 40, ZoneState{Phase: 1, Stage: ZoneFinal, Current: second, Target: second}},
	}
	for _, tc := range cases {
		if got := CircleAt(zone, tc.nowMs); got != tc.want {
			t.Errorf("%s: CircleAt(%d) = %+v, want %+v", tc.name, tc.nowMs, got, tc.want)
		}
	}
}

/*
================
TestCircleAtBlendsStayNested

During every shrink of planned zones the live circle contains the target
and stays inside the circle the phase started from.
================
*/
func TestCircleAtBlendsStayNested(t *testing.T) {
	arena := Circle{Center: Point{X: 9000, Z: 9000}, Radius: ReferenceArenaRadius}
	schedule := DefaultSchedule()
	const stepMs = 250
	var totalMs int64
	for _, phase := range schedule.Phases {
		totalMs += phase.WaitMs + phase.ShrinkMs
	}
	for seed := range uint64(50) {
		zone := PlanZone(schedule, arena, 0, newTestRng(seed))
		for nowMs := int64(0); nowMs <= totalMs; nowMs += stepMs {
			state := CircleAt(zone, nowMs)
			from := arena
			if state.Phase > 0 {
				from = zone.Targets[state.Phase-1]
			}
			if !circleInside(state.Target, state.Current) || !circleInside(state.Current, from) {
				t.Fatalf("seed %d at %d ms: %+v breaks target <= current <= %+v", seed, nowMs, state, from)
			}
		}
	}
}

/*
================
TestValidateSchedule
================
*/
func TestValidateSchedule(t *testing.T) {
	phase := func(wait, shrink int64, end float64) Phase {
		return Phase{WaitMs: wait, ShrinkMs: shrink, EndRadius: end, DamagePctPerSec: 1}
	}
	cases := []struct {
		name   string
		phases []Phase
		start  float64
		ok     bool
	}{
		{"default", DefaultSchedule().Phases, ReferenceArenaRadius, true},
		{"zero wait", []Phase{phase(0, 1000, 10)}, 100, true},
		{"closes to a point", []Phase{phase(10, 1000, 50), phase(10, 1000, 0)}, 100, true},
		{"no phases", nil, 100, false},
		{"zero start radius", []Phase{phase(10, 1000, 0)}, 0, false},
		{"NaN start radius", []Phase{phase(10, 1000, 0)}, math.NaN(), false},
		{"first phase does not shrink", []Phase{phase(10, 1000, 100)}, 100, false},
		{"first phase grows", []Phase{phase(10, 1000, 150)}, 100, false},
		{"second phase holds", []Phase{phase(10, 1000, 50), phase(10, 1000, 50)}, 100, false},
		{"second phase grows", []Phase{phase(10, 1000, 50), phase(10, 1000, 60)}, 100, false},
		{"negative wait", []Phase{phase(-1, 1000, 10)}, 100, false},
		{"instant shrink", []Phase{phase(10, 0, 10)}, 100, false},
		{"endless shrink", []Phase{phase(10, maxPhaseMs+1, 10)}, 100, false},
		{"endless wait", []Phase{phase(maxPhaseMs+1, 1000, 10)}, 100, false},
		{"negative end radius", []Phase{phase(10, 1000, -1)}, 100, false},
		{"NaN end radius", []Phase{phase(10, 1000, math.NaN())}, 100, false},
		{"negative damage", []Phase{{WaitMs: 10, ShrinkMs: 1000, EndRadius: 10, DamagePctPerSec: -1}}, 100, false},
		{"infinite ramp", []Phase{{WaitMs: 10, ShrinkMs: 1000, EndRadius: 10, DamageRampPerSec: math.Inf(1)}}, 100, false},
	}
	for _, tc := range cases {
		err := ValidateSchedule(Schedule{Phases: tc.phases}, tc.start)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, ErrInvalidSchedule) {
			t.Errorf("%s: error %v, want ErrInvalidSchedule", tc.name, err)
		}
	}
}

/*
================
TestOutsideDamage
================
*/
func TestOutsideDamage(t *testing.T) {
	flat := Phase{DamagePctPerSec: 1}
	ramp := Phase{DamagePctPerSec: 1, DamageRampPerSec: 0.5}
	cases := []struct {
		name      string
		maxHP     int64
		phase     Phase
		msOutside int64
		dtMs      int64
		want      int64
	}{
		{"flat first second", 1000, flat, 0, 1000, 10},
		{"flat stays flat", 1000, flat, 5000, 1000, 10},
		// D(2 s) = 1*2 + 0.5*4/2 = 3%.
		{"ramp first two seconds", 1000, ramp, 0, 2000, 30},
		// D(4 s) - D(2 s) = (4 + 4) - 3 = 5%: the same two seconds hurt
		// more than the first two.
		{"ramp next two seconds", 1000, ramp, 2000, 2000, 50},
		// D(1 s) = 1.25% of 1000 = 12.5, floored.
		{"fraction floors", 1000, ramp, 0, 1000, 12},
		{"no time", 1000, ramp, 0, 0, 0},
		{"negative time", 1000, ramp, 0, -500, 0},
		{"no max HP", 0, ramp, 0, 1000, 0},
		{"negative outside reads as zero", 1000, flat, -5000, 1000, 10},
		{"clamped to max HP", 1000, Phase{DamagePctPerSec: 500}, 0, 1000, 1000},
		{"no damage settings", 1000, Phase{}, 0, 60_000, 0},
	}
	for _, tc := range cases {
		if got := OutsideDamage(tc.maxHP, tc.phase, tc.msOutside, tc.dtMs); got != tc.want {
			t.Errorf("%s: OutsideDamage = %d, want %d", tc.name, got, tc.want)
		}
	}
}

/*
================
TestOutsideDamageRampsWithTimeOutside

With a ramp every further second outside hurts more than the one before;
without one every second hurts the same. Distance plays no part.
================
*/
func TestOutsideDamageRampsWithTimeOutside(t *testing.T) {
	const maxHP = 100_000
	for i, phase := range DefaultSchedule().Phases {
		previous := OutsideDamage(maxHP, phase, 0, 1000)
		for second := int64(1); second < 10; second++ {
			damage := OutsideDamage(maxHP, phase, second*1000, 1000)
			if damage <= previous {
				t.Errorf("phase %d second %d: %d after %d, want growth", i, second, damage, previous)
			}
			previous = damage
		}
		flat := phase
		flat.DamageRampPerSec = 0
		if first, later := OutsideDamage(maxHP, flat, 0, 1000), OutsideDamage(maxHP, flat, 9000, 1000); first != later {
			t.Errorf("phase %d without ramp: %d then %d, want equal", i, first, later)
		}
	}
}

/*
================
TestOutsideDamageIndependentOfTickLength

Summing per-tick damage over any tick length gives exactly the damage of
one tick spanning the same time.
================
*/
func TestOutsideDamageIndependentOfTickLength(t *testing.T) {
	const windowMs = 20_000
	phases := DefaultSchedule().Phases[:2]
	for _, maxHP := range []int64{1, 37, 1000, 98_765} {
		for i, phase := range phases {
			want := OutsideDamage(maxHP, phase, 0, windowMs)
			for _, tickMs := range []int64{1, 7, 50, 100, 333, 1000} {
				var total int64
				for elapsed := int64(0); elapsed < windowMs; elapsed += tickMs {
					total += OutsideDamage(maxHP, phase, elapsed, min(tickMs, windowMs-elapsed))
				}
				if total != want {
					t.Errorf("max HP %d phase %d tick %d ms: total %d, want %d", maxHP, i, tickMs, total, want)
				}
			}
		}
	}
}
