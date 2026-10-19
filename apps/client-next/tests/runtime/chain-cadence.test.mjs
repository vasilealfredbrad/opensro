/*
===========================================================================

chain-cadence.test.mjs - the shard's Chain Lightning hop period (port-only)

The bootstrap rule parses only when well formed; the cadence's own clock
(wait, slide, leave) follows the period; and the effect owner's AT_MOV_SPLASH
chain keeps its native timing without the rule, or for any other skill,
while under it each next victim's hit lands one period after the last hop
left, with the bolt unseen in between and re-aimed as it leaves.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { chainCadence, chainGap, chainHopLeave, chainHopWait, chainLightningHopFrom } = await import(
	"../../src/engine/foundation/animation/chain-cadence.ts"
);
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

// The cast's skill id, and the owner's rule timing it.
const SKILL = 1;
const OWNER_HOP = { hopMs: 500, skills: [ SKILL ] };
// The bolt's speed (units a second) and the frame step (seconds): exact in
// binary, so every clock below is exact.
const SPEED = 100;
const FRAME = .125;
// The shot event: the bolt leaves the caster here.
const LAUNCH = .25;

/*
================
bolt

A Chain Lightning-like stage: one AT_MOV_SPLASH mover through every result.
================
*/
const bolt = {
	resource: "bolt.efp",
	phase: "SHOT",
	startEvent: 1,
	damageEvent: true,
	action: "AT_MOV_SPLASH",
	move: "MOV_STRAIGHT",
	movement: { delayMs: 0, startSpeed: SPEED, endSpeed: SPEED },
	bone: null,
	targetBone: null,
	offset: [ 0, 0, 0 ],
	targetOffset: [ 0, 0, 0 ],
	life: 0,
	count: 1,
	scripts: []
};

/*
================
chainFixture

The caster (gid 1) at x 0 and one victim per entry of xs (gids 2, 3, ...),
the cast striking all of them, its shot event at LAUNCH. step advances the
owner to now with the given rule published in the gameplay state.
================
*/
function chainFixture( xs, hop, skill = SKILL ) {
	let serial = 0;
	const jobs = new Map(), random = createPresentationRandom( 7, 1, 100 );
	const catalog = { [skill]: { clips: [], stages: [ bolt ] } };
	const owner = createCharacterEffects(
		/** @type {any} The asset owner's request, take and cancel only. */ ({
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		}),
		"http://localhost",
		() => {},
		random
	);
	/** @type {any[]} Only the fields the effect owner reads. */
	const entities = [ 0, ...xs ].map( ( x, i ) => ({
		gid: i + 1,
		kind: i === 0 ? "local-player" : "monster",
		regionId: 257,
		x,
		y: 0,
		z: 0,
		heading: 0
	}) );
	const victims = xs.map( ( _, i ) => i + 2 );
	/** @type {any} Only the fields the effect and feedback owners read. */
	const cast = {
		token: 1,
		caster: 1,
		target: 2,
		skill,
		results: victims.map( target => ({ target, impacts: [ { damage: 10 } ] }) )
	};
	const trigger = { cast, phase: "SHOT", event: 1, at: LAUNCH };
	/** @type {any} Only the channels the effect owner reads. */
	const gameplay = hop ? { casts: [ cast ], chainLightningHop: hop } : { casts: [ cast ] };
	const step = ( now, events = [] ) => owner.step( entities, gameplay, now, () => true, () => 10, events );
	step( 0 );
	step( .125 );
	step( LAUNCH, [ trigger ] );
	return { owner, entities, cast, trigger, step };
}

/*
================
runChain

Frames from the launch to until: each victim's hop time and position,
and the frames at which the bolt itself was drawn. onFrame runs before
each frame's step.
================
*/
/**
 * @param {ReturnType<typeof chainFixture>} f
 * @param {number} until
 * @param {( now: number ) => void} [onFrame]
 */
function runChain( f, until, onFrame = () => {} ) {
	const hops = [], drawn = [];
	for ( let now = LAUNCH + FRAME; now <= until; now += FRAME ) {
		onFrame( now );
		if ( f.step( now ).some( actor => actor.model.includes( "bolt.efp" ) ) ) drawn.push( now );
		for ( const event of f.owner.takeImpacts() ) {
			if ( event.kind === "hop" ) {
				hops.push( { target: event.target, at: event.at, x: defined( event.position ).x } );
			}
		}
	}
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
	return { hops, drawn };
}

test("the bootstrap rule parses only when well formed", () => {
	assert.deepEqual( chainLightningHopFrom( { chainLightningHop: { hopMs: 500, skills: [ 8982, 8983 ] } } ), {
		hopMs: 500,
		skills: [ 8982, 8983 ]
	} );
	for (
		const bad of [
			null,
			{},
			{ chainLightningHop: null },
			{ chainLightningHop: { hopMs: 0, skills: [ 1 ] } },
			{ chainLightningHop: { hopMs: 2001, skills: [ 1 ] } },
			{ chainLightningHop: { hopMs: 12.5, skills: [ 1 ] } },
			{ chainLightningHop: { hopMs: "500", skills: [ 1 ] } },
			{ chainLightningHop: { hopMs: 500, skills: [] } },
			{ chainLightningHop: { hopMs: 500, skills: [ 0 ] } },
			{ chainLightningHop: { hopMs: 500, skills: [ "8982" ] } },
			{ chainLightningHop: { hopMs: 500 } }
		]
	) assert.equal( chainLightningHopFrom( bad ), undefined, JSON.stringify( bad ) );
});

test("the cadence times only the rule's skills and keeps the launch speed for the first hop", () => {
	assert.equal( chainCadence( undefined, SKILL, LAUNCH, SPEED ), undefined );
	assert.equal( chainCadence( OWNER_HOP, SKILL + 1, LAUNCH, SPEED ), undefined );
	assert.deepEqual( chainCadence( OWNER_HOP, SKILL, LAUNCH, SPEED ), {
		period: .5,
		anchor: LAUNCH,
		hops: 1,
		speed: SPEED,
		departing: false
	} );
});

test("a hop waits for its period, slides a late schedule and flies faster only when the gap needs it", () => {
	const cadence = defined( chainCadence( OWNER_HOP, SKILL, 0, SPEED ), "cadence" );
	assert.equal( chainHopWait( cadence, .125 ), .375, "hop 1 is due one period after the launch" );
	assert.equal( cadence.departing, true );
	chainHopLeave( cadence, SPEED, 10, .5 );
	assert.deepEqual( [ cadence.hops, cadence.speed, cadence.departing ], [ 2, SPEED, false ], "native speed fits" );
	assert.equal( chainHopWait( cadence, .625 ), .375, "hop 2 is due two periods after the launch" );
	chainHopLeave( cadence, SPEED, 1000, 1 );
	assert.equal( cadence.speed, 2000, "1000 units must land within the half second left" );
	assert.equal( chainHopWait( cadence, 1.75 ), 0, "a bolt arriving after its hop was due leaves at once" );
	assert.equal( cadence.anchor, .25, "and the schedule slides instead of catching up" );
	chainHopLeave( cadence, SPEED, 0, 1.75 );
	assert.equal( chainHopWait( cadence, 1.875 ), .375 );
});

test("the gap is measured across regions", () => {
	const at = ( regionId, x, y, z ) => ({ regionId, x, y, z, yaw: radians( 0 ) });
	assert.equal( chainGap( at( 257, 1910, 0, 0 ), at( 258, 10, 0, 0 ) ), 20 );
	assert.equal( chainGap( at( 257, 0, 0, 1910 ), at( 513, 0, 0, 10 ) ), 20 );
	assert.equal( chainGap( at( 257, 0, 3, 0 ), at( 257, 4, 0, 0 ) ), 5 );
});

test("without the rule, or for another skill, the chain keeps its native timing", () => {
	const native = { hops: [ [ 2, .5 ], [ 3, .625 ], [ 4, .75 ] ] };
	for ( const f of [ chainFixture( [ 10, 20, 30 ] ), chainFixture( [ 10, 20, 30 ], OWNER_HOP, SKILL + 1 ) ] ) {
		const { hops } = runChain( f, 1.5 );
		assert.deepEqual( hops.map( h => [ h.target, h.at ] ), native.hops );
	}
});

test("under the rule each next victim is struck one period after the last hop left, the bolt unseen between", () => {
	const { hops, drawn } = runChain( chainFixture( [ 10, 20, 30 ], OWNER_HOP ), 2 );
	// The primary on the native arrival, then hop k at the launch plus k periods.
	assert.deepEqual( hops.map( h => [ h.target, h.at ] ), [ [ 2, .5 ], [ 3, .75 ], [ 4, 1.25 ] ] );
	assert.deepEqual( hops.map( h => h.x ), [ 10, 20, 30 ] );
	for ( const waiting of [ .625, .875, 1, 1.125 ] ) {
		assert.ok( !drawn.includes( waiting ), `the bolt is drawn while it waits at ${waiting}` );
	}
	assert.ok( drawn.includes( .5 ) && drawn.includes( .75 ), "the bolt shows where it strikes" );
});

test("a gap too long for one period is flown in it, and a victim that moved while the bolt waited is met", () => {
	const far = runChain( chainFixture( [ 10, 20, 1010 ], OWNER_HOP ), 2.5 );
	// 990 units at 100 a second would take 9.9 s. The hop leaves at 1.25 at
	// 1980 a second, flying its full step on the leaving frame as a native
	// delayed mover does (8D9360), so it lands before hop 4 would be due.
	assert.deepEqual( far.hops.map( h => [ h.target, h.at ] ), [ [ 2, .5 ], [ 3, .75 ], [ 4, 1.625 ] ] );
	const f = chainFixture( [ 10, 20, 30 ], OWNER_HOP );
	const moved = runChain( f, 2, now => {
		if ( now === .625 ) f.entities[2].x = 25;
	} );
	assert.deepEqual( moved.hops.map( h => [ h.target, h.x ] ), [ [ 2, 10 ], [ 3, 25 ], [ 4, 30 ] ] );
});

test("each victim's hit, and its HP drop, is released when the bolt reaches it", () => {
	const f = chainFixture( [ 10, 20, 30 ], OWNER_HOP ), feedback = createDamageFeedback();
	const released = [];
	feedback.take( [ f.cast ], [ f.trigger ], () => 0, LAUNCH, LAUNCH * 1000, f.owner.takeImpacts() );
	for ( let now = LAUNCH + FRAME; now <= 2; now += FRAME ) {
		f.step( now );
		for ( const hit of feedback.take( [ f.cast ], [], () => 0, now, now * 1000, f.owner.takeImpacts() ) ) {
			released.push( [ hit.target, hit.at ] );
		}
	}
	assert.deepEqual( released, [ [ 2, .5 ], [ 3, .75 ], [ 4, 1.25 ] ] );
	f.owner.dispose();
});
