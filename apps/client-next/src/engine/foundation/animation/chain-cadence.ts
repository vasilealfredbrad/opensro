/*
===========================================================================

chain-cadence.ts - the shard's Chain Lightning hop period (port-only)

Port-only, not native. Natively an AT_MOV_SPLASH mover flies caster ->
first result target -> each next one at its own speed (effects.ts, after
8D8AD0), so a chain's hits follow as fast as the bolt covers the gaps:
Chain Lightning's wizard_light_lighting_c.efp crosses its native 10 m at
400 units a second.

Under the server's SRO_CHAIN_LIGHTNING_HOP_MS the world-entry bootstrap
names a period and the Chain Lightning skill ids (server enterworld
ChainLightningHop). Hop k then leaves its victim k periods after the bolt
left the caster: the bolt waits unseen at each victim, as a native mover
waits out its authored delay, and flies the gap at the mover's own speed,
faster only when the gap would not fit in one period. Each victim's hit,
HP drop and death still show as the bolt arrives, through the native
result hand-off (damage-feedback.ts, effective-hp.ts); the server keeps
each corpse long enough for it (action/chainlightning.go). Absent, every
chain flies natively.

===========================================================================
*/
import { hypot3 } from "@/engine/foundation/math/hypot";
import type { CharacterActor } from "@/engine/contracts/character";

type Position = CharacterActor["pose"];

// The server's bound on the period (action maxChainLightningHopMs) and on a
// rank's two-digit level, which bounds the skill list.
const MAX_HOP_MS = 2000;
const MAX_HOP_SKILLS = 99;
// The shortest flight a hop that leaves late is given, in seconds: about
// one 60 Hz frame.
const MIN_HOP_FLIGHT = 0.016;

/*
================
ChainLightningHop

The shard's rule as the bootstrap carries it: the period in milliseconds
and the skill ids whose chain it times.
================
*/
export interface ChainLightningHop {
	readonly hopMs: number;
	readonly skills: readonly number[];
}

/*
================
ChainCadence

One chain flight's clock. anchor is when hop 0 left the caster, slid later
whenever the bolt reaches a victim after its next hop was due, so a late
hop never makes the rest catch up in a burst. hops counts the hops that
have left (the caster's included); speed is the flying hop's; departing
marks a hop waiting to leave.
================
*/
export interface ChainCadence {
	readonly period: number;
	anchor: number;
	hops: number;
	speed: number;
	departing: boolean;
}

/*
================
chainLightningHopFrom

The rule a bootstrap value carries (its `chainLightningHop` field), or
undefined: an older or native shard sends none, and a malformed one is
ignored rather than trusted, so it can only leave the native flight.
================
*/
export function chainLightningHopFrom( bootstrap: unknown ): ChainLightningHop | undefined {
	const raw = (bootstrap as { chainLightningHop?: unknown; } | null)?.chainLightningHop;
	if ( !raw || typeof raw !== "object" ) return undefined;
	const { hopMs, skills } = raw as { hopMs?: unknown; skills?: unknown; };
	if ( typeof hopMs !== "number" || !Number.isInteger( hopMs ) || hopMs < 1 || hopMs > MAX_HOP_MS ) return undefined;
	if ( !Array.isArray( skills ) || !skills.length || skills.length > MAX_HOP_SKILLS ) return undefined;
	if ( !skills.every( id => typeof id === "number" && Number.isInteger( id ) && id > 0 ) ) return undefined;
	return { hopMs, skills: [ ...skills ] as number[] };
}

/*
================
chainCadence

The clock for a chain flight of skill that leaves the caster at launch at
speed, or undefined when the shard's rule does not time this skill.
================
*/
export function chainCadence(
	hop: ChainLightningHop | undefined,
	skill: number,
	launch: number,
	speed: number
): ChainCadence | undefined {
	if ( !hop || !hop.skills.includes( skill ) ) return undefined;
	return { period: hop.hopMs / 1000, anchor: launch, hops: 1, speed, departing: false };
}

/*
================
chainHopWait

The bolt reached a victim at arrivedAt and has a next one: how long it
waits there, unseen, before the next hop leaves. A bolt that arrives after
the hop was due leaves at once and slides the schedule.
================
*/
export function chainHopWait( cadence: ChainCadence, arrivedAt: number ): number {
	cadence.departing = true;
	const due = cadence.anchor + cadence.hops * cadence.period;
	if ( due >= arrivedAt ) return due - arrivedAt;
	cadence.anchor = arrivedAt - cadence.hops * cadence.period;
	return 0;
}

/*
================
chainHopLeave

The waiting hop leaves at now across distance: at the mover's own speed,
or faster when that would not land before the following hop is due.
================
*/
export function chainHopLeave( cadence: ChainCadence, speed: number, distance: number, now: number ): void {
	const window = Math.max( MIN_HOP_FLIGHT, cadence.anchor + (cadence.hops + 1) * cadence.period - now );
	cadence.speed = Math.max( speed, distance / window );
	cadence.hops++;
	cadence.departing = false;
}

/*
================
chainGap

The straight distance from start to end across regions; inside a dungeon
the coordinates are already shared (projectile-time.ts projectileSpace).
================
*/
export function chainGap( start: Position, end: Position ): number {
	const dungeon = !!(start.regionId & 0x8000);
	const dx = end.x + (dungeon ? 0 : ((end.regionId & 255) - (start.regionId & 255)) * 1920) - start.x;
	const dz = end.z + (dungeon ? 0 : ((end.regionId >>> 8) - (start.regionId >>> 8)) * 1920) - start.z;
	return hypot3( dx, end.y - start.y, dz );
}
