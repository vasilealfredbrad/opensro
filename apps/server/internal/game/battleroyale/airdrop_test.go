/*
===========================================================================

airdrop_test.go - the stage-8b airdrop's ultimate weapon rules

Port-only, not native. The ultimate weapon rolls at about 35 % a phase
and always on the final phase; it is a Seal of Sun weapon of the build
at the newest degree the killer can wear. Stage 9: the flight line is a
diagonal across the landing circle with both ends well outside it, and
its landing point lies on it, inside the circle, on walkable ground,
sliding along the line past refused ground and drawing a new line when
the chord has none.

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
TestAirdropUltimateChance
================
*/
func TestAirdropUltimateChance(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 4))
	const rolls, phases = 20000, 6
	hits := 0
	for range rolls {
		if RollAirdropUltimate(0, phases, rng) {
			hits++
		}
		if !RollAirdropUltimate(phases-1, phases, rng) {
			t.Fatal("the final phase's airdrop has no ultimate weapon")
		}
	}
	if share := float64(hits) / rolls; share < 0.33 || share > 0.37 {
		t.Fatalf("ultimate share %.3f, want 0.35", share)
	}
}

/*
================
TestUltimateWeaponCodename
================
*/
func TestUltimateWeaponCodename(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 5))
	for _, build := range Builds() {
		for level := int64(1); level <= MatchLevelCap; level++ {
			killer := Killer{Build: build, Level: level}
			name, ok := UltimateWeaponCodename(killer, rng)
			required, known := RequiredLevel(name)
			if !ok || !known || required > level || !strings.HasSuffix(name, "_C_RARE") {
				t.Fatalf("build %d level %d: %q (required %d)", build.ID, level, name, required)
			}
			slot, degree, ok := SlotOf(killer, name)
			if !ok || slot != SlotWeapon || degree != DegreeForLevel(level) {
				t.Fatalf("build %d level %d: %s is slot %s degree %d", build.ID, level, name, slot.Name(), degree)
			}
		}
	}
	if UltimateBanner("Ann") != "Ann found the ultimate weapon!" {
		t.Fatal(UltimateBanner("Ann"))
	}
}

/*
================
checkFlightLine

Fails t unless line crosses circle diagonally (within 15 degrees of a
diagonal), both ends at least flightOverhangM outside it, and its
landing point lies on it, inside the circle.
================
*/
func checkFlightLine(t *testing.T, line FlightLine, circle Circle) {
	t.Helper()
	dx, dz := line.B.X-line.A.X, line.B.Z-line.A.Z
	length := math.Hypot(dx, dz)
	offLine := math.Abs(dx*(line.Landing.Z-line.A.Z)-dz*(line.Landing.X-line.A.X)) / length
	if offLine > 1e-6 || Outside(circle, line.Landing) {
		t.Fatalf("landing %+v off the line (%.3g) or outside %+v", line.Landing, offLine, circle)
	}
	for _, end := range []Point{line.A, line.B} {
		if d := math.Hypot(end.X-circle.Center.X, end.Z-circle.Center.Z); d < circle.Radius+flightOverhangM*UnitsPerMeter-1e-6 {
			t.Fatalf("end %+v only %.0f from the centre of %+v", end, d, circle)
		}
	}
	angle := math.Mod(math.Atan2(dz, dx)+2*math.Pi, math.Pi/2)
	if math.Abs(angle-math.Pi/4) > flightJitter+1e-9 {
		t.Fatalf("line at %.1f degrees off its quadrant, not a diagonal", angle*180/math.Pi)
	}
	foot := math.Abs(dx*(circle.Center.Z-line.A.Z)-dz*(circle.Center.X-line.A.X)) / length
	if foot > flightOffsetShare*circle.Radius+1e-6 {
		t.Fatalf("line passes %.0f from the centre of %+v", foot, circle)
	}
}

/*
================
TestPlanFlightLine

Over many seeds and circles (a point target among them): the line and
its landing point keep checkFlightLine, the landing point is walkable,
and it is spread over the chord. A walkable band the uniform draw
mostly misses is found by sliding; a circle without walkable ground
has no flight line.
================
*/
func TestPlanFlightLine(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 7))
	everywhere := func(Point) bool { return true }
	for i := range 400 {
		target := Circle{Center: Point{X: 214080 + float64(i), Z: 175680}, Radius: float64(i%5) * 2500}
		line, ok := PlanFlightLine(target, everywhere, rng)
		if !ok {
			t.Fatalf("target %+v: no line", target)
		}
		checkFlightLine(t, line, AirdropLandingCircle(target))
	}
	if AirdropLandingCircle(Circle{Radius: 0}).Radius != SupplyMinRadius {
		t.Fatal("a point target lands on a point")
	}
	// Only a 20 m band west of the centre is walkable: the landing slides
	// onto it (most uniform draws land elsewhere on the chord).
	target := Circle{Center: Point{X: 0, Z: 0}, Radius: 6000}
	band := func(p Point) bool { return p.X > -1200 && p.X < -1000 }
	for range 200 {
		line, ok := PlanFlightLine(target, band, rng)
		if !ok {
			t.Fatal("no landing on the walkable band")
		}
		checkFlightLine(t, line, target)
		if !band(line.Landing) {
			t.Fatalf("landing %+v off the walkable band", line.Landing)
		}
	}
	if _, ok := PlanFlightLine(target, func(Point) bool { return false }, rng); ok {
		t.Fatal("a flight line over ground nowhere walkable")
	}
	// A walkable half-plane the first lines may miss entirely: a later
	// line finds it (or the slide does) and never lands off it.
	east := func(p Point) bool { return p.X > 3000 }
	for range 100 {
		line, ok := PlanFlightLine(target, east, rng)
		if !ok || !east(line.Landing) {
			t.Fatalf("east landing %+v (%v)", line.Landing, ok)
		}
		checkFlightLine(t, line, target)
	}
}

/*
================
TestAirdropTiming

The flight line shows for a minute before the airdrop lands, and every
phase outlasts it.
================
*/
func TestAirdropTiming(t *testing.T) {
	if AirdropLeadMs != 60_000 {
		t.Fatalf("lead %d ms", AirdropLeadMs)
	}
	for _, players := range []int{2, 10, 22} {
		for _, phase := range ScheduleFor(players, ArenaCircle().Radius).Phases {
			if phase.WaitMs+phase.ShrinkMs <= AirdropLeadMs {
				t.Fatalf("%d players: phase %+v ends before its airdrop lands", players, phase)
			}
		}
	}
}
