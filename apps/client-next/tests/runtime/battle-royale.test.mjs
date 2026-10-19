/*
===========================================================================

battle-royale.test.mjs - the Battle Royale match frame, pick and HUD

Port-only, not native (docs/battle-royale.md). Crafted 0x3FB2 and 0x3FB3
frames go through the production decoders, the worker owner and the
gameplay owner; the HUD's lines, rings, kill feed, picker and results
window are read from what they publish. Nothing shows without a frame (the
server flag off is native). The build choice survives every path that lost
it in the first playtest: a pick before membership, a pick the server
dropped, a member stored again with the default build, a re-pick.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const {
	battleRoyaleBuilds,
	battleRoyaleFogAlpha,
	battleRoyaleHidesRoster,
	battleRoyaleHudText,
	battleRoyaleOutsideDepth,
	battleRoyaleOutsideZone,
	battleRoyaleOwnKills,
	battleRoyalePickFrame,
	battleRoyaleResultRows,
	battleRoyaleZone,
	battleRoyaleZoneFog,
	BATTLE_ROYALE_FOG_MAX_ALPHA,
	clockText,
	decodeBattleRoyaleScoreboard,
	decodeBattleRoyaleState,
	BATTLE_ROYALE_RESULT_MS,
	OP_BATTLE_ROYALE_PICK,
	OP_BATTLE_ROYALE_SCOREBOARD,
	OP_BATTLE_ROYALE_STATE
} = await import( "../../src/engine/foundation/gameplay/battle-royale.ts" );
const { createBattleRoyale } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/battle-royale/battle-royale.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { serverNotification, KILL_FEED_NOTICE_CHANNEL } = await import(
	"../../src/engine/foundation/gameplay/system-notices.ts"
);
const {
	battleRoyaleBuildOf,
	battleRoyaleRings,
	createBattleRoyaleHud,
	minimapEllipse,
	ringQuads,
	BATTLE_ROYALE_BUILD_PREFIX,
	BATTLE_ROYALE_FOG_RGB,
	BATTLE_ROYALE_RESULTS_PANEL,
	KILL_FEED_SHOW_MS
} = await import( "../../src/engine/runtime/ui/hud/br-hud.ts" );
const { worldMapEllipse } = await import( "../../src/engine/foundation/ui/world-map.ts" );
const { assembleEquipmentAppearance } = await import(
	"../../src/engine/foundation/animation/equipment-appearance.ts"
);
const { playerModelSex } = await import( "../../src/engine/foundation/gameplay/skin-change.ts" );
const { createCharacterResources } = await import( "../../src/engine/runtime/characters/resources/resources.ts" );

// The lobby in Wind Town (region 0x5A74, local 1710, 330) in grid units.
const LOBBY_X = 0x74 * 1920 + 1710, LOBBY_Z = 0x5a * 1920 + 330;
const FIXED_BYTES = 59;

/*
================
stateFrame

A 0x3FB2 frame laid out as docs/battle-royale.md "Wire" says, from named
fields over a lobby frame's defaults.
================
*/
function stateFrame( fields = {} ) {
	const f = {
		version: 1,
		state: 1,
		role: 1,
		build: 0,
		lobby: [ LOBBY_X, LOBBY_Z, 150 ],
		members: 1,
		alive: 0,
		countdownMs: 0,
		phase: 0,
		phases: 5,
		stage: 0,
		stageMs: 0,
		current: [ 0, 0, 0 ],
		next: [ 0, 0, 0 ],
		damage: 0,
		winner: "",
		...fields
	};
	const winner = Buffer.from( f.winner, "utf8" ), b = Buffer.alloc( FIXED_BYTES + winner.length );
	b[0] = f.version;
	b[1] = f.state;
	b[2] = f.role;
	b[3] = f.build;
	f.lobby.forEach( ( value, i ) => b.writeFloatLE( value, 4 + i * 4 ) );
	b[16] = f.members;
	b[17] = f.alive;
	b.writeUInt32LE( f.countdownMs, 18 );
	b[22] = f.phase;
	b[23] = f.phases;
	b[24] = f.stage;
	b.writeUInt32LE( f.stageMs, 25 );
	f.current.forEach( ( value, i ) => b.writeFloatLE( value, 29 + i * 4 ) );
	f.next.forEach( ( value, i ) => b.writeFloatLE( value, 41 + i * 4 ) );
	b.writeFloatLE( f.damage, 53 );
	b.writeUInt16LE( winner.length, 57 );
	winner.copy( b, FIXED_BYTES );
	return { opcode: OP_BATTLE_ROYALE_STATE, payload: new Uint8Array( b ) };
}

// A running match: the zone at (1000, 2000) r 3000 shrinking to (1500,
// 2000) r 1000 over ten seconds.
const RUNNING = {
	state: 3,
	role: 2,
	build: 13,
	members: 6,
	alive: 4,
	phase: 1,
	stage: 1,
	stageMs: 10000,
	current: [ 1000, 2000, 3000 ],
	next: [ 1500, 2000, 1000 ],
	damage: 2
};

/*
================
decode

A match frame that must hold a match (state 1..4), decoded at now.
================
*/
/** @returns {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyaleState} */
function decode( fields, now = 1000, sequence = 1 ) {
	const state = decodeBattleRoyaleState( stateFrame( fields ).payload, now, sequence );
	if ( !state ) throw new Error( "no match" );
	return state;
}

test("each match state round-trips with its remaining times as local deadlines", () => {
	assert.equal(
		decodeBattleRoyaleState( stateFrame( { state: 0, role: 0 } ).payload, 0, 1 ),
		null,
		"state 0 hides everything"
	);
	const lobby = decode( { build: 2, members: 1 }, 500, 3 );
	assert.deepEqual( lobby, {
		phase: "lobby",
		role: "member",
		build: 2,
		lobby: { x: LOBBY_X, z: LOBBY_Z, radius: 150 },
		members: 1,
		alive: 0,
		countdownEndsAtMs: 500,
		zonePhase: 0,
		zonePhases: 5,
		stage: "waiting",
		stageStartedAtMs: 500,
		stageEndsAtMs: 500,
		current: { x: 0, z: 0, radius: 0 },
		next: { x: 0, z: 0, radius: 0 },
		damagePerSec: 0,
		winner: "",
		receivedAtMs: 500,
		sequence: 3
	} );
	const countdown = decode( { state: 2, members: 3, countdownMs: 30000 }, 2000 );
	assert.equal( countdown.phase, "countdown" );
	assert.equal( countdown.countdownEndsAtMs, 32000 );
	const running = decode( RUNNING, 4000 );
	assert.equal( running.phase, "running" );
	assert.equal( running.role, "alive" );
	assert.equal( running.stage, "shrinking" );
	assert.equal( running.stageEndsAtMs, 14000 );
	assert.deepEqual( running.current, { x: 1000, z: 2000, radius: 3000 } );
	assert.deepEqual( running.next, { x: 1500, z: 2000, radius: 1000 } );
	assert.equal( running.damagePerSec, 2 );
	const finished = decode( { state: 4, role: 3, alive: 1, winner: "Gina" } );
	assert.equal( finished.phase, "finished" );
	assert.equal( finished.role, "eliminated" );
	assert.equal( finished.winner, "Gina" );
	assert.equal( decode( { state: 4, role: 0, winner: "" } ).winner, "", "no winner is a draw" );
});

test("a malformed match frame is a protocol error", () => {
	const valid = stateFrame().payload;
	assert.throws( () => decodeBattleRoyaleState( valid.subarray( 0, FIXED_BYTES - 1 ), 0, 1 ), /Truncated/ );
	assert.throws( () => decode( { version: 2 } ), /version/ );
	assert.throws( () => decode( { state: 5 } ), /state/ );
	assert.throws( () => decode( { role: 4 } ), /role/ );
	assert.throws( () => decode( { stage: 3 } ), /stage/ );
	assert.throws( () => decode( { current: [ NaN, 0, 1 ] } ), /circle/ );
	assert.throws( () => decode( { lobby: [ 0, 0, -1 ] } ), /circle/ );
	assert.throws( () => decode( { damage: Infinity } ), /damage/ );
	const trailing = Uint8Array.from( [ ...valid, 0 ] );
	assert.throws( () => decodeBattleRoyaleState( trailing, 0, 1 ), /length/ );
	const short = stateFrame( { state: 4, winner: "Gina" } ).payload;
	assert.throws( () => decodeBattleRoyaleState( short.subarray( 0, short.length - 1 ), 0, 1 ), /length/ );
	const badName = stateFrame( { state: 4, winner: "ab" } ).payload;
	badName[FIXED_BYTES] = 0xff;
	assert.throws( () => decodeBattleRoyaleState( badName, 0, 1 ) );
});

test("the worker owner takes only its opcode and throws on a malformed frame", () => {
	const owner = createBattleRoyale();
	assert.equal( owner.receive( { opcode: 0x3fb0, payload: Uint8Array.of( 0, 0 ) }, 0 ), false );
	assert.equal( owner.state(), undefined );
	assert.equal( owner.receive( stateFrame( { members: 2 } ), 10 ), true );
	assert.equal( owner.state()?.members, 2 );
	assert.throws( () => owner.receive( { opcode: OP_BATTLE_ROYALE_STATE, payload: Uint8Array.of( 1 ) }, 20 ) );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 30 );
	assert.equal( owner.state(), undefined, "state 0 clears the match" );
});

test("a pick encodes 0x7FB2 and leaves only while the server would take it", () => {
	assert.deepEqual( battleRoyalePickFrame( 15 ), { opcode: OP_BATTLE_ROYALE_PICK, payload: Uint8Array.of( 15 ) } );
	assert.throws( () => battleRoyalePickFrame( 7 ), /Unknown battle royale build/ );
	assert.deepEqual( battleRoyaleBuilds().map( build => build.id ), [ 1, 2, 3, 4, 11, 12, 13, 14, 15, 16 ] );
	const owner = createBattleRoyale();
	assert.equal( owner.pick( 1, 0 ), null, "no match" );
	owner.receive( stateFrame( { role: 0 } ), 0 );
	assert.equal( owner.pick( 1, 0 ), null, "outside the lobby circle" );
	owner.receive( stateFrame(), 0 );
	assert.deepEqual( owner.pick( 3, 0 )?.payload, Uint8Array.of( 3 ) );
	owner.receive( stateFrame( { state: 2, countdownMs: 5000 } ), 0 );
	assert.deepEqual( owner.pick( 12, 0 )?.payload, Uint8Array.of( 12 ) );
	owner.receive( stateFrame( RUNNING ), 0 );
	assert.equal( owner.pick( 12, 0 ), null, "the match is running" );
	assert.equal( owner.pickDue( 60000 ), null, "nor again while it runs" );
	assert.throws( () => owner.pick( 7, 0 ), /Unknown battle royale build/, "a caller defect throws" );
});

test("the gameplay owner publishes the match and sends a pick command", () => {
	const sent = [], game = createGameplay( frame => sent.push( frame ) );
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	game.seed( local );
	game.take();
	assert.equal( game.receive( stateFrame( { countdownMs: 0 } ), 100 ), true );
	assert.equal( game.take()?.battleRoyale?.phase, "lobby" );
	assert.ok( game.command( { kind: "battle-royale-pick", build: 4 }, 200, undefined, local ) );
	assert.deepEqual( sent.at( -1 ), { opcode: OP_BATTLE_ROYALE_PICK, payload: Uint8Array.of( 4 ) } );
	game.receive( stateFrame( RUNNING ), 300 );
	const before = sent.length;
	assert.equal( game.command( { kind: "battle-royale-pick", build: 4 }, 400, undefined, local ), null );
	assert.equal( sent.length, before, "no pick while the match runs" );
	game.receive( stateFrame( { state: 0, role: 0 } ), 500 );
	assert.equal( game.take()?.battleRoyale, undefined );
	game.dispose();
});

test("a pick made before the first member frame goes when that frame arrives", () => {
	const owner = createBattleRoyale();
	assert.equal( owner.pick( 3, 0 ), null, "no membership yet: the choice is held" );
	assert.equal( owner.pickDue( 0 ), null );
	owner.receive( stateFrame( { role: 0 } ), 100 );
	assert.equal( owner.pickDue( 100 ), null, "still outside the circle" );
	owner.receive( stateFrame( { build: 11 } ), 200 );
	assert.deepEqual( owner.pickDue( 200 ), { opcode: OP_BATTLE_ROYALE_PICK, payload: Uint8Array.of( 3 ) } );
	assert.equal( owner.pickDue( 300 ), null, "the answer is a round trip away" );
	owner.receive( stateFrame( { build: 3 } ), 400 );
	assert.equal( owner.pickDue( 5000 ), null, "the server stored it" );
});

test("a pick the server drops goes again each second, a few times", () => {
	const owner = createBattleRoyale();
	owner.receive( stateFrame( { build: 11 } ), 0 );
	assert.ok( owner.pick( 15, 0 ) );
	// A frame sent before the pick arrived still names the stored build.
	owner.receive( stateFrame( { build: 11, members: 2 } ), 50 );
	assert.equal( owner.pickDue( 50 ), null, "not before its answer could arrive" );
	const sent = [];
	for ( let now = 100; now <= 20000; now += 100 ) {
		const frame = owner.pickDue( now );
		if ( frame ) sent.push( [ now, frame.payload[0] ] );
	}
	assert.deepEqual( sent, [ [ 1000, 15 ], [ 2000, 15 ], [ 3000, 15 ], [ 4000, 15 ], [ 5000, 15 ] ] );
	assert.ok( owner.pick( 15, 21000 ), "the player's own click always goes" );
	assert.ok( owner.pickDue( 22000 ), "and renews the tries" );
});

test("a member stored again with the default build gets the choice back", () => {
	const owner = createBattleRoyale();
	owner.receive( stateFrame( { build: 11 } ), 0 );
	owner.pick( 2, 0 );
	owner.receive( stateFrame( { build: 2 } ), 100 );
	assert.equal( owner.pickDue( 5000 ), null );
	// Out of the circle and back in: the server's new lobby entry holds the
	// default build (match.go EnterLobby).
	owner.receive( stateFrame( { state: 0, role: 0 } ), 6000 );
	assert.equal( owner.pickDue( 6000 ), null, "not a member" );
	owner.receive( stateFrame( { build: 11 } ), 6100 );
	assert.deepEqual( owner.pickDue( 6100 )?.payload, Uint8Array.of( 2 ), "at once on rejoining" );
	owner.receive( stateFrame( { build: 2 } ), 6200 );
	// Any later lobby or countdown frame naming another build is answered too.
	owner.receive( stateFrame( { state: 2, countdownMs: 30000, build: 11 } ), 7000 );
	assert.equal( owner.pickDue( 7000 ), null, "a second after the last send" );
	assert.deepEqual( owner.pickDue( 7100 )?.payload, Uint8Array.of( 2 ) );
});

test("a re-pick replaces the choice, also in the countdown's last second", () => {
	const owner = createBattleRoyale();
	owner.receive( stateFrame( { build: 11 } ), 0 );
	owner.pick( 1, 0 );
	owner.receive( stateFrame( { build: 1 } ), 100 );
	assert.deepEqual( owner.pick( 13, 200 )?.payload, Uint8Array.of( 13 ) );
	// The answer to the first pick arrives late.
	owner.receive( stateFrame( { build: 1 } ), 250 );
	assert.deepEqual( owner.pickDue( 1200 )?.payload, Uint8Array.of( 13 ), "the newer choice wins" );
	owner.receive( stateFrame( { build: 13 } ), 1300 );
	assert.equal( owner.pickDue( 9000 ), null );
	owner.receive( stateFrame( { state: 2, countdownMs: 500, build: 13 } ), 10000 );
	assert.deepEqual( owner.pick( 16, 10000 )?.payload, Uint8Array.of( 16 ), "half a second before the drop" );
	owner.receive( stateFrame( { ...RUNNING, build: 16 } ), 10600 );
	assert.equal( owner.state()?.build, 16, "the drop carries it" );
	assert.equal( owner.pickDue( 12000 ), null, "nothing goes once the match runs" );
	owner.clear();
	owner.receive( stateFrame( { build: 11 } ), 13000 );
	assert.equal( owner.pickDue( 13000 ), null, "a new session forgets the choice" );
});

test("the gameplay owner sends the remembered choice from its step", () => {
	const sent = [], game = createGameplay( frame => sent.push( frame ) );
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	game.seed( local );
	game.worldReady();
	const picks = () => sent.filter( frame => frame.opcode === OP_BATTLE_ROYALE_PICK ).map( frame => frame.payload[0] );
	game.receive( stateFrame( { build: 11 } ), 0 );
	assert.ok( game.command( { kind: "battle-royale-pick", build: 4 }, 100, undefined, local ) );
	// The server dropped it: its next frame still names the default build.
	game.receive( stateFrame( { build: 11, members: 2 } ), 600 );
	game.step( 600, local );
	assert.deepEqual( picks(), [ 4 ], "not before a second passed" );
	game.step( 1100, local );
	assert.deepEqual( picks(), [ 4, 4 ] );
	game.receive( stateFrame( { build: 4, members: 2 } ), 1200 );
	game.step( 5000, local );
	assert.deepEqual( picks(), [ 4, 4 ], "confirmed" );
	game.dispose();
});

test("the countdown's sounds: five seconds before the drop, the drop, the result", () => {
	const owner = createBattleRoyale();
	owner.receive( stateFrame( { state: 2, countdownMs: 30000 } ), 0 );
	assert.equal( owner.step( 20000 ), null );
	assert.equal( owner.step( 25000 ), "SND_COUNTDOWN" );
	assert.equal( owner.step( 26000 ), null, "once per countdown" );
	assert.equal( owner.step( 30000 ), "SND_COUNTSTART" );
	owner.receive( stateFrame( RUNNING ), 30100 );
	assert.equal( owner.step( 30200 ), null, "the drop already sounded" );
	owner.receive( stateFrame( { state: 4, role: 2, winner: "Gina" } ), 40000 );
	assert.equal( owner.step( 40000 ), "SND_COUNTEND" );
	const late = createBattleRoyale();
	late.receive( stateFrame( { state: 2, countdownMs: 3000 } ), 0 );
	assert.equal( late.step( 0 ), null, "too late into the last five seconds" );
	late.receive( stateFrame( RUNNING ), 1000 );
	assert.equal( late.step( 1000 ), "SND_COUNTSTART", "a drop before the local deadline still sounds" );
});

test("the zone shrinks linearly from current to next over the stage", () => {
	const running = decode( RUNNING, 0 );
	assert.deepEqual( battleRoyaleZone( running, 0 ), { x: 1000, z: 2000, radius: 3000 } );
	assert.deepEqual( battleRoyaleZone( running, 5000 ), { x: 1250, z: 2000, radius: 2000 } );
	assert.deepEqual( battleRoyaleZone( running, 10000 ), { x: 1500, z: 2000, radius: 1000 } );
	assert.deepEqual( battleRoyaleZone( running, 60000 ), { x: 1500, z: 2000, radius: 1000 } );
	const waiting = decode( { ...RUNNING, stage: 0 }, 0 );
	assert.deepEqual( battleRoyaleZone( waiting, 5000 ), waiting.current );
	const final = decode( { ...RUNNING, stage: 2 }, 0 );
	assert.deepEqual( battleRoyaleZone( final, 5000 ), final.current );
	// Region (0, 1): grid (x, 1920 + z).
	const inside = { regionId: 0x0100, x: 1000, z: 80 }, outside = { regionId: 0x0100, x: 4100, z: 80 };
	assert.equal( battleRoyaleOutsideZone( running, inside, 0 ), false );
	assert.equal( battleRoyaleOutsideZone( running, outside, 0 ), true );
	assert.equal( battleRoyaleOutsideZone( running, { regionId: 0x0100, x: 2600, z: 80 }, 0 ), false );
	assert.equal( battleRoyaleOutsideZone( running, { regionId: 0x0100, x: 2600, z: 80 }, 10000 ), true );
	assert.equal( battleRoyaleOutsideZone( decode( { ...RUNNING, role: 3 }, 0 ), outside, 0 ), false );
	assert.equal( battleRoyaleOutsideZone( running, { ...outside, regionId: 0x8001 }, 0 ), false, "dungeon" );
});

test("the fog outside the zone thickens with the distance out", () => {
	const running = decode( RUNNING, 0 );
	// Region (0, 1): grid (x, 1920 + z); the zone is radius 3000 about (1000, 2000).
	assert.equal( battleRoyaleOutsideDepth( running, { regionId: 0x0100, x: 1000, z: 80 }, 0 ), 0, "inside" );
	assert.ok( Math.abs( battleRoyaleOutsideDepth( running, { regionId: 0x0100, x: 4500, z: 80 }, 0 ) - 500 ) < 1e-9 );
	assert.equal(
		battleRoyaleOutsideDepth( decode( { ...RUNNING, role: 3 }, 0 ), { regionId: 0x0100, x: 4500, z: 80 }, 0 ),
		0
	);
	assert.equal( battleRoyaleOutsideDepth( undefined, { regionId: 0x0100, x: 4500, z: 80 }, 0 ), 0 );
	// 0.25 just past the edge, 0.6 from 100 m (1000 units) out, linear between.
	assert.equal( battleRoyaleFogAlpha( 0 ), 0 );
	assert.equal( battleRoyaleFogAlpha( -5 ), 0 );
	assert.ok( Math.abs( battleRoyaleFogAlpha( 1e-6 ) - .25 ) < 1e-6 );
	assert.ok( Math.abs( battleRoyaleFogAlpha( 500 ) - .425 ) < 1e-9 );
	assert.equal( battleRoyaleFogAlpha( 1000 ), BATTLE_ROYALE_FOG_MAX_ALPHA );
	assert.equal( battleRoyaleFogAlpha( 50000 ), .6 );
	assert.equal( battleRoyaleZoneFog( 0 ), 0 );
	assert.equal( battleRoyaleZoneFog( 1000 ), 1 );
	for ( let depth = 0; depth < 1200; depth += 50 ) {
		assert.ok( battleRoyaleFogAlpha( depth + 50 ) >= battleRoyaleFogAlpha( depth ), "never thins going out" );
	}
});

test("the HUD's lines follow the match state and role", () => {
	const none = { headline: "", status: "", lobby: "" };
	assert.deepEqual( battleRoyaleHudText( undefined, 0 ), none );
	assert.deepEqual( battleRoyaleHudText( decode( { role: 0 }, 0 ), 0 ), none, "an onlooker's lobby" );
	assert.equal(
		battleRoyaleHudText( decode( { build: 1 }, 0 ), 0 ).lobby,
		"Battle Royale - waiting for players: 1/2 - Build: Blade-Fire"
	);
	assert.match( battleRoyaleHudText( decode( {}, 0 ), 0 ).lobby, /Build: Default \(Warrior 2H\)/ );
	const countdown = decode( { state: 2, members: 3, countdownMs: 7500 }, 0 );
	assert.equal( battleRoyaleHudText( countdown, 0 ).headline, "Battle Royale starts in 8" );
	assert.equal( battleRoyaleHudText( countdown, 1000 ).headline, "Battle Royale starts in 7" );
	assert.match( battleRoyaleHudText( countdown, 0 ).lobby, /players in the lobby: 3/ );
	const waiting = decode( { ...RUNNING, stage: 0, stageMs: 83000 }, 0 );
	assert.equal( battleRoyaleHudText( waiting, 0 ).status, "Zone shrinks in 1:23    Alive: 4    Kills: 0" );
	assert.equal( battleRoyaleHudText( decode( RUNNING, 0 ), 0, 3 ).status, "Zone shrinking    Alive: 4    Kills: 3" );
	assert.equal(
		battleRoyaleHudText( decode( { ...RUNNING, stage: 2 }, 0 ), 0 ).status,
		"Final zone    Alive: 4    Kills: 0"
	);
	const won = decode( { state: 4, role: 2, alive: 1, winner: "Gina" }, 0 );
	assert.equal( battleRoyaleHudText( won, 0 ).headline, "Winner: Gina" );
	assert.equal( battleRoyaleHudText( won, BATTLE_ROYALE_RESULT_MS ).headline, "", "the result retires" );
	assert.equal( battleRoyaleHudText( decode( { state: 4, role: 3 }, 0 ), 0 ).headline, "Draw" );
	assert.equal( clockText( 0 ), "0:00" );
	assert.equal( clockText( 600001 ), "10:01" );
});

test("the kill feed is its own channel, kept out of the banners and the log", () => {
	const text = "Gina eliminated test", body = Buffer.from( text, "utf16le" );
	const payload = Uint8Array.from( [ KILL_FEED_NOTICE_CHANNEL, text.length, 0, ...body ] );
	assert.deepEqual( serverNotification( 0x3667, payload ), {
		key: "",
		text,
		value: 0,
		bannerOnly: true,
		killFeed: true
	} );
	assert.throws( () => serverNotification( 0x3667, payload.subarray( 0, payload.length - 1 ) ), /length/ );
	const hud = createBattleRoyaleHud();
	/** @type {import("../../src/engine/foundation/gameplay/system-notices.ts").SystemNotice[]} */
	const notices = [ { key: "", text, value: 0, bannerOnly: true, killFeed: true, sequence: 4 } ];
	assert.equal( hud.stepFeed( notices, 1000 ), true );
	assert.equal( hud.stepFeed( notices, 1100 ), false, "a notice is taken once" );
	assert.deepEqual( hud.feed( 1000 ).map( row => row.text ), [ text ] );
	assert.ok( hud.feed( 1000 + KILL_FEED_SHOW_MS - 500 )[0].alpha < 1, "the line fades" );
	assert.equal( hud.stepFeed( notices, 1000 + KILL_FEED_SHOW_MS ), true );
	assert.deepEqual( hud.feed( 1000 + KILL_FEED_SHOW_MS ), [] );
});

test("joining the lobby opens the picker once; rings and controls are well formed", () => {
	const hud = createBattleRoyaleHud();
	assert.equal( hud.observe( decode( { role: 0 }, 0 ) ), false );
	assert.equal( hud.observe( decode( {}, 0 ) ), true, "became a member" );
	assert.equal( hud.observe( decode( { state: 2, countdownMs: 9000 }, 0 ) ), false, "still a member" );
	assert.equal( hud.observe( decode( RUNNING, 0 ) ), false );
	assert.equal( hud.observe( decode( {}, 0 ) ), true, "a member again for the next match" );
	assert.equal( battleRoyaleBuildOf( BATTLE_ROYALE_BUILD_PREFIX + "13" ), 13 );
	assert.equal( battleRoyaleBuildOf( "close-window:Battle Royale" ), undefined );
	assert.deepEqual( battleRoyaleRings( decode( {}, 0 ), 0 ).map( ring => ring.lobby ), [ true ] );
	const running = battleRoyaleRings( decode( RUNNING, 0 ), 5000 );
	assert.equal( running.length, 2, "an alive participant sees the zone, not the lobby" );
	assert.deepEqual( running[1].circle, { x: 1250, z: 2000, radius: 2000 } );
	assert.equal( battleRoyaleRings( decode( { ...RUNNING, stage: 2 }, 0 ), 0 ).length, 1, "no next circle" );
	// One region at 96 px: the lobby 15 m east of a player at its centre.
	const ellipse = minimapEllipse(
		{ regionId: 0x5a74, x: 1710 - 150, z: 330 },
		{ x: LOBBY_X, z: LOBBY_Z, radius: 150 },
		96,
		[ 50, 50 ]
	);
	assert.deepEqual( ellipse, { x: 57.5, y: 50, rx: 7.5, ry: 7.5 } );
	if ( !ellipse ) return;
	/** @type {import("../../src/engine/contracts/ui.ts").UiRect} */
	const clip = [ 0, 0, 100, 100 ];
	const quads = ringQuads( ellipse, [ 1, 0, 0, 1 ], clip, 2 );
	assert.ok( quads.length >= 24 && quads.every( q => q.texture === "" && q.rect[3] === 2 ) );
	for ( const q of quads ) {
		const cx = q.rect[0] + q.rect[2] / 2, cy = q.rect[1] + q.rect[3] / 2;
		assert.ok( Math.abs( Math.hypot( cx - 57.5, cy - 50 ) - 7.5 ) < .2, "segments sit on the ring" );
	}
	const huge = ringQuads( { x: 50, y: 50, rx: 5000, ry: 5000 }, [ 1, 0, 0, 1 ], clip, 2 );
	assert.equal( huge.length, 0, "a ring far outside the clip draws nothing" );
	/** @type {Parameters<typeof worldMapEllipse>[0]} */
	const page = {
		page: 0,
		clip: [ 0, 0, 400, 300 ],
		pan: [ 0, 0 ],
		center: { regionId: 0x5a74, x: 0, y: 0, z: 0, angle: 0 }
	};
	const big = worldMapEllipse( page, { x: LOBBY_X, z: LOBBY_Z, radius: 2000 } ),
		small = worldMapEllipse( page, { x: LOBBY_X, z: LOBBY_Z, radius: 1000 } );
	assert.equal( big.x, small.x );
	assert.ok( Math.abs( big.rx - 2 * small.rx ) < 1e-9 && big.rx > 0 && big.ry > 0 );
});

/*
================
hudScenario

A production HUD stepped with a match state, its last publication read.
================
*/
function hudScenario( battleRoyale, notices = [] ) {
	const f = uiFixture();
	let now = 0;
	const seen = {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
	};
	Object.assign( f.state.gameplay, { battleRoyale, notices } );
	for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
	return { f, ids: new Set( seen.presentation?.controls.map( control => control.id ) ?? [] ) };
}

test("the production HUD draws nothing without a match frame", () => {
	const { f, ids } = hudScenario( undefined );
	try {
		assert.ok( ![ ...ids ].some( id => id.startsWith( BATTLE_ROYALE_BUILD_PREFIX ) ) );
		assert.ok( !ids.has( "open-window:Battle Royale" ) );
		assert.equal( f.ui.stats().panel, "" );
	} finally {
		f.dispose();
	}
});

test("a lobby member gets the picker, the hint's button and both race columns", () => {
	const { f, ids } = hudScenario( decode( { build: 12, members: 1 }, 0 ) );
	try {
		assert.equal( f.ui.stats().panel, "Battle Royale" );
		assert.ok( ids.has( "open-window:Battle Royale" ), "the lobby hint reopens the picker" );
		for ( const build of battleRoyaleBuilds() ) assert.ok( ids.has( BATTLE_ROYALE_BUILD_PREFIX + build.id ) );
		assert.ok( f.hasText( "Current build: Knight" ) );
		assert.ok( f.hasText( "Battle Royale - waiting for players: 1/2 - Build: Knight" ) );
	} finally {
		f.dispose();
	}
});

test("a picker button sends the pick command", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		let now = 0;
		f.state.gameplay.battleRoyale = decode( {}, 0 );
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
		f.ui.event( { kind: "activate", id: BATTLE_ROYALE_BUILD_PREFIX + "15" } );
		assert.ok(
			sent.some( command =>
				command.kind === "gameplay" && command.command.kind === "battle-royale-pick" &&
				command.command.build === 15
			)
		);
	} finally {
		f.dispose();
	}
});

test("the countdown headline, the running zone line and the kill feed draw", () => {
	const countdown = hudScenario( decode( { state: 2, role: 0, countdownMs: 60000 }, 0 ) );
	try {
		assert.ok( countdown.f.hasText( "Battle Royale starts in", "4" ) );
	} finally {
		countdown.f.dispose();
	}
	const notice = { key: "", text: "Gina eliminated test", value: 0, bannerOnly: true, killFeed: true, sequence: 9 };
	const running = hudScenario( decode( { ...RUNNING, stage: 2 }, 0 ), [ notice ] );
	try {
		assert.ok( running.f.hasText( "Final zone    Alive: 4    Kills: 0" ) );
		assert.ok( running.f.hasText( "Gina eliminated test" ) );
	} finally {
		running.f.dispose();
	}
});

test("a participant outside the zone sees the fog veil and a faint red pulse under the HUD", () => {
	// The fixture's pose stands at grid (1920, 0): 500 units outside a
	// radius-500 final zone about (1920, 1000).
	const outside = hudScenario( decode( { ...RUNNING, stage: 2, current: [ 1920, 1000, 500 ] }, 0 ) );
	try {
		const quads = outside.f.scenes.at( -1 )?.quads ?? [];
		const veil = quads.find( q =>
			q.texture === "" && q.rect.join() === "0,0,1600,900" && q.color[0] === BATTLE_ROYALE_FOG_RGB[0]
		);
		assert.ok( veil, "the grey veil covers the screen" );
		assert.ok( Math.abs( veil.color[3] - .425 ) < 1e-9 );
		const red = quads.find( q => q.texture === "" && q.rect.join() === "0,0,1600,900" && q.color[0] === .85 );
		assert.ok( red && red.color[3] <= .15, "the red pulse stays faint" );
		assert.ok( quads.indexOf( veil ) < quads.indexOf( red ), "the pulse lies over the veil" );
	} finally {
		outside.f.dispose();
	}
	const inside = hudScenario( decode( { ...RUNNING, stage: 2, current: [ 1920, 0, 500 ] }, 0 ) );
	try {
		const quads = inside.f.scenes.at( -1 )?.quads ?? [];
		assert.ok( !quads.some( q => q.texture === "" && q.color[0] === BATTLE_ROYALE_FOG_RGB[0] ), "no veil inside" );
	} finally {
		inside.f.dispose();
	}
});

/*
================
swappedBody

A Chinese suit worn by a European man's body, as a race swap in place can
present for a frame before the converted equipment arrives.
================
*/
function swappedBody( lenientBodies ) {
	const suit = 9026;
	return assembleEquipmentAppearance( {
		resource: { codename: "CHAR_EU_MAN_ADVENTURER", cover: {} },
		dress: {
			equipment: {
				[suit]: {
					slot: 8,
					armorClass: 0,
					thiefSuit: true,
					visualMask: 0x73f,
					visualPriority: 50,
					model: "res/item/china/man_item/thief_02.bsr",
					source: null,
					bodies: { CH_M: { glb: "/assets/char/equipment/thief_02.glb", parts: [ "EQ0" ] } }
				}
			}
		},
		equipment: [ { slot: 8, refObjId: suit, plus: 0 } ],
		avatars: [],
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: true,
		lenientBodies
	} );
}

test("a race swapped in place degrades a worn item of the other body instead of failing", () => {
	assert.throws( () => swappedBody( false ), /Equipment visual body mismatch 9026: EU_M/ );
	const assembly = swappedBody( true );
	assert.deepEqual( assembly.degraded, [ 9026 ] );
	assert.ok( !assembly.parts.some( part => part.model.includes( "thief" ) ), "the item draws nothing" );
	// The item requirements read the worn body's sex, not the character list's.
	const models = [ { refObjId: 1907, sex: 1 }, { refObjId: 14717, sex: 0 } ];
	assert.equal( playerModelSex( models, 14717 ), 0 );
	assert.equal( playerModelSex( models, 99 ), undefined );
	assert.equal( playerModelSex( models, undefined ), undefined );
});

test("a degraded appearance reaches the asset owner's report queue once a code per world", () => {
	/** @type {any[]} */
	const reported = [];
	/** @type {any} */
	const assets = { reportDegradation: degradation => reported.push( degradation ) };
	/** @type {any} */
	const renderer = { retainCharacterModels() {} };
	const resources = createCharacterResources( assets, renderer, "https://fixture.invalid/" );
	const degradation = { code: "equipment-visual-body-mismatch", message: "Equipment visual body mismatch 9026" };
	resources.degrade( degradation );
	resources.degrade( { ...degradation, message: "again" } );
	assert.deepEqual( reported, [ degradation ] );
	resources.reset();
	resources.degrade( degradation );
	assert.equal( reported.length, 2, "a new world reports it again" );
});

// ============================================================================

/*
================
scoreFrame

A 0x3FB3 frame laid out as docs/battle-royale.md "Stage 2" says: version,
final, count, then per entry nameLen, the UTF-8 name, kills, placement
and self.
================
*/
/** @param {{ name: string, kills: number, placement: number, self?: boolean }[]} entries */
function scoreFrame( entries, final = 0, version = 1 ) {
	const parts = [ Buffer.from( [ version, final, entries.length ] ) ];
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

// A finished match of three: the receiving player came second.
const BOARD = [
	{ name: "Ölaf", kills: 0, placement: 3 },
	{ name: "Gina", kills: 3, placement: 1 },
	{ name: "test", kills: 1, placement: 2, self: true }
];

test("the scoreboard round-trips and a malformed one is a protocol error", () => {
	const board = decodeBattleRoyaleScoreboard( scoreFrame( BOARD, 1 ).payload, 4 );
	assert.deepEqual( board, {
		final: true,
		entries: [
			{ name: "Ölaf", kills: 0, placement: 3, self: false },
			{ name: "Gina", kills: 3, placement: 1, self: false },
			{ name: "test", kills: 1, placement: 2, self: true }
		],
		sequence: 4
	} );
	// The server encoder's own fixture (wire/battleroyale_test.go).
	const server = Uint8Array.from( [
		1,
		1,
		2,
		4,
		0,
		71,
		105,
		110,
		97,
		3,
		0,
		1,
		1,
		4,
		0,
		116,
		101,
		115,
		116,
		0x2c,
		1,
		2,
		0
	] );
	assert.deepEqual( decodeBattleRoyaleScoreboard( server, 9 ).entries, [
		{ name: "Gina", kills: 3, placement: 1, self: true },
		{ name: "test", kills: 300, placement: 2, self: false }
	] );
	assert.deepEqual( decodeBattleRoyaleScoreboard( scoreFrame( [] ).payload, 1 ), {
		final: false,
		entries: [],
		sequence: 1
	} );
	const valid = scoreFrame( BOARD ).payload;
	assert.throws( () => decodeBattleRoyaleScoreboard( valid.subarray( 0, 2 ), 1 ), /Truncated/ );
	assert.throws( () => decodeBattleRoyaleScoreboard( valid.subarray( 0, valid.length - 1 ), 1 ), /Truncated/ );
	assert.throws( () => decodeBattleRoyaleScoreboard( Uint8Array.from( [ ...valid, 0 ] ), 1 ), /Trailing/ );
	assert.throws( () => decodeBattleRoyaleScoreboard( scoreFrame( BOARD, 0, 2 ).payload, 1 ), /version/ );
	assert.throws( () => decodeBattleRoyaleScoreboard( scoreFrame( BOARD, 2 ).payload, 1 ), /final/ );
	const twoSelves = BOARD.map( entry => ({ ...entry, self: true }) );
	assert.throws( () => decodeBattleRoyaleScoreboard( scoreFrame( twoSelves ).payload, 1 ), /claim/ );
	const longName = [ { name: "x".repeat( 65 ), kills: 0, placement: 0 } ];
	assert.throws( () => decodeBattleRoyaleScoreboard( scoreFrame( longName ).payload, 1 ), /name length/ );
	const badSelf = scoreFrame( [ { name: "a", kills: 0, placement: 0 } ] ).payload;
	badSelf[badSelf.length - 1] = 2;
	assert.throws( () => decodeBattleRoyaleScoreboard( badSelf, 1 ), /self/ );
	const badName = scoreFrame( [ { name: "ab", kills: 0, placement: 0 } ] ).payload;
	badName[5] = 0xff;
	assert.throws( () => decodeBattleRoyaleScoreboard( badName, 1 ) );
});

test("the worker owner keeps the scoreboard until a new match's lobby", () => {
	const owner = createBattleRoyale();
	assert.equal( owner.receive( { opcode: 0x3fbe, payload: Uint8Array.of( 1, 0, 0 ) }, 0 ), false, "foreign" );
	owner.receive( stateFrame( RUNNING ), 0 );
	assert.equal( owner.receive( scoreFrame( BOARD ), 10 ), true );
	assert.equal( owner.scoreboard()?.final, false );
	assert.throws( () => owner.receive( { opcode: OP_BATTLE_ROYALE_SCOREBOARD, payload: Uint8Array.of( 1 ) }, 20 ) );
	owner.receive( scoreFrame( BOARD, 1 ), 30 );
	owner.receive( stateFrame( { state: 4, role: 2, winner: "Gina" } ), 40 );
	owner.receive( stateFrame( { state: 0, role: 0 } ), 50 );
	assert.equal( owner.scoreboard()?.final, true, "the results outlive the match" );
	owner.receive( stateFrame( { build: 11 } ), 60 );
	assert.equal( owner.scoreboard(), undefined, "a new lobby retires them" );
	owner.receive( scoreFrame( BOARD ), 70 );
	owner.clear();
	assert.equal( owner.scoreboard(), undefined );
});

test("own kills, the results order and the HUD's kills", () => {
	assert.equal( battleRoyaleOwnKills( undefined ), 0, "before the first elimination" );
	const board = decodeBattleRoyaleScoreboard( scoreFrame( BOARD ).payload, 1 );
	assert.equal( battleRoyaleOwnKills( board ), 1 );
	assert.deepEqual( battleRoyaleResultRows( board ).map( row => row.name ), [ "Gina", "test", "Ölaf" ] );
	const running = decodeBattleRoyaleScoreboard(
		scoreFrame( [
			{ name: "a", kills: 1, placement: 0 },
			{ name: "b", kills: 0, placement: 4 },
			{ name: "c", kills: 5, placement: 0 },
			{ name: "d", kills: 2, placement: 3 }
		] ).payload,
		2
	);
	assert.deepEqual( battleRoyaleResultRows( running ).map( row => row.name ), [ "d", "b", "c", "a" ] );
	assert.deepEqual( battleRoyaleResultRows( undefined ), [] );
	assert.equal(
		battleRoyaleHudText( decode( RUNNING, 0 ), 0, battleRoyaleOwnKills( board ) ).status,
		"Zone shrinking    Alive: 4    Kills: 1"
	);
});

test("the gameplay owner publishes the scoreboard", () => {
	const game = createGameplay( () => {} );
	/** @type {any} */
	const local = { regionId: 0x5a6f, x: 960, y: 0, z: 960, angle: 0, gid: 7, heading: 0 };
	game.seed( local );
	game.take();
	game.receive( stateFrame( RUNNING ), 0 );
	assert.equal( game.take()?.battleRoyaleScoreboard, undefined );
	assert.equal( game.receive( scoreFrame( BOARD, 1 ), 10 ), true );
	assert.equal( game.take()?.battleRoyaleScoreboard?.entries.length, 3 );
	game.dispose();
});

/*
================
rosterFrame

A beta world map roster (0x3FB0) of one other player.
================
*/
function rosterFrame() {
	const name = Buffer.from( "Gina" ), b = Buffer.alloc( 2 + 15 + name.length );
	b.writeUInt16LE( 1, 0 );
	b.writeUInt32LE( 99, 2 );
	b.writeUInt16LE( 0x5a74, 6 );
	b.writeFloatLE( 100, 8 );
	b.writeFloatLE( 200, 12 );
	b[16] = name.length;
	name.copy( b, 17 );
	return { opcode: 0x3fb0, payload: new Uint8Array( b ) };
}

test("a Battle Royale player sees no beta roster from the lobby to its match's end", () => {
	assert.equal( battleRoyaleHidesRoster( undefined ), false );
	assert.equal( battleRoyaleHidesRoster( decode( { role: 0 } ) ), false, "an onlooker" );
	assert.equal( battleRoyaleHidesRoster( decode( {} ) ), true, "a lobby member" );
	assert.equal( battleRoyaleHidesRoster( decode( { state: 2, countdownMs: 1000 } ) ), true );
	assert.equal( battleRoyaleHidesRoster( decode( RUNNING ) ), true );
	assert.equal( battleRoyaleHidesRoster( decode( { ...RUNNING, role: 3 } ) ), true, "eliminated" );
	assert.equal( battleRoyaleHidesRoster( decode( { state: 4, role: 2, winner: "Gina" } ) ), false, "the end" );
	const game = createGameplay( () => {} );
	/** @type {any} */
	const local = { regionId: 0x5a74, x: 1710, y: 0, z: 330, angle: 0, gid: 7, heading: 0 };
	game.seed( local );
	game.receive( rosterFrame(), 0 );
	assert.equal( game.take()?.betaPlayers?.length, 1 );
	game.receive( stateFrame(), 10 );
	assert.deepEqual( game.take()?.betaPlayers, [], "in the lobby" );
	game.receive( stateFrame( RUNNING ), 20 );
	game.receive( rosterFrame(), 30 );
	assert.deepEqual( game.take()?.betaPlayers, [], "in the match" );
	game.receive( stateFrame( { state: 0, role: 0 } ), 40 );
	assert.equal( game.take()?.betaPlayers?.length, 1, "out of it" );
	game.dispose();
});

test("streak and supply banners arrive as the native channel-7 notice banner", () => {
	for ( const text of [ "Gina is on a rampage", "Gina is unstoppable", "Supply drop at the next zone!" ] ) {
		const body = Buffer.from( text, "utf16le" );
		const notice = serverNotification( 0x3667, Uint8Array.from( [ 7, text.length, 0, ...body ] ) );
		assert.deepEqual( notice, { key: "", text, value: 0, notificationBanner: true, bannerOnly: true } );
	}
	const { f } = hudScenario( undefined, [ {
		key: "",
		text: "Gina is on a rampage",
		value: 0,
		notificationBanner: true,
		bannerOnly: true,
		sequence: 3
	} ] );
	try {
		// The notice banner draws its line in the outlined style (ui.ts drawTransientBanners).
		assert.ok( f.hasText( "Gina is on a rampage", "0", 2 ), "drawn on the notice banner" );
	} finally {
		f.dispose();
	}
});

test("the final scoreboard opens the results window once", () => {
	const hud = createBattleRoyaleHud();
	const running = decodeBattleRoyaleScoreboard( scoreFrame( BOARD ).payload, 1 ),
		final = decodeBattleRoyaleScoreboard( scoreFrame( BOARD, 1 ).payload, 2 );
	assert.equal( hud.openResults( undefined, true ), false );
	assert.equal( hud.openResults( running, true ), false, "not before the end" );
	assert.equal( hud.openResults( final, false ), false, "a window that may not close yet" );
	assert.equal( hud.openResults( final, true ), true );
	assert.equal( hud.openResults( final, true ), false, "once" );
	assert.equal( hud.openResults( { ...final, sequence: 3 }, true ), true, "the next match's" );
	hud.reset();
	assert.equal( hud.openResults( { ...final, sequence: 3 }, true ), true, "a new session" );
});

test("the results window lists placement, name and kills, and the running HUD the player's kills", () => {
	const board = decodeBattleRoyaleScoreboard( scoreFrame( BOARD, 1 ).payload, 5 );
	const f = uiFixture();
	try {
		let now = 0;
		const seen = {
			/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null} */ presentation: null
		};
		const step = () => {
			for ( let i = 0; i < 16; i++ ) seen.presentation = f.ui.step( f.state, now += 100 ) ?? seen.presentation;
		};
		Object.assign( f.state.gameplay, {
			battleRoyale: decode( { ...RUNNING, stage: 2 }, 0 ),
			battleRoyaleScoreboard: decodeBattleRoyaleScoreboard( scoreFrame( BOARD ).payload, 4 )
		} );
		step();
		assert.ok( f.hasText( "Final zone    Alive: 4    Kills: 1" ) );
		assert.equal( f.ui.stats().panel, "", "no results while the match runs" );
		f.state.gameplay = { ...f.state.gameplay, battleRoyaleScoreboard: board };
		step();
		assert.equal( f.ui.stats().panel, BATTLE_ROYALE_RESULTS_PANEL );
		for ( const text of [ "Battle Royale results", "Place", "Gina", "test", "Ölaf" ] ) {
			assert.ok( f.hasText( text ), text );
		}
		const close = "close-window:" + BATTLE_ROYALE_RESULTS_PANEL;
		assert.ok( seen.presentation?.controls.some( control => control.id === close ) );
		f.ui.event( { kind: "activate", id: close } );
		step();
		assert.equal( f.ui.stats().panel, "", "closed" );
		step();
		assert.equal( f.ui.stats().panel, "", "and not opened again by the same board" );
	} finally {
		f.dispose();
	}
});

test("the bootstrap lobby decodes to grid units and guides a nearby player", async () => {
	const { battleRoyaleLobbyFrom, battleRoyaleLobbyHint } = await import(
		"../../src/engine/foundation/gameplay/battle-royale.ts"
	);
	assert.equal( battleRoyaleLobbyFrom( {} ), undefined );
	const lobby = battleRoyaleLobbyFrom( { battleRoyaleLobby: { regionId: 0x5c87, x: 1049, z: 846, radiusM: 15 } } );
	assert.deepEqual( lobby, { x: 0x87 * 1920 + 1049, z: 0x5c * 1920 + 846, radius: 150 } );
	assert.throws( () => battleRoyaleLobbyFrom( { battleRoyaleLobby: { regionId: 0, x: 1, z: 1, radiusM: 15 } } ) );
	assert.match( battleRoyaleLobbyHint( lobby, undefined, lobby.x + 400, lobby.z ), /stand in the red circle/ );
	assert.equal( battleRoyaleLobbyHint( lobby, undefined, lobby.x + 5000, lobby.z ), "" );
	assert.equal( battleRoyaleLobbyHint( undefined, undefined, 0, 0 ), "" );
});
