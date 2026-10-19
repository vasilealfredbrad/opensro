/*
===========================================================================

drop.go - where participants land in the arena

Port-only, not native (see zone.go). At the start of a match every
participant gets one drop point: uniform over the arena (kept off its
edge), spread apart by a minimum distance when the arena has room, drawn
from the match seed so a replay lands everyone on the same spot.

The bots (stage 7) do not drop over the whole arena: the first live
match spread them over 5 km2 of Roc and nobody met one. Bot i lands in a
ring BotRingMinM-BotRingMaxM around real player i mod n (spread among
the players), on accepted ground, never nearer than BotRingMinM to any
player, BotRingSpacingM apart from the other bots; a ring too full for
that widens (stage 8c).

===========================================================================
*/
package battleroyale

import (
	"math"
	"math/rand/v2"
)

const (
	// BotRingMinM and BotRingMaxM bound a bot's drop around its player.
	BotRingMinM = 150
	BotRingMaxM = 400
	// BotRingSpacingM keeps the bots of a ring apart (stage 8c: two bots
	// dropped within each other's sense started fighting at once; 120 m
	// is past the Normal 70 m sense with room to farm). botRingGrowM is
	// how much a full ring widens, at most botRingGrowths times.
	BotRingSpacingM = 120
	botRingGrowM    = 100
	botRingGrowths  = 12
	// botRingStream selects the PCG stream of the ring draws.
	botRingStream = 0x52696e67 // "Ring"
	// dropEdgeFraction keeps drop points off the arena's edge.
	dropEdgeFraction = 0.9
	// dropAttempts is how many candidates a drop point tries before it
	// settles for the best-spaced one.
	dropAttempts = 64
)

/*
================
DropPoints

count drop points inside the arena (within dropEdgeFraction of its
radius), each at least spacing from the ones before it when the arena has
room. Each point tries dropAttempts uniform candidates and keeps the first
that is far enough, or else the one farthest from its nearest neighbour,
so an over-full arena degrades to best-effort spreading instead of failing.
================
*/
func DropPoints(arena Circle, count int, spacing float64, rng *rand.Rand) []Point {
	return DropPointsFiltered(arena, count, spacing, rng, nil)
}

/*
================
DropPointsFiltered

DropPoints with a ground veto (stage 4: the all-Roc arena is mostly
other ground): a candidate keep refuses never stands while an accepted
one exists; when every candidate of a point is refused the best-spaced
refused one stands and the wiring redraws it. A nil keep is DropPoints.
Each point tries dropAttempts candidates whatever keep says, so a seed
replays the same points under the same keep.
================
*/
func DropPointsFiltered(arena Circle, count int, spacing float64, rng *rand.Rand, keep func(Point) bool) []Point {
	area := Circle{Center: arena.Center, Radius: arena.Radius * dropEdgeFraction}
	points := make([]Point, 0, max(count, 0))
	for range count {
		var best Point
		bestGap, bestKept := -1.0, false
		for range dropAttempts {
			// A zero-radius circle inside the area is a uniform point in it.
			candidate := NextCircle(area, 0, rng).Center
			kept := keep == nil || keep(candidate)
			gap := nearestDistance(candidate, points)
			if (kept && !bestKept) || (kept == bestKept && gap > bestGap) {
				best, bestGap, bestKept = candidate, gap, kept
			}
			if kept && gap >= spacing {
				break
			}
		}
		points = append(points, best)
	}
	return points
}

/*
================
nearestDistance

The distance from p to the closest of points, +Inf when there are none.
================
*/
func nearestDistance(p Point, points []Point) float64 {
	nearest := math.Inf(1)
	for _, q := range points {
		nearest = min(nearest, math.Hypot(p.X-q.X, p.Z-q.Z))
	}
	return nearest
}

/*
================
BotRingPoints

count bot drop points around the real players' drops (anchors): bot i
draws in the ring around anchors[i mod n] (area-uniform, BotRingMinM to
the anchor's outer radius, BotRingMaxM at first), inside the arena's
drop area, nearer to no player than BotRingMinM. Each point tries
dropAttempts candidates and keeps the first that keep accepts and lies
BotRingSpacingM from every bot before it. When a whole round finds none
(the ring is full: a 150-400 m ring holds about 34 bots 120 m apart) the
anchor's outer radius grows by botRingGrowM and the point draws again,
at most botRingGrowths times; past that it keeps the accepted candidate
farthest from the bots, else the best-spaced refused one (the wiring
redraws it). The ring only ever grows, so the next bots of a full ring
draw in the wider one at once. With no anchor the bots drop as
DropPointsFiltered.
================
*/
func BotRingPoints(anchors []Point, count int, arena Circle, rng *rand.Rand, keep func(Point) bool) []Point {
	if len(anchors) == 0 {
		return DropPointsFiltered(arena, count, BotRingSpacingM*UnitsPerMeter, rng, keep)
	}
	area := Circle{Center: arena.Center, Radius: arena.Radius * dropEdgeFraction}
	inner, spacing := BotRingMinM*UnitsPerMeter, BotRingSpacingM*UnitsPerMeter
	outer := make([]float64, len(anchors))
	for i := range outer {
		outer[i] = BotRingMaxM * UnitsPerMeter
	}
	points := make([]Point, 0, max(count, 0))
	for i := range count {
		a := i % len(anchors)
		anchor := anchors[a]
		var best Point
		bestGap, bestKept := -1.0, false
		for growth := 0; ; growth++ {
			placed := false
			for range dropAttempts {
				angle := rng.Float64() * 2 * math.Pi
				radius := math.Sqrt(inner*inner + rng.Float64()*(outer[a]*outer[a]-inner*inner))
				candidate := Point{X: anchor.X + radius*math.Cos(angle), Z: anchor.Z + radius*math.Sin(angle)}
				kept := !Outside(area, candidate) && nearestDistance(candidate, anchors) >= inner && (keep == nil || keep(candidate))
				gap := nearestDistance(candidate, points)
				if (kept && !bestKept) || (kept == bestKept && gap > bestGap) {
					best, bestGap, bestKept = candidate, gap, kept
				}
				if kept && gap >= spacing {
					placed = true
					break
				}
			}
			if placed || growth == botRingGrowths {
				break
			}
			outer[a] += botRingGrowM * UnitsPerMeter
		}
		points = append(points, best)
	}
	return points
}

/*
================
BotRingReach

The farthest a bot of BotRingPoints may land from its anchor: the ring
fully grown.
================
*/
func BotRingReach() float64 {
	return (BotRingMaxM + botRingGrowM*botRingGrowths) * UnitsPerMeter
}
