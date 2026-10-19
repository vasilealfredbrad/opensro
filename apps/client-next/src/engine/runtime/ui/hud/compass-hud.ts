/*
===========================================================================

compass-hud.ts - the compass strip at the top of the screen

Port-only, not native (docs/battle-royale.md, Stage 9 item 3). A
horizontal strip at the top centre shows which way the player looks: the
eight directions (N, NE, E, SE, S, SW, W, NW) with a tick every 15
degrees, scrolling as the camera turns, and under it the bearing and the
player's position in the game's own X/Y metres (the minimap's numbers,
hud-readouts.ts minimapCoordinates). It shows in Battle Royale lobbies and
matches; the Experimental row "Compass everywhere" shows it in the whole
world.

The bearing follows the camera, so the strip matches the view: the orbit
camera sits at (sin yaw, cos yaw) from the player on the world's (x, z)
(world.ts updateCamera) and looks back at it, so it faces yaw + pi, with
+z north and +x east as on the minimap. Without a camera yaw the
character's heading stands in.

This owner keeps what the HUD remembers between frames: the bearing tick
last drawn (a still world redraws only when the strip would move) and the
lowest HUD line above the strip this frame. ui.ts draws what the pure
functions here lay out.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { headingRadians } from "@/engine/foundation/math/angles";
import { minimapCoordinates } from "@/engine/foundation/ui/hud-readouts";

type Rgba = readonly [number, number, number, number];

// The strip: its size, how many pixels a degree takes (180 degrees across)
// and the tick pitch, with a label every LABEL_DEGREES.
export const COMPASS_WIDTH = 360;
export const COMPASS_HEIGHT = 22;
export const COMPASS_PX_PER_DEGREE = 2;
export const COMPASS_TICK_DEGREES = 15;
const LABEL_DEGREES = 45;
export const COMPASS_MAJOR_TICK_PX = 7;
export const COMPASS_MINOR_TICK_PX = 4;
export const COMPASS_TICK_WIDTH_PX = 1;
// The gap above the strip and between it and the bearing line.
export const COMPASS_MARGIN_PX = 4;
const COMPASS_LABELS = [ "N", "NE", "E", "SE", "S", "SW", "W", "NW" ] as const;
const DEGREES = 360;
export const COMPASS_BACKING_RGBA: Rgba = [ 0, 0, 0, .55 ];
export const COMPASS_TICK_RGBA: Rgba = [ 1, 1, 1, .85 ];
export const COMPASS_NORTH_RGBA: Rgba = [ 1, .35, .25, 1 ];
export const COMPASS_CENTRE_RGBA: Rgba = [ 1, .85, .35, 1 ];

/*
================
CompassMark

A tick on the strip: its offset from the strip's left edge in pixels, its
bearing in degrees [0, 360) and its label ("" for a minor tick).
================
*/
export interface CompassMark {
	readonly offset: number;
	readonly degrees: number;
	readonly label: string;
}

/*
================
normalizeDegrees
================
*/
function normalizeDegrees( degrees: number ): number {
	const wrapped = degrees % DEGREES;
	return wrapped < 0 ? wrapped + DEGREES : wrapped;
}

/*
================
compassBearing

The direction the player looks, in degrees clockwise from north [0, 360):
the camera's when there is one (cameraYaw, input.ts), else the character's
heading (the wire angle, counter-clockwise from east).
================
*/
export function compassBearing( cameraYaw: number | undefined, heading: number | undefined ): number {
	if ( cameraYaw !== undefined && Number.isFinite( cameraYaw ) ) {
		return normalizeDegrees( (cameraYaw + Math.PI) * 180 / Math.PI );
	}
	if ( heading === undefined ) return 0;
	return normalizeDegrees( 90 - headingRadians( heading ) * 180 / Math.PI );
}

/*
================
compassLabel

The nearest of the eight directions to a bearing.
================
*/
export function compassLabel( bearing: number ): string {
	return COMPASS_LABELS[Math.round( normalizeDegrees( bearing ) / LABEL_DEGREES ) % COMPASS_LABELS.length]!;
}

/*
================
compassMarks

The ticks a strip width pixels wide shows around bearing, left to right:
one every COMPASS_TICK_DEGREES, the eight directions labelled, the bearing
at the strip's centre.
================
*/
export function compassMarks( bearing: number, width: number, pxPerDegree: number ): CompassMark[] {
	const half = width / 2 / pxPerDegree, marks: CompassMark[] = [];
	const first = Math.ceil( (bearing - half) / COMPASS_TICK_DEGREES ) * COMPASS_TICK_DEGREES;
	for ( let degrees = first; degrees <= bearing + half; degrees += COMPASS_TICK_DEGREES ) {
		const wrapped = normalizeDegrees( degrees );
		marks.push( {
			offset: width / 2 + (degrees - bearing) * pxPerDegree,
			degrees: wrapped,
			label: wrapped % LABEL_DEGREES === 0 ? COMPASS_LABELS[wrapped / LABEL_DEGREES]! : ""
		} );
	}
	return marks;
}

/*
================
compassReadout

The line under the strip: the nearest direction and the whole bearing,
then the position as the minimap shows it ("NW 312  X:1234  Y:567"); a
dungeon, off the world grid, shows the bearing alone.
================
*/
export function compassReadout( bearing: number, pose: Pick<Pose, "regionId" | "x" | "z"> | undefined ): string {
	const whole = Math.round( bearing ) % DEGREES, heading = compassLabel( bearing ) + " " + whole;
	if ( !pose ) return heading;
	const [x, y] = minimapCoordinates( pose );
	return x ? heading + "  " + x.replace( /: +/, ":" ) + "  " + y.replace( /: +/, ":" ) : heading;
}

/*
================
compassShown

The strip shows in the world to a Battle Royale lobby member or match
participant (the server sends the match state to them alone), and to
everyone when the Experimental "Compass everywhere" row is on.
================
*/
export function compassShown( inWorld: boolean, battleRoyale: boolean, everywhere: boolean ): boolean {
	return inWorld && (battleRoyale || everywhere);
}

/*
================
createCompassHud
================
*/
export function createCompassHud() {
	// The whole bearing last drawn (-1: hidden), and the lowest HUD line
	// above the strip this frame.
	let tick = -1, above = 0;
	return {
		/*
		================
		redraw

		True when a still world must be drawn again: the strip appeared or
		went, or the bearing turned a whole degree.
		================
		*/
		redraw( shown: boolean, bearing: number ): boolean {
			const next = shown ? Math.round( bearing ) % DEGREES : -1, changed = next !== tick;
			tick = next;
			return changed;
		},
		/*
		================
		below

		A HUD line drawn before the strip ends at bottom; the strip goes under
		the lowest one.
		================
		*/
		below( bottom: number ) {
			above = Math.max( above, bottom );
		},
		/*
		================
		top

		Where the strip goes this frame, at least floor: under the lines
		below() reported. Each frame starts again.
		================
		*/
		top( floor: number ): number {
			const value = Math.max( above, floor );
			above = 0;
			return value;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			tick = -1;
			above = 0;
		}
	};
}
