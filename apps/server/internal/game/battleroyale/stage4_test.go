/*
===========================================================================

stage4_test.go - the stage-4 rules of the core

Port-only, not native. The ground mask keeps only the largest walkable
component of Roc and draws inside a circle from it; filtered drop points
stand on accepted ground and keep their spacing (docs/battle-royale.md,
"Server notes (stage 4)").

===========================================================================
*/
package battleroyale

import (
	"math"
	"math/rand/v2"
	"testing"
)

/*
================
TestGroundMaskKeepsTheLargestComponent

Walkable ground made of a big western block and a small eastern island
in the northern strip apart from it: the mask keeps the block only,
Contains and Within agree with it, and a draw inside a circle lands on
kept cells inside the circle.
================
*/
func TestGroundMaskKeepsTheLargestComponent(t *testing.T) {
	minX, minZ, _, _ := RocBounds()
	block := func(p Point) bool { return p.X < minX+10_000 && p.Z < minZ+20_000 }
	island := func(p Point) bool { return p.X > minX+20_000 && p.X < minX+22_000 && p.Z < minZ+2_000 }
	mask := BuildGroundMask(func(p Point) bool { return block(p) || island(p) })
	if !mask.Contains(Point{X: minX + 5_000, Z: minZ + 5_000}) || mask.Contains(Point{X: minX + 21_000, Z: minZ + 1_000}) {
		t.Fatal("the mask kept the island or lost the block")
	}
	if mask.Contains(Point{X: minX - 50, Z: minZ + 5_000}) || mask.Contains(Point{X: minX + 15_000, Z: minZ + 5_000}) {
		t.Fatal("the mask holds ground outside the block")
	}
	// The block is 104 cell columns by 208 rows, less the 20 rows of the
	// northern strip (y 84), which is Roc only at x 113-117.
	cell := GroundMaskStep * GroundMaskStep / (UnitsPerMeter * UnitsPerMeter)
	if want := 104 * (208 - 20) * cell; mask.Area() != want {
		t.Fatalf("area %.0f m2, want %.0f", mask.Area(), want)
	}
	circle := Circle{Center: Point{X: minX + 9_000, Z: minZ + 3_000}, Radius: 3_000}
	ground := mask.Within(circle)
	if ground.Len() == 0 {
		t.Fatal("no ground inside the circle")
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		p, ok := ground.Sample(rng)
		if !ok || !mask.Contains(p) || Outside(Circle{Center: circle.Center, Radius: circle.Radius + GroundMaskStep}, p) {
			t.Fatalf("sample %+v outside the kept ground of the circle", p)
		}
	}
	if _, ok := (GroundCells{}).Sample(rng); ok || mask.Within(Circle{Center: Point{X: minX + 21_000, Z: minZ + 1_000}, Radius: 500}).Len() != 0 {
		t.Fatal("empty ground produced a point")
	}
	var none *GroundMask
	if none.Contains(circle.Center) || none.Area() != 0 || none.Within(circle).Len() != 0 {
		t.Fatal("a nil mask holds ground")
	}
}

/*
================
TestDropPointsFilteredStandOnAcceptedGround

Under a veto of the western half every drop point is in the eastern half
and the spacing holds; a nil filter replays DropPoints exactly.
================
*/
func TestDropPointsFilteredStandOnAcceptedGround(t *testing.T) {
	arena := ArenaCircle()
	east := func(p Point) bool { return p.X >= arena.Center.X }
	points := DropPointsFiltered(arena, 16, 3_000, rand.New(rand.NewPCG(4, 5)), east)
	for i, p := range points {
		if !east(p) || Outside(arena, p) {
			t.Fatalf("drop %d at %+v", i, p)
		}
		for j := range i {
			if math.Hypot(p.X-points[j].X, p.Z-points[j].Z) < 3_000 {
				t.Fatalf("drops %d and %d too close", i, j)
			}
		}
	}
	plain := DropPoints(arena, 8, 3_000, rand.New(rand.NewPCG(4, 5)))
	filtered := DropPointsFiltered(arena, 8, 3_000, rand.New(rand.NewPCG(4, 5)), nil)
	for i := range plain {
		if plain[i] != filtered[i] {
			t.Fatalf("nil filter changed drop %d", i)
		}
	}
}
