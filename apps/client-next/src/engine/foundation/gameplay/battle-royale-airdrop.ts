/*
===========================================================================

battle-royale-airdrop.ts - the Battle Royale airdrop wire and its rules

Port-only, not native (docs/battle-royale.md, Stages 8b and 9). At each
zone phase the server drops an airdrop guarded by a giant. 0x3FB6 tells
every participant about it: version 2 first announces the flight (state 1,
a straight line across the next circle and the time to landing), then the
landing (state 2, the point on that line), and removes it (state 0) when
the guardian dies or the phase ends. Version 1, the Stage 8b frame, is
still read as a landing without a line. This module decodes the frame and
answers what the HUD asks of it: whether the maps show the line or the
mark, and the line under the zone line ("Airdrop lands in 42 s" while it
flies, the distance once it landed). The banners that go with an airdrop
("Airdrop incoming", "<name> found the ultimate weapon!") are the
server's channel-7 notices and need nothing here.

battle-royale.ts holds the rest of the match wire; this file stands apart
only because that one is full.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { battleRoyaleGridPoint, type BattleRoyaleState } from "./battle-royale";

export const OP_BATTLE_ROYALE_AIRDROP = 0x3fb6;
// Version 1 (Stage 8b): version, active (u8), x and z (f32), phase (u8).
const AIRDROP_V1 = 1;
const AIRDROP_V1_BYTES = 11;
// Version 2 (Stage 9): version, state, phase (u8), ax az bx bz x z (f32),
// etaMs (u32).
const AIRDROP_V2 = 2;
const AIRDROP_V2_BYTES = 31;
const AIRDROP_NONE = 0;
const AIRDROP_INCOMING = 1;
const AIRDROP_LANDED = 2;
// World grid units per metre (a region is 1920 units, 192 m).
const GRID_UNITS_PER_METER = 10;
const MS_PER_SECOND = 1000;

/*
================
BattleRoyaleAirdropLine

The flight path in world grid units, from (ax, az) to (bx, bz).
================
*/
export interface BattleRoyaleAirdropLine {
	readonly ax: number;
	readonly az: number;
	readonly bx: number;
	readonly bz: number;
}

/*
================
BattleRoyaleIncomingAirdrop

An airdrop on its way: the flight line, the zone phase and when it lands
on the simulation clock (receipt plus etaMs).
================
*/
export interface BattleRoyaleIncomingAirdrop {
	readonly state: "incoming";
	readonly phase: number;
	readonly line: BattleRoyaleAirdropLine;
	readonly landsAtMs: number;
	readonly receivedAtMs: number;
}

/*
================
BattleRoyaleLandedAirdrop

A landed airdrop: its grid position, the zone phase, the line it flew
(absent from a version 1 frame) and when the mark arrived.
================
*/
export interface BattleRoyaleLandedAirdrop {
	readonly state: "landed";
	readonly phase: number;
	readonly x: number;
	readonly z: number;
	readonly line?: BattleRoyaleAirdropLine;
	readonly receivedAtMs: number;
}

/*
================
BattleRoyaleAirdrop
================
*/
export type BattleRoyaleAirdrop = BattleRoyaleIncomingAirdrop | BattleRoyaleLandedAirdrop;

/*
================
decodeBattleRoyaleAirdrop

One 0x3FB6 payload: the airdrop, or null when the frame removes it. A
malformed frame throws: a size that does not match its version, an unknown
version or state, a coordinate that is not finite.
================
*/
export function decodeBattleRoyaleAirdrop( payload: Uint8Array, nowMs: number ): BattleRoyaleAirdrop | null {
	if ( payload.length < 1 ) throw new Error( "Truncated battle royale airdrop" );
	const version = payload[0];
	if ( version !== AIRDROP_V1 && version !== AIRDROP_V2 ) {
		throw new Error( "Unknown battle royale airdrop version" );
	}
	const size = version === AIRDROP_V1 ? AIRDROP_V1_BYTES : AIRDROP_V2_BYTES;
	if ( payload.length < size ) throw new Error( "Truncated battle royale airdrop" );
	if ( payload.length > size ) throw new Error( "Trailing battle royale airdrop bytes" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	if ( version === AIRDROP_V1 ) return decodeVersion1( payload, view, nowMs );
	const state = payload[1];
	if ( state !== AIRDROP_NONE && state !== AIRDROP_INCOMING && state !== AIRDROP_LANDED ) {
		throw new Error( "Invalid battle royale airdrop state" );
	}
	const line = {
			ax: view.getFloat32( 3, true ),
			az: view.getFloat32( 7, true ),
			bx: view.getFloat32( 11, true ),
			bz: view.getFloat32( 15, true )
		},
		x = view.getFloat32( 19, true ),
		z = view.getFloat32( 23, true ),
		etaMs = view.getUint32( 27, true );
	if ( ![ line.ax, line.az, line.bx, line.bz, x, z ].every( Number.isFinite ) ) {
		throw new Error( "Invalid battle royale airdrop position" );
	}
	const phase = payload[2]!;
	if ( state === AIRDROP_NONE ) return null;
	if ( state === AIRDROP_INCOMING ) {
		return { state: "incoming", phase, line, landsAtMs: nowMs + etaMs, receivedAtMs: nowMs };
	}
	// A landing may carry the line it flew; an all-zero one is none.
	const flown = line.ax !== line.bx || line.az !== line.bz;
	return { state: "landed", phase, x, z, ...(flown ? { line } : {}), receivedAtMs: nowMs };
}

/*
================
decodeVersion1

The Stage 8b frame: a landing without a line (active 1) or the removal.
================
*/
function decodeVersion1( payload: Uint8Array, view: DataView, nowMs: number ): BattleRoyaleLandedAirdrop | null {
	const active = payload[1];
	if ( active !== 1 && active !== 0 ) throw new Error( "Invalid battle royale airdrop state" );
	const x = view.getFloat32( 2, true ), z = view.getFloat32( 6, true );
	if ( !Number.isFinite( x ) || !Number.isFinite( z ) ) throw new Error( "Invalid battle royale airdrop position" );
	if ( active === 0 ) return null;
	return { state: "landed", phase: payload[10]!, x, z, receivedAtMs: nowMs };
}

/*
================
battleRoyaleParticipant

The maps and the HUD show the airdrop to every participant of a running
match, alive or eliminated, as they show the living bots (Stage 8b: "marks
it for every participant").
================
*/
function battleRoyaleParticipant( state: BattleRoyaleState | undefined ): boolean {
	return state?.phase === "running" && (state.role === "alive" || state.role === "eliminated");
}

/*
================
battleRoyaleShowsAirdrop

Whether the maps show the landed airdrop's mark.
================
*/
export function battleRoyaleShowsAirdrop(
	state: BattleRoyaleState | undefined,
	airdrop: BattleRoyaleAirdrop | undefined
): airdrop is BattleRoyaleLandedAirdrop {
	return battleRoyaleParticipant( state ) && airdrop?.state === "landed";
}

/*
================
battleRoyaleShowsAirdropLine

Whether the maps show an incoming airdrop's flight line (Stage 9 item 5).
At landing the line goes and the mark comes.
================
*/
export function battleRoyaleShowsAirdropLine(
	state: BattleRoyaleState | undefined,
	airdrop: BattleRoyaleAirdrop | undefined
): airdrop is BattleRoyaleIncomingAirdrop {
	return battleRoyaleParticipant( state ) && airdrop?.state === "incoming";
}

/*
================
battleRoyaleAirdropMeters

Whole metres from a pose to the landed airdrop, or null in a dungeon region
(its local frame is not on the world grid).
================
*/
export function battleRoyaleAirdropMeters(
	airdrop: Pick<BattleRoyaleLandedAirdrop, "x" | "z">,
	pose: Pick<Pose, "regionId" | "x" | "z">
): number | null {
	const local = battleRoyaleGridPoint( pose );
	if ( !local ) return null;
	return Math.round( Math.hypot( airdrop.x - local.x, airdrop.z - local.z ) / GRID_UNITS_PER_METER );
}

/*
================
battleRoyaleAirdropSeconds

Whole seconds until an incoming airdrop lands at simulationMs, never below
zero.
================
*/
export function battleRoyaleAirdropSeconds( airdrop: BattleRoyaleIncomingAirdrop, simulationMs: number ): number {
	return Math.max( 0, Math.ceil( (airdrop.landsAtMs - simulationMs) / MS_PER_SECOND ) );
}

/*
================
battleRoyaleAirdropText

The HUD line under the zone line: "Airdrop lands in 42 s" while one flies,
"Airdrop: 240 m" once it landed; empty when the HUD shows none or the
player has no grid pose for the distance. simulationMs is the clock the
landing time is on.
================
*/
export function battleRoyaleAirdropText(
	state: BattleRoyaleState | undefined,
	airdrop: BattleRoyaleAirdrop | undefined,
	pose: Pick<Pose, "regionId" | "x" | "z"> | undefined,
	simulationMs: number
): string {
	if ( battleRoyaleShowsAirdropLine( state, airdrop ) ) {
		return `Airdrop lands in ${battleRoyaleAirdropSeconds( airdrop, simulationMs )} s`;
	}
	if ( !pose || !battleRoyaleShowsAirdrop( state, airdrop ) ) return "";
	const meters = battleRoyaleAirdropMeters( airdrop, pose );
	return meters === null ? "" : `Airdrop: ${meters} m`;
}
