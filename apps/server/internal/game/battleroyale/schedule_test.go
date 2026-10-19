/*
===========================================================================

schedule_test.go - the default schedule and its scaling rules

The exact numbers will be tuned; these tests pin the rules: the default
table (stage 4), the direction and anchors of the wait scaling, the
arena-size scaling, the three-minute shrink floor, and that every scaled
schedule is valid and outrunnable.

===========================================================================
*/
package battleroyale

import (
	"math"
	"reflect"
	"testing"
)

const (
	// radiusTolerance compares radii built by different float paths.
	radiusTolerance = 1e-9
	msPerMinute     = 60_000
)

/*
================
scheduleDurationMs
================
*/
func scheduleDurationMs(schedule Schedule) int64 {
	var total int64
	for _, phase := range schedule.Phases {
		total += phase.WaitMs + phase.ShrinkMs
	}
	return total
}

/*
================
TestDefaultScheduleTable

The default schedule is the header's table at the reference (all-Roc)
arena: six phases, every shrink three minutes, 24:05 in all.
================
*/
func TestDefaultScheduleTable(t *testing.T) {
	const shrink = 180_000
	want := []Phase{
		{WaitMs: 120_000, ShrinkMs: shrink, EndRadius: 16800, DamagePctPerSec: 1, DamageRampPerSec: 0.05},
		{WaitMs: 90_000, ShrinkMs: shrink, EndRadius: 13200, DamagePctPerSec: 2, DamageRampPerSec: 0.10},
		{WaitMs: 60_000, ShrinkMs: shrink, EndRadius: 9600, DamagePctPerSec: 3.5, DamageRampPerSec: 0.18},
		{WaitMs: 45_000, ShrinkMs: shrink, EndRadius: 6000, DamagePctPerSec: 5, DamageRampPerSec: 0.25},
		{WaitMs: 30_000, ShrinkMs: shrink, EndRadius: 2600, DamagePctPerSec: 8, DamageRampPerSec: 0.40},
		{WaitMs: 20_000, ShrinkMs: shrink, EndRadius: 0, DamagePctPerSec: 12, DamageRampPerSec: 0.60},
	}
	if ReferenceArenaRadius != ArenaCircle().Radius {
		t.Fatalf("reference arena %v is not the Roc arena %v", ReferenceArenaRadius, ArenaCircle().Radius)
	}
	got := DefaultSchedule()
	if len(got.Phases) != len(want) {
		t.Fatalf("%d phases, want %d", len(got.Phases), len(want))
	}
	for i, phase := range got.Phases {
		radius := phase.EndRadius
		phase.EndRadius = want[i].EndRadius
		if phase != want[i] || math.Abs(radius-want[i].EndRadius) > radiusTolerance {
			t.Errorf("phase %d: %+v (radius %v), want %+v", i, phase, radius, want[i])
		}
	}
	if err := ValidateSchedule(got, ReferenceArenaRadius); err != nil {
		t.Fatalf("default schedule invalid: %v", err)
	}
	if total := scheduleDurationMs(got); total != 24*msPerMinute+5_000 {
		t.Errorf("default lasts %d ms, want 24:05", total)
	}
}

/*
================
TestScheduleForPlayerCounts

Fewer players wait less; the circles do not depend on the player count.
On the Roc arena 2, 5 and 10 players last 21:02.5, 22:33.75 and 24:05, all
inside the owner's 20-25 minutes.
================
*/
func TestScheduleForPlayerCounts(t *testing.T) {
	cases := []struct {
		players     int
		firstWaitMs int64
		totalMs     int64
	}{
		{2, 60_000, 21*msPerMinute + 2_500},
		{5, 90_000, 22*msPerMinute + 33_750},
		{10, 120_000, 24*msPerMinute + 5_000},
	}
	ten := ScheduleFor(10, ReferenceArenaRadius)
	for _, tc := range cases {
		schedule := ScheduleFor(tc.players, ReferenceArenaRadius)
		if got := schedule.Phases[0].WaitMs; got != tc.firstWaitMs {
			t.Errorf("%d players: first wait %d, want %d", tc.players, got, tc.firstWaitMs)
		}
		if total := scheduleDurationMs(schedule); total != tc.totalMs || total < 20*msPerMinute || total > 25*msPerMinute {
			t.Errorf("%d players: lasts %d ms, want %d", tc.players, total, tc.totalMs)
		}
		for phase := range schedule.Phases {
			if schedule.Phases[phase].EndRadius != ten.Phases[phase].EndRadius || schedule.Phases[phase].ShrinkMs != ten.Phases[phase].ShrinkMs {
				t.Errorf("%d players phase %d: circle or shrink differs from 10 players", tc.players, phase)
			}
		}
	}
}

/*
================
TestScheduleForHoldsOutsideAnchors
================
*/
func TestScheduleForHoldsOutsideAnchors(t *testing.T) {
	if got, want := ScheduleFor(1, ReferenceArenaRadius), ScheduleFor(2, ReferenceArenaRadius); !reflect.DeepEqual(got, want) {
		t.Errorf("1 player differs from 2 players")
	}
	if got, want := ScheduleFor(64, ReferenceArenaRadius), ScheduleFor(10, ReferenceArenaRadius); !reflect.DeepEqual(got, want) {
		t.Errorf("64 players differ from 10 players")
	}
	five, seven, ten := ScheduleFor(5, ReferenceArenaRadius), ScheduleFor(7, ReferenceArenaRadius), ScheduleFor(10, ReferenceArenaRadius)
	if wait := seven.Phases[0].WaitMs; wait <= five.Phases[0].WaitMs || wait >= ten.Phases[0].WaitMs {
		t.Errorf("7 players wait %d, want between %d and %d", wait, five.Phases[0].WaitMs, ten.Phases[0].WaitMs)
	}
}

/*
================
TestScheduleForArenaSize

Doubling the arena doubles every radius and every time, so the zone edge
keeps its speed; halving it halves the radii and waits but never takes a
shrink under three minutes.
================
*/
func TestScheduleForArenaSize(t *testing.T) {
	base := ScheduleFor(10, ReferenceArenaRadius)
	double := ScheduleFor(10, 2*ReferenceArenaRadius)
	half := ScheduleFor(10, ReferenceArenaRadius/2)
	for i := range base.Phases {
		b, d, h := base.Phases[i], double.Phases[i], half.Phases[i]
		if d.WaitMs != 2*b.WaitMs || d.ShrinkMs != 2*b.ShrinkMs {
			t.Errorf("phase %d: times %d/%d, want %d/%d", i, d.WaitMs, d.ShrinkMs, 2*b.WaitMs, 2*b.ShrinkMs)
		}
		if math.Abs(d.EndRadius-2*b.EndRadius) > radiusTolerance || math.Abs(2*h.EndRadius-b.EndRadius) > radiusTolerance {
			t.Errorf("phase %d: radius %v / %v, want %v / %v", i, d.EndRadius, h.EndRadius, 2*b.EndRadius, b.EndRadius/2)
		}
		if h.ShrinkMs != minPhaseShrinkMs || h.WaitMs != max(scaleMs(b.WaitMs, 0.5), minPhaseWaitMs) {
			t.Errorf("phase %d: half-size times %d/%d", i, h.WaitMs, h.ShrinkMs)
		}
		if d.DamagePctPerSec != b.DamagePctPerSec || d.DamageRampPerSec != b.DamageRampPerSec {
			t.Errorf("phase %d: damage changed with arena size", i)
		}
	}
}

/*
================
TestScheduleForValidAndOutrunnable

Every scaled schedule validates, respects the time floors (every shrink
three minutes or more), and never moves its edge faster than
maxZoneEdgeSpeed, the outrunnable limit.
================
*/
func TestScheduleForValidAndOutrunnable(t *testing.T) {
	for _, radius := range []float64{100, 500, 1500, 3000, 6000, ReferenceArenaRadius, 40_000} {
		for players := 1; players <= 12; players++ {
			schedule := ScheduleFor(players, radius)
			if err := ValidateSchedule(schedule, radius); err != nil {
				t.Fatalf("radius %v players %d: %v", radius, players, err)
			}
			previous := radius
			for i, phase := range schedule.Phases {
				if phase.WaitMs < minPhaseWaitMs || phase.ShrinkMs < minPhaseShrinkMs {
					t.Errorf("radius %v players %d phase %d: times %d/%d below the floors", radius, players, i, phase.WaitMs, phase.ShrinkMs)
				}
				speed := 2 * (previous - phase.EndRadius) / (float64(phase.ShrinkMs) / msPerSecond)
				if speed > maxZoneEdgeSpeed+radiusTolerance {
					t.Errorf("radius %v players %d phase %d: edge speed %v above %v", radius, players, i, speed, maxZoneEdgeSpeed)
				}
				previous = phase.EndRadius
			}
		}
	}
}
