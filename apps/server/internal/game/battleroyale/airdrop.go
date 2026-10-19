/*
===========================================================================

airdrop.go - the phase airdrop and its ultimate weapon

Port-only, not native (stage 8b, docs/battle-royale.md "Stage 8b:
airdrop with an ultimate weapon", "Stage 9" item 5). Every zone phase
start announces an airdrop: a straight flight line across the next
circle (PlanFlightLine) shows on every participant's minimap and world
map (0x3FB6 v2 state 1) for AirdropLeadMs, then the airdrop lands at
its landing point, a walkable point of the line inside the next
circle, so it always falls in the safe zone. There the supply-drop
elite (loot.go SupplyElite, a giant match wolf) guards it (0x3FB6
state 2) until the guardian dies or the phase ends (state 0); its loot
is SupplyDropLoot plus, at AirdropUltimateChance percent (always on the
final phase's airdrop), the ultimate weapon.

The flight line: a direction within flightJitter of one of the four
diagonals, crossing the landing circle (the next circle, at least
SupplyMinRadius around a point target) at a perpendicular offset of at
most flightOffsetShare of its radius, with both ends flightOverhangM
beyond the circle. The landing point is a uniform point of the chord
(kept flightChordShare inside its ends); when the ground there is not
walkable it slides along the line, in GroundMaskStep steps alternating
both ways, to the nearest walkable point of the chord; a chord without
one draws a new line (flightLineAttempts in all).

The ultimate weapon: a Seal of Sun weapon of the killer's build of the
highest degree the killer can wear (DegreeForLevel: every weapon degree
needs 8 (d - 1), so the newest usable degree; the loot window [level-3,
level] holds no weapon row at levels like 30, so the window rule would
give none), at UltimatePlus, with the most blue options its native pool
allows at their top values and every white stat at 100 % (the wiring
finishes it: action/battleroyale_loot.go).

===========================================================================
*/
package battleroyale

import (
	"fmt"
	"math"
	"math/rand/v2"
)

const (
	// AirdropUltimateChance is the percent chance an airdrop's guardian
	// carries the ultimate weapon (the final phase's always does).
	AirdropUltimateChance = 35
	// AirdropBanner is the channel-7 notice of every airdrop, sent with
	// its flight line (stage 9).
	AirdropBanner = "Airdrop incoming - watch the flight line on your map!"
	// AirdropLeadMs is how long the flight line shows before the airdrop
	// lands: it lands AirdropLeadMs after its phase starts (every phase
	// lasts longer: its shrink alone is three minutes).
	AirdropLeadMs = 60_000

	// The flight line (file header).
	flightLineAttempts = 32
	flightJitter       = math.Pi / 12
	flightOffsetShare  = 0.6
	flightChordShare   = 0.95
	flightOverhangM    = 500
	// ultimateBannerFormat names the finder in the shard-wide notice.
	ultimateBannerFormat = "%s found the ultimate weapon!"
	// devilSpiritBannerFormat names the finder of an airdrop's Devil's
	// Spirit (port-only item, game/item/devilspirit).
	devilSpiritBannerFormat = "%s found a Devil's Spirit!"
)

/*
================
RollAirdropUltimate

Whether the airdrop of phase (0-based) of a schedule of phases carries
the ultimate weapon: always on the final phase, else
AirdropUltimateChance percent.
================
*/
func RollAirdropUltimate(phase, phases int, rng *rand.Rand) bool {
	if phase >= phases-1 {
		return true
	}
	return rng.IntN(percentRoll) < AirdropUltimateChance
}

/*
================
UltimateWeaponCodename

The ultimate weapon's codename for killer: a Seal of Sun weapon of one
of the build's kinds (drawn uniformly) at DegreeForLevel of its level;
false for a build without a weapon row.
================
*/
func UltimateWeaponCodename(killer Killer, rng *rand.Rand) (string, bool) {
	degree := DegreeForLevel(killer.Level)
	var names []string
	for _, kind := range killer.Build.WeaponKinds {
		if name, ok := SealWeaponCodename(killer.Build.Race, kind, degree, SealSun); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	return names[rng.IntN(len(names))], true
}

/*
================
UltimateBanner

The shard-wide notice of a found ultimate weapon.
================
*/
func UltimateBanner(name string) string {
	return fmt.Sprintf(ultimateBannerFormat, name)
}

/*
================
DevilSpiritBanner

The shard-wide notice of a Devil's Spirit found in an airdrop.
================
*/
func DevilSpiritBanner(name string) string {
	return fmt.Sprintf(devilSpiritBannerFormat, name)
}

//============================================================================

/*
================
FlightLine

An airdrop's flight: the line from A to B (grid units) and the landing
point on it.
================
*/
type FlightLine struct {
	A, B    Point
	Landing Point
}

/*
================
AirdropLandingCircle

Where an airdrop of a phase whose next circle is target may land: the
target itself, or SupplyMinRadius around the point of the final phase.
================
*/
func AirdropLandingCircle(target Circle) Circle {
	return Circle{Center: target.Center, Radius: max(target.Radius, SupplyMinRadius)}
}

/*
================
PlanFlightLine

The flight line of an airdrop whose next circle is target (file header):
a diagonal line crossing AirdropLandingCircle(target) with both ends
well outside it, and its landing point, a point of the line inside the
circle where walkable says yes; false when no line of
flightLineAttempts finds walkable ground.
================
*/
func PlanFlightLine(target Circle, walkable func(Point) bool, rng *rand.Rand) (FlightLine, bool) {
	circle := AirdropLandingCircle(target)
	for range flightLineAttempts {
		diagonal := math.Pi/4 + float64(rng.IntN(4))*math.Pi/2
		angle := diagonal + (rng.Float64()*2-1)*flightJitter
		ux, uz := math.Cos(angle), math.Sin(angle)
		offset := (rng.Float64()*2 - 1) * flightOffsetShare * circle.Radius
		// The point of the line nearest the centre, and the half chord.
		foot := Point{X: circle.Center.X - uz*offset, Z: circle.Center.Z + ux*offset}
		half := math.Sqrt(circle.Radius*circle.Radius-offset*offset) * flightChordShare
		at := func(t float64) Point { return Point{X: foot.X + ux*t, Z: foot.Z + uz*t} }
		reach := circle.Radius + flightOverhangM*UnitsPerMeter
		line := FlightLine{A: at(-reach), B: at(reach)}
		start := (rng.Float64()*2 - 1) * half
		for step := 0; float64(step)*GroundMaskStep <= 2*half; step++ {
			for _, sign := range []float64{1, -1} {
				t := start + sign*float64(step)*GroundMaskStep
				if math.Abs(t) > half || (step == 0 && sign < 0) {
					continue
				}
				if p := at(t); walkable == nil || walkable(p) {
					line.Landing = p
					return line, true
				}
			}
		}
	}
	return FlightLine{}, false
}
