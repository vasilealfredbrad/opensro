/*
===========================================================================

fireworks.ts - the Battle Royale airdrop's fireworks

Port-only, not native (docs/battle-royale.md, Stage 9 item 2, which
replaces the Stage 2 item 8 fireworks). While an airdrop lies landed and
its guardian lives (0x3FB6 state 2), the game's own firework programs go
off at the airdrop point every 3 to 6 seconds for a participant who can
see the point, and stop when the server removes the airdrop (state 0).
Nothing else fires: the Stage 2 random fireworks in the zone and the
winner's burst are gone (owner, 2026-10-18: "no fireworks" but the
airdrop's). The client draws them from the airdrop frame; the server sends
no effect, and only participants hold an airdrop.

Each firework is one world-placed .efp program, drawn as a stage actor
beside the orbs (presentation-auxiliary.ts admits both) and voiced with
its item record's shot and explosion sounds (namedEffectRecords:
ITEM_ETC_FIREWORK_*). Like an orb, a firework's clock starts only once its
program is admitted, so none plays invisibly while the program loads.

The owner steps on the frame clock. A fixed pool of slots holds the
fireworks in flight: steady frames allocate nothing.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { SoundEvent } from "@/engine/contracts/audio";
import type { Pose } from "@/engine/contracts/gameplay";
import type { BattleRoyaleAirdrop } from "@/engine/foundation/gameplay/battle-royale-airdrop";
import { radians } from "@/engine/foundation/math/angles";

// The effect program bundle and the firework sounds' folder.
const PROGRAMS = "/assets/effects/programs.json#";
const SOUNDS = "/assets/audio/sfx/prim/snd/firework/";
// A firework program's one clip (effects.ts modelFor).
const PROGRAM_CLIP = "effect";

/*
================
FireworkKind

One firework: its program and its item record's two sound stages, the
shot at launch and the explosion after explosionMs (ITEM_ETC_FIREWORK_*
stage 2 move delay).
================
*/
interface FireworkKind {
	readonly model: string;
	readonly shot: string;
	readonly explosion: string;
	readonly explosionSeconds: number;
}

/*
================
fireworkKind
================
*/
function fireworkKind( program: string, shot: string, explosion: string, explosionMs: number ): FireworkKind {
	return {
		model: PROGRAMS + encodeURIComponent( program ),
		shot: SOUNDS + shot,
		explosion: SOUNDS + explosion,
		explosionSeconds: explosionMs / 1000
	};
}

// The shipped firework items' programs and sounds (namedEffectRecords).
export const FIREWORK_KINDS: readonly FireworkKind[] = [
	fireworkKind( "system/firework_star_shower.efp", "firework_shot_01.wav", "firework_explosion_04.wav", 1300 ),
	fireworkKind( "system/firework_chainbomb.efp", "firework_shot_03.wav", "firework_explosion_02.wav", 600 ),
	fireworkKind( "system/item_firework_bomb_r.efp", "firework_shot_01.wav", "firework_explosion_01.wav", 1300 ),
	fireworkKind( "system/item_firework_bomb_g.efp", "firework_shot_01.wav", "firework_explosion_01.wav", 1300 ),
	fireworkKind( "system/item_firework_bomb_b.efp", "firework_shot_01.wav", "firework_explosion_01.wav", 1300 ),
	fireworkKind( "system/item_firework_bomb_v.efp", "firework_shot_01.wav", "firework_explosion_01.wav", 1300 )
];

// The spec's cadence while an airdrop lies landed (Stage 9 item 2).
export const FIREWORK_MIN_INTERVAL_SECONDS = 3;
export const FIREWORK_MAX_INTERVAL_SECONDS = 6;
// INFERENCE: the spec says the fireworks go off at the airdrop point; a
// player farther from it than FIREWORK_SIGHT_UNITS (beyond the range the
// world draws characters and effects at) would not see one, so none is
// launched for it. Each goes up within FIREWORK_SCATTER_UNITS of the
// point, so two in the air at once do not draw over each other.
export const FIREWORK_SIGHT_UNITS = 1000;
export const FIREWORK_SCATTER_UNITS = 30;
// The item path lifts a firework 8 units over its user (effects.ts); the
// terrain under the airdrop is not resident here, so the player's own
// height stands for it (the player is within sight of the point).
const LAUNCH_LIFT = 8;
// A program whose duration is not known yet shows this long.
const FALLBACK_LIFE_SECONDS = 5;
// Fireworks in flight at once: a program runs a few seconds, one goes up
// every 3 to 6.
const MAX_FIREWORKS = 4;
const SOUND_SECONDS = .5;
// World grid units per region side; a dungeon region is off the grid.
const REGION_UNITS = 1920;
const DUNGEON_REGION = 0x8000;

/*
================
FireworkResources

The character resource owner's admission (resources.ts).
================
*/
export interface FireworkResources {
	ready( path: string ): boolean;
	failed( path: string ): boolean;
	duration( path: string, clip: string ): number;
}

/*
================
FireworkActor

A firework's stage actor; its clock is advanced in place every frame.
================
*/
type FireworkActor = Omit<CharacterActor, "time"> & { time: number; };

/*
================
idleActor

A free slot's actor, replaced at each launch.
================
*/
function idleActor(): FireworkActor {
	return {
		gid: 0,
		model: FIREWORK_KINDS[0]!.model,
		pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		clip: PROGRAM_CLIP,
		time: 0,
		loop: false,
		pickable: false,
		scale: 1,
		absoluteEffectScale: true
	};
}

/*
================
FireworkSlot

One firework in flight: pending until its program is admitted, then live
from started (frame seconds).
================
*/
interface FireworkSlot {
	active: boolean;
	kind: FireworkKind;
	started: number;
	exploded: boolean;
	serial: number;
	x: number;
	y: number;
	z: number;
	actor: FireworkActor;
}

/*
================
createFireworks

random returns [0, 1); it is injected so tests can place the fireworks.
================
*/
export function createFireworks(
	play: ( event: SoundEvent ) => void,
	allocate: () => number,
	random: () => number = Math.random
) {
	const slots: FireworkSlot[] = [];
	for ( let i = 0; i < MAX_FIREWORKS; i++ ) {
		slots.push( {
			active: false,
			kind: FIREWORK_KINDS[0]!,
			started: -1,
			exploded: false,
			serial: 0,
			x: 0,
			y: 0,
			z: 0,
			actor: idleActor()
		} );
	}
	// The live fireworks' actors, refilled by every step.
	const live: CharacterActor[] = [];
	// The next firework (NaN: not scheduled) and the launches so far.
	let nextAt = NaN, serial = 0;
	/*
	================
	roll

	One uniform draw in [0, 1) from the injected source.
	================
	*/
	const roll = () => random();
	/*
	================
	launch

	Takes a free slot for a random kind at a grid point; a full pool skips
	the firework. Each launch is a new presentation actor (its own id), as
	each orb is, so a reused slot never continues the last program.
	================
	*/
	const launch = ( x: number, y: number, z: number ) => {
		let slot: FireworkSlot | undefined;
		for ( const row of slots ) {
			if ( row.active ) continue;
			slot = row;
			break;
		}
		if ( !slot ) return;
		const kind =
			FIREWORK_KINDS[Math.min( FIREWORK_KINDS.length - 1, Math.floor( roll() * FIREWORK_KINDS.length ) )]!;
		slot.active = true;
		slot.kind = kind;
		slot.started = -1;
		slot.exploded = false;
		slot.serial = ++serial;
		slot.x = x;
		slot.y = y;
		slot.z = z;
		slot.actor = {
			...slot.actor,
			gid: allocate(),
			model: kind.model,
			pose: { regionId: 0, x, y, z, yaw: radians( roll() * 2 * Math.PI ) },
			time: 0
		};
	};
	/*
	================
	sound
	================
	*/
	const sound = ( slot: FireworkSlot, path: string, part: string, seconds: number ) =>
		play( {
			id: "firework:" + slot.serial + ":" + part,
			path,
			gain: 1,
			spatial: true,
			x: slot.x,
			y: slot.y,
			z: slot.z,
			expires: seconds + SOUND_SECONDS
		} );
	/*
	================
	schedule

	The next firework's time: FIREWORK_MIN_INTERVAL_SECONDS to
	FIREWORK_MAX_INTERVAL_SECONDS from seconds.
	================
	*/
	const schedule = ( seconds: number ) =>
		seconds + FIREWORK_MIN_INTERVAL_SECONDS +
		roll() * (FIREWORK_MAX_INTERVAL_SECONDS - FIREWORK_MIN_INTERVAL_SECONDS);
	return {
		/*
		================
		step

		Advances the fireworks to the frame clock's seconds for the airdrop
		and the local pose; the live ones' actors, in a list the next step
		refills. A landed airdrop fires one at once and then every 3 to 6
		seconds; any other state stops the schedule (those in the air burn
		out).
		================
		*/
		step(
			airdrop: BattleRoyaleAirdrop | undefined,
			pose: Pick<Pose, "regionId" | "x" | "y" | "z"> | null | undefined,
			seconds: number,
			resources: FireworkResources
		): readonly CharacterActor[] {
			live.length = 0;
			if ( airdrop?.state !== "landed" ) nextAt = NaN;
			else if ( Number.isNaN( nextAt ) || seconds >= nextAt ) {
				nextAt = schedule( seconds );
				const placed = !!pose && !(pose.regionId & DUNGEON_REGION);
				// The player on the world grid (battleRoyaleGridPoint, without its
				// allocation).
				const px = placed ? (pose!.regionId & 255) * REGION_UNITS + pose!.x : 0,
					pz = placed ? (pose!.regionId >>> 8) * REGION_UNITS + pose!.z : 0;
				if ( placed && Math.hypot( airdrop.x - px, airdrop.z - pz ) <= FIREWORK_SIGHT_UNITS ) {
					const angle = roll() * 2 * Math.PI, reach = roll() * FIREWORK_SCATTER_UNITS;
					launch(
						airdrop.x + Math.cos( angle ) * reach,
						pose!.y + LAUNCH_LIFT,
						airdrop.z + Math.sin( angle ) * reach
					);
				}
			}
			for ( const slot of slots ) {
				if ( !slot.active ) continue;
				const model = slot.kind.model;
				if ( slot.started < 0 ) {
					if ( resources.failed( model ) ) {
						slot.active = false;
						continue;
					}
					if ( !resources.ready( model ) ) continue;
					slot.started = seconds;
					sound( slot, slot.kind.shot, "shot", seconds );
				}
				const age = seconds - slot.started;
				if ( !slot.exploded && age >= slot.kind.explosionSeconds ) {
					slot.exploded = true;
					sound( slot, slot.kind.explosion, "explosion", seconds );
				}
				const life = resources.duration( model, PROGRAM_CLIP ) || FALLBACK_LIFE_SECONDS;
				if ( age >= life ) {
					slot.active = false;
					continue;
				}
				slot.actor.time = age;
				live.push( slot.actor );
			}
			return live;
		},
		/*
		================
		reset

		A new world: nothing in flight, nothing scheduled.
		================
		*/
		reset() {
			for ( const slot of slots ) slot.active = false;
			live.length = 0;
			nextAt = NaN;
		}
	};
}
