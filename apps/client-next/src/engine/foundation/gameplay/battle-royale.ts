/*
===========================================================================

battle-royale.ts - the Battle Royale match wire and its pure rules

Port-only, not native (docs/battle-royale.md, SRO_BATTLE_ROYALE). The
server pushes the match as the player sees it in 0x3FB2, the scoreboard in
0x3FB3, the bot roster in 0x3FB4 and the living bots' positions in 0x3FB5,
and takes a build pick in 0x7FB2 and a GM's bot settings in 0x7FB4;
nothing arrives while the server
flag is off, so the client shows nothing. This module decodes and encodes
those frames, owns the build table the picker lists, and answers the
questions the HUD asks of a published match: the zone circle at a moment,
whether a pose stands outside it, the lines the HUD shows and the rows of
the results window.

Times on the wire are milliseconds remaining when the frame was sent. The
decoder turns them into deadlines on the simulation clock at receipt, so
the HUD counts down and the zone shrinks without a frame per second.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { Pose } from "@/engine/contracts/gameplay";

export const OP_BATTLE_ROYALE_STATE = 0x3fb2;
export const OP_BATTLE_ROYALE_PICK = 0x7fb2;
export const OP_BATTLE_ROYALE_SCOREBOARD = 0x3fb3;
export const OP_BATTLE_ROYALE_BOTS = 0x3fb4;
export const OP_BATTLE_ROYALE_BOT_SWITCH = 0x7fb4;
export const OP_BATTLE_ROYALE_BOT_POSITIONS = 0x3fb5;
// Stage 5 roster: version, botsEnabled, count (u8). Stage 7 roster: version,
// botsEnabled, difficulty, configured (u16), count (u16). Each bot is gid
// (u32), nameLen (u16) and the name's UTF-8 bytes (docs/battle-royale.md).
const BOTS_VERSION_STAGE5 = 1;
const BOTS_VERSION = 2;
const BOTS_STAGE5_HEADER_BYTES = 3;
const BOTS_HEADER_BYTES = 7;
const BOT_GID_BYTES = 4;
const BOT_NAME_LENGTH_BYTES = 2;
// The bots a switched-on lobby adds to the next match by default, and the
// range a GM may choose (Stage 7).
export const BATTLE_ROYALE_BOT_COUNT = 20;
export const BATTLE_ROYALE_MIN_BOTS = 1;
export const BATTLE_ROYALE_MAX_BOTS = 200;
// Bot difficulties on the wire: 0 easy, 1 normal, 2 hard (Stage 7). A stage-5
// roster carries none and means normal.
export const BATTLE_ROYALE_BOT_EASY = 0;
export const BATTLE_ROYALE_BOT_NORMAL = 1;
export const BATTLE_ROYALE_BOT_HARD = 2;
// 0x7FB4 version 2: enabled (u8), difficulty (u8), count (u16).
const BOT_SWITCH_BYTES = 4;
// 0x3FB5: version, count (u16); each bot is gid (u32), x and z (f32 grid
// units) and level (u8).
const BOT_POSITIONS_VERSION = 1;
const BOT_POSITIONS_HEADER_BYTES = 3;
const BOT_POSITION_BYTES = 13;
const BATTLE_ROYALE_VERSION = 1;
const SCOREBOARD_VERSION = 1;
// version, final, count; each entry is nameLen (u16), the name's UTF-8
// bytes, kills (u16), placement (u8) and self (u8).
const SCOREBOARD_HEADER_BYTES = 3;
const SCORE_NAME_LENGTH_BYTES = 2;
const SCORE_TAIL_BYTES = 4;
// A participant name's byte budget, as the winner's.
const MAX_SCORE_NAME_BYTES = 64;
// version, state, role, build; lobby circle; members, alive; countdownMs;
// phase, phases, stage; stageMs; current and next circles; damagePerSec;
// winnerLen. The winner's UTF-8 bytes follow.
const BATTLE_ROYALE_FIXED_BYTES = 59;
// The winner name's byte budget (a character name is far shorter).
const MAX_WINNER_BYTES = 64;
// A match starts with this many lobby members (docs/battle-royale.md, Lobby).
export const BATTLE_ROYALE_MIN_PLAYERS = 2;
// With the bots switched on one real player is enough (Stage 5: the
// minimum counts bots).
const BATTLE_ROYALE_MIN_PLAYERS_WITH_BOTS = 1;
// The build a member who never picks plays (docs/battle-royale.md, Builds).
export const BATTLE_ROYALE_DEFAULT_BUILD = 11;
// How long the result line stays after the finishing frame. INFERENCE: the
// spec names no end for it, and a restored winner may never see state 0.
export const BATTLE_ROYALE_RESULT_MS = 15000;
// World grid units per region side (1 m = 10 units).
const REGION_UNITS = 1920;
const DUNGEON_REGION = 0x8000;
const MS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;

export type BattleRoyalePhase = "lobby" | "countdown" | "running" | "finished";
export type BattleRoyaleRole = "outside" | "member" | "alive" | "eliminated";
export type BattleRoyaleStage = "waiting" | "shrinking" | "final";

/*
================
BattleRoyaleCircle

A circle in world grid units (sectorX * 1920 + local x, likewise z).
================
*/
export interface BattleRoyaleCircle {
	readonly x: number;
	readonly z: number;
	readonly radius: number;
}

// The lobby hint shows within this many world units of the lobby edge.
const LOBBY_HINT_UNITS = 600;
const UNITS_PER_METER = 10;
const SECTOR_UNITS = 1920;

/*
================
battleRoyaleLobbyFrom

The bootstrap's lobby circle (enterworld/brlobby.go) in world grid units,
or undefined when the shard runs no Battle Royale.
================
*/
export function battleRoyaleLobbyFrom( value: unknown ): BattleRoyaleCircle | undefined {
	const lobby = (value as { battleRoyaleLobby?: unknown; } | null)?.battleRoyaleLobby as {
		regionId?: unknown;
		x?: unknown;
		z?: unknown;
		radiusM?: unknown;
	} | undefined;
	if ( lobby === undefined ) return undefined;
	const { regionId, x, z, radiusM } = lobby;
	if (
		!Number.isInteger( regionId ) || (regionId as number) <= 0 || (regionId as number) > 0x7fff ||
		typeof x !== "number" || typeof z !== "number" || typeof radiusM !== "number" ||
		![ x, z, radiusM ].every( Number.isFinite ) || radiusM <= 0
	) throw Error( "Invalid Battle Royale lobby" );
	return {
		x: ((regionId as number) & 0xff) * SECTOR_UNITS + x,
		z: ((regionId as number) >>> 8) * SECTOR_UNITS + z,
		radius: radiusM * UNITS_PER_METER
	};
}

/*
================
battleRoyaleLobbyHint

The line a player near the lobby who is not yet a member reads, from its
grid position; empty when far away or already part of a match.
================
*/
export function battleRoyaleLobbyHint(
	lobby: BattleRoyaleCircle | undefined,
	state: BattleRoyaleState | undefined,
	x: number,
	z: number
): string {
	if ( !lobby || (state && state.role !== "outside") ) return "";
	const distance = Math.hypot( x - lobby.x, z - lobby.z );
	if ( distance > lobby.radius + LOBBY_HINT_UNITS ) return "";
	return distance <= lobby.radius ?
		"Battle Royale - joining..." :
		`Battle Royale lobby - stand in the red circle to join (${BATTLE_ROYALE_MIN_PLAYERS} players start the match)`;
}

/*
================
BattleRoyaleState

One 0x3FB2 frame as published to presentation: the wire's remaining times
became deadlines on the simulation clock (receivedAtMs is that clock at
receipt). sequence counts frames, so a repeated frame is still news.
================
*/
export interface BattleRoyaleState {
	readonly phase: BattleRoyalePhase;
	readonly role: BattleRoyaleRole;
	readonly build: number;
	readonly lobby: BattleRoyaleCircle;
	readonly members: number;
	readonly alive: number;
	readonly countdownEndsAtMs: number;
	readonly zonePhase: number;
	readonly zonePhases: number;
	readonly stage: BattleRoyaleStage;
	readonly stageStartedAtMs: number;
	readonly stageEndsAtMs: number;
	readonly current: BattleRoyaleCircle;
	readonly next: BattleRoyaleCircle;
	readonly damagePerSec: number;
	readonly winner: string;
	readonly receivedAtMs: number;
	readonly sequence: number;
}

/*
================
BattleRoyaleScore

One 0x3FB3 entry: placement 0 while the participant is alive, else 1 for
the winner, 2 for second and so on; self marks the receiving player.
================
*/
export interface BattleRoyaleScore {
	readonly name: string;
	readonly kills: number;
	readonly placement: number;
	readonly self: boolean;
}

/*
================
BattleRoyaleScoreboard

One 0x3FB3 frame as published to presentation. final marks the match's
end (the results window opens); sequence counts frames, so a repeated
frame is still news.
================
*/
export interface BattleRoyaleScoreboard {
	readonly final: boolean;
	readonly entries: readonly BattleRoyaleScore[];
	readonly sequence: number;
}

/*
================
BattleRoyaleBuild

One row of the build table (docs/battle-royale.md, Builds). The id fixes
the race; the server owns what the build grants.
================
*/
export interface BattleRoyaleBuild {
	readonly id: number;
	readonly race: "CH" | "EU";
	readonly name: string;
	readonly masteries: string;
	readonly weapons: string;
}

/*
================
battleRoyaleBuilds

The build table, Chinese builds first, in the spec's order.
================
*/
export function battleRoyaleBuilds(): readonly BattleRoyaleBuild[] {
	return [
		{ id: 1, race: "CH", name: "Blade-Fire", masteries: "Blade, Fire", weapons: "Sword, Blade (+ shield)" },
		{ id: 2, race: "CH", name: "Glaive-Lightning", masteries: "Spear, Lightning", weapons: "Spear, Glaive" },
		{ id: 3, race: "CH", name: "Bow-Cold", masteries: "Bow, Cold", weapons: "Bow" },
		{ id: 4, race: "CH", name: "Blade-Heal", masteries: "Blade, Force", weapons: "Sword, Blade" },
		{ id: 11, race: "EU", name: "Warrior 2H", masteries: "Warrior, Cleric", weapons: "Two-handed sword" },
		{ id: 12, race: "EU", name: "Knight", masteries: "Warrior, Cleric", weapons: "One-handed sword (+ shield)" },
		{ id: 13, race: "EU", name: "Ranger", masteries: "Rogue, Warrior", weapons: "Crossbow" },
		{ id: 14, race: "EU", name: "Assassin", masteries: "Rogue, Warrior", weapons: "Dagger" },
		{ id: 15, race: "EU", name: "Wizard", masteries: "Wizard, Cleric", weapons: "Staff" },
		{ id: 16, race: "EU", name: "Warlock", masteries: "Warlock, Bard", weapons: "Dark staff" }
	];
}

/*
================
battleRoyaleBuild

The table row of a build id, or undefined for none (0) or an unknown id.
================
*/
export function battleRoyaleBuild( id: number ): BattleRoyaleBuild | undefined {
	return battleRoyaleBuilds().find( row => row.id === id );
}

/*
================
battleRoyaleBuildName

What the HUD calls a stored build: no pick plays the default build, and an
id the table does not know (a newer server) is named by its number.
================
*/
export function battleRoyaleBuildName( id: number ): string {
	if ( id === 0 ) return `Default (${battleRoyaleBuild( BATTLE_ROYALE_DEFAULT_BUILD )!.name})`;
	return battleRoyaleBuild( id )?.name ?? `Build ${id}`;
}

/*
================
decodePhase

The wire state; null for 0 (no match).
================
*/
function decodePhase( value: number ): BattleRoyalePhase | null {
	if ( value === 1 ) return "lobby";
	if ( value === 2 ) return "countdown";
	if ( value === 3 ) return "running";
	if ( value === 4 ) return "finished";
	return null;
}

/*
================
decodeRole
================
*/
function decodeRole( value: number ): BattleRoyaleRole {
	if ( value === 0 ) return "outside";
	if ( value === 1 ) return "member";
	if ( value === 2 ) return "alive";
	if ( value === 3 ) return "eliminated";
	throw new Error( "Invalid battle royale role" );
}

/*
================
decodeStage
================
*/
function decodeStage( value: number ): BattleRoyaleStage {
	if ( value === 0 ) return "waiting";
	if ( value === 1 ) return "shrinking";
	if ( value === 2 ) return "final";
	throw new Error( "Invalid battle royale stage" );
}

/*
================
readCircle

Three f32 at offset: centre x, centre z, radius. A circle that is not
finite or has a negative radius is a protocol error.
================
*/
function readCircle( view: DataView, offset: number ): BattleRoyaleCircle {
	const x = view.getFloat32( offset, true ),
		z = view.getFloat32( offset + 4, true ),
		radius = view.getFloat32( offset + 8, true );
	if ( !Number.isFinite( x ) || !Number.isFinite( z ) || !Number.isFinite( radius ) || radius < 0 ) {
		throw new Error( "Invalid battle royale circle" );
	}
	return { x, z, radius };
}

/*
================
decodeBattleRoyaleState

One 0x3FB2 payload received at nowMs on the simulation clock. Null for
state 0 (no match: hide everything). A malformed frame throws: it is a
protocol error, never a partial match.
================
*/
export function decodeBattleRoyaleState(
	payload: Uint8Array,
	nowMs: number,
	sequence: number
): BattleRoyaleState | null {
	if ( payload.length < BATTLE_ROYALE_FIXED_BYTES ) throw new Error( "Truncated battle royale state" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	if ( payload[0] !== BATTLE_ROYALE_VERSION ) throw new Error( "Unknown battle royale state version" );
	const state = payload[1]!;
	if ( state > 4 ) throw new Error( "Invalid battle royale state" );
	const role = decodeRole( payload[2]! ), build = payload[3]!;
	const lobby = readCircle( view, 4 );
	const members = payload[16]!, alive = payload[17]!;
	const countdownMs = view.getUint32( 18, true );
	const zonePhase = payload[22]!, zonePhases = payload[23]!, stage = decodeStage( payload[24]! );
	const stageMs = view.getUint32( 25, true );
	const current = readCircle( view, 29 ), next = readCircle( view, 41 );
	const damagePerSec = view.getFloat32( 53, true );
	if ( !Number.isFinite( damagePerSec ) || damagePerSec < 0 ) {
		throw new Error( "Invalid battle royale zone damage" );
	}
	const winnerLength = view.getUint16( 57, true );
	if ( winnerLength > MAX_WINNER_BYTES ) throw new Error( "Invalid battle royale winner length" );
	if ( BATTLE_ROYALE_FIXED_BYTES + winnerLength !== payload.length ) {
		throw new Error( "Invalid battle royale state length" );
	}
	const winner = new TextDecoder( "utf-8", { fatal: true } ).decode(
		payload.subarray( BATTLE_ROYALE_FIXED_BYTES, BATTLE_ROYALE_FIXED_BYTES + winnerLength )
	);
	const phase = decodePhase( state );
	if ( !phase ) return null;
	return {
		phase,
		role,
		build,
		lobby,
		members,
		alive,
		countdownEndsAtMs: nowMs + countdownMs,
		zonePhase,
		zonePhases,
		stage,
		stageStartedAtMs: nowMs,
		stageEndsAtMs: nowMs + stageMs,
		current,
		next,
		damagePerSec,
		winner: phase === "finished" ? winner : "",
		receivedAtMs: nowMs,
		sequence
	};
}

/*
================
decodeBattleRoyaleScoreboard

One 0x3FB3 payload (docs/battle-royale.md, Stage 2). A malformed frame
throws, as the state frame does: a truncated or trailing body, an unknown
version, a final flag or self flag that is not 0 or 1, a name past its
budget or not UTF-8, or two entries both claiming the receiving player.
================
*/
export function decodeBattleRoyaleScoreboard( payload: Uint8Array, sequence: number ): BattleRoyaleScoreboard {
	if ( payload.length < SCOREBOARD_HEADER_BYTES ) throw new Error( "Truncated battle royale scoreboard" );
	if ( payload[0] !== SCOREBOARD_VERSION ) throw new Error( "Unknown battle royale scoreboard version" );
	const final = payload[1]!, count = payload[2]!;
	if ( final > 1 ) throw new Error( "Invalid battle royale scoreboard final flag" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	const names = new TextDecoder( "utf-8", { fatal: true } );
	const entries: BattleRoyaleScore[] = [];
	let at = SCOREBOARD_HEADER_BYTES, selves = 0;
	for ( let i = 0; i < count; i++ ) {
		if ( at + SCORE_NAME_LENGTH_BYTES > payload.length ) throw new Error( "Truncated battle royale scoreboard" );
		const length = view.getUint16( at, true );
		at += SCORE_NAME_LENGTH_BYTES;
		if ( length > MAX_SCORE_NAME_BYTES ) throw new Error( "Invalid battle royale scoreboard name length" );
		if ( at + length + SCORE_TAIL_BYTES > payload.length ) throw new Error( "Truncated battle royale scoreboard" );
		const name = names.decode( payload.subarray( at, at + length ) );
		at += length;
		const kills = view.getUint16( at, true ), placement = payload[at + 2]!, self = payload[at + 3]!;
		at += SCORE_TAIL_BYTES;
		if ( self > 1 ) throw new Error( "Invalid battle royale scoreboard self flag" );
		if ( self && ++selves > 1 ) throw new Error( "Two battle royale scoreboard entries claim the player" );
		entries.push( { name, kills, placement, self: self === 1 } );
	}
	if ( at !== payload.length ) throw new Error( "Trailing battle royale scoreboard bytes" );
	return { final: final === 1, entries, sequence };
}

/*
================
battleRoyaleOwnKills

The receiving player's kills on a scoreboard; 0 before the first one (no
elimination yet) or when the board has no entry for the player.
================
*/
export function battleRoyaleOwnKills( scoreboard: BattleRoyaleScoreboard | undefined ): number {
	return scoreboard?.entries.find( entry => entry.self )?.kills ?? 0;
}

/*
================
battleRoyaleResultRows

The results window's order: placed participants by placement (winner
first), then any still unplaced (placement 0) by kills, most first. Ties
keep the server's order.
================
*/
export function battleRoyaleResultRows( scoreboard: BattleRoyaleScoreboard | undefined ): readonly BattleRoyaleScore[] {
	if ( !scoreboard ) return [];
	return [ ...scoreboard.entries ].sort( ( a, b ) => {
		if ( (a.placement === 0) !== (b.placement === 0) ) return a.placement === 0 ? 1 : -1;
		return a.placement === 0 ? b.kills - a.kills : a.placement - b.placement;
	} );
}

/*
================
battleRoyaleHidesRoster

True while the beta world map roster (0x3FB0) must not show other players
to this one: it is in the lobby, waiting out the countdown, or in (or
eliminated from) a running match (docs/battle-royale.md, Stage 2 item 6).
The server stops sending them; this guards the client as well. A finished
match shows the roster again: a restored player may never see state 0.
================
*/
export function battleRoyaleHidesRoster( state: BattleRoyaleState | undefined ): boolean {
	return !!state && state.role !== "outside" && state.phase !== "finished";
}

/*
================
battleRoyaleCanPick

The server accepts a pick only from a lobby member during the lobby or
the countdown; the client sends nothing it would ignore.
================
*/
export function battleRoyaleCanPick( state: BattleRoyaleState | undefined ): boolean {
	return !!state && state.role === "member" && (state.phase === "lobby" || state.phase === "countdown");
}

/*
================
battleRoyalePickFrame

0x7FB2 { u8 build }. An id outside the table is a caller defect.
================
*/
export function battleRoyalePickFrame( build: number ): WireFrame {
	if ( !battleRoyaleBuild( build ) ) throw new Error( "Unknown battle royale build " + build );
	return { opcode: OP_BATTLE_ROYALE_PICK, payload: Uint8Array.of( build ) };
}

/*
================
BattleRoyaleBot

One 0x3FB4 roster entry: a bot's world object id and its player-like name.
================
*/
export interface BattleRoyaleBot {
	readonly gid: number;
	readonly name: string;
}

/*
================
BattleRoyaleBotRoster

One 0x3FB4 frame: the lobby's bot settings (the switch, the difficulty and
the chosen count) and the bots standing in the world.
================
*/
export interface BattleRoyaleBotRoster {
	readonly enabled: boolean;
	readonly difficulty: number;
	readonly configured: number;
	readonly bots: readonly BattleRoyaleBot[];
}

/*
================
BattleRoyaleBots

The bots as published to presentation: the latest roster, and names, every
bot name seen this match (a dead bot leaves the roster but keeps its row on
the scoreboard). sequence counts frames, so a repeated frame is still news.
================
*/
export interface BattleRoyaleBots extends BattleRoyaleBotRoster {
	readonly names: readonly string[];
	readonly sequence: number;
}

/*
================
BattleRoyaleBotSettings

What a GM asks for in 0x7FB4 version 2: the switch, the difficulty
(BATTLE_ROYALE_BOT_EASY, _NORMAL or _HARD) and the bot count.
================
*/
export interface BattleRoyaleBotSettings {
	readonly enabled: boolean;
	readonly difficulty: number;
	readonly count: number;
}

/*
================
decodeBattleRoyaleBots

One 0x3FB4 payload, version 2 (Stage 7) or version 1 (Stage 5, read as
normal difficulty and the default count). A malformed frame throws: a
truncated or trailing body, an unknown version, a switch that is not 0 or
1, a difficulty past hard, a configured count outside 1..200, a gid of 0
or listed twice, a name empty, past its budget or not UTF-8.
================
*/
export function decodeBattleRoyaleBots( payload: Uint8Array ): BattleRoyaleBotRoster {
	if ( payload.length < 1 ) throw new Error( "Truncated battle royale bots" );
	const version = payload[0];
	if ( version !== BOTS_VERSION && version !== BOTS_VERSION_STAGE5 ) {
		throw new Error( "Unknown battle royale bots version" );
	}
	const stage5 = version === BOTS_VERSION_STAGE5,
		header = stage5 ? BOTS_STAGE5_HEADER_BYTES : BOTS_HEADER_BYTES;
	if ( payload.length < header ) throw new Error( "Truncated battle royale bots" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength );
	const enabled = payload[1]!,
		difficulty = stage5 ? BATTLE_ROYALE_BOT_NORMAL : payload[2]!,
		configured = stage5 ? BATTLE_ROYALE_BOT_COUNT : view.getUint16( 3, true ),
		count = stage5 ? payload[2]! : view.getUint16( 5, true );
	if ( enabled > 1 ) throw new Error( "Invalid battle royale bots switch" );
	if ( difficulty > BATTLE_ROYALE_BOT_HARD ) {
		throw new Error( "Invalid battle royale bot difficulty" );
	}
	if ( configured < BATTLE_ROYALE_MIN_BOTS || configured > BATTLE_ROYALE_MAX_BOTS ) {
		throw new Error( "Invalid battle royale bot count" );
	}
	const names = new TextDecoder( "utf-8", { fatal: true } );
	const bots: BattleRoyaleBot[] = [], seen = new Set<number>();
	let at = header;
	for ( let i = 0; i < count; i++ ) {
		if ( at + BOT_GID_BYTES + BOT_NAME_LENGTH_BYTES > payload.length ) {
			throw new Error( "Truncated battle royale bots" );
		}
		const gid = view.getUint32( at, true ), length = view.getUint16( at + BOT_GID_BYTES, true );
		at += BOT_GID_BYTES + BOT_NAME_LENGTH_BYTES;
		if ( gid === 0 || seen.has( gid ) ) throw new Error( "Invalid battle royale bot gid" );
		if ( length === 0 || length > MAX_SCORE_NAME_BYTES ) throw new Error( "Invalid battle royale bot name length" );
		if ( at + length > payload.length ) throw new Error( "Truncated battle royale bots" );
		seen.add( gid );
		bots.push( { gid, name: names.decode( payload.subarray( at, at + length ) ) } );
		at += length;
	}
	if ( at !== payload.length ) throw new Error( "Trailing battle royale bots bytes" );
	return { enabled: enabled === 1, difficulty, configured, bots };
}

/*
================
battleRoyaleCanSwitchBots

The server takes the bot settings only from a GM lobby member during the
lobby or the countdown; nobody else sees the controls. gm is the entry
eligibility, a presentation gate: the server checks the privilege itself.
================
*/
export function battleRoyaleCanSwitchBots( state: BattleRoyaleState | undefined, gm: boolean ): boolean {
	return gm && battleRoyaleCanPick( state );
}

/*
================
battleRoyaleClampBotCount

A bot count a GM may choose: a whole number within 1..200.
================
*/
export function battleRoyaleClampBotCount( count: number ): number {
	if ( !Number.isFinite( count ) ) return BATTLE_ROYALE_BOT_COUNT;
	return Math.min( BATTLE_ROYALE_MAX_BOTS, Math.max( BATTLE_ROYALE_MIN_BOTS, Math.round( count ) ) );
}

/*
================
battleRoyaleValidBotSettings

True for settings 0x7FB4 can carry: a boolean switch, a known difficulty
and a whole count within 1..200.
================
*/
export function battleRoyaleValidBotSettings( settings: BattleRoyaleBotSettings ): boolean {
	return typeof settings.enabled === "boolean" && Number.isInteger( settings.difficulty ) &&
		settings.difficulty >= BATTLE_ROYALE_BOT_EASY && settings.difficulty <= BATTLE_ROYALE_BOT_HARD &&
		Number.isInteger( settings.count ) && settings.count >= BATTLE_ROYALE_MIN_BOTS &&
		settings.count <= BATTLE_ROYALE_MAX_BOTS;
}

/*
================
battleRoyaleBotSwitchFrame

0x7FB4 version 2 { u8 enabled, u8 difficulty, u16 count }. Settings the
wire cannot carry are a caller defect and throw.
================
*/
export function battleRoyaleBotSwitchFrame( settings: BattleRoyaleBotSettings ): WireFrame {
	if ( !battleRoyaleValidBotSettings( settings ) ) throw new Error( "Invalid battle royale bot settings" );
	const payload = new Uint8Array( BOT_SWITCH_BYTES );
	payload[0] = settings.enabled ? 1 : 0;
	payload[1] = settings.difficulty;
	new DataView( payload.buffer ).setUint16( 2, settings.count, true );
	return { opcode: OP_BATTLE_ROYALE_BOT_SWITCH, payload };
}

/*
================
battleRoyaleBotDifficultyName
================
*/
export function battleRoyaleBotDifficultyName( difficulty: number ): string {
	if ( difficulty === BATTLE_ROYALE_BOT_EASY ) return "Easy";
	if ( difficulty === BATTLE_ROYALE_BOT_HARD ) return "Hard";
	return "Normal";
}

/*
================
BattleRoyaleBotPosition

One 0x3FB5 entry: a living bot's grid position (world grid units, as the
zone circles) and its current level.
================
*/
export interface BattleRoyaleBotPosition {
	readonly gid: number;
	readonly x: number;
	readonly z: number;
	readonly level: number;
}

/*
================
BattleRoyaleBotPositions

The latest 0x3FB5 as published: the living bots, when the frame arrived on
the simulation clock (the server sends one every two seconds) and a frame
sequence.
================
*/
export interface BattleRoyaleBotPositions {
	readonly bots: readonly BattleRoyaleBotPosition[];
	readonly receivedAtMs: number;
	readonly sequence: number;
}

/*
================
decodeBattleRoyaleBotPositions

One 0x3FB5 payload (docs/battle-royale.md, Stage 7). A malformed frame
throws: a truncated or trailing body, an unknown version, a gid of 0 or
listed twice, a position that is not finite.
================
*/
export function decodeBattleRoyaleBotPositions(
	payload: Uint8Array,
	nowMs: number,
	sequence: number
): BattleRoyaleBotPositions {
	if ( payload.length < BOT_POSITIONS_HEADER_BYTES ) throw new Error( "Truncated battle royale bot positions" );
	if ( payload[0] !== BOT_POSITIONS_VERSION ) throw new Error( "Unknown battle royale bot positions version" );
	const view = new DataView( payload.buffer, payload.byteOffset, payload.byteLength ),
		count = view.getUint16( 1, true ),
		size = BOT_POSITIONS_HEADER_BYTES + count * BOT_POSITION_BYTES;
	if ( payload.length < size ) throw new Error( "Truncated battle royale bot positions" );
	if ( payload.length > size ) throw new Error( "Trailing battle royale bot positions bytes" );
	const bots: BattleRoyaleBotPosition[] = [], seen = new Set<number>();
	for ( let i = 0, at = BOT_POSITIONS_HEADER_BYTES; i < count; i++, at += BOT_POSITION_BYTES ) {
		const gid = view.getUint32( at, true ),
			x = view.getFloat32( at + 4, true ),
			z = view.getFloat32( at + 8, true );
		if ( gid === 0 || seen.has( gid ) ) throw new Error( "Invalid battle royale bot position gid" );
		if ( !Number.isFinite( x ) || !Number.isFinite( z ) ) throw new Error( "Invalid battle royale bot position" );
		seen.add( gid );
		bots.push( { gid, x, z, level: payload[at + 12]! } );
	}
	return { bots, receivedAtMs: nowMs, sequence };
}

/*
================
battleRoyaleBotName

The roster name of a world object, or undefined when it is not a bot.
================
*/
export function battleRoyaleBotName( bots: BattleRoyaleBots | undefined, gid: number ): string | undefined {
	if ( !bots || !gid ) return undefined;
	for ( const bot of bots.bots ) if ( bot.gid === gid ) return bot.name;
	return undefined;
}

/*
================
battleRoyaleBotEntity

An entity as the HUD shows it: a bot carries its roster name in place of
its monster name (nameplate, target window); anything else is returned
as given.
================
*/
export function battleRoyaleBotEntity<T extends { readonly gid: number; readonly name?: string; }>(
	entity: T,
	bots: BattleRoyaleBots | undefined
): T {
	const name = battleRoyaleBotName( bots, entity.gid );
	return name === undefined || name === entity.name ? entity : { ...entity, name };
}

/*
================
battleRoyaleIsBotName

True when a scoreboard or kill feed name belongs to a bot of this match.
================
*/
export function battleRoyaleIsBotName( bots: BattleRoyaleBots | undefined, name: string ): boolean {
	return !!bots && bots.names.includes( name );
}

/*
================
battleRoyaleZone

The current circle at nowMs. While shrinking it moves linearly from the
frame's current circle to its next one over the stage; otherwise it holds.
================
*/
export function battleRoyaleZone( state: BattleRoyaleState, nowMs: number ): BattleRoyaleCircle {
	if ( state.stage !== "shrinking" ) return state.current;
	const span = state.stageEndsAtMs - state.stageStartedAtMs;
	const t = span > 0 ? Math.min( 1, Math.max( 0, (nowMs - state.stageStartedAtMs) / span ) ) : 1;
	const from = state.current, to = state.next;
	return {
		x: from.x + (to.x - from.x) * t,
		z: from.z + (to.z - from.z) * t,
		radius: from.radius + (to.radius - from.radius) * t
	};
}

/*
================
battleRoyaleGridPoint

A region-local pose in world grid units, or null in a dungeon region (its
local frame is not on the world grid).
================
*/
export function battleRoyaleGridPoint(
	pose: Pick<Pose, "regionId" | "x" | "z">
): { readonly x: number; readonly z: number; } | null {
	if ( pose.regionId & DUNGEON_REGION ) return null;
	return { x: (pose.regionId & 255) * REGION_UNITS + pose.x, z: (pose.regionId >>> 8) * REGION_UNITS + pose.z };
}

/*
================
battleRoyaleCircleDepth

How far a pose stands outside a circle, in world grid units; 0 inside it,
on its edge or in a dungeon region.
================
*/
export function battleRoyaleCircleDepth(
	circle: BattleRoyaleCircle,
	pose: Pick<Pose, "regionId" | "x" | "z">
): number {
	const point = battleRoyaleGridPoint( pose );
	if ( !point ) return 0;
	return Math.max( 0, Math.hypot( point.x - circle.x, point.z - circle.z ) - circle.radius );
}

/*
================
battleRoyaleOutsideDepth

How far the local player stands outside the current circle of a running
match, in world grid units, while it is an alive participant; 0 inside
or when the zone cannot hurt it.
================
*/
export function battleRoyaleOutsideDepth(
	state: BattleRoyaleState | undefined,
	pose: Pick<Pose, "regionId" | "x" | "z"> | null | undefined,
	nowMs: number
): number {
	if ( !state || state.phase !== "running" || state.role !== "alive" || !pose ) return 0;
	return battleRoyaleCircleDepth( battleRoyaleZone( state, nowMs ), pose );
}

/*
================
battleRoyaleOutsideZone

True while the local player is an alive participant standing outside the
current circle of a running match: the zone is hurting it.
================
*/
export function battleRoyaleOutsideZone(
	state: BattleRoyaleState | undefined,
	pose: Pick<Pose, "regionId" | "x" | "z"> | null | undefined,
	nowMs: number
): boolean {
	return battleRoyaleOutsideDepth( state, pose, nowMs ) > 0;
}

// The fog a player outside the zone stands in (owner brief: "like a fog"):
// the screen's grey veil is FOG_EDGE_ALPHA just past the edge and thickens
// to BATTLE_ROYALE_FOG_MAX_ALPHA FOG_FULL_UNITS (100 m) out.
const FOG_EDGE_ALPHA = .25;
export const BATTLE_ROYALE_FOG_MAX_ALPHA = .6;
const FOG_FULL_UNITS = 1000;

/*
================
battleRoyaleFogAlpha

The outside-zone veil's alpha at a depth outside the circle (world grid
units, battleRoyaleOutsideDepth); 0 inside.
================
*/
export function battleRoyaleFogAlpha( depth: number ): number {
	if ( !(depth > 0) ) return 0;
	return FOG_EDGE_ALPHA + (BATTLE_ROYALE_FOG_MAX_ALPHA - FOG_EDGE_ALPHA) * Math.min( 1, depth / FOG_FULL_UNITS );
}

/*
================
battleRoyaleZoneFog

How far the world's own fog closes in at a depth outside the circle: the
veil's alpha as a share of its densest (0 inside, 1 from 100 m out).
================
*/
export function battleRoyaleZoneFog( depth: number ): number {
	return battleRoyaleFogAlpha( depth ) / BATTLE_ROYALE_FOG_MAX_ALPHA;
}

/*
================
clockText

Whole seconds left, rounded up, as m:ss.
================
*/
export function clockText( ms: number ): string {
	const seconds = Math.max( 0, Math.ceil( ms / MS_PER_SECOND ) );
	const minutes = Math.floor( seconds / SECONDS_PER_MINUTE );
	return minutes + ":" + String( seconds % SECONDS_PER_MINUTE ).padStart( 2, "0" );
}

/*
================
countdownSeconds
================
*/
export function countdownSeconds( state: BattleRoyaleState, nowMs: number ): number {
	return Math.max( 0, Math.ceil( (state.countdownEndsAtMs - nowMs) / MS_PER_SECOND ) );
}

/*
================
battleRoyaleBotsText

The read-only bot line every lobby member reads: "Bots: 20 (Normal)", as
the server's roster (0x3FB4) holds the settings.
================
*/
export function battleRoyaleBotsText( bots: BattleRoyaleBotRoster ): string {
	return `Bots: ${bots.configured} (${battleRoyaleBotDifficultyName( bots.difficulty )})`;
}

/*
================
BattleRoyaleHudText

The HUD's lines for a match: headline is the large centred line (the
countdown or the result), status the zone line of a running match, lobby
the small hint a lobby member sees. Empty strings draw nothing.
================
*/
export interface BattleRoyaleHudText {
	readonly headline: string;
	readonly status: string;
	readonly lobby: string;
}

/*
================
battleRoyaleHudText

kills is the player's own count (battleRoyaleOwnKills), shown on the zone
line of a running match; bots, when switched on, joins the lobby line.
================
*/
export function battleRoyaleHudText(
	state: BattleRoyaleState | undefined,
	nowMs: number,
	kills = 0,
	bots?: BattleRoyaleBots
): BattleRoyaleHudText {
	const none = { headline: "", status: "", lobby: "" };
	if ( !state ) return none;
	// Switched-on bots fill the match: one real player starts it (Stage 5).
	const botsOn = !!bots?.enabled,
		build = (bots && botsOn ? battleRoyaleBotsText( bots ) + " - " : "") + "Build: " +
			battleRoyaleBuildName( state.build ),
		needed = botsOn ? BATTLE_ROYALE_MIN_PLAYERS_WITH_BOTS : BATTLE_ROYALE_MIN_PLAYERS;
	if ( state.phase === "lobby" ) {
		if ( state.role !== "member" ) return none;
		return { ...none, lobby: `Battle Royale - waiting for players: ${state.members}/${needed} - ${build}` };
	}
	if ( state.phase === "countdown" ) {
		return {
			...none,
			headline: `Battle Royale starts in ${countdownSeconds( state, nowMs )}`,
			lobby: state.role === "member" ? `Battle Royale - players in the lobby: ${state.members} - ${build}` : ""
		};
	}
	if ( state.phase === "running" ) {
		const zone = state.stage === "waiting" ?
			"Zone shrinks in " + clockText( state.stageEndsAtMs - nowMs ) :
			state.stage === "shrinking" ?
			"Zone shrinking" :
			"Final zone";
		return { ...none, status: `${zone}    Alive: ${state.alive}    Kills: ${kills}` };
	}
	if ( nowMs - state.receivedAtMs >= BATTLE_ROYALE_RESULT_MS ) return none;
	return { ...none, headline: state.winner ? "Winner: " + state.winner : "Draw" };
}
