/*
===========================================================================

ground-rings.ts - the Battle Royale circles laid on the ground

Port-only, not native (docs/battle-royale.md). The footprint owner, the
character presentation's other ground decal, hands this module each
frame's match state; it publishes the lobby circle (to everyone but an
alive participant, who is in the arena) and the current zone circle of a
running match to the world renderer, which lays them on the terrain
(foundation/rendering/ground-ring.ts). The zone is a thin red line on the
ground and a fog wall standing on it, seen from inside and outside. A
circle is published again only when it has moved by RING_STEP_UNITS, so a
shrinking zone rewrites its strip and wall a few times a second, not
every frame. It also tells the renderer how far the scene's fog closes in
while the local player stands outside the zone, in ZONE_FOG_STEPS steps.
Nothing is published without a match (the server flag off is native).

The zone shrinks on the simulation clock; this owner steps on the frame
clock and follows the simulation from the frame a match frame was first
seen, within a frame's publication delay: far below the re-lay step.

===========================================================================
*/
import type { Renderer } from "@/engine/contracts/runtime";
import type { Pose } from "@/engine/contracts/gameplay";
import type { GroundRing } from "@/engine/contracts/ground-ring";
import {
	battleRoyaleCircleDepth,
	type BattleRoyaleCircle,
	type BattleRoyaleState,
	battleRoyaleZone,
	battleRoyaleZoneFog
} from "@/engine/foundation/gameplay/battle-royale";

// How far a circle moves (centre or radius, world units) before its strip
// is laid again.
const RING_STEP_UNITS = 5;
// The lobby and the current zone are red on the ground, as on the maps.
const RING_COLOR: GroundRing["color"] = [ 1, .18, .12, .75 ];
// The zone's edge line, thinner than the lobby's (world units).
const ZONE_RING_WIDTH = 8;
// The zone's fog wall: its height over the terrain (80 m) and its
// bluish grey, the alpha its density at the ground.
const FOG_WALL_HEIGHT = 800;
const FOG_WALL_COLOR: GroundRing["color"] = [ .78, .81, .86, .7 ];
// The scene fog's strength is published in these steps, so a walking
// player re-publishes it only every few metres.
const ZONE_FOG_STEPS = 20;
// The lobby is a target of three red rings: a wide bright edge and two
// fainter inner rings (radius fraction, strip width in world units, alpha
// while waiting and during a countdown).
const LOBBY_RINGS: readonly (readonly [number, number, number, number])[] = [
	[ 1, 30, .9, 1 ],
	[ .62, 12, .5, .8 ],
	[ .25, 10, .4, .7 ]
];
const MS_PER_SECOND = 1000;

/*
================
moved

Whether a published ring no longer stands for a circle (or the circle
appeared or went away).
================
*/
function moved( published: GroundRing | null, circle: BattleRoyaleCircle | null ): boolean {
	if ( !published || !circle ) return published !== circle;
	return Math.abs( published.x - circle.x ) >= RING_STEP_UNITS ||
		Math.abs( published.z - circle.z ) >= RING_STEP_UNITS ||
		Math.abs( published.radius - circle.radius ) >= RING_STEP_UNITS;
}

/*
================
createGroundRings
================
*/
export function createGroundRings( renderer: Pick<Renderer, "setGroundRings" | "setZoneFog"> ) {
	let lobby: GroundRing | null = null, zone: GroundRing | null = null, wall: GroundRing | null = null;
	// The scene fog strength last published (ZONE_FOG_STEPS steps).
	let fog = 0;
	// The lobby target's rings and whether they were laid for a countdown.
	let lobbyRings: readonly GroundRing[] = [], lobbyHot = false;
	// The match frame last seen and the frame clock when it was.
	let seenSequence = -1, seenAtSeconds = 0;
	/*
	================
	ring
	================
	*/
	const ring = ( circle: BattleRoyaleCircle | null ): GroundRing | null =>
		circle ? { x: circle.x, z: circle.z, radius: circle.radius, color: RING_COLOR } : null;
	/*
	================
	publish
	================
	*/
	const publish = () => {
		const rings: GroundRing[] = [ ...lobbyRings ];
		if ( zone ) rings.push( zone );
		if ( wall ) rings.push( wall );
		renderer.setGroundRings?.( rings );
	};
	/*
	================
	publishFog
	================
	*/
	const publishFog = ( strength: number ) => {
		const stepped = Math.round( strength * ZONE_FOG_STEPS ) / ZONE_FOG_STEPS;
		if ( stepped === fog ) return;
		fog = stepped;
		renderer.setZoneFog?.( fog );
	};
	return {
		/*
		================
		step

		The circles of the match at the frame clock's seconds, published when
		one moved, and the scene fog for the local pose.
		================
		*/
		step(
			state: BattleRoyaleState | undefined,
			seconds: number,
			lobbyCircle?: BattleRoyaleCircle,
			pose?: Pick<Pose, "regionId" | "x" | "z"> | null
		) {
			if ( state && state.sequence !== seenSequence ) {
				seenSequence = state.sequence;
				seenAtSeconds = seconds;
			}
			const simulationMs = state ? state.receivedAtMs + (seconds - seenAtSeconds) * MS_PER_SECOND : 0;
			// Everyone but an alive participant (who is in the arena) sees the
			// lobby: from the match frame, else from the world-entry bootstrap.
			const shown = state?.role === "alive" ?
				null :
				state && state.lobby.radius > 0 ?
				state.lobby :
				lobbyCircle ?? null;
			const hot = state?.phase === "countdown";
			const zoneCircle = state?.phase === "running" ? battleRoyaleZone( state, simulationMs ) : null;
			publishFog(
				zoneCircle && state?.role === "alive" && pose ?
					battleRoyaleZoneFog( battleRoyaleCircleDepth( zoneCircle, pose ) ) :
					0
			);
			const lobbyMoved = moved( lobby, shown ) || hot !== lobbyHot, zoneMoved = moved( zone, zoneCircle );
			if ( !lobbyMoved && !zoneMoved ) return;
			if ( lobbyMoved ) {
				lobby = ring( shown );
				lobbyHot = hot;
				lobbyRings = shown ?
					LOBBY_RINGS.map( ( [fraction, width, idle, countdown] ) => ({
						x: shown.x,
						z: shown.z,
						radius: shown.radius * fraction,
						width,
						color: [ 1, hot ? .25 : .12, hot ? .1 : .08, hot ? countdown : idle ] as const
					}) ) :
					[];
			}
			if ( zoneMoved ) {
				zone = zoneCircle ? { ...zoneCircle, color: RING_COLOR, width: ZONE_RING_WIDTH } : null;
				wall = zoneCircle ? { ...zoneCircle, color: FOG_WALL_COLOR, wall: FOG_WALL_HEIGHT } : null;
			}
			publish();
		},
		/*
		================
		clear

		Teardown: no circles stay on the ground.
		================
		*/
		clear() {
			publishFog( 0 );
			if ( !lobby && !zone ) return;
			lobby = zone = wall = null;
			lobbyRings = [];
			publish();
		}
	};
}
