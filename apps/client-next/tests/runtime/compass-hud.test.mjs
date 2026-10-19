/*
===========================================================================

compass-hud.test.mjs - the compass strip (Battle Royale Stage 9)

Port-only, not native (docs/battle-royale.md, Stage 9 item 3). The
production compass owner turns the camera's yaw (or the character's
heading) into a bearing, lays out the 15-degree ticks and the eight
direction labels around it, and reads the position in the game's own X/Y
metres; the production HUD draws it for Battle Royale lobby members and
participants, and everywhere only with the Experimental row on.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const {
	COMPASS_PX_PER_DEGREE,
	COMPASS_WIDTH,
	compassBearing,
	compassLabel,
	compassMarks,
	compassReadout,
	compassShown,
	createCompassHud
} = await import( "../../src/engine/runtime/ui/hud/compass-hud.ts" );
const { decodeBattleRoyaleState } = await import( "../../src/engine/foundation/gameplay/battle-royale.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/*
================
close
================
*/
function close( actual, expected, label = "" ) {
	// A wire heading is 1/65535 of a turn, so half a turn is 180.0027 degrees.
	assert.ok( Math.abs( actual - expected ) < 1e-2, `${label} ${actual} != ${expected}` );
}

/*
================
lobbyState

A Battle Royale lobby member's decoded 0x3FB2 frame.
================
*/
function lobbyState() {
	const b = Buffer.alloc( 59 );
	b[0] = 1;
	b[1] = 1;
	b[2] = 1;
	b.writeFloatLE( 150, 12 );
	b[16] = 1;
	b[23] = 5;
	return decodeBattleRoyaleState( new Uint8Array( b ), 0, 1 );
}

test("the bearing follows the camera, or the character's heading without one", () => {
	// The camera sits at (sin yaw, cos yaw) from the player and looks back.
	close( compassBearing( Math.PI, undefined ), 0, "camera south of the player looks north" );
	close( compassBearing( 0, undefined ), 180, "camera north looks south" );
	close( compassBearing( -Math.PI / 2, undefined ), 90, "camera west looks east" );
	close( compassBearing( Math.PI / 2 + 4 * Math.PI, undefined ), 270, "whole turns wrap" );
	close( compassBearing( 0, 32768 ), 180, "the camera wins over the heading" );
	// The wire heading turns counter-clockwise from east.
	close( compassBearing( undefined, 0 ), 90, "east" );
	close( compassBearing( undefined, 32768 ), 270, "west" );
	close( compassBearing( NaN, 0 ), 90, "no usable camera" );
	assert.equal( compassBearing( undefined, undefined ), 0 );
});

test("labels and ticks sit where the bearing puts them", () => {
	const at = ( bearing, label ) =>
		compassMarks( bearing, COMPASS_WIDTH, COMPASS_PX_PER_DEGREE ).find( mark => mark.label === label )?.offset;
	// Facing north: W at the left edge, N centred, E at the right edge.
	const north = compassMarks( 0, COMPASS_WIDTH, COMPASS_PX_PER_DEGREE );
	assert.equal( north.length, 13, "every 15 degrees across 180" );
	assert.deepEqual( north.filter( mark => mark.label ).map( mark => [ mark.label, mark.offset ] ), [
		[ "W", 0 ],
		[ "NW", 90 ],
		[ "N", 180 ],
		[ "NE", 270 ],
		[ "E", 360 ]
	] );
	assert.deepEqual( north.slice( 0, 3 ).map( mark => mark.degrees ), [ 270, 285, 300 ] );
	assert.equal( north[1].label, "", "a minor tick" );
	// Turned to 100: E 10 degrees left of centre.
	assert.equal( at( 100, "E" ), 160 );
	assert.equal( at( 100, "SE" ), 250 );
	assert.equal( at( 100, "S" ), 340 );
	assert.equal( at( 100, "N" ), undefined, "behind the player" );
	// Across north from 350: N 10 degrees right of centre.
	assert.equal( at( 350, "N" ), 200 );
	assert.equal( at( 350, "NW" ), 110 );
	// Between ticks every mark keeps the pitch.
	const odd = compassMarks( 7.5, COMPASS_WIDTH, COMPASS_PX_PER_DEGREE );
	for ( let i = 1; i < odd.length; i++ ) assert.equal( odd[i].offset - odd[i - 1].offset, 30 );
	assert.equal( at( 7.5, "N" ), 165 );
	assert.equal( compassLabel( 22 ), "N" );
	assert.equal( compassLabel( 23 ), "NE" );
	assert.equal( compassLabel( 200 ), "S" );
	assert.equal( compassLabel( 338 ), "N" );
	assert.equal( compassLabel( -40 ), "NW" );
});

test("the readout shows the bearing and the game's X/Y metres", () => {
	// X = (rx - 135) * 192 + lx / 10, Y = (ry - 92) * 192 + lz / 10.
	const centre = { regionId: (92 << 8) | 135, x: 1234, z: 567 };
	assert.equal( compassReadout( 0, centre ), "N 0  X:123  Y:56" );
	const southWest = { regionId: (90 << 8) | 134, x: 50, z: 1910 };
	assert.equal( compassReadout( 225.4, southWest ), "SW 225  X:-187  Y:-193" );
	const roc = { regionId: 0x5a6f, x: 960, z: 960 };
	assert.equal(
		compassReadout( 359.6, roc ),
		`N 0  X:${(0x6f - 135) * 192 + 96}  Y:${(0x5a - 92) * 192 + 96}`,
		"359.6 rounds to 0"
	);
	assert.equal( compassReadout( 90, { regionId: 0x8001, x: 10, z: 10 } ), "E 90", "a dungeon has no grid" );
	assert.equal( compassReadout( 90, undefined ), "E 90" );
});

test("the strip shows in Battle Royale, everywhere only when asked, and redraws on a turn", () => {
	assert.equal( compassShown( true, true, false ), true );
	assert.equal( compassShown( true, false, false ), false, "native default" );
	assert.equal( compassShown( true, false, true ), true, "Compass everywhere" );
	assert.equal( compassShown( false, true, true ), false, "outside the world" );
	assert.equal( experimentalOptions().compassEverywhere, false );
	assert.equal( experimentalOptions( { compassEverywhere: 1 } ).compassEverywhere, false );
	assert.equal( experimentalOptions( { compassEverywhere: true } ).compassEverywhere, true );
	const hud = createCompassHud();
	assert.equal( hud.redraw( true, 10.2 ), true, "appeared" );
	assert.equal( hud.redraw( true, 10.4 ), false, "the same degree" );
	assert.equal( hud.redraw( true, 11 ), true );
	assert.equal( hud.redraw( false, 11 ), true, "went" );
	assert.equal( hud.redraw( false, 50 ), false );
	hud.below( 40 );
	hud.below( 72 );
	assert.equal( hud.top( 0 ), 72, "under the lowest line" );
	assert.equal( hud.top( 4 ), 4, "each frame starts again" );
	hud.below( 10 );
	assert.equal( hud.top( 60 ), 60 );
});

test("the HUD draws the compass for a lobby member and, with the row on, for anyone", () => {
	const f = uiFixture();
	try {
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 12; i++ ) f.ui.step( f.state, now += 100 );
		};
		// The fixture's pose: region 1 (rx 1, ry 0), local (0, 0), heading east.
		const position = `X:${(1 - 135) * 192}  Y:${(0 - 92) * 192}`;
		step();
		assert.ok( !f.hasText( position ), "off by default outside Battle Royale" );
		f.state.gameplay = { ...f.state.gameplay, battleRoyale: lobbyState() };
		step();
		assert.ok( f.hasText( "E 90  " + position ), "the heading stands in for the camera" );
		Object.assign( f.state, { cameraYaw: Math.PI } );
		step();
		assert.ok( f.hasText( "N 0  " + position ), "the camera turned the strip" );
		assert.ok( f.hasText( "NW" ) && f.hasText( "NE" ) );
		f.state.gameplay = { ...f.state.gameplay, battleRoyale: undefined };
		step();
		assert.ok( !f.hasText( "N 0  " + position ) );
		f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { compassEverywhere: true } ) } );
		step();
		assert.ok( f.hasText( "N 0  " + position ), "Compass everywhere" );
	} finally {
		f.dispose();
	}
});
