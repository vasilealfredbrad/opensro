/*
===========================================================================

battle-royale-bot-settings.test.mjs - the Battle Royale bots, Stage 7: settings and map

Port-only, not native (docs/battle-royale.md, Stage 7). Crafted 0x3FB4
version 2 rosters and 0x3FB5 position frames go through the production
decoders, the worker owner and the gameplay owner; a GM's settings leave
as 0x7FB4 version 2 with the whole setting; the production HUD shows the
settings row to GM lobby members alone, clamps the count to 1..200, and
draws the living bots as red dots on the minimap and the world map.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const {
	battleRoyaleBotSwitchFrame,
	battleRoyaleClampBotCount,
	decodeBattleRoyaleBotPositions,
	decodeBattleRoyaleBots,
	decodeBattleRoyaleState,
	OP_BATTLE_ROYALE_BOT_POSITIONS,
	OP_BATTLE_ROYALE_BOT_SWITCH,
	OP_BATTLE_ROYALE_BOTS,
	OP_BATTLE_ROYALE_STATE
} = await import( "../../src/engine/foundation/gameplay/battle-royale.ts" );
const { createBattleRoyale } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/battle-royale/battle-royale.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	BATTLE_ROYALE_BOT_CONTROLS,
	BATTLE_ROYALE_BOT_DOT_PREFIX,
	BATTLE_ROYALE_BOT_DOT_PX,
	BATTLE_ROYALE_BOT_POSITIONS_STALE_MS,
	BATTLE_ROYALE_BOT_RGBA,
	BATTLE_ROYALE_BOTS_HINT_PREFIX,
	BATTLE_ROYALE_BOTS_ID,
	BATTLE_ROYALE_BOTS_WINDOW_ID,
	BATTLE_ROYALE_BOTS_WINDOW_PREFIX,
	BATTLE_ROYALE_GRID_UNIT,
	BATTLE_ROYALE_MAX_BOT_DOTS,
	battleRoyaleBotDots,
	battleRoyaleBotRequest,
	battleRoyaleShowsBots,
	minimapEllipse
} = await import( "../../src/engine/runtime/ui/hud/br-hud.ts" );

const FIXED_BYTES = 59;
// World grid units per region side.
const REGION_UNITS = 1920;

/*
================
stateFrame

A 0x3FB2 frame (docs/battle-royale.md "Wire"): a lobby member's lobby frame
unless fields say otherwise.
================
*/
function stateFrame( { state = 1, role = 1, members = 1 } = {} ) {
	const b = Buffer.alloc( FIXED_BYTES );
	b[0] = 1;
	b[1] = state;
	b[2] = role;
	b.writeFloatLE( 150, 12 );
	b[16] = members;
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
rosterFrame

A 0x3FB4 version 2 frame: version, botsEnabled, difficulty, configured
(u16), count (u16), then per bot gid (u32), nameLen (u16) and the name.
================
*/
/** @param {{ gid: number, name: string }[]} bots */
function rosterFrame( enabled, difficulty, configured, bots = [] ) {
	const head = Buffer.alloc( 7 );
	head[0] = 2;
	head[1] = enabled;
	head[2] = difficulty;
	head.writeUInt16LE( configured, 3 );
	head.writeUInt16LE( bots.length, 5 );
	const parts = [ head ];
	for ( const bot of bots ) {
		const name = Buffer.from( bot.name, "utf8" ), entry = Buffer.alloc( 6 );
		entry.writeUInt32LE( bot.gid, 0 );
		entry.writeUInt16LE( name.length, 4 );
		parts.push( entry, name );
	}
	return { opcode: OP_BATTLE_ROYALE_BOTS, payload: new Uint8Array( Buffer.concat( parts ) ) };
}

/*
================
positionsFrame

A 0x3FB5 frame: version, count (u16), then per bot gid (u32), x and z
(f32 grid units) and level (u8).
================
*/
/** @param {{ gid: number, x: number, z: number, level?: number }[]} bots */
function positionsFrame( bots, version = 1 ) {
	const b = Buffer.alloc( 3 + bots.length * 13 );
	b[0] = version;
	b.writeUInt16LE( bots.length, 1 );
	for ( const [index, bot] of bots.entries() ) {
		const at = 3 + index * 13;
		b.writeUInt32LE( bot.gid, at );
		b.writeFloatLE( bot.x, at + 4 );
		b.writeFloatLE( bot.z, at + 8 );
		b[at + 12] = bot.level ?? 1;
	}
	return { opcode: OP_BATTLE_ROYALE_BOT_POSITIONS, payload: new Uint8Array( b ) };
}

const ROSTER = [ { gid: 50, name: "Ares" }, { gid: 51, name: "Mira" } ];

/*
================
published

Bots as the worker publishes them, for HUD fixtures.
================
*/
function published( enabled, difficulty = 1, configured = 20, bots = ROSTER ) {
	return { enabled, difficulty, configured, bots, names: bots.map( bot => bot.name ), sequence: 1 };
}

test("a version 2 roster carries the settings and more than 255 bots", () => {
	assert.deepEqual( decodeBattleRoyaleBots( rosterFrame( 1, 2, 150, ROSTER ).payload ), {
		enabled: true,
		difficulty: 2,
		configured: 150,
		bots: ROSTER
	} );
	const many = Array.from( { length: 300 }, ( _, i ) => ({ gid: 1000 + i, name: "Bot" + i }) );
	assert.equal( decodeBattleRoyaleBots( rosterFrame( 1, 0, 200, many ).payload ).bots.length, 300 );
	const good = rosterFrame( 0, 1, 20, ROSTER ).payload;
	/** @type {[Uint8Array, RegExp][]} */
	const malformed = [
		[ Uint8Array.of( 2, 1, 1, 20 ), /Truncated/ ],
		[ rosterFrame( 1, 3, 20 ).payload, /Invalid battle royale bot difficulty/ ],
		[ rosterFrame( 1, 1, 0 ).payload, /Invalid battle royale bot count/ ],
		[ rosterFrame( 1, 1, 201 ).payload, /Invalid battle royale bot count/ ],
		[ rosterFrame( 2, 1, 20 ).payload, /Invalid battle royale bots switch/ ],
		[ good.subarray( 0, good.length - 1 ), /Truncated/ ],
		[ Uint8Array.of( ...good, 0 ), /Trailing/ ]
	];
	for ( const [payload, error] of malformed ) assert.throws( () => decodeBattleRoyaleBots( payload ), error );
});

test("the settings encode 0x7FB4 version 2 and settings the wire cannot carry throw", () => {
	assert.deepEqual( battleRoyaleBotSwitchFrame( { enabled: true, difficulty: 0, count: 1 } ), {
		opcode: OP_BATTLE_ROYALE_BOT_SWITCH,
		payload: Uint8Array.of( 1, 0, 1, 0 )
	} );
	assert.deepEqual(
		battleRoyaleBotSwitchFrame( { enabled: false, difficulty: 2, count: 200 } ).payload,
		Uint8Array.of( 0, 2, 200, 0 )
	);
	for ( const bad of [ { difficulty: 3, count: 20 }, { difficulty: 1, count: 0 }, { difficulty: 1, count: 201 } ] ) {
		assert.throws( () => battleRoyaleBotSwitchFrame( { enabled: true, ...bad } ), /Invalid/ );
	}
	assert.deepEqual( [ 0, 1, 57.4, 200, 999, NaN ].map( battleRoyaleClampBotCount ), [ 1, 1, 57, 200, 200, 20 ] );
});

test("0x3FB5 decodes the living bots and a malformed one is a protocol error", () => {
	const frame = positionsFrame( [ { gid: 50, x: 2000.5, z: -40, level: 7 }, { gid: 51, x: 0, z: 0, level: 30 } ] );
	assert.deepEqual( decodeBattleRoyaleBotPositions( frame.payload, 1234, 9 ), {
		bots: [ { gid: 50, x: 2000.5, z: -40, level: 7 }, { gid: 51, x: 0, z: 0, level: 30 } ],
		receivedAtMs: 1234,
		sequence: 9
	} );
	assert.deepEqual( decodeBattleRoyaleBotPositions( positionsFrame( [] ).payload, 0, 1 ).bots, [] );
	const good = frame.payload;
	/** @type {[Uint8Array, RegExp][]} */
	const malformed = [
		[ Uint8Array.of( 1, 0 ), /Truncated/ ],
		[ positionsFrame( [], 2 ).payload, /Unknown battle royale bot positions version/ ],
		[ good.subarray( 0, good.length - 1 ), /Truncated/ ],
		[ Uint8Array.of( ...good, 0 ), /Trailing/ ],
		[ positionsFrame( [ { gid: 0, x: 0, z: 0 } ] ).payload, /gid/ ],
		[ positionsFrame( [ { gid: 5, x: 0, z: 0 }, { gid: 5, x: 1, z: 1 } ] ).payload, /gid/ ],
		[ positionsFrame( [ { gid: 5, x: NaN, z: 0 } ] ).payload, /Invalid battle royale bot position/ ]
	];
	for ( const [payload, error] of malformed ) {
		assert.throws( () => decodeBattleRoyaleBotPositions( payload, 0, 1 ), error );
	}
});

test("the owner keeps the positions of a running match only", () => {
	const owner = createBattleRoyale(), frame = positionsFrame( [ { gid: 50, x: 10, z: 20 } ] );
	assert.equal( owner.receive( { opcode: 0x3fbe, payload: new Uint8Array() }, 0 ), false, "a foreign frame" );
	assert.throws( () => owner.receive( { opcode: OP_BATTLE_ROYALE_BOT_POSITIONS, payload: Uint8Array.of( 1 ) }, 0 ) );
	owner.receive( stateFrame(), 0 );
	assert.equal( owner.receive( frame, 10 ), true );
	assert.equal( owner.botPositions(), undefined, "not in the lobby" );
	owner.receive( stateFrame( { state: 3, role: 2 } ), 20 );
	owner.receive( frame, 30 );
	assert.deepEqual( owner.botPositions()?.bots, [ { gid: 50, x: 10, z: 20, level: 1 } ] );
	assert.equal( owner.botPositions()?.receivedAtMs, 30 );
	owner.receive( stateFrame( { state: 3, role: 3 } ), 40 );
	assert.ok( owner.botPositions(), "an eliminated participant keeps them" );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 50 );
	assert.equal( owner.botPositions(), undefined, "the match's end drops them" );
	owner.receive( stateFrame( { state: 3, role: 2 } ), 60 );
	owner.receive( frame, 70 );
	owner.clear();
	assert.equal( owner.botPositions(), undefined, "a new session" );
	// A roster frame keeps its settings through the match's end.
	owner.receive( stateFrame(), 80 );
	owner.receive( rosterFrame( 1, 2, 150, ROSTER ), 90 );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 100 );
	assert.deepEqual( [ owner.bots()?.enabled, owner.bots()?.difficulty, owner.bots()?.configured ], [
		false,
		2,
		150
	] );
});

test("the gameplay owner publishes the positions and sends a GM's whole setting", () => {
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	const sent = [], game = createGameplay( frame => sent.push( frame ) );
	try {
		game.bootstrap( { character: { gmPrivilege: true } } );
		game.seed( local );
		game.take();
		game.receive( stateFrame(), 100 );
		const before = sent.length;
		game.command( { kind: "battle-royale-bots", enabled: true, difficulty: 2, count: 150 }, 200, undefined, local );
		game.command( { kind: "battle-royale-bots", enabled: true, difficulty: 5, count: 150 }, 210, undefined, local );
		game.command( { kind: "battle-royale-bots", enabled: true, difficulty: 1, count: 500 }, 220, undefined, local );
		assert.deepEqual( sent.slice( before ), [ {
			opcode: OP_BATTLE_ROYALE_BOT_SWITCH,
			payload: Uint8Array.of( 1, 2, 150, 0 )
		} ], "Hard, 150 bots; settings the wire cannot carry stay home" );
		game.receive( stateFrame( { state: 3, role: 2 } ), 300 );
		assert.equal( game.receive( positionsFrame( [ { gid: 50, x: 1, z: 2, level: 4 } ] ), 400 ), true );
		const positions = game.take()?.battleRoyaleBotPositions;
		assert.deepEqual( positions?.bots, [ { gid: 50, x: 1, z: 2, level: 4 } ] );
		assert.equal( positions?.receivedAtMs, 400 );
	} finally {
		game.dispose();
	}
});

test("each settings control asks for the whole setting, the count clamped to 1..200", () => {
	const hint = key => BATTLE_ROYALE_BOTS_HINT_PREFIX + key, window = key => BATTLE_ROYALE_BOTS_WINDOW_PREFIX + key;
	assert.deepEqual( battleRoyaleBotRequest( BATTLE_ROYALE_BOTS_ID, published( true, 2, 150 ) ), {
		enabled: false,
		difficulty: 2,
		count: 150
	} );
	assert.deepEqual( battleRoyaleBotRequest( BATTLE_ROYALE_BOTS_WINDOW_ID, undefined ), {
		enabled: true,
		difficulty: 1,
		count: 20
	}, "before any roster: off at the defaults" );
	assert.deepEqual( battleRoyaleBotRequest( hint( "difficulty:2" ), published( true ) ), {
		enabled: true,
		difficulty: 2,
		count: 20
	} );
	assert.deepEqual( battleRoyaleBotRequest( window( "difficulty:0" ), published( false ) ), {
		enabled: false,
		difficulty: 0,
		count: 20
	} );
	assert.equal( battleRoyaleBotRequest( hint( "step:10" ), published( true, 1, 195 ) )?.count, 200 );
	assert.equal( battleRoyaleBotRequest( hint( "step:-10" ), published( true, 1, 5 ) )?.count, 1 );
	assert.equal( battleRoyaleBotRequest( window( "step:-1" ), published( true, 1, 1 ) )?.count, 1 );
	assert.equal( battleRoyaleBotRequest( window( "step:1" ), published( true, 1, 20 ) )?.count, 21 );
	assert.equal( battleRoyaleBotRequest( window( "preset:100" ), published( true, 1, 7 ) )?.count, 100 );
	assert.equal( battleRoyaleBotRequest( hint( "value" ), published( true ) ), undefined, "the count slot" );
	assert.equal( battleRoyaleBotRequest( hint( "step:7" ), published( true ) ), undefined );
	assert.equal( battleRoyaleBotRequest( "battle-royale-build:11", published( true ) ), undefined );
	assert.deepEqual(
		BATTLE_ROYALE_BOT_CONTROLS.filter( control => !control.value ).map( control => control.caption ),
		[ "Easy", "Normal", "Hard", "-10", "-1", "+1", "+10", "20", "50", "100", "200" ]
	);
});

/*
================
lobbyHud

A production HUD stepped as a lobby member; gm sets the entry eligibility.
Returns the fixture, a stepper (the last publication's controls) and the
commands it sent.
================
*/
function lobbyHud( gm, bots = published( true ) ) {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	let now = 0;
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */
	let presentation = null;
	Object.assign( f.state.gameplay, {
		battleRoyale: decodeState(),
		battleRoyaleBots: bots,
		eligibility: { gm, pcRoomEvent: false }
	} );
	const step = () => {
		for ( let i = 0; i < 16; i++ ) presentation = f.ui.step( f.state, now += 100 ) ?? presentation;
		return new Map( (presentation?.controls ?? []).map( control => [ control.id, control ] ) );
	};
	return { f, sent, step };
}

test("only a GM lobby member gets the settings row; a press sends the whole setting", () => {
	const player = lobbyHud( false, published( true, 2, 150 ) );
	try {
		const controls = player.step();
		assert.ok(
			![ ...controls.keys() ].some( id =>
				id.startsWith( BATTLE_ROYALE_BOTS_HINT_PREFIX ) || id.startsWith( BATTLE_ROYALE_BOTS_WINDOW_PREFIX )
			),
			"a player gets no settings"
		);
		assert.ok( player.f.hasText( "Bots: 150 (Hard)" ), "but reads them" );
		player.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_HINT_PREFIX + "difficulty:0" } );
		assert.ok( !player.sent.some( c => c.kind === "gameplay" && c.command.kind === "battle-royale-bots" ) );
	} finally {
		player.f.dispose();
	}
	const gm = lobbyHud( true, published( true, 1, 50 ) );
	try {
		const controls = gm.step();
		for ( const prefix of [ BATTLE_ROYALE_BOTS_HINT_PREFIX, BATTLE_ROYALE_BOTS_WINDOW_PREFIX ] ) {
			for ( const control of BATTLE_ROYALE_BOT_CONTROLS.filter( c => !c.value ) ) {
				assert.ok( controls.has( prefix + control.key ), prefix + control.key );
			}
			assert.equal( controls.get( prefix + "difficulty:1" )?.selected, true, "Normal is selected" );
			assert.equal( controls.get( prefix + "difficulty:2" )?.selected, false );
			assert.equal( controls.get( prefix + "preset:50" )?.selected, true, "so is the echoed preset" );
		}
		assert.ok( gm.f.hasText( "50" ), "the count shows" );
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_WINDOW_PREFIX + "difficulty:2" } );
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_HINT_PREFIX + "step:-10" } );
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_HINT_PREFIX + "preset:200" } );
		assert.deepEqual(
			gm.sent.filter( c => c.kind === "gameplay" && c.command.kind === "battle-royale-bots" ).map( c =>
				c.command
			),
			[
				{ kind: "battle-royale-bots", enabled: true, difficulty: 2, count: 50 },
				{ kind: "battle-royale-bots", enabled: true, difficulty: 1, count: 40 },
				{ kind: "battle-royale-bots", enabled: true, difficulty: 1, count: 200 }
			],
			"each press asks from the echoed setting"
		);
		// The server's echo moves the selection.
		gm.f.state.gameplay = { ...gm.f.state.gameplay, battleRoyaleBots: published( true, 2, 200 ) };
		const echoed = gm.step();
		assert.equal( echoed.get( BATTLE_ROYALE_BOTS_HINT_PREFIX + "difficulty:2" )?.selected, true );
		assert.equal( echoed.get( BATTLE_ROYALE_BOTS_HINT_PREFIX + "preset:200" )?.selected, true );
		gm.f.state.gameplay = { ...gm.f.state.gameplay, battleRoyale: decodeState( { state: 3, role: 2 } ) };
		const running = gm.step();
		assert.ok( ![ ...running.keys() ].some( id => id.startsWith( BATTLE_ROYALE_BOTS_HINT_PREFIX ) ) );
	} finally {
		gm.f.dispose();
	}
});

test("the bot dots share the rings' projection, stay in the clip and are capped", () => {
	const pose = { regionId: 0x5a74, x: 1710, z: 330 };
	const local = { x: (0x74 & 255) * REGION_UNITS + 1710, z: (0x5a74 >> 8) * REGION_UNITS + 330 };
	const origin = minimapEllipse( pose, BATTLE_ROYALE_GRID_UNIT, 96, [ 50, 50 ] );
	assert.ok( origin );
	if ( !origin ) return;
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 100, 100 ];
	const positions = {
		bots: [
			{ gid: 50, x: local.x + 150, z: local.z, level: 1 },
			{ gid: 51, x: local.x, z: local.z + 200, level: 1 },
			{ gid: 52, x: local.x + 100000, z: local.z, level: 1 }
		],
		receivedAtMs: 0,
		sequence: 1
	};
	const dots = battleRoyaleBotDots( origin, positions, published( true ), clip );
	assert.equal( dots.length, 2, "the far bot is outside the radar" );
	// The same point as the lobby ring test: 15 m east is 7.5 px at 96 px a region.
	assert.ok( Math.abs( dots[0].x - 57.5 ) < 1e-6 && Math.abs( dots[0].y - 50 ) < 1e-6 );
	assert.ok( Math.abs( dots[1].x - 50 ) < 1e-6 && Math.abs( dots[1].y - 40 ) < 1e-6, "north is up" );
	assert.deepEqual( dots.map( dot => dot.name ), [ "Ares", "Mira" ] );
	const crowd = {
		bots: Array.from( { length: 250 }, ( _, i ) => ({ gid: 100 + i, x: local.x, z: local.z, level: 1 }) ),
		receivedAtMs: 0,
		sequence: 2
	};
	assert.equal( battleRoyaleBotDots( origin, crowd, undefined, clip ).length, BATTLE_ROYALE_MAX_BOT_DOTS );
	const running = decodeState( { state: 3, role: 2 } );
	assert.equal( battleRoyaleShowsBots( running, positions, 1000 ), true );
	assert.equal( battleRoyaleShowsBots( decodeState( { state: 3, role: 3 } ), positions, 1000 ), true, "a spectator" );
	assert.equal( battleRoyaleShowsBots( decodeState( { state: 3, role: 0 } ), positions, 1000 ), false, "outside" );
	assert.equal( battleRoyaleShowsBots( decodeState(), positions, 1000 ), false, "the lobby" );
	assert.equal( battleRoyaleShowsBots( running, positions, BATTLE_ROYALE_BOT_POSITIONS_STALE_MS ), false, "stale" );
});

/*
================
botDots

The bot dots in the last scene drawn: untextured quads of the dot's size
in the bot red.
================
*/
function botDots( f ) {
	return f.scenes.at( -1 ).quads.filter( q =>
		q.texture === "" && q.rect[2] === BATTLE_ROYALE_BOT_DOT_PX &&
		q.color.join() === BATTLE_ROYALE_BOT_RGBA.join()
	);
}

test("a participant's minimap and world map draw the living bots, named on the map", () => {
	const f = uiFixture();
	try {
		let now = 0;
		const step = () => {
			for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		};
		// The player stands at region 0x5A74's corner, a page of the world map.
		const region = 0x5a74, gx = (region & 255) * REGION_UNITS, gz = (region >> 8) * REGION_UNITS;
		f.state.entities[0] = { ...f.state.entities[0], regionId: region };
		f.state.gameplay.pose = { ...f.state.gameplay.pose, regionId: region };
		const near = [ { gid: 50, x: gx + 40, z: gz + 40, level: 3 }, { gid: 51, x: gx - 40, z: gz - 40, level: 9 } ];
		Object.assign( f.state.gameplay, {
			battleRoyale: decodeState( { state: 3, role: 2 } ),
			battleRoyaleBots: published( true ),
			battleRoyaleBotPositions: { bots: near, receivedAtMs: 0, sequence: 1 }
		} );
		step();
		assert.equal( botDots( f ).length, 2, "two dots on the minimap" );
		// Out of the match (the lobby) nothing is drawn.
		f.state.gameplay = { ...f.state.gameplay, battleRoyale: decodeState() };
		step();
		assert.equal( botDots( f ).length, 0 );
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyale: decodeState( { state: 3, role: 3 }, now ),
			battleRoyaleBotPositions: { bots: near, receivedAtMs: now, sequence: 2 }
		};
		f.ui.event( { kind: "activate", id: "toggle-window:Map" } );
		step();
		assert.equal( f.ui.stats().panel, "Map" );
		assert.equal( botDots( f ).length, 4, "the minimap's and the world map's" );
		assert.ok( f.hasText( "Ares" ) && f.hasText( "Mira" ), "few bots: every name shows" );
		// Many bots: only the hovered dot is named.
		const crowd = Array.from( { length: 40 }, ( _, i ) => ({ gid: 100 + i, x: gx + i, z: gz + i, level: 1 }) );
		f.state.gameplay = {
			...f.state.gameplay,
			battleRoyaleBots: published( true, 1, 40, crowd.map( bot => ({ gid: bot.gid, name: "Bot" + bot.gid }) ) ),
			battleRoyaleBotPositions: { bots: crowd, receivedAtMs: now, sequence: 3 }
		};
		step();
		assert.ok( !f.hasText( "Bot105" ) );
		f.ui.event( { kind: "hover", id: BATTLE_ROYALE_BOT_DOT_PREFIX + 105 } );
		step();
		assert.ok( f.hasText( "Bot105" ), "the hovered bot's name" );
		assert.ok( !f.hasText( "Bot106" ) );
	} finally {
		f.dispose();
	}
});
