/*
===========================================================================

battle-royale-fireworks.test.mjs - the Battle Royale airdrop's fireworks

Port-only, not native (docs/battle-royale.md, Stage 9 item 2). The
production fireworks owner is stepped on the frame clock with published
airdrops and a scripted random source: a landed airdrop fires the game's
firework programs at its point every 3 to 6 seconds for a player within
sight of it, an incoming or removed one fires nothing, and neither a
running match nor its end fires anything any more (the Stage 2 zone
fireworks and the winner's burst are gone).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	createFireworks,
	FIREWORK_KINDS,
	FIREWORK_MAX_INTERVAL_SECONDS,
	FIREWORK_MIN_INTERVAL_SECONDS,
	FIREWORK_SCATTER_UNITS,
	FIREWORK_SIGHT_UNITS
} = await import( "../../src/engine/runtime/characters/fireworks.ts" );

// The player in the Roc arena (region 0x5A6F, local 960/960) and its grid point.
const POSE = { regionId: 0x5a6f, x: 960, y: 50, z: 960 };
const PX = 0x6f * 1920 + 960, PZ = 0x5a * 1920 + 960;
// The firework's lift over its user (fireworks.ts LAUNCH_LIFT, effects.ts).
const LIFT = 8;
// A program's length in the scripted resources.
const LIFE = 2;

/*
================
landed

A landed airdrop dx, dz grid units from the player.
================
*/
/** @returns {import("../../src/engine/foundation/gameplay/battle-royale-airdrop.ts").BattleRoyaleAirdrop} */
function landed( dx = 200, dz = 0 ) {
	return { state: "landed", phase: 1, x: PX + dx, z: PZ + dz, receivedAtMs: 0 };
}

/*
================
incoming
================
*/
/** @returns {import("../../src/engine/foundation/gameplay/battle-royale-airdrop.ts").BattleRoyaleAirdrop} */
function incoming() {
	return {
		state: "incoming",
		phase: 1,
		line: { ax: PX, az: PZ, bx: PX + 10, bz: PZ },
		landsAtMs: 1,
		receivedAtMs: 0
	};
}

/*
================
matchState

A published running (or finished) match around the player, for the
regression that matches alone fire nothing.
================
*/
/**
 * @param {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyalePhase} phase
 * @returns {import("../../src/engine/foundation/gameplay/battle-royale.ts").BattleRoyaleState}
 */
function matchState( phase, winner = "" ) {
	return {
		phase,
		role: "alive",
		build: 11,
		lobby: { x: 0, z: 0, radius: 150 },
		members: 2,
		alive: 2,
		countdownEndsAtMs: 0,
		zonePhase: 0,
		zonePhases: 5,
		stage: "waiting",
		stageStartedAtMs: 0,
		stageEndsAtMs: 60000,
		current: { x: PX, z: PZ, radius: 3000 },
		next: { x: PX, z: PZ, radius: 1500 },
		damagePerSec: 1,
		winner,
		receivedAtMs: 0,
		sequence: 1
	};
}

/*
================
rig

A fireworks owner over scripted bindings: random cycles through values,
programs in loading stay unready, programs in broken fail, and every
sound played is kept.
================
*/
function rig( values = [ .5 ] ) {
	let draw = 0, gid = 0;
	/** @type {import("../../src/engine/contracts/audio.ts").SoundEvent[]} */
	const sounds = [];
	const loading = new Set(), broken = new Set();
	const owner = createFireworks( event => sounds.push( event ), () => --gid, () => values[draw++ % values.length] );
	/** @type {import("../../src/engine/runtime/characters/fireworks.ts").FireworkResources} */
	const resources = {
		ready: path => !loading.has( path ),
		failed: path => broken.has( path ),
		duration: () => LIFE
	};
	return {
		owner,
		sounds,
		loading,
		broken,
		/** @param {any} airdrop @param {number} seconds */
		step: ( airdrop, seconds, pose = POSE ) => owner.step( airdrop, pose, seconds, resources )
	};
}

test("nothing goes off without a landed airdrop", () => {
	const r = rig();
	for ( const airdrop of [ undefined, incoming() ] ) {
		for ( let seconds = 0; seconds <= 60; seconds += .5 ) assert.deepEqual( r.step( airdrop, seconds ), [] );
	}
	assert.deepEqual( r.sounds, [] );
	assert.ok( FIREWORK_MIN_INTERVAL_SECONDS === 3 && FIREWORK_MAX_INTERVAL_SECONDS === 6, "the spec's cadence" );
});

test("a landed airdrop fires at its point at once, then every 3 to 6 seconds", () => {
	const r = rig(), drop = landed();
	// Draw .5: the next one 4.5 s on; the scatter's angle half a turn and its
	// reach half of FIREWORK_SCATTER_UNITS; kind 3 of 6.
	const [actor, ...rest] = r.step( drop, 10 );
	assert.equal( rest.length, 0 );
	assert.equal( actor.model, FIREWORK_KINDS[3].model );
	assert.equal( actor.clip, "effect" );
	assert.equal( actor.time, 0 );
	assert.equal( actor.pickable, false );
	assert.ok( Math.abs( actor.pose.x - (drop.state === "landed" ? drop.x : 0) + FIREWORK_SCATTER_UNITS / 2 ) < 1e-6 );
	assert.ok( Math.hypot( actor.pose.x - (PX + 200), actor.pose.z - PZ ) <= FIREWORK_SCATTER_UNITS );
	assert.equal( actor.pose.regionId, 0, "world grid units" );
	assert.equal( actor.pose.y, POSE.y + LIFT );
	assert.deepEqual( r.sounds.map( sound => sound.path ), [ FIREWORK_KINDS[3].shot ] );
	assert.equal( r.sounds[0].spatial, true );
	r.step( drop, 10 + FIREWORK_KINDS[3].explosionSeconds );
	assert.deepEqual( r.sounds.map( sound => sound.path ), [ FIREWORK_KINDS[3].shot, FIREWORK_KINDS[3].explosion ] );
	assert.deepEqual( r.step( drop, 10 + LIFE ), [], "the program ran its length" );
	assert.deepEqual( r.step( drop, 14.4 ), [] );
	assert.equal( r.step( drop, 14.5 ).length, 1, "the next one 4.5 s later" );
	// The cadence stays within the spec over a long run of draws.
	const spread = rig( [ 0, .99, .3, .7 ] ), launches = [];
	for ( let seconds = 0; seconds <= 120; seconds += .1 ) {
		const before = spread.sounds.length;
		spread.step( drop, seconds );
		if ( spread.sounds.some( ( sound, i ) => i >= before && sound.id.endsWith( ":shot" ) ) ) {
			launches.push( seconds );
		}
	}
	assert.ok( launches.length > 15 );
	for ( let i = 1; i < launches.length; i++ ) {
		const gap = launches[i] - launches[i - 1];
		assert.ok(
			gap >= FIREWORK_MIN_INTERVAL_SECONDS - .11 && gap <= FIREWORK_MAX_INTERVAL_SECONDS + .11,
			`gap ${gap}`
		);
	}
});

test("the fireworks stop when the airdrop goes, and start again at the next landing", () => {
	const r = rig(), drop = landed();
	assert.equal( r.step( drop, 0 ).length, 1 );
	// Removed (the guardian died): the one in the air burns out, no more go up.
	assert.equal( r.step( undefined, 1 ).length, 1 );
	for ( let seconds = LIFE; seconds <= 30; seconds += .5 ) assert.deepEqual( r.step( undefined, seconds ), [] );
	assert.equal( r.sounds.filter( sound => sound.id.endsWith( ":shot" ) ).length, 1 );
	// The next phase's airdrop: first an incoming one (nothing), then landed.
	assert.deepEqual( r.step( incoming(), 31 ), [] );
	assert.equal( r.step( drop, 40 ).length, 1, "at once on landing" );
});

test("a player out of sight of the airdrop, or in a dungeon, sees none", () => {
	const r = rig(), far = landed( FIREWORK_SIGHT_UNITS + 1 );
	for ( let seconds = 0; seconds <= 30; seconds += 1 ) assert.deepEqual( r.step( far, seconds ), [] );
	const dungeon = rig(), pose = { ...POSE, regionId: 0x8001 };
	for ( let seconds = 0; seconds <= 30; seconds += 1 ) {
		assert.deepEqual( dungeon.step( landed(), seconds, pose ), [] );
	}
	// Walking into sight shows the next one.
	let seen = 0;
	for ( let seconds = 31; seconds <= 40; seconds += 1 ) seen += r.step( landed(), seconds ).length;
	assert.ok( seen > 0 );
});

test("steady frames reuse the list and the actor; a loading program holds its clock", () => {
	const r = rig(), drop = landed();
	r.loading.add( FIREWORK_KINDS[3].model );
	assert.deepEqual( r.step( drop, 0 ), [], "launched but still loading" );
	assert.deepEqual( r.sounds, [] );
	r.loading.clear();
	const first = r.step( drop, 1 ), actor = first[0];
	assert.equal( actor.time, 0, "the clock starts on admission" );
	const second = r.step( drop, 1.5 );
	assert.equal( second, first, "the same list every frame" );
	assert.equal( second[0], actor, "the same actor, its clock advanced" );
	assert.equal( actor.time, .5 );
	const broken = rig();
	broken.broken.add( FIREWORK_KINDS[3].model );
	assert.deepEqual( broken.step( drop, 0 ), [] );
	assert.deepEqual( broken.step( drop, 1 ), [], "a failed program is dropped" );
	assert.deepEqual( broken.sounds, [] );
});

test("a new world clears what is in flight", () => {
	const r = rig(), drop = landed();
	assert.equal( r.step( drop, 0 ).length, 1 );
	r.owner.reset();
	assert.equal( r.step( undefined, .1 ).length, 0 );
});

test("the stage pass fires for a participant's landed airdrop alone", async () => {
	const { createAuxiliaryPresentation } = await import(
		"../../src/engine/runtime/characters/presentation-auxiliary.ts"
	);
	/** @type {import("../../src/engine/contracts/audio.ts").SoundEvent[]} */
	const sounds = [];
	let gid = -1000;
	const auxiliary = createAuxiliaryPresentation( () => --gid, event => sounds.push( event ) );
	/** @type {any} */
	const bindings = {
		renderer: { setCharacterAssembly() {} },
		resources: { duration: () => LIFE, ready: () => true, failed: () => false },
		effects: { animationModel: () => undefined, takeBudget: () => ({ actors: 0, gaveWay: 0 }) },
		probe: undefined
	};
	/** @param {number} seconds @param {any} gameplay */
	const frame = ( seconds, gameplay ) => ({
		orbActors: [],
		effectActors: [],
		next: new Map(),
		seconds,
		animationHolders: [],
		gameplay: { pose: { ...POSE, angle: 0 }, ...gameplay }
	});
	// A running match without an airdrop, and its end with a winner: nothing
	// (the Stage 2 zone fireworks and the winner's burst are gone).
	for ( let seconds = 0; seconds <= 20; seconds += .5 ) {
		const next = frame( seconds, { battleRoyale: matchState( seconds < 15 ? "running" : "finished", "test" ) } );
		auxiliary.presentStages( next, bindings );
		assert.equal( next.next.size, 0 );
	}
	// A landed airdrop outside a running match (the lobby's leftover): none.
	const outside = frame( 21, {
		battleRoyale: { ...matchState( "running" ), role: "outside" },
		battleRoyaleAirdrop: landed()
	} );
	auxiliary.presentStages( outside, bindings );
	assert.equal( outside.next.size, 0 );
	const shown = frame( 22, { battleRoyale: matchState( "running" ), battleRoyaleAirdrop: landed() } );
	auxiliary.presentStages( shown, bindings );
	const drawn = [ ...shown.next.values() ];
	assert.equal( drawn.length, 1 );
	assert.ok( FIREWORK_KINDS.some( kind => drawn[0].model === `effect:${drawn[0].gid}:${kind.model}` ) );
	assert.equal( drawn[0].effectEntity, true );
	assert.equal( sounds.length, 1, "the shot" );
	auxiliary.resetStages();
	const after = frame( 30, {} );
	auxiliary.presentStages( { ...after, gameplay: null }, bindings );
	assert.equal( after.next.size, 0, "a reset world draws none" );
});
