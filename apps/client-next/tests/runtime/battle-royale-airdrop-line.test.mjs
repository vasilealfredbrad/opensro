/*
===========================================================================

battle-royale-airdrop-line.test.mjs - the airdrop's flight line (Stage 9)

Port-only, not native (docs/battle-royale.md, Stage 9 item 5). The
production line owner projects an incoming airdrop's flight path through
the maps' grid origin, clips it to the map and dashes it, and moves the
plane from the line's start to its end over the last minute; the
production HUD draws the dashes and the plane on the minimap and the
world map with the landing countdown under the zone line, and swaps them
for the landed mark at landing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { decodeBattleRoyaleState, OP_BATTLE_ROYALE_STATE } = await import(
	"../../src/engine/foundation/gameplay/battle-royale.ts"
);
const { battleRoyaleAirdropSeconds, battleRoyaleAirdropText, battleRoyaleShowsAirdropLine } = await import(
	"../../src/engine/foundation/gameplay/battle-royale-airdrop.ts"
);
const {
	AIRDROP_FLIGHT_MS,
	AIRDROP_LINE_PX,
	AIRDROP_LINE_RGBA,
	AIRDROP_MAX_DASHES,
	AIRDROP_PLANE_MAP_PX,
	AIRDROP_PLANE_PX,
	airdropLineQuads,
	airdropLineSegment,
	airdropPlane,
	clipSegment
} = await import( "../../src/engine/runtime/ui/hud/br-airdrop-line.ts" );
const { BATTLE_ROYALE_AIRDROP_ARROW, BATTLE_ROYALE_AIRDROP_PX, BATTLE_ROYALE_AIRDROP_RGBA } = await import(
	"../../src/engine/runtime/ui/hud/br-hud.ts"
);

const FIXED_BYTES = 59;
const REGION_UNITS = 1920;

/*
================
decodeState

A 0x3FB2 match frame (docs/battle-royale.md "Wire") decoded: a running
match's alive participant unless fields say otherwise.
================
*/
function decodeState( { state = 3, role = 2 } = {} ) {
	const b = Buffer.alloc( FIXED_BYTES );
	b[0] = 1;
	b[1] = state;
	b[2] = role;
	b.writeFloatLE( 150, 12 );
	b[16] = 1;
	b[23] = 5;
	const decoded = decodeBattleRoyaleState( new Uint8Array( b ), 0, 1 );
	if ( !decoded || OP_BATTLE_ROYALE_STATE !== 0x3fb2 ) throw new Error( "no match" );
	return decoded;
}

/*
================
incoming

An incoming airdrop over the given line, landing at landsAtMs.
================
*/
/**
 * @param {{ ax: number; az: number; bx: number; bz: number; }} line
 * @returns {import("../../src/engine/foundation/gameplay/battle-royale-airdrop.ts").BattleRoyaleIncomingAirdrop}
 */
function incoming( line, landsAtMs = 60000 ) {
	return { state: "incoming", phase: 1, line, landsAtMs, receivedAtMs: 0 };
}

/*
================
close
================
*/
function close( actual, expected, label = "" ) {
	assert.ok( Math.abs( actual - expected ) < 1e-6, `${label} ${actual} != ${expected}` );
}

test("a segment is clipped to the map, or dropped when it misses it", () => {
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 100, 100 ];
	assert.deepEqual( clipSegment( 10, 10, 90, 90, clip ), { x0: 10, y0: 10, x1: 90, y1: 90 } );
	const across = clipSegment( -50, 50, 150, 50, clip );
	assert.ok( across );
	if ( !across ) return;
	assert.deepEqual( [ across.x0, across.y0, across.x1, across.y1 ], [ 0, 50, 100, 50 ] );
	const diagonal = clipSegment( -100, -100, 200, 200, clip );
	assert.deepEqual( diagonal, { x0: 0, y0: 0, x1: 100, y1: 100 } );
	assert.equal( clipSegment( -10, -10, -5, 200, clip ), null, "left of the map" );
	assert.equal( clipSegment( 200, 0, 300, 100, clip ), null );
});

test("the line projects through a map origin, north up, and dashes along it", () => {
	// Grid (0, 0) at (100, 500), half a pixel per unit: the world map's
	// projection as the airdrop mark's test uses it.
	const origin = { x: 100, y: 500, rx: .5, ry: .5 };
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 600, 600 ];
	const segment = airdropLineSegment( origin, { ax: 0, az: 0, bx: 400, bz: 600 }, clip );
	assert.ok( segment );
	if ( !segment ) return;
	assert.deepEqual( [ segment.x0, segment.y0 ], [ 100, 500 ] );
	assert.deepEqual( [ segment.x1, segment.y1 ], [ 300, 200 ], "+z is up the page" );
	// A line crossing the whole page is clipped at its edges.
	const long = airdropLineSegment( origin, { ax: -1000, az: 0, bx: 2000, bz: 0 }, clip );
	assert.deepEqual( long, { x0: 0, y0: 500, x1: 600, y1: 500 } );
	if ( !long ) return;
	const dashes = airdropLineQuads( long, clip );
	assert.equal( dashes.length, 60, "6 px dashes, 4 px gaps over 600 px" );
	for ( const dash of dashes ) {
		assert.equal( dash.texture, "" );
		assert.deepEqual( dash.color, AIRDROP_LINE_RGBA );
		assert.equal( dash.rect[3], AIRDROP_LINE_PX );
		assert.equal( dash.rotation, 0 );
		assert.equal( dash.mask, undefined );
	}
	assert.deepEqual( dashes[0].rect, [ 0, 499, 6, 2 ] );
	// A line far longer than the pattern allows keeps the dash count bounded.
	const huge = airdropLineQuads( { x0: 0, y0: 0, x1: 0, y1: 100000 }, [ 0, 0, 10, 100000 ] );
	assert.ok( huge.length <= AIRDROP_MAX_DASHES + 1 );
	close( huge[0].rotation ?? 0, Math.PI / 2, "south" );
	assert.equal( airdropLineSegment( origin, { ax: 5000, az: 0, bx: 6000, bz: 0 }, clip ), null, "off the page" );
});

test("the plane flies from the line's start to its end over the last minute", () => {
	const origin = { x: 0, y: 1000, rx: 1, ry: 1 };
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 1000, 1000 ];
	const drop = incoming( { ax: 100, az: 500, bx: 700, bz: 500 }, 60000 );
	const start = airdropPlane( origin, drop, 0, clip );
	assert.ok( start );
	if ( !start ) return;
	assert.deepEqual( [ start.x, start.y ], [ 100, 500 ] );
	const half = airdropPlane( origin, drop, 60000 - AIRDROP_FLIGHT_MS / 2, clip );
	assert.deepEqual( half && [ half.x, half.y ], [ 400, 500 ] );
	const landed = airdropPlane( origin, drop, 90000, clip );
	assert.deepEqual( landed && [ landed.x, landed.y ], [ 700, 500 ], "never past the end" );
	// Announced late (a reconnect): part-way along already.
	const late = airdropPlane( origin, incoming( drop.line, 70000 ), 40000, clip );
	assert.deepEqual( late && [ late.x, late.y ], [ 400, 500 ] );
	// Eastward the arrow takes the rotation minimapEdge gives an arrow
	// pointing east (offset [1, 0]: -(0 + pi)).
	close( start.rotation, -Math.PI, "east" );
	assert.equal( airdropPlane( origin, drop, 0, [ 200, 0, 800, 1000 ] ), null, "outside the clip" );
	assert.equal( airdropPlane( origin, incoming( { ax: 1, az: 1, bx: 1, bz: 1 } ), 0, clip ), null );
});

test("the countdown line and the line's visibility follow the match", () => {
	const drop = incoming( { ax: 0, az: 0, bx: 10, bz: 10 }, 60000 );
	const running = decodeState();
	assert.equal( battleRoyaleShowsAirdropLine( running, drop ), true );
	assert.equal( battleRoyaleShowsAirdropLine( decodeState( { role: 3 } ), drop ), true, "a spectator" );
	assert.equal( battleRoyaleShowsAirdropLine( decodeState( { role: 0 } ), drop ), false, "outside" );
	assert.equal( battleRoyaleShowsAirdropLine( decodeState( { state: 1, role: 1 } ), drop ), false, "the lobby" );
	assert.equal(
		battleRoyaleShowsAirdropLine( running, { state: "landed", phase: 1, x: 0, z: 0, receivedAtMs: 0 } ),
		false
	);
	assert.equal( battleRoyaleAirdropSeconds( drop, 18000 ), 42 );
	assert.equal( battleRoyaleAirdropSeconds( drop, 18001 ), 42, "whole seconds round up" );
	assert.equal( battleRoyaleAirdropSeconds( drop, 61000 ), 0 );
	assert.equal( battleRoyaleAirdropText( running, drop, undefined, 18000 ), "Airdrop lands in 42 s" );
	assert.equal( battleRoyaleAirdropText( decodeState( { state: 1, role: 1 } ), drop, undefined, 0 ), "" );
});

/*
================
lineDashes

The flight line's dashes in the last scene drawn: masked ones on the
minimap, unmasked on the world map.
================
*/
function lineDashes( f, masked ) {
	return f.scenes.at( -1 ).quads.filter( q =>
		q.texture === "" && q.rect[3] === AIRDROP_LINE_PX && q.color.join() === AIRDROP_LINE_RGBA.join() &&
		!!q.mask === masked
	);
}

/*
================
planes
================
*/
function planes( f, size ) {
	return f.scenes.at( -1 ).quads.filter( q => q.texture === BATTLE_ROYALE_AIRDROP_ARROW && q.rect[2] === size );
}

test("a participant sees the line and the plane on both maps until the landing", () => {
	const f = uiFixture();
	try {
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		};
		const region = 0x5a74, gx = (region & 255) * REGION_UNITS, gz = (region >> 8) * REGION_UNITS;
		f.state.entities[0] = { ...f.state.entities[0], regionId: region };
		f.state.gameplay.pose = { ...f.state.gameplay.pose, regionId: region };
		Object.assign( f.state, { simulationTimeMs: 18000 } );
		Object.assign( f.state.gameplay, {
			battleRoyale: decodeState(),
			// West to east through the player, landing in 30 s: the plane is
			// half-way, over the player.
			battleRoyaleAirdrop: incoming( { ax: gx - 2000, az: gz, bx: gx + 2000, bz: gz }, 48000 )
		} );
		step();
		assert.ok( lineDashes( f, true ).length > 0, "the line on the minimap" );
		assert.equal( planes( f, AIRDROP_PLANE_PX ).length, 1, "the plane on the minimap" );
		assert.ok( f.hasText( "Airdrop lands in 30 s" ) );
		f.ui.event( { kind: "activate", id: "toggle-window:Map" } );
		step();
		assert.equal( f.ui.stats().panel, "Map" );
		assert.ok( lineDashes( f, false ).length > 0, "the line on the world map" );
		assert.equal( planes( f, AIRDROP_PLANE_MAP_PX ).length, 1, "the plane on the world map" );
		// The landing: the line goes, the mark comes.
		f.ui.event( { kind: "activate", id: "toggle-window:Map" } );
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyaleAirdrop: { state: "landed", phase: 1, x: gx + 240, z: gz, receivedAtMs: 60000 }
		};
		step();
		assert.equal( lineDashes( f, true ).length, 0 );
		assert.equal( planes( f, AIRDROP_PLANE_PX ).length, 0 );
		assert.equal(
			f.scenes.at( -1 ).quads.filter( q =>
				q.texture === "" && q.rect[2] === BATTLE_ROYALE_AIRDROP_PX &&
				q.color.join() === BATTLE_ROYALE_AIRDROP_RGBA.join()
			).length,
			1,
			"the landed mark"
		);
		assert.ok( f.hasText( "Airdrop: 24 m" ) );
		assert.ok( !f.hasText( "Airdrop lands in" ) );
		// Outside a match nothing flies.
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyale: decodeState( { state: 1, role: 1 } ),
			battleRoyaleAirdrop: incoming( { ax: gx - 2000, az: gz, bx: gx + 2000, bz: gz }, 60000 )
		};
		step();
		assert.equal( lineDashes( f, true ).length, 0 );
		assert.ok( !f.hasText( "Airdrop lands in" ) );
	} finally {
		f.dispose();
	}
});
