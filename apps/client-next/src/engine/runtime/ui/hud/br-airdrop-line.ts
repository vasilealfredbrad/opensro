/*
===========================================================================

br-airdrop-line.ts - the Battle Royale airdrop's flight line on the maps

Port-only, not native (docs/battle-royale.md, Stage 9 item 5). For the
minute before an airdrop lands, every participant sees its flight path on
the minimap and the world map: a dashed gold line across the next circle,
clipped to the map, with a small arrow (the plane) moving along it from
its start toward its end. At landing the line goes and br-hud.ts draws the
landed mark instead.

The maps project world grid units the same way for the rings, the bots and
the mark: an origin ellipse (br-hud.ts minimapEllipse or world-map.ts
worldMapEllipse of the unit circle) whose rx and ry are pixels per grid
unit, y growing south. This module takes that origin as plain numbers so
it stays apart from br-hud.ts, which is full. ui.ts owns it and draws what
it returns.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import type {
	BattleRoyaleAirdropLine,
	BattleRoyaleIncomingAirdrop
} from "@/engine/foundation/gameplay/battle-royale-airdrop";

type Rgba = readonly [number, number, number, number];

// A bright gold, lighter than the landed mark's so the two never read as
// one another.
export const AIRDROP_LINE_RGBA: Rgba = [ 1, .92, .3, .95 ];
export const AIRDROP_LINE_PX = 2;
// The dash pattern; a very long line on the world map is drawn with longer
// dashes rather than more of them.
export const AIRDROP_DASH_PX = 6;
export const AIRDROP_GAP_PX = 4;
export const AIRDROP_MAX_DASHES = 160;
// The plane's arrow on the minimap and on the world map.
export const AIRDROP_PLANE_PX = 16;
export const AIRDROP_PLANE_MAP_PX = 20;
const AIRDROP_PLANE_RGBA: Rgba = [ 1, .95, .55, 1 ];
// The server shows the line this long before the landing (Stage 9 item 5:
// "60 s before each airdrop lands"); the plane crosses the whole line in
// that time, so one announced late (a reconnect) starts part-way along.
export const AIRDROP_FLIGHT_MS = 60000;

/*
================
MapOrigin

A map's projection of the world grid: grid (0, 0) at (x, y) pixels, rx and
ry pixels per grid unit, y growing south.
================
*/
export interface MapOrigin {
	readonly x: number;
	readonly y: number;
	readonly rx: number;
	readonly ry: number;
}

/*
================
ScreenSegment
================
*/
export interface ScreenSegment {
	readonly x0: number;
	readonly y0: number;
	readonly x1: number;
	readonly y1: number;
}

/*
================
AirdropPlane

The plane's arrow: its centre and the rotation that points the minimap's
arrow art along the flight (minimapEdge's convention).
================
*/
export interface AirdropPlane {
	readonly x: number;
	readonly y: number;
	readonly rotation: number;
}

/*
================
clipSegment

Liang-Barsky: the part of (x0, y0)-(x1, y1) inside clip, or null when none
of it is.
================
*/
export function clipSegment( x0: number, y0: number, x1: number, y1: number, clip: UiRect ): ScreenSegment | null {
	const dx = x1 - x0, dy = y1 - y0;
	const edges = [
		[ -dx, x0 - clip[0] ],
		[ dx, clip[0] + clip[2] - x0 ],
		[ -dy, y0 - clip[1] ],
		[ dy, clip[1] + clip[3] - y0 ]
	] as const;
	let enter = 0, leave = 1;
	for ( const [p, q] of edges ) {
		if ( p === 0 ) {
			if ( q < 0 ) return null;
			continue;
		}
		const t = q / p;
		if ( p < 0 ) enter = Math.max( enter, t );
		else leave = Math.min( leave, t );
		if ( enter > leave ) return null;
	}
	return { x0: x0 + enter * dx, y0: y0 + enter * dy, x1: x0 + leave * dx, y1: y0 + leave * dy };
}

/*
================
airdropLineSegment

The flight line projected through origin and clipped to clip; null when it
misses the map.
================
*/
export function airdropLineSegment(
	origin: MapOrigin,
	line: BattleRoyaleAirdropLine,
	clip: UiRect
): ScreenSegment | null {
	return clipSegment(
		origin.x + line.ax * origin.rx,
		origin.y - line.az * origin.ry,
		origin.x + line.bx * origin.rx,
		origin.y - line.bz * origin.ry,
		clip
	);
}

/*
================
airdropLineQuads

A segment as gold dashes (texture "" is white), each a thin rotated quad
clipped like any HUD quad; at most AIRDROP_MAX_DASHES, so the dashes grow
on a line longer than the pattern allows.
================
*/
export function airdropLineQuads( segment: ScreenSegment, clip: UiRect, mask?: UiQuad["mask"] ): UiQuad[] {
	const dx = segment.x1 - segment.x0, dy = segment.y1 - segment.y0, length = Math.hypot( dx, dy );
	if ( !(length > 0) ) return [];
	const pitch = Math.max( AIRDROP_DASH_PX + AIRDROP_GAP_PX, length / AIRDROP_MAX_DASHES ),
		dash = pitch * AIRDROP_DASH_PX / (AIRDROP_DASH_PX + AIRDROP_GAP_PX),
		ux = dx / length,
		uy = dy / length,
		rotation = Math.atan2( dy, dx );
	const quads: UiQuad[] = [];
	for ( let start = 0; start < length; start += pitch ) {
		const size = Math.min( dash, length - start ), mid = start + size / 2;
		const cx = segment.x0 + ux * mid, cy = segment.y0 + uy * mid;
		quads.push( {
			rect: [ cx - size / 2, cy - AIRDROP_LINE_PX / 2, size, AIRDROP_LINE_PX ],
			uv: [ 0, 0, 1, 1 ],
			texture: "",
			color: AIRDROP_LINE_RGBA,
			clip,
			rotation,
			...(mask ? { mask } : {})
		} );
	}
	return quads;
}

/*
================
airdropPlane

Where the plane is at simulationMs: along the projected line from its start
toward its end, reaching the end at the landing. Null on a line of no
length or with the plane outside clip.
================
*/
export function airdropPlane(
	origin: MapOrigin,
	airdrop: BattleRoyaleIncomingAirdrop,
	simulationMs: number,
	clip: UiRect
): AirdropPlane | null {
	const line = airdrop.line;
	const ax = origin.x + line.ax * origin.rx, ay = origin.y - line.az * origin.ry;
	const dx = (line.bx - line.ax) * origin.rx, dy = -(line.bz - line.az) * origin.ry;
	if ( !(Math.hypot( dx, dy ) > 0) ) return null;
	const t = Math.min( 1, Math.max( 0, 1 - (airdrop.landsAtMs - simulationMs) / AIRDROP_FLIGHT_MS ) );
	const x = ax + dx * t, y = ay + dy * t;
	if ( x < clip[0] || x > clip[0] + clip[2] || y < clip[1] || y > clip[1] + clip[3] ) return null;
	return { x, y, rotation: -(Math.atan2( dy, dx ) + Math.PI) };
}

/*
================
airdropPlaneQuad

The plane as the arrow art of size pixels, tinted; the caller demands the
texture and draws it only once loaded.
================
*/
export function airdropPlaneQuad(
	plane: AirdropPlane,
	size: number,
	texture: string,
	clip: UiRect,
	mask?: UiQuad["mask"]
): UiQuad {
	return {
		rect: [ plane.x - size / 2, plane.y - size / 2, size, size ],
		uv: [ 0, 0, 1, 1 ],
		texture,
		color: AIRDROP_PLANE_RGBA,
		clip,
		rotation: plane.rotation,
		...(mask ? { mask } : {})
	};
}
