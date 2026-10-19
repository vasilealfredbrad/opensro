/*
===========================================================================

ground-ring.ts - a ring laid on the terrain, and the zone's fog wall

Port-only, not native: the Battle Royale lobby and zone circles drawn on
the ground (contracts/ground-ring.ts), and the fog wall standing on the
zone's edge (writeFogWall). A ring is a strip of
GROUND_RING_SEGMENTS quads around its circle; every vertex stands on the
terrain height under it, read from the resident terrain cells through the
terrain's own triangle split (terrain-interaction.ts tile), and is lifted a
little so the strip clears the ground between vertices. The topology never
changes, so a moving circle rewrites the positions in place
(writeGroundRing) and the indices are uploaded once.

A zone starts as all of Roc, about 2000 m in radius, far past the
resident terrain: the strip is cut into GROUND_RING_SEGMENTS quads (a pair
about every 3 m even then), and the part of the ring over no resident
terrain collapses to nothing instead of floating at a borrowed height over
land it cannot see.

The fog wall is a translucent curtain on the same circle: columns of
FOG_WALL_ROWS vertices from a little under the terrain to the wall's
height, densest at the ground and clear at the top. Its colours (the
vertical gradient and a fixed ragged variation around the circle) depend
only on the topology, so they are written once at upload; a moved circle
rewrites the positions in place, as the strip's.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import { identity } from "./world-math";
import { terrainCellKey, type TerrainCells } from "./terrain-interaction";

// Quads around a ring: a 900 m zone gets a vertex pair every 1.4 m, under
// the 2 m terrain tile, so the lifted strip does not dip into the ground
// between pairs (1024 left 5.5 m at 900 m); a 2000 m zone, one every 3 m.
export const GROUND_RING_SEGMENTS = 4096;
// The fog wall's columns around its circle (one every 6 m at 2000 m: the
// wall's foot is sunk under the terrain, so a coarser cut does not open
// gaps) and its rows from foot to top (fogWallRowHeight, fogWallRowAlpha:
// denser near the ground).
export const FOG_WALL_SEGMENTS = 2048;
export const FOG_WALL_ROWS = 4;
// FOG_WALL_SEGMENTS * FOG_WALL_ROWS.
export const FOG_WALL_VERTICES = 8192;
// How far the wall's foot stands under the terrain, in world units.
export const FOG_WALL_SINK = 20;
// The ragged variation around the circle: a column's alpha scales between
// 1 - FOG_WALL_RAGGED and 1, from three fixed waves of these periods.
const FOG_WALL_RAGGED = .35;
const FOG_WALL_WAVE_A = 7;
const FOG_WALL_WAVE_B = 23;
const FOG_WALL_WAVE_C = 61;

/*
================
fogWallRowHeight

The height fraction of the wall's row r, foot (0) to top (1).
================
*/
function fogWallRowHeight( r: number ): number {
	switch ( r ) {
		case 0:
			return 0;
		case 1:
			return .12;
		case 2:
			return .4;
		default:
			return 1;
	}
}

/*
================
fogWallRowAlpha

The share of the wall's alpha its row r carries: dense at the foot,
clear at the top.
================
*/
function fogWallRowAlpha( r: number ): number {
	switch ( r ) {
		case 0:
			return 1;
		case 1:
			return .85;
		case 2:
			return .4;
		default:
			return 0;
	}
}
// The strip's width and its lift over the terrain, in world units.
export const GROUND_RING_WIDTH = 15;
const GROUND_RING_LIFT = 2;
// A terrain tile is 20 units; a cell holds 16 x 16 tiles as 17 x 17 heights.
const TILE_UNITS = 20;
const CELL_TILES = 16;
const CELL_STRIDE = 17;

/*
================
terrainHeight

The terrain height at a scene-local point, or undefined where no terrain
cell is resident. Each tile splits along the diagonal tile() uses: from
its low corner to its high corner when the tile's x and z parities agree,
across the other diagonal when they differ.
================
*/
export function terrainHeight( cells: TerrainCells, x: number, z: number ): number | undefined {
	const tx = Math.floor( x / TILE_UNITS ), tz = Math.floor( z / TILE_UNITS );
	const cx = Math.floor( tx / CELL_TILES ),
		cz = Math.floor( tz / CELL_TILES ),
		cell = cells.get( terrainCellKey( cx, cz ) );
	if ( !cell ) return undefined;
	const ix = tx - cx * CELL_TILES, iz = tz - cz * CELL_TILES, at = iz * CELL_STRIDE + ix;
	const h00 = cell.heights[at]!,
		h10 = cell.heights[at + 1]!,
		h01 = cell.heights[at + CELL_STRIDE]!,
		h11 = cell.heights[at + CELL_STRIDE + 1]!;
	const fx = x / TILE_UNITS - tx, fz = z / TILE_UNITS - tz;
	if ( (tx & 1) === (tz & 1) ) {
		return fz >= fx ? h00 + (h11 - h01) * fx + (h01 - h00) * fz : h00 + (h10 - h00) * fx + (h11 - h10) * fz;
	}
	return fx + fz <= 1 ?
		h00 + (h10 - h00) * fx + (h01 - h00) * fz :
		h11 + (h01 - h11) * (1 - fx) + (h10 - h11) * (1 - fz);
}

/*
================
writeGroundRing

Writes the ring's vertex pairs (inner, outer) into positions, a
GROUND_RING_SEGMENTS * 2 * 3 array, around a scene-local centre. A vertex
over no resident terrain takes its partner's height; a pair with neither
on terrain collapses onto the ring's centre line at the last height found
(zero width: no strip draws there). False when no vertex stood on terrain
(nothing to draw).
================
*/
export function writeGroundRing(
	positions: Float32Array,
	cells: TerrainCells,
	ring: { readonly x: number; readonly z: number; readonly radius: number; readonly width?: number; }
): boolean {
	const width = ring.width ?? GROUND_RING_WIDTH;
	const inner = Math.max( 0, ring.radius - width / 2 ), outer = ring.radius + width / 2;
	let last: number | undefined, first = -1;
	for ( let i = 0; i < GROUND_RING_SEGMENTS; i++ ) {
		const angle = 2 * Math.PI * i / GROUND_RING_SEGMENTS, c = Math.cos( angle ), s = Math.sin( angle );
		const ix = ring.x + inner * c, iz = ring.z + inner * s, ox = ring.x + outer * c, oz = ring.z + outer * s;
		const innerHeight = terrainHeight( cells, ix, iz ), outerHeight = terrainHeight( cells, ox, oz );
		const at = i * 6;
		if ( innerHeight === undefined && outerHeight === undefined ) {
			const x = ring.x + ring.radius * c, z = ring.z + ring.radius * s, y = (last ?? 0) + GROUND_RING_LIFT;
			positions[at] = positions[at + 3] = x;
			positions[at + 1] = positions[at + 4] = y;
			positions[at + 2] = positions[at + 5] = z;
			continue;
		}
		if ( first < 0 ) first = at;
		last = outerHeight ?? innerHeight!;
		positions[at] = ix;
		positions[at + 1] = (innerHeight ?? outerHeight!) + GROUND_RING_LIFT;
		positions[at + 2] = iz;
		positions[at + 3] = ox;
		positions[at + 4] = last + GROUND_RING_LIFT;
		positions[at + 5] = oz;
	}
	if ( first < 0 ) return false;
	// The vertices before the first on terrain had no height to borrow yet:
	// they take the ring's last one.
	for ( let at = 0; at < first; at += 3 ) positions[at + 1] = last! + GROUND_RING_LIFT;
	return true;
}

/*
================
groundRingGeometry

The ring's strip over positions already written by writeGroundRing: an
unlit, blended, double-sided decal in one colour (no texture: white).
================
*/
export function groundRingGeometry(
	positions: Float32Array,
	color: readonly [number, number, number, number]
): Geometry {
	const indices = new Uint32Array( GROUND_RING_SEGMENTS * 6 );
	for ( let i = 0; i < GROUND_RING_SEGMENTS; i++ ) {
		const a = i * 2, b = a + 1, c = ((i + 1) % GROUND_RING_SEGMENTS) * 2, d = c + 1, at = i * 6;
		indices[at] = a;
		indices[at + 1] = b;
		indices[at + 2] = d;
		indices[at + 3] = a;
		indices[at + 4] = d;
		indices[at + 5] = c;
	}
	return {
		world: true,
		positions,
		uvs: new Float32Array( GROUND_RING_SEGMENTS * 2 * 2 ),
		indices,
		transform: identity(),
		instances: identity(),
		dynamicVertices: true,
		material: { color, alphaCutoff: 0, blend: true, doubleSided: true, unlit: true, decal: true }
	};
}

// ============================================================================

/*
================
writeFogWall

Writes the wall's columns into positions, a FOG_WALL_VERTICES * 3 array,
around a scene-local centre: column i's rows are vertices i * FOG_WALL_ROWS
onward, from FOG_WALL_SINK under the terrain to height over it. A column
over no resident terrain collapses to one point at the last height found
(zero height: no wall draws there). False when no column stood on terrain
(nothing to draw).
================
*/
export function writeFogWall(
	positions: Float32Array,
	cells: TerrainCells,
	wall: { readonly x: number; readonly z: number; readonly radius: number; readonly height: number; }
): boolean {
	let last: number | undefined, first = -1;
	for ( let i = 0; i < FOG_WALL_SEGMENTS; i++ ) {
		const angle = 2 * Math.PI * i / FOG_WALL_SEGMENTS;
		const x = wall.x + wall.radius * Math.cos( angle ), z = wall.z + wall.radius * Math.sin( angle );
		const ground = terrainHeight( cells, x, z ), at = i * FOG_WALL_ROWS * 3;
		if ( ground !== undefined ) {
			if ( first < 0 ) first = i;
			last = ground;
		}
		const foot = (last ?? 0) - FOG_WALL_SINK, rise = ground === undefined ? 0 : wall.height + FOG_WALL_SINK;
		for ( let r = 0; r < FOG_WALL_ROWS; r++ ) {
			positions[at + r * 3] = x;
			positions[at + r * 3 + 1] = foot + rise * fogWallRowHeight( r );
			positions[at + r * 3 + 2] = z;
		}
	}
	if ( first < 0 ) return false;
	// The columns before the first on terrain had no height to borrow yet:
	// they take the wall's last one.
	for ( let at = 0; at < first * FOG_WALL_ROWS * 3; at += 3 ) positions[at + 1] = last! - FOG_WALL_SINK;
	return true;
}

/*
================
fogWallAlpha

The share of the wall's alpha at column i, row r: the row's gradient
step, scaled by the column's fixed ragged variation.
================
*/
export function fogWallAlpha( i: number, r: number ): number {
	const angle = 2 * Math.PI * i / FOG_WALL_SEGMENTS;
	const wave = Math.sin( angle * FOG_WALL_WAVE_A + FOG_WALL_WAVE_A ) +
		Math.sin( angle * FOG_WALL_WAVE_B + FOG_WALL_WAVE_B ) + Math.sin( angle * FOG_WALL_WAVE_C + FOG_WALL_WAVE_C );
	// wave lies in [-3, 3]: the column keeps between 1 - RAGGED and 1.
	const ragged = 1 - FOG_WALL_RAGGED * (.5 + wave / 6);
	return fogWallRowAlpha( r ) * ragged;
}

/*
================
fogWallGeometry

The wall over positions already written by writeFogWall: an unlit,
blended, double-sided curtain that writes no depth, in one colour whose
alpha the vertex colours grade (no texture: white).
================
*/
export function fogWallGeometry(
	positions: Float32Array,
	color: readonly [number, number, number, number]
): Geometry {
	const quads = FOG_WALL_SEGMENTS * (FOG_WALL_ROWS - 1), indices = new Uint32Array( quads * 6 );
	const colors = new Float32Array( FOG_WALL_VERTICES * 4 ).fill( 1 );
	for ( let i = 0; i < FOG_WALL_SEGMENTS; i++ ) {
		const column = i * FOG_WALL_ROWS, next = ((i + 1) % FOG_WALL_SEGMENTS) * FOG_WALL_ROWS;
		for ( let r = 0; r < FOG_WALL_ROWS; r++ ) colors[(column + r) * 4 + 3] = fogWallAlpha( i, r );
		for ( let r = 0; r < FOG_WALL_ROWS - 1; r++ ) {
			const a = column + r, b = a + 1, c = next + r, d = c + 1, at = (i * (FOG_WALL_ROWS - 1) + r) * 6;
			indices[at] = a;
			indices[at + 1] = b;
			indices[at + 2] = d;
			indices[at + 3] = a;
			indices[at + 4] = d;
			indices[at + 5] = c;
		}
	}
	return {
		world: true,
		positions,
		colors,
		uvs: new Float32Array( FOG_WALL_VERTICES * 2 ),
		indices,
		transform: identity(),
		instances: identity(),
		dynamicVertices: true,
		material: { color, alphaCutoff: 0, blend: true, doubleSided: true, unlit: true, depthWrite: false }
	};
}
