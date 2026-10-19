/*
===========================================================================

places_licensed_test.go - the lobby and arena stands against the navmesh

Port-only, not native. Re-checks places.go against the shipped world
authority: the Hotan lobby disc is walkable and flat and clear of the
storage keeper, and the all-Roc arena (stage 4) keeps its drops, monsters
and zone centres on the main walkable component of Roc. Skips without
the licensed data.

===========================================================================
*/
package battleroyale_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"opensro.online/server/internal/game/battleroyale"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	// The storage keeper's stand (docs/battle-royale.md, stage 2 item 2).
	keeperX, keeperZ = 1134.28, 610.79
	// lobbyKeeperMinDistance keeps the circle off the NPC: 25 m to the
	// centre, 10 m from the edge.
	lobbyKeeperMinDistance = 250.0
	lobbyKeeperMaxDistance = 300.0
	// lobbyMaxHeightSpread is the flatness bound of the lobby disc (1 m).
	lobbyMaxHeightSpread = 10.0
	sampleStep           = 50.0
)

/*
================
TestLobbyStandsOnFlatHotanGround
================
*/
func TestLobbyStandsOnFlatHotanGround(t *testing.T) {
	v := movement.NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	if !v.SpawnRegionAvailable(battleroyale.LobbyRegion) {
		t.Fatalf("lobby region %04x unavailable", battleroyale.LobbyRegion)
	}
	dx, dz := battleroyale.LobbyLocalX-keeperX, battleroyale.LobbyLocalZ-keeperZ
	if d := math.Hypot(dx, dz); d < lobbyKeeperMinDistance || d > lobbyKeeperMaxDistance {
		t.Fatalf("lobby centre %.1f units from the keeper", d)
	}
	radius := battleroyale.LobbyRadiusM * battleroyale.UnitsPerMeter
	minY, maxY := math.Inf(1), math.Inf(-1)
	for x := -radius; x <= radius; x += sampleStep / 5 {
		for z := -radius; z <= radius; z += sampleStep / 5 {
			if x*x+z*z > radius*radius {
				continue
			}
			y, ok := v.WalkableTerrainHeightAt(battleroyale.LobbyRegion, battleroyale.LobbyLocalX+x, battleroyale.LobbyLocalZ+z)
			if !ok {
				t.Fatalf("lobby tile %.0f/%.0f not walkable", battleroyale.LobbyLocalX+x, battleroyale.LobbyLocalZ+z)
			}
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	if maxY-minY > lobbyMaxHeightSpread {
		t.Fatalf("lobby disc height spread %.1f units", maxY-minY)
	}
}

/*
================
rocWalkable

The navmesh test the wiring asks (battleroyale_match.go battleRoyaleGround
uses the same walkable-terrain tiles): the point's region is available and
its tile walkable.
================
*/
func rocWalkable(v *movement.WaterValidator) func(battleroyale.Point) bool {
	return func(p battleroyale.Point) bool {
		region := worldgeom.RegionIDForSectors(int(p.X/worldgeom.OutdoorRegionSize), int(p.Z/worldgeom.OutdoorRegionSize))
		local := worldgeom.LocalFromGrid(region, p)
		if !v.SpawnRegionAvailable(local.RegionID) {
			return false
		}
		_, ok := v.WalkableTerrainHeightAt(local.RegionID, local.X, local.Z)
		return ok
	}
}

/*
================
TestArenaCoversWalkableRoc

Stage 4: the arena circle covers every Roc region and the ground mask
keeps the main walkable component of Roc, about 5 km2 (at least 90 % of
the walkable Roc cells). On it, for many seeds: every zone target centre
of the six-phase schedule is walkable ground of the mask (the filter
found one, it never fell back), every drop point is, and nearly every
monster point is walkable at its exact spot (the wiring redraws the
rest).
================
*/
func TestArenaCoversWalkableRoc(t *testing.T) {
	v := movement.NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	walkable := rocWalkable(v)
	mask := battleroyale.BuildGroundMask(walkable)
	all := battleroyale.BuildGroundMask(nil)
	arena := battleroyale.ArenaCircle()
	walkableRoc := 0
	minX, minZ, maxX, maxZ := battleroyale.RocBounds()
	for x := minX + battleroyale.GroundMaskStep/2; x < maxX; x += battleroyale.GroundMaskStep {
		for z := minZ + battleroyale.GroundMaskStep/2; z < maxZ; z += battleroyale.GroundMaskStep {
			p := battleroyale.Point{X: x, Z: z}
			if all.Contains(p) && walkable(p) {
				walkableRoc++
			}
			if all.Contains(p) && battleroyale.Outside(arena, p) {
				t.Fatalf("Roc point %+v outside the arena", p)
			}
		}
	}
	area, cell := mask.Area(), math.Pow(battleroyale.GroundMaskStep/battleroyale.UnitsPerMeter, 2)
	if area < 4.5e6 || area > 5.5e6 || area < 0.9*float64(walkableRoc)*cell {
		t.Fatalf("main component %.2f km2 of %.2f km2 walkable Roc", area/1e6, float64(walkableRoc)*cell/1e6)
	}
	keep := func(p battleroyale.Point) bool { return mask.Contains(p) && walkable(p) }
	ground := mask.Within(arena)
	for seed := uint64(1); seed <= 40; seed++ {
		schedule := battleroyale.ScheduleFor(int(seed%16)+1, arena.Radius)
		zone := battleroyale.PlanZoneFiltered(schedule, arena, 0, rand.New(rand.NewPCG(seed, 1)), keep)
		for i, target := range zone.Targets {
			if !keep(target.Center) {
				t.Fatalf("seed %d: target %d centre %+v off the walkable ground", seed, i, target.Center)
			}
		}
		for i, p := range battleroyale.DropPointsFiltered(arena, 16, 500, rand.New(rand.NewPCG(seed, 2)), keep) {
			if !keep(p) {
				t.Fatalf("seed %d: drop %d %+v off the walkable ground", seed, i, p)
			}
		}
	}
	plan := battleroyale.MonsterPlan(ground, rand.New(rand.NewPCG(3, 4)))
	good := 0
	for _, spawn := range plan {
		if keep(spawn.At) {
			good++
		}
	}
	if len(plan) != battleroyale.ArenaMonsters || good < len(plan)*85/100 {
		t.Fatalf("%d of %d monster points walkable", good, len(plan))
	}
	t.Logf("main component %.2f km2, walkable Roc %.2f km2, %d of %d monster points walkable", area/1e6,
		float64(walkableRoc)*cell/1e6, good, len(plan))
}
