/*
===========================================================================

battle-royale-airdrop.test.mjs - the Battle Royale airdrop mark (Stages 8b, 9)

Port-only, not native (docs/battle-royale.md, Stages 8b and 9). Crafted
0x3FB6 frames (version 1, read as a landing, and version 2 with its
incoming, landed and removed states) go through the production decoder,
the worker owner and the gameplay owner; the airdrop lives until the
server removes it, the match ends or the session resets; br-hud projects
the landed mark on the minimap (in place, or as the rim arrow beyond the
radar) and the world map; the production HUD draws the gold diamond on
both maps, the "Airdrop" label on the world map and the distance line, or
while it flies the landing countdown, under the zone line.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const { decodeBattleRoyaleState, OP_BATTLE_ROYALE_STATE } = await import(
	"../../src/engine/foundation/gameplay/battle-royale.ts"
);
const {
	battleRoyaleAirdropMeters,
	battleRoyaleAirdropText,
	battleRoyaleShowsAirdrop,
	decodeBattleRoyaleAirdrop,
	OP_BATTLE_ROYALE_AIRDROP
} = await import( "../../src/engine/foundation/gameplay/battle-royale-airdrop.ts" );
const { minimapEdge } = await import( "../../src/engine/foundation/ui/minimap-markers.ts" );
const { createBattleRoyale } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/battle-royale/battle-royale.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	BATTLE_ROYALE_AIRDROP_ARROW,
	BATTLE_ROYALE_AIRDROP_MAP_PX,
	BATTLE_ROYALE_AIRDROP_PX,
	BATTLE_ROYALE_AIRDROP_RGBA,
	BATTLE_ROYALE_GRID_UNIT,
	battleRoyaleAirdropMinimap,
	battleRoyaleAirdropQuads,
	battleRoyaleAirdropWorldMap,
	minimapEllipse
} = await import( "../../src/engine/runtime/ui/hud/br-hud.ts" );

const FIXED_BYTES = 59;
// World grid units per region side.
const REGION_UNITS = 1920;
// The radar's rim, where minimapEdge puts a beyond-radar arrow.
const RIM_PX = 47;

/*
================
stateFrame

A 0x3FB2 frame (docs/battle-royale.md "Wire"): a lobby member's lobby frame
unless fields say otherwise.
================
*/
function stateFrame( { state = 1, role = 1 } = {} ) {
	const b = Buffer.alloc( FIXED_BYTES );
	b[0] = 1;
	b[1] = state;
	b[2] = role;
	b.writeFloatLE( 150, 12 );
	b[16] = 1;
	b[23] = 5;
	return { opcode: OP_BATTLE_ROYALE_STATE, payload: new Uint8Array( b ) };
}

/*
================
decodeState
================
*/
function decodeState( fields, now = 0 ) {
	const state = decodeBattleRoyaleState( stateFrame( fields ).payload, now, 1 );
	if ( !state ) throw new Error( "no match" );
	return state;
}

/*
================
airdropFrame

A version 1 0x3FB6 frame: version, active, x and z (f32 grid units),
phase.
================
*/
function airdropFrame( { active = 1, x = 0, z = 0, phase = 1, version = 1 } = {} ) {
	const b = Buffer.alloc( 11 );
	b[0] = version;
	b[1] = active;
	b.writeFloatLE( x, 2 );
	b.writeFloatLE( z, 6 );
	b[10] = phase;
	return { opcode: OP_BATTLE_ROYALE_AIRDROP, payload: new Uint8Array( b ) };
}

/*
================
airdropV2Frame

A version 2 0x3FB6 frame (docs/battle-royale.md): version, state, phase,
the line's ends, the landing point (f32 grid units) and etaMs.
================
*/
function airdropV2Frame(
	{ state = 1, phase = 2, ax = 0, az = 0, bx = 0, bz = 0, x = 0, z = 0, etaMs = 0, version = 2 } = {}
) {
	const b = Buffer.alloc( 31 );
	b[0] = version;
	b[1] = state;
	b[2] = phase;
	[ ax, az, bx, bz, x, z ].forEach( ( value, i ) => b.writeFloatLE( value, 3 + i * 4 ) );
	b.writeUInt32LE( etaMs, 27 );
	return { opcode: OP_BATTLE_ROYALE_AIRDROP, payload: new Uint8Array( b ) };
}

test("0x3FB6 version 2 decodes the flight, the landing and the removal", () => {
	const line = { ax: 100, az: -200, bx: 3000.5, bz: 4000 };
	assert.deepEqual( decodeBattleRoyaleAirdrop( airdropV2Frame( { ...line, etaMs: 60000 } ).payload, 1000 ), {
		state: "incoming",
		phase: 2,
		line,
		landsAtMs: 61000,
		receivedAtMs: 1000
	} );
	assert.deepEqual(
		decodeBattleRoyaleAirdrop( airdropV2Frame( { ...line, state: 2, phase: 3, x: 1500, z: 1800 } ).payload, 5 ),
		{ state: "landed", phase: 3, x: 1500, z: 1800, line, receivedAtMs: 5 }
	);
	assert.deepEqual(
		decodeBattleRoyaleAirdrop( airdropV2Frame( { state: 2, x: 7, z: 8 } ).payload, 5 ),
		{ state: "landed", phase: 2, x: 7, z: 8, receivedAtMs: 5 },
		"an all-zero line is none"
	);
	assert.equal( decodeBattleRoyaleAirdrop( airdropV2Frame( { state: 0 } ).payload, 5 ), null );
	const good = airdropV2Frame().payload;
	/** @type {[Uint8Array, RegExp][]} */
	const malformed = [
		[ good.subarray( 0, 30 ), /Truncated/ ],
		[ Uint8Array.of( ...good, 0 ), /Trailing/ ],
		[ airdropV2Frame( { version: 3 } ).payload, /Unknown battle royale airdrop version/ ],
		[ airdropV2Frame( { state: 3 } ).payload, /Invalid battle royale airdrop state/ ],
		[ airdropV2Frame( { ax: NaN } ).payload, /Invalid battle royale airdrop position/ ],
		[ airdropV2Frame( { bz: Infinity } ).payload, /Invalid battle royale airdrop position/ ],
		[ airdropV2Frame( { state: 2, x: NaN } ).payload, /Invalid battle royale airdrop position/ ]
	];
	for ( const [payload, error] of malformed ) assert.throws( () => decodeBattleRoyaleAirdrop( payload, 0 ), error );
});

test("the owner follows an airdrop from its flight to its landing and removal", () => {
	const owner = createBattleRoyale();
	owner.receive( stateFrame( { state: 3, role: 2 } ), 0 );
	owner.receive( airdropV2Frame( { ax: 1, az: 2, bx: 3, bz: 4, etaMs: 60000 } ), 100 );
	const incoming = owner.airdrop();
	assert.equal( incoming?.state, "incoming" );
	assert.equal( incoming?.state === "incoming" ? incoming.landsAtMs : 0, 60100 );
	owner.receive( airdropV2Frame( { state: 2, ax: 1, az: 2, bx: 3, bz: 4, x: 2, z: 3 } ), 60100 );
	assert.deepEqual( owner.airdrop(), {
		state: "landed",
		phase: 2,
		x: 2,
		z: 3,
		line: { ax: 1, az: 2, bx: 3, bz: 4 },
		receivedAtMs: 60100
	} );
	owner.receive( airdropV2Frame( { state: 0 } ), 70000 );
	assert.equal( owner.airdrop(), undefined, "the guardian died" );
	owner.receive( airdropV2Frame( { etaMs: 1000 } ), 80000 );
	owner.receive( stateFrame( { state: 4, role: 2 } ), 80001 );
	assert.equal( owner.airdrop(), undefined, "the result ends a flight too" );
});

test("0x3FB6 version 1 decodes a landing, active 0 removes it, and a malformed one is a protocol error", () => {
	assert.equal( OP_BATTLE_ROYALE_AIRDROP, 0x3fb6 );
	assert.deepEqual( decodeBattleRoyaleAirdrop( airdropFrame( { x: 1500.5, z: -20, phase: 3 } ).payload, 40 ), {
		state: "landed",
		x: 1500.5,
		z: -20,
		phase: 3,
		receivedAtMs: 40
	} );
	assert.equal( decodeBattleRoyaleAirdrop( airdropFrame( { active: 0 } ).payload, 40 ), null );
	const good = airdropFrame().payload;
	/** @type {[Uint8Array, RegExp][]} */
	const malformed = [
		[ new Uint8Array(), /Truncated/ ],
		[ good.subarray( 0, 10 ), /Truncated/ ],
		[ Uint8Array.of( ...good, 0 ), /Trailing/ ],
		[ airdropFrame( { version: 2 } ).payload, /Truncated/ ],
		[ airdropFrame( { version: 9 } ).payload, /Unknown battle royale airdrop version/ ],
		[ airdropFrame( { active: 2 } ).payload, /Invalid battle royale airdrop state/ ],
		[ airdropFrame( { x: NaN } ).payload, /Invalid battle royale airdrop position/ ],
		[ airdropFrame( { z: Infinity } ).payload, /Invalid battle royale airdrop position/ ]
	];
	for ( const [payload, error] of malformed ) assert.throws( () => decodeBattleRoyaleAirdrop( payload, 0 ), error );
	const owner = createBattleRoyale();
	assert.equal( owner.receive( { opcode: 0x3fbe, payload: good }, 0 ), false, "a foreign frame" );
	assert.throws( () => owner.receive( { opcode: OP_BATTLE_ROYALE_AIRDROP, payload: Uint8Array.of( 1 ) }, 0 ) );
});

test("the owner keeps the mark until it is removed, the match ends or the session resets", () => {
	const owner = createBattleRoyale(), mark = airdropFrame( { x: 10, z: 20, phase: 1 } );
	// The first phase's drop may come just before the frame that starts the match.
	owner.receive( stateFrame( { state: 2 } ), 0 );
	assert.equal( owner.receive( mark, 10 ), true );
	owner.receive( stateFrame( { state: 3, role: 2 } ), 20 );
	assert.deepEqual( owner.airdrop(), { state: "landed", x: 10, z: 20, phase: 1, receivedAtMs: 10 } );
	owner.receive( airdropFrame( { active: 0 } ), 30 );
	assert.equal( owner.airdrop(), undefined, "looted or expired" );
	owner.receive( airdropFrame( { x: 5, z: 6, phase: 2 } ), 40 );
	owner.receive( stateFrame( { state: 3, role: 3 } ), 50 );
	assert.equal( owner.airdrop()?.phase, 2, "an eliminated participant keeps it" );
	owner.receive( stateFrame( { state: 4, role: 3 } ), 60 );
	assert.equal( owner.airdrop(), undefined, "the result ends it" );
	owner.receive( stateFrame( { state: 3, role: 2 } ), 70 );
	owner.receive( mark, 80 );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 90 );
	assert.equal( owner.airdrop(), undefined, "state 0 ends it" );
	owner.receive( stateFrame( { state: 3, role: 2 } ), 100 );
	owner.receive( mark, 110 );
	owner.receive( stateFrame(), 120 );
	assert.equal( owner.airdrop(), undefined, "a new lobby ends it" );
	owner.receive( mark, 130 );
	owner.clear();
	assert.equal( owner.airdrop(), undefined, "a new session" );
});

test("the gameplay owner publishes the mark and drops it with the match", () => {
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	const game = createGameplay( () => {} );
	try {
		game.bootstrap( { character: {} } );
		game.seed( local );
		game.take();
		game.receive( stateFrame( { state: 3, role: 2 } ), 100 );
		assert.equal( game.receive( airdropFrame( { x: 1, z: 2, phase: 4 } ), 200 ), true );
		assert.deepEqual( game.take()?.battleRoyaleAirdrop, {
			state: "landed",
			x: 1,
			z: 2,
			phase: 4,
			receivedAtMs: 200
		} );
		game.receive( stateFrame( { state: 0, role: 0 } ), 300 );
		assert.equal( game.take()?.battleRoyaleAirdrop, undefined );
	} finally {
		game.dispose();
	}
});

test("the maps and the HUD show the mark to participants of a running match", () => {
	/** @type {import("../../src/engine/foundation/gameplay/battle-royale-airdrop.ts").BattleRoyaleAirdrop} */
	const airdrop = { state: "landed", x: 1000, z: 2000, phase: 1, receivedAtMs: 0 };
	const running = decodeState( { state: 3, role: 2 } );
	assert.equal( battleRoyaleShowsAirdrop( running, airdrop ), true );
	assert.equal( battleRoyaleShowsAirdrop( decodeState( { state: 3, role: 3 } ), airdrop ), true, "a spectator" );
	assert.equal( battleRoyaleShowsAirdrop( decodeState( { state: 3, role: 0 } ), airdrop ), false, "outside" );
	assert.equal( battleRoyaleShowsAirdrop( decodeState( { state: 2 } ), airdrop ), false, "the countdown" );
	assert.equal( battleRoyaleShowsAirdrop( running, undefined ), false );
	// Pose (region 0, 700, 2400) is 300 east and 400 south: 500 units, 50 m.
	const pose = { regionId: 0x0100, x: 700, z: 480 };
	assert.equal( battleRoyaleAirdropMeters( airdrop, { regionId: 0, x: 1300, z: 2400 } ), 50 );
	assert.equal( battleRoyaleAirdropMeters( airdrop, pose ), 50, "the grid crosses regions" );
	assert.equal( battleRoyaleAirdropText( running, airdrop, pose, 0 ), "Airdrop: 50 m" );
	assert.equal( battleRoyaleAirdropMeters( airdrop, { regionId: 0x8001, x: 0, z: 0 } ), null, "a dungeon" );
	assert.equal( battleRoyaleAirdropText( running, airdrop, { regionId: 0x8001, x: 0, z: 0 }, 0 ), "" );
	assert.equal( battleRoyaleAirdropText( decodeState(), airdrop, pose, 0 ), "", "the lobby" );
	assert.equal( battleRoyaleAirdropText( running, airdrop, undefined, 0 ), "" );
});

test("the mark projects in place on the radar, on its rim beyond it, and on the world map", () => {
	const pose = { regionId: 0x5a74, x: 1710, z: 330 };
	const local = { x: (0x74 & 255) * REGION_UNITS + 1710, z: (0x5a74 >> 8) * REGION_UNITS + 330 };
	const origin = minimapEllipse( pose, BATTLE_ROYALE_GRID_UNIT, 96, [ 50, 50 ] );
	assert.ok( origin );
	if ( !origin ) return;
	// 15 m east is 7.5 px at 96 px a region, as the bot dots.
	const near = battleRoyaleAirdropMinimap( origin, { x: local.x + 150, z: local.z }, [
		50,
		50
	] );
	assert.equal( near.rim, false );
	assert.ok( Math.abs( near.x - 57.5 ) < 1e-6 && Math.abs( near.y - 50 ) < 1e-6 );
	// Far to the north-east: on the rim, pointing as the quest arrow would.
	const far = battleRoyaleAirdropMinimap(
		origin,
		{ x: local.x + 20000, z: local.z + 20000 },
		[ 50, 50 ]
	);
	/** @type {[number, number]} */
	const offset = [ 20000 * 96 / REGION_UNITS, -20000 * 96 / REGION_UNITS ];
	const edge = minimapEdge( offset, "quest" );
	assert.equal( far.rim, true );
	assert.ok( Math.abs( Math.hypot( far.x - 50, far.y - 50 ) - RIM_PX ) < 1e-6, "on the rim" );
	assert.ok( far.x > 50 && far.y < 50, "toward the north-east" );
	assert.equal( far.rotation, edge.rotation );
	const arrow = battleRoyaleAirdropQuads( far, BATTLE_ROYALE_AIRDROP_PX, [ 0, 0, 100, 100 ], 0 );
	assert.equal( arrow.length, 1 );
	assert.equal( arrow[0].texture, BATTLE_ROYALE_AIRDROP_ARROW );
	const diamond = battleRoyaleAirdropQuads( near, BATTLE_ROYALE_AIRDROP_PX, [ 0, 0, 100, 100 ], 0 );
	assert.equal( diamond.length, 3, "halo, outline, diamond" );
	assert.deepEqual( diamond[2].rect, [ 57.5 - 4, 46, 8, 8 ] );
	assert.deepEqual( diamond[2].color, BATTLE_ROYALE_AIRDROP_RGBA );
	assert.equal( diamond[2].rotation, Math.PI / 4 );
	// The world map: grid (0, 0) at (100, 500), half a pixel per unit.
	const map = { x: 100, y: 500, rx: .5, ry: .5 };
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 600, 600 ];
	assert.deepEqual( battleRoyaleAirdropWorldMap( map, { x: 400, z: 200 }, clip ), {
		x: 300,
		y: 400,
		rim: false,
		rotation: 0
	} );
	assert.equal(
		battleRoyaleAirdropWorldMap( map, { x: 4000, z: 200 }, clip ),
		null,
		"off the page"
	);
});

/*
================
airdropDiamonds

The gold diamonds of the given size in the last scene drawn.
================
*/
function airdropDiamonds( f, size ) {
	return f.scenes.at( -1 ).quads.filter( q =>
		q.texture === "" && q.rect[2] === size && q.color.join() === BATTLE_ROYALE_AIRDROP_RGBA.join()
	);
}

/*
================
goldGlyphs

The glyphs painted in the airdrop gold in the last scene drawn.
================
*/
function goldGlyphs( f ) {
	return f.scenes.at( -1 ).quads.filter( q =>
		q.texture === fontAtlas.image && q.color.join() === BATTLE_ROYALE_AIRDROP_RGBA.join()
	).length;
}

test("a participant sees the mark on both maps, the rim arrow and the distance line", () => {
	const f = uiFixture();
	try {
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		};
		const region = 0x5a74, gx = (region & 255) * REGION_UNITS, gz = (region >> 8) * REGION_UNITS;
		f.state.entities[0] = { ...f.state.entities[0], regionId: region };
		f.state.gameplay.pose = { ...f.state.gameplay.pose, regionId: region };
		Object.assign( f.state.gameplay, {
			battleRoyale: decodeState( { state: 3, role: 2 } ),
			battleRoyaleAirdrop: { state: "landed", x: gx + 240, z: gz, phase: 1, receivedAtMs: 0 }
		} );
		step();
		assert.equal( airdropDiamonds( f, BATTLE_ROYALE_AIRDROP_PX ).length, 1, "the diamond on the minimap" );
		assert.ok( f.hasText( "Airdrop: 24 m" ), "the distance line" );
		// Far away: the rim arrow instead of the diamond.
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyaleAirdrop: { state: "landed", x: gx + 90000, z: gz, phase: 1, receivedAtMs: 0 }
		};
		step();
		assert.equal( airdropDiamonds( f, BATTLE_ROYALE_AIRDROP_PX ).length, 0 );
		assert.equal( f.scenes.at( -1 ).quads.filter( q => q.texture === BATTLE_ROYALE_AIRDROP_ARROW ).length, 1 );
		assert.ok( f.hasText( "Airdrop: 9000 m" ) );
		// The world map labels it: seven more gold glyphs than the line alone.
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyaleAirdrop: { state: "landed", x: gx + 240, z: gz, phase: 1, receivedAtMs: 0 }
		};
		step();
		const lineGlyphs = goldGlyphs( f );
		f.ui.event( { kind: "activate", id: "toggle-window:Map" } );
		step();
		assert.equal( f.ui.stats().panel, "Map" );
		assert.equal( airdropDiamonds( f, BATTLE_ROYALE_AIRDROP_MAP_PX ).length, 1, "the diamond on the world map" );
		assert.equal( goldGlyphs( f ) - lineGlyphs, "Airdrop".length, "the map label" );
		// Out of the match (the lobby) nothing is drawn.
		f.state.gameplay = { ...f.state.gameplay, battleRoyale: decodeState() };
		step();
		assert.equal( airdropDiamonds( f, BATTLE_ROYALE_AIRDROP_PX ).length, 0 );
		assert.equal( airdropDiamonds( f, BATTLE_ROYALE_AIRDROP_MAP_PX ).length, 0 );
		assert.ok( !f.hasText( "Airdrop: 24 m" ) );
	} finally {
		f.dispose();
	}
});
