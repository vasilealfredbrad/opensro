/*
===========================================================================

runtime_placement.go - the character's live stand

The action runtime's reads of where a character is right now (the live
position plane, never the persisted goal), the owner-aware walk
constraints every server-driven move goes through, and the one write
back of an accepted stand onto the record.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
writeBackWorld

writeBackWorld persists the accepted ground pose onto the character record.
Authored displacement state keeps its existing destination semantics; the
ordinary ground mover never persists a destination it has not reached.

The write-back owns ONLY spawn/movementMode/spawnSet; every other world
field (dungeonMinimap, movementSourceSeeded, updatedAt, moveSegment echo,
and any future record keys) copies through untouched, like the Node
handlers' {...world} spread (item-lane mirror of SCOUT-B FINDING 500).
Copy-then-swap so aliases of the old record (the character snapshot's
shallow copy) stay unchanged.
==================
*/
func writeBackWorld(character *enterworld.Character, state simulation.WorldState) {
	spawn := state.PersistedSpawn()
	regionID := int64(spawn.RegionID)
	x, y, z := spawn.X, spawn.Y, spawn.Z
	angle := int64(spawn.Angle)
	mode := int64(state.MovementMode)
	next := enterworld.CharacterWorld{}
	if character.World != nil {
		next = *character.World
	}

	next.Spawn = &enterworld.WorldSpawn{
		RegionID: &regionID,
		X:        &x,
		Y:        &y,
		Z:        &z,
		Angle:    &angle,
	}

	next.MovementMode = &mode
	next.SpawnSet = state.SpawnSet
	character.World = &next
}

/*
==================
liveSpawn

liveSpawn samples the character's LIVE position - the bug-D plane. Every
position-dependent op reads THIS, never the goal in WorldState.Spawn.
==================
*/
func (rt *Runtime) liveSpawn(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) simulation.Spawn {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs)
}

/*
==================
LiveSpawnFor

The character's live position now, for lanes outside action that gate on
native range (the party invite's CGObjChar_CheckHitRange).
==================
*/
func (rt *Runtime) LiveSpawnFor(divisionID string, character *enterworld.Character) simulation.Spawn {
	return rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
}

/*
==================
liveNav

liveNav is liveSpawn plus the surface owner retained at that point.
==================
*/
func (rt *Runtime) liveNav(
	worldKey string,
	character *enterworld.Character,
	nowMs int64,
) (simulation.Spawn, simulation.NavOwner) {
	snapshot := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})

	return snapshot.LiveSpawnAt(nowMs), snapshot.LiveOwnerAt(nowMs)
}

/*
==================
constrainWalk

constrainWalk routes a character move through the owner-aware constraint,
degrading to the owner-less one only in compositions that lack it.
==================
*/
func (rt *Runtime) constrainWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.ConstrainWalk != nil {
		return rt.ConstrainWalk(characterName, from, fromOwner, to)
	}

	if rt.ConstrainMovement != nil {
		committed, refusal := rt.ConstrainMovement(characterName, from, to)
		return committed, simulation.NavWalk{}, refusal
	}

	return to, simulation.NavWalk{}, nil
}

/*
==================
admitGroundWalk

Player approaches retain their intended goal while the world owner checks
each elapsed movement step. A skill displacement still resolves its whole
authored chord through constrainWalk before it commits.
==================
*/
func (rt *Runtime) admitGroundWalk(
	characterName string,
	from simulation.Spawn,
	fromOwner simulation.NavOwner,
	to simulation.Spawn,
) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if rt.AdmitGroundWalk != nil {
		return rt.AdmitGroundWalk(characterName, from, fromOwner, to)
	}
	return rt.constrainWalk(characterName, from, fromOwner, to)
}

/*
==================
samePlacement

samePlacement compares where a move lands. Walked goals stand on their
surface's height, so the requested int16 Y is not part of the comparison.
==================
*/
func samePlacement(a, b simulation.Spawn) bool {
	return a.RegionID == b.RegionID && a.X == b.X && a.Z == b.Z
}
