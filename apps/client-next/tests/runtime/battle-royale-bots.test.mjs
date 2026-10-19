/*
===========================================================================

battle-royale-bots.test.mjs - the Battle Royale bots: roster, GM switch, red names

Port-only, not native (docs/battle-royale.md, Stage 5; the Stage 7 settings
and map are in battle-royale-bot-settings.test.mjs). Crafted 0x3FB4
rosters go through the production decoder, the worker owner and the
gameplay owner; the GM switch (0x7FB4) leaves only from a GM lobby member;
the production HUD shows the switch to GMs alone, a bot's nameplate and
target window under its roster name in red, and a bot's results row in red.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const {
	battleRoyaleBotEntity,
	battleRoyaleBotName,
	battleRoyaleBotSwitchFrame,
	battleRoyaleCanSwitchBots,
	battleRoyaleHudText,
	battleRoyaleIsBotName,
	decodeBattleRoyaleBots,
	decodeBattleRoyaleScoreboard,
	decodeBattleRoyaleState,
	OP_BATTLE_ROYALE_BOT_SWITCH,
	OP_BATTLE_ROYALE_BOTS,
	OP_BATTLE_ROYALE_SCOREBOARD,
	OP_BATTLE_ROYALE_STATE
} = await import( "../../src/engine/foundation/gameplay/battle-royale.ts" );
const { createBattleRoyale } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/battle-royale/battle-royale.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	BATTLE_ROYALE_BOT_RGBA,
	BATTLE_ROYALE_BOTS_ID,
	BATTLE_ROYALE_BOTS_WINDOW_ID,
	BATTLE_ROYALE_RESULTS_PANEL
} = await import( "../../src/engine/runtime/ui/hud/br-hud.ts" );

const FIXED_BYTES = 59;

/*
================
stateFrame

A 0x3FB2 frame (docs/battle-royale.md "Wire"): a lobby member's lobby frame
unless fields say otherwise.
================
*/
function stateFrame( { state = 1, role = 1, members = 1, stage = 0, winner = "" } = {} ) {
	const name = Buffer.from( winner, "utf8" ), b = Buffer.alloc( FIXED_BYTES + name.length );
	b[0] = 1;
	b[1] = state;
	b[2] = role;
	b.writeFloatLE( 150, 12 );
	b[16] = members;
	b[23] = 5;
	b[24] = stage;
	b.writeUInt16LE( name.length, 57 );
	name.copy( b, FIXED_BYTES );
	return { opcode: OP_BATTLE_ROYALE_STATE, payload: new Uint8Array( b ) };
}

/*
================
decodeState
================
*/
function decodeState( fields ) {
	const state = decodeBattleRoyaleState( stateFrame( fields ).payload, 0, 1 );
	if ( !state ) throw new Error( "no match" );
	return state;
}

/*
================
botsFrame

A 0x3FB4 frame: version, botsEnabled, count, then per bot gid (u32),
nameLen (u16) and the UTF-8 name.
================
*/
/** @param {{ gid: number, name: string }[]} bots */
function botsFrame( enabled, bots = [], version = 1 ) {
	const parts = [ Buffer.from( [ version, enabled, bots.length ] ) ];
	for ( const bot of bots ) {
		const name = Buffer.from( bot.name, "utf8" ), head = Buffer.alloc( 6 );
		head.writeUInt32LE( bot.gid, 0 );
		head.writeUInt16LE( name.length, 4 );
		parts.push( head, name );
	}
	return { opcode: OP_BATTLE_ROYALE_BOTS, payload: new Uint8Array( Buffer.concat( parts ) ) };
}

/*
================
scoreFrame
================
*/
/** @param {{ name: string, kills: number, placement: number, self?: boolean }[]} entries */
function scoreFrame( entries, final = 1 ) {
	const parts = [ Buffer.from( [ 1, final, entries.length ] ) ];
	for ( const entry of entries ) {
		const name = Buffer.from( entry.name, "utf8" ), head = Buffer.alloc( 2 ), tail = Buffer.alloc( 4 );
		head.writeUInt16LE( name.length, 0 );
		tail.writeUInt16LE( entry.kills, 0 );
		tail[2] = entry.placement;
		tail[3] = entry.self ? 1 : 0;
		parts.push( head, name, tail );
	}
	return { opcode: OP_BATTLE_ROYALE_SCOREBOARD, payload: new Uint8Array( Buffer.concat( parts ) ) };
}

const ROSTER = [ { gid: 50, name: "Ares" }, { gid: 51, name: "Mira" } ];
// The settings a GM sends: on, normal, 20 bots.
const SETTINGS = { enabled: true, difficulty: 1, count: 20 };

/*
================
published

Bots as the worker publishes them, for HUD fixtures.
================
*/
function published( enabled, bots = ROSTER ) {
	return { enabled, difficulty: 1, configured: 20, bots, names: bots.map( bot => bot.name ), sequence: 1 };
}

test("the bot roster round-trips and a malformed one is a protocol error", () => {
	// A stage-5 roster reads as normal difficulty and the default count.
	assert.deepEqual( decodeBattleRoyaleBots( botsFrame( 1, ROSTER ).payload ), {
		enabled: true,
		difficulty: 1,
		configured: 20,
		bots: ROSTER
	} );
	assert.deepEqual( decodeBattleRoyaleBots( botsFrame( 0 ).payload ), {
		enabled: false,
		difficulty: 1,
		configured: 20,
		bots: []
	} );
	assert.deepEqual( decodeBattleRoyaleBots( botsFrame( 1, [ { gid: 9, name: "Ölaf" } ] ).payload ).bots, [ {
		gid: 9,
		name: "Ölaf"
	} ] );
	const good = botsFrame( 1, ROSTER ).payload;
	/** @type {[Uint8Array, RegExp][]} */
	const malformed = [
		[ Uint8Array.of( 1, 0 ), /Truncated/ ],
		[ botsFrame( 1, [], 3 ).payload, /Unknown battle royale bots version/ ],
		[ botsFrame( 2 ).payload, /Invalid battle royale bots switch/ ],
		[ good.subarray( 0, good.length - 1 ), /Truncated/ ],
		[ Uint8Array.of( ...good, 0 ), /Trailing/ ],
		[ botsFrame( 1, [ { gid: 0, name: "Zero" } ] ).payload, /Invalid battle royale bot gid/ ],
		[ botsFrame( 1, [ ROSTER[0], { gid: 50, name: "Twin" } ] ).payload, /Invalid battle royale bot gid/ ],
		[ botsFrame( 1, [ { gid: 5, name: "" } ] ).payload, /Invalid battle royale bot name length/ ],
		[ botsFrame( 1, [ { gid: 5, name: "x".repeat( 65 ) } ] ).payload, /Invalid battle royale bot name length/ ],
		[ Uint8Array.of( 1, 1, 1, 5, 0, 0, 0, 1, 0, 0xff ), /./ ]
	];
	for ( const [payload, error] of malformed ) {
		assert.throws( () => decodeBattleRoyaleBots( payload ), error );
	}
});

test("the owner keeps the roster and this match's bot names until the next lobby", () => {
	const owner = createBattleRoyale();
	assert.equal( owner.receive( { opcode: 0x3fbe, payload: new Uint8Array() }, 0 ), false, "a foreign frame" );
	assert.throws( () => owner.receive( { opcode: OP_BATTLE_ROYALE_BOTS, payload: Uint8Array.of( 1 ) }, 0 ) );
	owner.receive( stateFrame(), 0 );
	assert.equal( owner.receive( botsFrame( 1 ), 0 ), true );
	assert.equal( owner.bots()?.enabled, true );
	owner.receive( stateFrame( { state: 3, role: 2, members: 1 } ), 10 );
	owner.receive( botsFrame( 1, ROSTER ), 20 );
	assert.deepEqual( owner.bots()?.bots, ROSTER );
	// Ares dies: the roster drops it, its scoreboard row still names a bot.
	owner.receive( botsFrame( 1, [ ROSTER[1] ] ), 30 );
	assert.deepEqual( owner.bots()?.bots, [ ROSTER[1] ] );
	assert.deepEqual( owner.bots()?.names, [ "Ares", "Mira" ] );
	owner.receive( stateFrame( { state: 4, role: 3, winner: "Mira" } ), 40 );
	assert.deepEqual( owner.bots()?.names, [ "Ares", "Mira" ], "the results still mark both" );
	// State 0 ends the match: no roster or switch, the names stay with the results.
	owner.receive( stateFrame( { state: 0, role: 0 } ), 45 );
	const ended = owner.bots();
	assert.deepEqual( [ ended?.enabled, ended?.bots, ended?.names ], [ false, [], [ "Ares", "Mira" ] ] );
	// The next match's lobby keeps the switch but not the last match's names.
	owner.receive( botsFrame( 1 ), 50 );
	owner.receive( stateFrame(), 60 );
	assert.deepEqual( owner.bots()?.names, [] );
	assert.equal( owner.bots()?.enabled, true );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 70 );
	assert.equal( owner.bots(), undefined, "no names to keep: nothing left" );
	owner.receive( stateFrame(), 80 );
	owner.receive( botsFrame( 0, ROSTER ), 90 );
	owner.clear();
	assert.equal( owner.bots(), undefined, "a new session" );
});

test("the bot switch encodes 0x7FB4 and leaves only from a GM lobby member", () => {
	assert.deepEqual( battleRoyaleBotSwitchFrame( SETTINGS ), {
		opcode: OP_BATTLE_ROYALE_BOT_SWITCH,
		payload: Uint8Array.of( 1, 1, 20, 0 )
	} );
	assert.deepEqual(
		battleRoyaleBotSwitchFrame( { ...SETTINGS, enabled: false } ).payload,
		Uint8Array.of( 0, 1, 20, 0 )
	);
	assert.equal( battleRoyaleCanSwitchBots( undefined, true ), false );
	assert.equal( battleRoyaleCanSwitchBots( decodeState(), false ), false, "not a GM" );
	assert.equal( battleRoyaleCanSwitchBots( decodeState(), true ), true );
	assert.equal( battleRoyaleCanSwitchBots( decodeState( { state: 2 } ), true ), true, "the countdown" );
	assert.equal( battleRoyaleCanSwitchBots( decodeState( { role: 0 } ), true ), false, "outside the circle" );
	assert.equal( battleRoyaleCanSwitchBots( decodeState( { state: 3, role: 2 } ), true ), false, "running" );
	const owner = createBattleRoyale();
	assert.equal( owner.switchBots( SETTINGS, true ), null, "no match" );
	owner.receive( stateFrame(), 0 );
	assert.equal( owner.switchBots( SETTINGS, false ), null, "not a GM" );
	assert.deepEqual( owner.switchBots( SETTINGS, true )?.payload, Uint8Array.of( 1, 1, 20, 0 ) );
});

test("the gameplay owner publishes the bots and sends a GM's switch only", () => {
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	for ( const gm of [ false, true ] ) {
		const sent = [], game = createGameplay( frame => sent.push( frame ) );
		game.bootstrap( { character: { gmPrivilege: gm } } );
		game.seed( local );
		game.take();
		game.receive( stateFrame(), 100 );
		assert.equal( game.receive( botsFrame( 1, ROSTER ), 100 ), true );
		assert.deepEqual( game.take()?.battleRoyaleBots?.bots, ROSTER );
		const before = sent.length;
		game.command( { kind: "battle-royale-bots", ...SETTINGS, enabled: false }, 200, undefined, local );
		assert.deepEqual(
			sent.slice( before ),
			gm ? [ { opcode: OP_BATTLE_ROYALE_BOT_SWITCH, payload: Uint8Array.of( 0, 1, 20, 0 ) } ] : [],
			gm ? "a GM's switch" : "a player's is never sent"
		);
		game.receive( stateFrame( { state: 0, role: 0 } ), 300 );
		const ended = game.take()?.battleRoyaleBots;
		assert.deepEqual( [ ended?.enabled, ended?.bots ], [ false, [] ], "the match's end empties the roster" );
		game.reset();
		assert.equal( game.take()?.battleRoyaleBots, undefined, "a new session forgets the bots" );
		game.dispose();
	}
});

test("the lobby line counts the bots and a switched-on lobby needs one player", () => {
	const lobby = decodeState();
	assert.equal(
		battleRoyaleHudText( lobby, 0, 0, published( true ) ).lobby,
		"Battle Royale - waiting for players: 1/1 - Bots: 20 (Normal) - Build: Default (Warrior 2H)"
	);
	assert.equal(
		battleRoyaleHudText( lobby, 0, 0, published( false ) ).lobby,
		"Battle Royale - waiting for players: 1/2 - Build: Default (Warrior 2H)"
	);
	assert.match(
		battleRoyaleHudText( decodeState( { state: 2 } ), 0, 0, published( true ) ).lobby,
		/Bots: 20 \(Normal\)/
	);
});

test("a bot entity wears its roster name; anything else stays as given", () => {
	const bots = published( true ), monster = { gid: 50, kind: "monster", name: "Bandit" };
	assert.equal( battleRoyaleBotName( bots, 50 ), "Ares" );
	assert.equal( battleRoyaleBotName( bots, 7 ), undefined );
	assert.equal( battleRoyaleBotName( undefined, 50 ), undefined );
	assert.deepEqual( battleRoyaleBotEntity( monster, bots ), { ...monster, name: "Ares" } );
	const other = { ...monster, gid: 52 };
	assert.equal( battleRoyaleBotEntity( other, bots ), other );
	assert.equal( battleRoyaleIsBotName( bots, "Mira" ), true );
	assert.equal( battleRoyaleIsBotName( bots, "test" ), false );
	assert.equal( battleRoyaleIsBotName( undefined, "Mira" ), false );
});

/*
================
lobbyHud

A production HUD stepped as a lobby member with the bots switched on; gm
sets the entry eligibility. Returns the fixture, a stepper, the commands
it sent and the last publication's control ids.
================
*/
function lobbyHud( gm ) {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	let now = 0;
	const seen = {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
	};
	Object.assign( f.state.gameplay, {
		battleRoyale: decodeState(),
		battleRoyaleBots: published( true, [] ),
		eligibility: { gm, pcRoomEvent: false }
	} );
	const step = () => {
		for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
		return new Set( seen.presentation?.controls.map( control => control.id ) ?? [] );
	};
	return { f, sent, step, seen };
}

test("only a GM lobby member sees the bot switch, and it asks for the other setting", () => {
	const player = lobbyHud( false );
	try {
		const ids = player.step();
		assert.equal( player.f.ui.stats().panel, "Battle Royale" );
		assert.ok(
			!ids.has( BATTLE_ROYALE_BOTS_ID ) && !ids.has( BATTLE_ROYALE_BOTS_WINDOW_ID ),
			"a player gets no switch"
		);
		assert.ok( player.f.hasText( "Bots: 20 (Normal)" ), "but reads the bot settings" );
		player.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_ID } );
		assert.ok( !player.sent.some( c => c.kind === "gameplay" && c.command.kind === "battle-royale-bots" ) );
	} finally {
		player.f.dispose();
	}
	const gm = lobbyHud( true );
	try {
		let ids = gm.step();
		assert.ok( ids.has( BATTLE_ROYALE_BOTS_WINDOW_ID ), "in the picker" );
		assert.ok( ids.has( BATTLE_ROYALE_BOTS_ID ), "and on the lobby hint" );
		assert.ok( gm.f.hasText( "Bots: ON" ) );
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_WINDOW_ID } );
		assert.ok(
			gm.sent.some( c =>
				c.kind === "gameplay" && c.command.kind === "battle-royale-bots" && c.command.enabled === false
			)
		);
		gm.sent.length = 0;
		gm.f.ui.event( { kind: "activate", id: "close-window:Battle Royale" } );
		ids = gm.step();
		assert.equal( gm.f.ui.stats().panel, "" );
		assert.ok( ids.has( BATTLE_ROYALE_BOTS_ID ) && !ids.has( BATTLE_ROYALE_BOTS_WINDOW_ID ) );
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_ID } );
		assert.ok(
			gm.sent.some( c =>
				c.kind === "gameplay" && c.command.kind === "battle-royale-bots" && c.command.enabled === false
			)
		);
		gm.f.state.gameplay = { ...gm.f.state.gameplay, battleRoyale: decodeState( { state: 3, role: 2 } ) };
		ids = gm.step();
		assert.ok( !ids.has( BATTLE_ROYALE_BOTS_ID ), "no switch once the match runs" );
		gm.sent.length = 0;
		gm.f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BOTS_ID } );
		assert.equal( gm.sent.filter( c => c.kind === "gameplay" ).length, 0, "nor a stale press" );
	} finally {
		gm.f.dispose();
	}
});

/*
================
redGlyphs

The font quads anchored on gid in the last scene drawn in the bot red.
================
*/
function redGlyphs( f, gid ) {
	return f.scenes.at( -1 ).quads.filter( q =>
		q.texture === fontAtlas.image && (gid === undefined || q.characterAnchor === gid) &&
		q.color.join() === BATTLE_ROYALE_BOT_RGBA.join()
	);
}

test("a bot's nameplate and target window show its roster name, the plate in red", () => {
	const f = uiFixture();
	try {
		/** @type {any} */
		const monster = {
			gid: 50,
			refObjId: 1954,
			regionId: 1,
			x: 100,
			y: 0,
			z: 0,
			heading: 0,
			kind: "monster",
			name: "Bandit",
			maxHp: 100
		};
		f.state.entities.push( monster, { ...monster, gid: 60, x: 120 } );
		Object.assign( f.state.gameplay, { battleRoyaleBots: published( true ) } );
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		assert.ok( f.hasText( "Ares" ), "the bot's roster name" );
		assert.ok( f.hasText( "Bandit" ), "an ordinary monster keeps its name" );
		assert.equal( redGlyphs( f, 50 ).length, "Ares".length );
		assert.equal( redGlyphs( f, 60 ).length, 0 );
		// Out of name range the plate hides; selected, the target window names the bot.
		monster.x = 400;
		f.state.gameplay = { ...f.state.gameplay, target: 50 };
		for ( let t = 1200; t < 2400; t += 100 ) f.ui.step( f.state, t );
		assert.equal( redGlyphs( f, 50 ).length, 0 );
		assert.ok( f.scenes.at( -1 ).quads.every( q => q.characterAnchor !== 50 || q.texture !== fontAtlas.image ) );
		assert.ok( f.hasText( "Ares" ), "the target window" );
	} finally {
		f.dispose();
	}
});

test("the results window names a bot's row in red", () => {
	const f = uiFixture();
	try {
		let now = 0;
		Object.assign( f.state.gameplay, {
			battleRoyale: decodeState( { state: 4, role: 3, winner: "Ares" } ),
			battleRoyaleBots: published( true, [] ),
			battleRoyaleScoreboard: decodeBattleRoyaleScoreboard(
				scoreFrame( [ { name: "Ares", kills: 2, placement: 1 }, {
					name: "test",
					kills: 0,
					placement: 2,
					self: true
				} ] ).payload,
				3
			)
		} );
		f.state.gameplay.battleRoyaleBots = { ...f.state.gameplay.battleRoyaleBots, names: [ "Ares" ] };
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		assert.equal( f.ui.stats().panel, BATTLE_ROYALE_RESULTS_PANEL );
		assert.equal( redGlyphs( f ).length, "Ares".length, "the bot's name, red" );
		assert.ok( f.hasText( "test" ) );
	} finally {
		f.dispose();
	}
});
