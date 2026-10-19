/*
===========================================================================

ground-ring.ts - a circle the world renderer draws on the terrain

Port-only, not native: the Battle Royale lobby and zone circles
(docs/battle-royale.md). The character presentation owner publishes them
(ground-rings.ts); the world renderer lays each one on the terrain as a
strip, or as a standing fog wall (foundation/rendering/ground-ring.ts).

===========================================================================
*/

/*
================
GroundRing

A circle in world grid units (sectorX * 1920 + local x, likewise z), its
colour and, optionally, its strip width in world units (default
GROUND_RING_WIDTH). A ring with a wall height (world units over the
terrain) is the zone's fog wall instead: a translucent curtain on the
circle, the colour's alpha its density at the ground.
================
*/
export interface GroundRing {
	readonly x: number;
	readonly z: number;
	readonly radius: number;
	readonly color: readonly [number, number, number, number];
	readonly width?: number;
	readonly wall?: number;
}
