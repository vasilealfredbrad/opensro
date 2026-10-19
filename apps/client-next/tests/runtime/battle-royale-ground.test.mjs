/*
===========================================================================

battle-royale-ground.test.mjs - the Battle Royale circles on the ground

Port-only, not native (docs/battle-royale.md). The lobby and zone circles
are laid on the terrain by the world renderer: each vertex stands on the
terrain height under it, a moved circle rewrites its strip in place, and
nothing is drawn without a match. The zone also stands as a fog wall on
its circle, and the scene's fog closes in around a player outside it. The world owner is driven with a fake
GPU, as world-renderer.test.mjs drives its decals.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	terrainHeight,
	writeGroundRing,
	groundRingGeometry,
	writeFogWall,
	fogWallGeometry,
	fogWallAlpha,
	GROUND_RING_SEGMENTS,
	GROUND_RING_WIDTH,
	FOG_WALL_ROWS,
	FOG_WALL_SEGMENTS,
	FOG_WALL_SINK,
	FOG_WALL_VERTICES
} = await import( "../../src/engine/foundation/rendering/ground-ring.ts" );
const { applyZoneFog } = await import( "../../src/engine/foundation/rendering/world-environment.ts" );
const { terrainCellKey } = await import( "../../src/engine/foundation/rendering/terrain-interaction.ts" );
const { createGroundRings } = await import( "../../src/engine/runtime/characters/ground-rings.ts" );
const { createWorldRenderer } = await import( "../../src/engine/runtime/renderer/world/world.ts" );

const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
// The strip's lift over the terrain (ground-ring.ts).
const LIFT = 2;

/*
================
slopedCell

One terrain cell (0, 0) whose height grows 1 a tile eastward and 2 a
tile northward: a plane, so every triangle split reads the same height.
================
*/
/** @returns {import("../../src/engine/contracts/scene.ts").TerrainRange} */
function slopedCell() {
	return {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 160, 0, 160 ],
		radius: 230,
		heights: Array.from( { length: 289 }, ( _, i ) => (i % 17) + 2 * Math.floor( i / 17 ) )
	};
}

test("terrain height follows each tile's own triangle split", () => {
	// Tile (0, 0) splits from its low corner to its high corner; tile (1, 0)
	// across the other diagonal. Raise the corner they share and read both.
	const heights = Array( 289 ).fill( 0 );
	heights[17 + 1] = 10;
	const cells = new Map( [ [ terrainCellKey( 0, 0 ), { ...slopedCell(), heights } ] ] );
	assert.equal( terrainHeight( cells, 0, 0 ), 0 );
	assert.equal( terrainHeight( cells, 20, 20 ) ?? NaN, 10 );
	assert.ok( Math.abs( (terrainHeight( cells, 10, 10 ) ?? NaN) - 5 ) < 1e-9, "on the shared diagonal" );
	assert.equal( terrainHeight( cells, 15, 5 ), 2.5 );
	// Tile (1, 0) splits across the other diagonal: its raised corner is
	// its north-west one, so (35, 15) lies in the triangle away from it.
	assert.equal( terrainHeight( cells, 25, 15 ), 7.5 );
	assert.equal( terrainHeight( cells, 35, 15 ), 2.5 );
	assert.equal( terrainHeight( cells, 400, 10 ), undefined, "no resident cell" );
	const plane = new Map( [ [ terrainCellKey( 0, 0 ), slopedCell() ] ] );
	for ( const [x, z] of [ [ 7, 3 ], [ 33, 51 ], [ 211, 97 ], [ 299, 300 ] ] ) {
		assert.ok( Math.abs( (terrainHeight( plane, x, z ) ?? NaN) - (x / 20 + 2 * z / 20) ) < 1e-9 );
	}
});

test("a ring's strip stands on the terrain, inner and outer edge a vertex pair", () => {
	const cells = new Map( [ [ terrainCellKey( 0, 0 ), slopedCell() ] ] );
	const positions = new Float32Array( GROUND_RING_SEGMENTS * 6 );
	assert.equal( writeGroundRing( positions, cells, { x: 160, z: 160, radius: 100 } ), true );
	for ( let i = 0; i < GROUND_RING_SEGMENTS * 2; i++ ) {
		const x = positions[i * 3], y = positions[i * 3 + 1], z = positions[i * 3 + 2];
		const radius = Math.hypot( x - 160, z - 160 );
		assert.ok( Math.abs( radius - (100 + (i % 2 ? 1 : -1) * GROUND_RING_WIDTH / 2) ) < 1e-3 );
		assert.ok( Math.abs( y - (x / 20 + 2 * z / 20 + LIFT) ) < 1e-3, "on the terrain, lifted" );
	}
	// Half the ring leaves the cell: every vertex stays finite (the pairs off
	// it collapse, see the 900 m test).
	assert.equal( writeGroundRing( positions, cells, { x: 320, z: 160, radius: 100 } ), true );
	assert.ok( positions.every( Number.isFinite ) );
	assert.equal( writeGroundRing( positions, cells, { x: 5000, z: 5000, radius: 100 } ), false, "no terrain" );
	const geometry = groundRingGeometry( positions, [ 1, 0, 0, .5 ] );
	assert.equal( geometry.indices.length, GROUND_RING_SEGMENTS * 6 );
	assert.ok( geometry.indices.every( index => index < GROUND_RING_SEGMENTS * 2 ), "the strip closes on itself" );
	assert.deepEqual( geometry.material?.color, [ 1, 0, 0, .5 ] );
	assert.equal( geometry.dynamicVertices, true );
});

/*
================
pairWidth

The width of vertex pair i of a written strip (inner to outer, in x/z).
================
*/
/** @param {Float32Array} positions @param {number} i */
function pairWidth( positions, i ) {
	const at = i * 6;
	return Math.hypot( positions[at + 3] - positions[at], positions[at + 5] - positions[at + 2] );
}

test("a 900 m zone stays on the ground: pairs a tile apart, nothing where no terrain is", () => {
	// Stage 2 zones start at 900 m (docs/battle-royale.md): consecutive pairs
	// stay closer than one 20-unit terrain tile.
	const radius = 9000, chord = 2 * radius * Math.sin( Math.PI / GROUND_RING_SEGMENTS );
	assert.ok( chord < 20, `pairs ${chord.toFixed( 1 )} units apart` );
	// The ring's centre 900 m west of the one resident cell: only its
	// eastern arc crosses terrain, and only that arc has width.
	const cells = new Map( [ [ terrainCellKey( 0, 0 ), slopedCell() ] ] );
	const positions = new Float32Array( GROUND_RING_SEGMENTS * 6 );
	assert.equal( writeGroundRing( positions, cells, { x: 160 - radius, z: 160, radius } ), true );
	assert.ok( positions.every( Number.isFinite ) );
	let wide = 0;
	for ( let i = 0; i < GROUND_RING_SEGMENTS; i++ ) {
		const width = pairWidth( positions, i );
		if ( width === 0 ) continue;
		wide++;
		assert.ok( Math.abs( width - GROUND_RING_WIDTH ) < 1e-2, "a pair on terrain keeps the strip's width" );
		const x = positions[i * 6], z = positions[i * 6 + 2];
		assert.ok( x >= -GROUND_RING_WIDTH && x <= 320 + GROUND_RING_WIDTH && z >= 0 && z <= 320, "on the cell" );
	}
	assert.ok( wide > 0 && wide < GROUND_RING_SEGMENTS / 50, `${wide} pairs drawn, the arc over the cell` );
	// Half of a 10 m ring leaves the cell: those pairs collapse instead of
	// floating at a borrowed height.
	assert.equal( writeGroundRing( positions, cells, { x: 320, z: 160, radius: 100 } ), true );
	assert.equal( pairWidth( positions, 0 ), 0, "east of the cell: no terrain" );
	assert.ok( Math.abs( pairWidth( positions, GROUND_RING_SEGMENTS / 2 ) - GROUND_RING_WIDTH ) < 1e-2, "west: on it" );
});

/*
================
matchState

A published match: the lobby circle, and a running zone shrinking from
radius 3000 to 1000 over ten seconds when running (a later frame).
================
*/
/**
 * @param {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyalePhase} phase
 * @param {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyaleRole} role
 * @returns {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyaleState}
 */
function matchState( phase, role ) {
	return {
		phase,
		role,
		build: 0,
		lobby: { x: 222870, z: 173130, radius: 150 },
		members: 2,
		alive: 2,
		countdownEndsAtMs: 0,
		zonePhase: 0,
		zonePhases: 5,
		stage: "shrinking",
		stageStartedAtMs: 0,
		stageEndsAtMs: 10000,
		current: { x: 1000, z: 2000, radius: 3000 },
		next: { x: 1000, z: 2000, radius: 1000 },
		damagePerSec: 1,
		winner: "",
		receivedAtMs: 0,
		sequence: phase === "running" ? 2 : 1
	};
}

// The lobby target of a 150-unit lobby: the edge and two inner rings.
const LOBBY_TARGET = [ 150, 150 * .62, 150 * .25 ];

test("the ring owner publishes the lobby and the zone only when a circle moved", () => {
	/** @type {(readonly any[])[]} */
	const published = [];
	const rings = createGroundRings( { setGroundRings: value => published.push( value ) } );
	rings.step( undefined, 0 );
	assert.equal( published.length, 0, "nothing without a match" );
	rings.step( matchState( "lobby", "member" ), 0 );
	assert.deepEqual( published.at( -1 )?.map( ring => ring.radius ), LOBBY_TARGET );
	rings.step( matchState( "lobby", "member" ), 100 );
	assert.equal( published.length, 1, "a still lobby is not published again" );
	// The running frame is first seen at frame second 40: its stage clock
	// starts there.
	rings.step( matchState( "running", "alive" ), 40 );
	assert.deepEqual(
		published.at( -1 )?.map( ring => ring.radius ),
		[ 3000, 3000 ],
		"an alive participant sees no lobby: the zone's line and its wall"
	);
	rings.step( matchState( "running", "alive" ), 40.01 );
	assert.equal( published.length, 2, "two units of shrink stay under the step" );
	rings.step( matchState( "running", "alive" ), 45 );
	assert.deepEqual( published.at( -1 )?.map( ring => ring.radius ), [ 2000, 2000 ] );
	rings.clear();
	assert.deepEqual( published.at( -1 ), [] );
});

/*
================
fakeGeometry

The GPU geometry commands the world owner calls, recording uploads,
position rewrites and releases.
================
*/
function fakeGeometry() {
	const log = { uploads: 0, rewrites: 0, released: new Set() };
	/** @type {any} */
	const commands = {
		/*
			================
			upload
			================
			*/
		upload( data, image ) {
			log.uploads++;
			return { data, image };
		},
		/*
			================
			updatePositions
			================
			*/
		updatePositions() {
			log.rewrites++;
		},
		/*
			================
			release
			================
			*/
		release( draw ) {
			log.released.add( draw );
		},
		updateIndices: draw => draw,
		updateMaterialColors() {},
		updateInstances: draw => draw
	};
	return { log, commands };
}

test("the world renderer lays the rings with the decals, rewriting a moved one in place", () => {
	const world = createWorldRenderer(),
		gpu = fakeGeometry(),
		textures = /** @type {any} */ ({ upload: () => ({}), release() {} });
	world.scene( {
		id: "roc",
		originRegion: 1,
		warnings: [],
		groups: [ {
			id: "roc",
			center: [ 960, 0, 960 ],
			radius: 10000,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true, terrain: true },
			ranges: [ slopedCell() ],
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				instances: identity(),
				transform: identity()
			}
		} ]
	} );
	/** @type {( t: number ) => any[]} */
	const decals = t => [ ...(world.prepare( gpu.commands, textures, 1, t ).decalDraws ?? []) ];
	assert.deepEqual( decals( 0 ), [], "no rings, no draws" );
	// A ring around (160, 160) in region 1 (grid x 1920 + 160).
	/** @type {[number, number, number, number]} */
	const red = [ 1, .2, .1, .75 ];
	world.groundRings( [ { x: 1920 + 160, z: 160, radius: 100, color: red } ] );
	const first = decals( 0 )[0];
	assert.ok( first );
	assert.equal( first.data.material.decal, true );
	assert.deepEqual( first.data.material.color, red );
	assert.ok( Math.abs( first.data.positions[0] - (160 + 100 - GROUND_RING_WIDTH / 2) ) < 1e-3, "scene-local" );
	assert.equal( decals( .1 )[0], first, "a still ring keeps its draw" );
	world.groundRings( [ { x: 1920 + 160, z: 160, radius: 90, color: red } ] );
	assert.equal( decals( .2 )[0], first, "a moved ring keeps its draw" );
	assert.equal( gpu.log.rewrites, 1, "and rewrites its strip in place" );
	world.groundRings( [ { x: 1920 + 160, z: 160, radius: 90, color: [ 1, 1, 1, 1 ] } ] );
	const white = decals( .3 )[0];
	assert.notEqual( white, first, "a new colour builds the strip again" );
	assert.ok( gpu.log.released.has( first ) );
	world.groundRings( [] );
	assert.deepEqual( decals( .4 ), [] );
	assert.ok( gpu.log.released.has( white ) );
	world.dispose( gpu.commands, textures );
});

test("everyone sees the bootstrap lobby without a match frame, brighter in a countdown", () => {
	/** @type {(readonly any[])[]} */
	const published = [];
	const rings = createGroundRings( { setGroundRings: value => published.push( value ) } );
	const lobby = { x: 222870, z: 173130, radius: 150 };
	rings.step( undefined, 0, lobby );
	assert.deepEqual( published.at( -1 )?.map( ring => ring.radius ), LOBBY_TARGET );
	assert.equal( published.at( -1 )?.[0]?.width, 30, "the edge is the wide ring" );
	const idle = published.at( -1 )?.[0]?.color[3];
	rings.step( undefined, 1, lobby );
	assert.equal( published.length, 1, "a still lobby is not laid again" );
	rings.step( matchState( "countdown", "outside" ), 2, lobby );
	assert.ok( published.at( -1 )?.[0]?.color[3] > idle, "the countdown brightens the edge" );
	rings.step( matchState( "running", "alive" ), 3, lobby );
	assert.ok( published.at( -1 )?.every( ring => ring.radius !== 150 ), "an alive participant sees no lobby" );
});

test("a fog wall stands on the terrain, dense at its foot and clear at its top", () => {
	const cells = new Map( [ [ terrainCellKey( 0, 0 ), slopedCell() ] ] );
	const positions = new Float32Array( FOG_WALL_VERTICES * 3 ), height = 800;
	assert.equal( FOG_WALL_VERTICES, FOG_WALL_SEGMENTS * FOG_WALL_ROWS );
	assert.equal( writeFogWall( positions, cells, { x: 160, z: 160, radius: 100, height } ), true );
	for ( let i = 0; i < FOG_WALL_SEGMENTS; i++ ) {
		const at = i * FOG_WALL_ROWS * 3, x = positions[at], z = positions[at + 2], ground = x / 20 + 2 * z / 20;
		assert.ok( Math.abs( Math.hypot( x - 160, z - 160 ) - 100 ) < 1e-3, "on the circle" );
		assert.ok( Math.abs( positions[at + 1] - (ground - FOG_WALL_SINK) ) < 1e-3, "the foot under the terrain" );
		const top = at + (FOG_WALL_ROWS - 1) * 3;
		assert.ok( Math.abs( positions[top + 1] - (ground + height) ) < 1e-3, "the top the wall's height over it" );
		for ( let r = 1; r < FOG_WALL_ROWS; r++ ) {
			assert.ok( positions[at + r * 3 + 1] > positions[at + (r - 1) * 3 + 1], "rows rise" );
			assert.equal( positions[at + r * 3], x );
			assert.equal( positions[at + r * 3 + 2], z );
		}
	}
	// Half the wall leaves the cell: those columns collapse to a point.
	assert.equal( writeFogWall( positions, cells, { x: 320, z: 160, radius: 100, height } ), true );
	assert.ok( positions.every( Number.isFinite ) );
	assert.equal( positions[1], positions[(FOG_WALL_ROWS - 1) * 3 + 1], "east of the cell: no height" );
	const west = FOG_WALL_SEGMENTS / 2 * FOG_WALL_ROWS * 3;
	assert.ok( positions[west + (FOG_WALL_ROWS - 1) * 3 + 1] - positions[west + 1] > height, "west: standing" );
	assert.equal( writeFogWall( positions, cells, { x: 5000, z: 5000, radius: 100, height } ), false, "no terrain" );
	// A 2000 m zone: only the arc over the resident cell stands.
	const radius = 20000;
	assert.equal( writeFogWall( positions, cells, { x: 160 - radius, z: 160, radius, height } ), true );
	let standing = 0;
	for ( let i = 0; i < FOG_WALL_SEGMENTS; i++ ) {
		const at = i * FOG_WALL_ROWS * 3;
		if ( positions[at + (FOG_WALL_ROWS - 1) * 3 + 1] > positions[at + 1] ) standing++;
	}
	assert.ok( standing > 0 && standing < FOG_WALL_SEGMENTS / 50, `${standing} columns stand, the arc over the cell` );

	const geometry = fogWallGeometry( positions, [ .8, .8, .9, .7 ] );
	const colors = geometry.colors ?? new Float32Array( 0 );
	assert.equal( geometry.indices.length, FOG_WALL_SEGMENTS * (FOG_WALL_ROWS - 1) * 6 );
	assert.ok( geometry.indices.every( index => index < FOG_WALL_VERTICES ), "the wall closes on itself" );
	assert.equal( colors.length, FOG_WALL_VERTICES * 4 );
	for ( let i = 0; i < FOG_WALL_SEGMENTS; i++ ) {
		const alphas = Array.from( { length: FOG_WALL_ROWS }, ( _, r ) => colors[(i * FOG_WALL_ROWS + r) * 4 + 3] );
		assert.ok( alphas[0] > 0 && alphas[0] <= 1, "dense at the foot" );
		for ( let r = 1; r < FOG_WALL_ROWS; r++ ) assert.ok( alphas[r] < alphas[r - 1], "thinning upward" );
		assert.equal( alphas[FOG_WALL_ROWS - 1], 0, "clear at the top" );
		assert.ok( Math.abs( alphas[0] - fogWallAlpha( i, 0 ) ) < 1e-6 );
		assert.ok( alphas[0] >= .65 - 1e-6, "the ragged variation keeps the foot dense" );
	}
	assert.deepEqual( geometry.material, {
		color: [ .8, .8, .9, .7 ],
		alphaCutoff: 0,
		blend: true,
		doubleSided: true,
		unlit: true,
		depthWrite: false
	} );
	assert.equal( geometry.dynamicVertices, true );
});

/*
================
rocScene

A world scene in region 1 whose one terrain cell is slopedCell.
================
*/
/** @returns {any} */
function rocScene() {
	return {
		id: "roc",
		originRegion: 1,
		warnings: [],
		groups: [ {
			id: "roc",
			center: [ 960, 0, 960 ],
			radius: 10000,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true, terrain: true },
			ranges: [ slopedCell() ],
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				instances: identity(),
				transform: identity()
			}
		} ]
	};
}

test("the world renderer re-lays a moved fog wall in place, uploading nothing", () => {
	const world = createWorldRenderer(), textures = /** @type {any} */ ({ upload: () => ({}), release() {} });
	const gpu = fakeGeometry();
	/** @type {Float32Array[]} */
	const rewritten = [];
	gpu.commands.updatePositions = ( /** @type {any} */ _draw, /** @type {Float32Array} */ positions, colors ) => {
		assert.equal( colors, undefined, "the wall's colours stay as uploaded" );
		rewritten.push( positions );
	};
	world.scene( rocScene() );
	/** @type {( t: number ) => any[]} */
	const decals = t => [ ...(world.prepare( gpu.commands, textures, 1, t ).decalDraws ?? []) ];
	/** @type {[number, number, number, number]} */
	const fog = [ .78, .81, .86, .7 ];
	world.groundRings( [ { x: 1920 + 160, z: 160, radius: 100, color: fog, wall: 800 } ] );
	const first = decals( 0 )[0];
	assert.ok( first );
	assert.equal( first.data.positions.length, FOG_WALL_VERTICES * 3, "laid as a wall, not a strip" );
	assert.equal( first.data.material.depthWrite, false );
	assert.equal( first.data.material.decal, undefined );
	const uploads = gpu.log.uploads;
	for ( let k = 1; k <= 3; k++ ) {
		world.groundRings( [ { x: 1920 + 160, z: 160, radius: 100 - 5 * k, color: fog, wall: 800 } ] );
		assert.equal( decals( k / 10 )[0], first, "a moved wall keeps its draw" );
	}
	assert.equal( gpu.log.uploads, uploads, "and uploads nothing" );
	assert.equal( rewritten.length, 3 );
	assert.ok( rewritten.every( positions => positions === first.data.positions ), "rewriting the same array" );
	// A strip at the same slot with the same colour is another geometry.
	world.groundRings( [ { x: 1920 + 160, z: 160, radius: 85, color: fog } ] );
	const strip = decals( .5 )[0];
	assert.notEqual( strip, first );
	assert.equal( strip.data.positions.length, GROUND_RING_SEGMENTS * 2 * 3 );
	assert.ok( gpu.log.released.has( first ) );
	world.dispose( gpu.commands, textures );
});

test("the zone's line, its fog wall and the outside fog follow the match", () => {
	/** @type {(readonly any[])[]} */
	const published = [];
	/** @type {number[]} */
	const fog = [];
	const rings = createGroundRings( {
		setGroundRings: value => published.push( value ),
		setZoneFog: value => fog.push( value )
	} );
	// The zone shrinks from radius 3000 about (1000, 2000): region (0, 1) local
	// (1000, 80) is grid (1000, 2000), its centre.
	const centre = { regionId: 0x0100, x: 1000, z: 80 }, outside = { regionId: 0x0100, x: 4500, z: 80 };
	rings.step( matchState( "running", "alive" ), 0, undefined, centre );
	const [line, wall] = published.at( -1 ) ?? [];
	assert.equal( line.wall, undefined );
	assert.ok( line.width < GROUND_RING_WIDTH, "the zone's line is thinner than the default strip" );
	assert.ok( wall.wall >= 600 && wall.wall <= 1200, "a 60-120 m wall" );
	assert.ok( wall.color[0] > .6 && wall.color[2] >= wall.color[0], "a bluish grey" );
	assert.deepEqual( fog, [], "no fog inside" );
	// 500 units out: the scene fog closes in, in steps.
	rings.step( matchState( "running", "alive" ), 0, undefined, outside );
	assert.equal( fog.length, 1 );
	assert.ok( Math.abs( fog[0] - .7 ) < 1e-9, `${fog[0]}` );
	rings.step( matchState( "running", "alive" ), 0, undefined, { ...outside, x: 4501 } );
	assert.equal( fog.length, 1, "a step's worth of walking re-publishes nothing" );
	// Running on, the zone shrinks under the player: the fog thickens to full.
	rings.step( matchState( "running", "alive" ), 10, undefined, outside );
	assert.equal( fog.at( -1 ), 1 );
	// An eliminated player sees the wall but stands in no fog.
	rings.step( matchState( "running", "eliminated" ), 10, undefined, outside );
	assert.equal( fog.at( -1 ), 0 );
	rings.step( matchState( "running", "alive" ), 10, undefined, outside );
	rings.clear();
	assert.equal( fog.at( -1 ), 0, "teardown clears the fog" );
	assert.deepEqual( published.at( -1 ), [] );
});

test("the zone fog tightens the scene's fog toward grey, never loosening it", () => {
	/*
	================
	target
	================
	*/
	const target = () => {
		const values = new Float32Array( 36 ).fill( .2 );
		values[32] = 100;
		values[33] = 2500;
		return values;
	};
	const none = applyZoneFog( target(), 0 );
	assert.deepEqual( [ ...none ], [ ...target() ], "no fog leaves the target" );
	const full = applyZoneFog( target(), 1 );
	assert.ok( full[33] < 1000 && full[32] <= 100 && full[32] < full[33], "the fog closes in" );
	for ( const i of [ 27, 28, 29, 18, 19, 20 ] ) assert.ok( full[i] > .6, "fog and horizon turn grey" );
	const half = applyZoneFog( target(), .5 );
	assert.ok( half[33] < 2500 && half[33] > full[33] );
	const dense = target();
	dense[33] = 300;
	assert.equal( applyZoneFog( dense, 1 )[33], 300, "an already denser fog keeps its range" );
});
