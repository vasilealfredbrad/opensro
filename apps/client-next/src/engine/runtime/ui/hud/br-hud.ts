/*
===========================================================================

br-hud.ts - the Battle Royale HUD's state and geometry

Port-only, not native (docs/battle-royale.md). The match state arrives in
GameplayState.battleRoyale only while the server's SRO_BATTLE_ROYALE flag
sends 0x3FB2, so every surface here stays empty on a native shard. This
owner keeps what the HUD remembers between frames: whether the player was
already a lobby member (joining opens the build picker once), which final
scoreboard already opened the results window, the kill feed's recent
lines, and the clock ticks that make a still world redraw (the countdown,
the shrinking zone, the outside-zone fog and red pulse). It also
builds the zone rings as segment quads, so a ring keeps a constant line
width at any radius on the minimap and the world map, projects the living
bots (0x3FB5) to red dots on both maps, draws the marked airdrop (0x3FB6,
Stage 8b) as a pulsing gold diamond on both maps (an arrow on the radar's
rim when it lies beyond the radar), and owns the GM bot settings
controls (Stage 7: on or off, difficulty, count) and the settings each one
asks for. ui.ts owns this module, draws the windows and lines and routes
their events.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import type { Pose } from "@/engine/contracts/gameplay";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";
import type { UiPanel } from "@/engine/foundation/ui/panels";
import { scaleTextRun } from "@/engine/foundation/rendering/text-run";
import { minimapEdge } from "@/engine/foundation/ui/minimap-markers";
import type { BattleRoyaleLandedAirdrop } from "@/engine/foundation/gameplay/battle-royale-airdrop";
import {
	BATTLE_ROYALE_BOT_COUNT,
	BATTLE_ROYALE_BOT_EASY,
	BATTLE_ROYALE_BOT_HARD,
	BATTLE_ROYALE_BOT_NORMAL,
	battleRoyaleBotDifficultyName,
	type BattleRoyaleBotPositions,
	type BattleRoyaleBots,
	type BattleRoyaleBotSettings,
	battleRoyaleCanPick,
	battleRoyaleClampBotCount,
	type BattleRoyaleCircle,
	battleRoyaleGridPoint,
	BATTLE_ROYALE_RESULT_MS,
	type BattleRoyaleScoreboard,
	type BattleRoyaleState,
	battleRoyaleZone
} from "@/engine/foundation/gameplay/battle-royale";

export const BATTLE_ROYALE_PANEL: UiPanel = "Battle Royale";
// The end-of-match scoreboard (0x3FB3 final): placement, name and kills.
export const BATTLE_ROYALE_RESULTS_PANEL: UiPanel = "Battle Royale results";
// The results window's rows: a match holds 16 players at most
// (docs/battle-royale.md, server notes); a longer board shows its first rows.
export const BATTLE_ROYALE_RESULT_ROWS = 16;
// The picker's build buttons: this prefix and the build id.
export const BATTLE_ROYALE_BUILD_PREFIX = "battle-royale-build:";
// The lobby hint's button opens the picker through the shared window id.
export const BATTLE_ROYALE_OPEN_ID = "open-window:Battle Royale";
// A GM lobby member's bot switch (Stage 5), on the lobby hint and in the
// picker (each its own control: both show while the picker is open);
// nobody else gets the control.
export const BATTLE_ROYALE_BOTS_ID = "battle-royale-bots";
export const BATTLE_ROYALE_BOTS_WINDOW_ID = "battle-royale-bots:window";
// The GM's bot settings row (Stage 7): each control's key after the prefix
// of the surface it sits on, so the hint's and the picker's never share an id.
export const BATTLE_ROYALE_BOTS_HINT_PREFIX = "battle-royale-bots:hint:";
export const BATTLE_ROYALE_BOTS_WINDOW_PREFIX = "battle-royale-bots:window:";
// The settings row's height and the gap between its controls.
export const BATTLE_ROYALE_BOT_ROW_HEIGHT = 24;
export const BATTLE_ROYALE_BOT_CONTROL_GAP = 4;
// A living bot on the maps: a red dot this many pixels wide, at most this
// many dots, names always shown on the world map up to this many bots
// (above it only the hovered dot's), and a hovered dot's hit square.
export const BATTLE_ROYALE_BOT_DOT_PX = 4;
export const BATTLE_ROYALE_MAX_BOT_DOTS = 200;
export const BATTLE_ROYALE_BOT_LABELS_MAX = 30;
export const BATTLE_ROYALE_BOT_HIT_PX = 10;
// The world map's hover control of a bot dot: this prefix and the bot gid.
export const BATTLE_ROYALE_BOT_DOT_PREFIX = "battle-royale-bot-dot:";
// Positions older than this draw nothing: the server sends every 2 s while
// a bot lives, so a missing frame or two still shows the last place, but
// the last frame of a match whose bots all died does not linger.
// INFERENCE: the spec names no expiry; 2.5 send intervals.
export const BATTLE_ROYALE_BOT_POSITIONS_STALE_MS = 5000;
// The unit circle at the grid origin: minimapEllipse and worldMapEllipse
// project it to the place of grid (0, 0) and the pixels per grid unit, the
// affine map battleRoyaleBotDots places every bot with.
export const BATTLE_ROYALE_GRID_UNIT: BattleRoyaleCircle = { x: 0, z: 0, radius: 1 };

// Desktop placement: the zone line or lobby hint's top, the headline's top
// as a share of the screen height, and the kill feed's top and its right
// edge's distance from the screen's (left of the 129 px minimap).
export const BATTLE_ROYALE_LINE_TOP = 40;
export const BATTLE_ROYALE_HEADLINE_Y = .3;
export const BATTLE_ROYALE_FEED_TOP = 8;
export const BATTLE_ROYALE_FEED_RIGHT = 140;
// The lobby hint's picker button.
export const BATTLE_ROYALE_BUTTON_WIDTH = 110;
// A kill feed line shows this long, fading over its last second, and the
// stack keeps the newest few.
export const KILL_FEED_SHOW_MS = 6000;
const KILL_FEED_FADE_MS = 1000;
export const KILL_FEED_ROWS = 5;
// The outside-zone tint pulses once a second between these alphas: a
// faint damage cue over the zone's grey fog veil (BATTLE_ROYALE_FOG_RGB).
const TINT_PERIOD_MS = 1000;
const TINT_MIN_ALPHA = .05;
const TINT_MAX_ALPHA = .15;
// The fog veil's colour, a bluish white like the zone's fog wall; its
// alpha grows with the distance outside (battleRoyaleFogAlpha).
export const BATTLE_ROYALE_FOG_RGB: readonly [number, number, number] = [ .8, .83, .87 ];
// Redraw cadences: the shrinking zone at 10 Hz, the tint and the feed's
// fades at about 30 Hz; the countdowns redraw on their whole seconds.
const ZONE_TICK_MS = 100;
const PULSE_TICK_MS = 33;
const MS_PER_SECOND = 1000;
// A ring's line width on the minimap and the world map.
export const BATTLE_ROYALE_RING_PX = 2;
// The lobby ring's smallest on-screen radius on the world map, in pixels.
export const BATTLE_ROYALE_LOBBY_MIN_PX = 7;
// A ring is cut into segments of about this many pixels, within bounds.
const RING_SEGMENT_PX = 8;
const RING_MIN_SEGMENTS = 24;
const RING_MAX_SEGMENTS = 256;
// Segments overlap by this much so the joints never open.
const RING_JOINT_PX = 1;
// A ring narrower than this draws nothing.
const RING_MIN_RADIUS_PX = 2;
// World grid units per region side.
const REGION_UNITS = 1920;
// The centred headline (countdown, result): the large font drawn at twice
// its size, as the region banner scales its title (region-banner.ts).
export const HEADLINE_FONT = 4;
const HEADLINE_SCALE = 2;

type Rgba = readonly [number, number, number, number];
// The current zone and the lobby are red, the circle the zone shrinks to is
// white (docs/battle-royale.md: "A red circle"; owner brief).
export const BATTLE_ROYALE_RED: Rgba = [ 1, .18, .12, .95 ];
export const BATTLE_ROYALE_WHITE: Rgba = [ 1, 1, 1, .9 ];
// A bot's name colour on its nameplate and in the results window: red
// (docs/battle-royale.md, Stage 5 "Names in red").
export const BATTLE_ROYALE_BOT_RGBA: Rgba = [ 1, .2, .2, 1 ];
// The airdrop on the maps (Stage 8b): a gold diamond with a dark outline,
// this many pixels across on the minimap and on the world map, inside a
// gold halo that swells and fades once a second. No native image shows a
// supply drop, so the mark is drawn, like the bot dots.
export const BATTLE_ROYALE_AIRDROP_PX = 8;
export const BATTLE_ROYALE_AIRDROP_MAP_PX = 12;
export const BATTLE_ROYALE_AIRDROP_RGBA: Rgba = [ 1, .8, .15, 1 ];
const AIRDROP_OUTLINE_RGBA: Rgba = [ .12, .08, 0, .95 ];
const AIRDROP_OUTLINE_PX = 2;
const AIRDROP_HALO_PX = 8;
const AIRDROP_HALO_ALPHA = .5;
const AIRDROP_PULSE_MS = 1000;
// The world map's label over the diamond.
export const BATTLE_ROYALE_AIRDROP_LABEL = "Airdrop";
// An airdrop beyond the radar: the native quest arrow (mm_sign_questarrow,
// the minimap's only free-standing direction sign), tinted orange so it
// never reads as the tracked quest's, on the rim where minimapEdge puts
// every beyond-radar arrow (47 px from the centre).
export const BATTLE_ROYALE_AIRDROP_ARROW = "/assets/images/Media_extracted/interface/minimap/mm_sign_questarrow.png";
const AIRDROP_ARROW_RGBA: Rgba = [ 1, .55, .1, 1 ];
const MINIMAP_RIM_PX = 47;
// The arrow image's square, as minimapEdge sizes the quest arrow.
export const BATTLE_ROYALE_AIRDROP_ARROW_PX = 32;

/*
================
BattleRoyaleBotControl

One control of the GM's bot settings row: a difficulty, a count step or
a count preset; value is the slot that shows the current count.
================
*/
export interface BattleRoyaleBotControl {
	readonly key: string;
	readonly caption: string;
	readonly width: number;
	readonly difficulty?: number;
	readonly step?: number;
	readonly preset?: number;
	readonly value?: boolean;
}

// The row, left to right: Easy / Normal / Hard, then -10 -1 [count] +1 +10
// and the presets 20 / 50 / 100 / 200.
const BOT_DIFFICULTY_WIDTH = 60;
const BOT_STEP_WIDTH = 36;
const BOT_VALUE_WIDTH = 36;
const BOT_PRESET_WIDTH = 36;
const BOT_COUNT_STEPS = [ -10, -1, 1, 10 ] as const;
const BOT_COUNT_PRESETS = [ 20, 50, 100, 200 ] as const;
export const BATTLE_ROYALE_BOT_CONTROLS: readonly BattleRoyaleBotControl[] = [
	...[ BATTLE_ROYALE_BOT_EASY, BATTLE_ROYALE_BOT_NORMAL, BATTLE_ROYALE_BOT_HARD ].map( difficulty => ({
		key: "difficulty:" + difficulty,
		caption: battleRoyaleBotDifficultyName( difficulty ),
		width: BOT_DIFFICULTY_WIDTH,
		difficulty
	}) ),
	...BOT_COUNT_STEPS.slice( 0, 2 ).map( step => ({
		key: "step:" + step,
		caption: String( step ),
		width: BOT_STEP_WIDTH,
		step
	}) ),
	{ key: "value", caption: "", width: BOT_VALUE_WIDTH, value: true },
	...BOT_COUNT_STEPS.slice( 2 ).map( step => ({
		key: "step:" + step,
		caption: "+" + step,
		width: BOT_STEP_WIDTH,
		step
	}) ),
	...BOT_COUNT_PRESETS.map( preset => ({
		key: "preset:" + preset,
		caption: String( preset ),
		width: BOT_PRESET_WIDTH,
		preset
	}) )
];
// The row's width: every control and the gaps between them.
export const BATTLE_ROYALE_BOT_ROW_WIDTH = BATTLE_ROYALE_BOT_CONTROLS.reduce(
	( width, control ) => width + control.width,
	(BATTLE_ROYALE_BOT_CONTROLS.length - 1) * BATTLE_ROYALE_BOT_CONTROL_GAP
);

/*
================
BattleRoyaleBotDot

A living bot projected on a map: its gid, its roster name ("" before the
roster names it) and the dot's centre in pixels.
================
*/
export interface BattleRoyaleBotDot {
	readonly gid: number;
	readonly name: string;
	readonly x: number;
	readonly y: number;
}

/*
================
BattleRoyaleAirdropMark

The airdrop projected on a map: the centre in pixels, and on the minimap
whether it sits on the radar's rim (beyond the radar) with the rim arrow's
rotation.
================
*/
export interface BattleRoyaleAirdropMark {
	readonly x: number;
	readonly y: number;
	readonly rim: boolean;
	readonly rotation: number;
}

/*
================
BattleRoyaleRing

A circle the maps draw, in world grid units, and its colour.
================
*/
export interface BattleRoyaleRing {
	readonly circle: BattleRoyaleCircle;
	readonly color: Rgba;
	readonly lobby: boolean;
}

/*
================
ScreenEllipse

A ring projected to the screen: centre and the two radii in pixels (the
world map's axes may scale differently).
================
*/
export interface ScreenEllipse {
	readonly x: number;
	readonly y: number;
	readonly rx: number;
	readonly ry: number;
}

/*
================
KillFeedRow
================
*/
export interface KillFeedRow {
	readonly text: string;
	readonly alpha: number;
}

/*
================
battleRoyaleBuildOf

The build id a picker control names, or undefined for any other control.
================
*/
export function battleRoyaleBuildOf( id: string ): number | undefined {
	if ( !id.startsWith( BATTLE_ROYALE_BUILD_PREFIX ) ) return undefined;
	const build = Number( id.slice( BATTLE_ROYALE_BUILD_PREFIX.length ) );
	return Number.isInteger( build ) && build > 0 ? build : undefined;
}

/*
================
battleRoyaleBotsLabel

The bot switch's caption: the lobby's switch as the server last sent it.
================
*/
export function battleRoyaleBotsLabel( bots: BattleRoyaleBots | undefined ): string {
	return bots?.enabled ? "Bots: ON" : "Bots: OFF";
}

/*
================
battleRoyaleBotSettingsOf

The lobby's bot settings as the server's roster last echoed them; before
any roster, off at the defaults (normal, 20).
================
*/
export function battleRoyaleBotSettingsOf( bots: BattleRoyaleBots | undefined ): BattleRoyaleBotSettings {
	return {
		enabled: !!bots?.enabled,
		difficulty: bots?.difficulty ?? BATTLE_ROYALE_BOT_NORMAL,
		count: bots?.configured ?? BATTLE_ROYALE_BOT_COUNT
	};
}

/*
================
battleRoyaleBotRequest

The full settings a bot control asks for, from the echoed ones: the switch
toggles, a difficulty replaces the difficulty, a step or a preset sets the
count clamped to 1..200. Undefined for any other control (and for the
count slot, which only shows the value). The other fields are kept, so the
0x7FB4 always carries the whole setting.
================
*/
export function battleRoyaleBotRequest(
	id: string,
	bots: BattleRoyaleBots | undefined
): BattleRoyaleBotSettings | undefined {
	const current = battleRoyaleBotSettingsOf( bots );
	if ( id === BATTLE_ROYALE_BOTS_ID || id === BATTLE_ROYALE_BOTS_WINDOW_ID ) {
		return { ...current, enabled: !current.enabled };
	}
	const key = id.startsWith( BATTLE_ROYALE_BOTS_HINT_PREFIX ) ?
		id.slice( BATTLE_ROYALE_BOTS_HINT_PREFIX.length ) :
		id.startsWith( BATTLE_ROYALE_BOTS_WINDOW_PREFIX ) ?
		id.slice( BATTLE_ROYALE_BOTS_WINDOW_PREFIX.length ) :
		undefined;
	const control = key === undefined ? undefined : BATTLE_ROYALE_BOT_CONTROLS.find( c => c.key === key );
	if ( !control ) return undefined;
	if ( control.difficulty !== undefined ) return { ...current, difficulty: control.difficulty };
	if ( control.step !== undefined ) {
		return { ...current, count: battleRoyaleClampBotCount( current.count + control.step ) };
	}
	if ( control.preset !== undefined ) return { ...current, count: battleRoyaleClampBotCount( control.preset ) };
	return undefined;
}

/*
================
battleRoyaleBotControlSelected

True when a settings control shows the echoed setting: the chosen
difficulty, or the preset equal to the chosen count.
================
*/
export function battleRoyaleBotControlSelected(
	control: BattleRoyaleBotControl,
	settings: BattleRoyaleBotSettings
): boolean {
	return control.difficulty === settings.difficulty || control.preset === settings.count;
}

/*
================
battleRoyaleShowsBots

The maps show the living bots to every participant of a running match,
alive or eliminated (docs/battle-royale.md, Stage 7 "On the map"), while
the latest positions are fresh.
================
*/
export function battleRoyaleShowsBots(
	state: BattleRoyaleState | undefined,
	positions: BattleRoyaleBotPositions | undefined,
	nowMs: number
): positions is BattleRoyaleBotPositions {
	return state?.phase === "running" && (state.role === "alive" || state.role === "eliminated") &&
		!!positions && nowMs - positions.receivedAtMs < BATTLE_ROYALE_BOT_POSITIONS_STALE_MS;
}

// The roster's names by gid, built once per published roster.
const rosterNames = new WeakMap<BattleRoyaleBots, ReadonlyMap<number, string>>();

/*
================
botNameIndex
================
*/
function botNameIndex( bots: BattleRoyaleBots | undefined ): ReadonlyMap<number, string> | undefined {
	if ( !bots ) return undefined;
	let index = rosterNames.get( bots );
	if ( !index ) {
		index = new Map( bots.bots.map( bot => [ bot.gid, bot.name ] ) );
		rosterNames.set( bots, index );
	}
	return index;
}

/*
================
battleRoyaleBotDots

The living bots inside clip, projected through origin: the screen place of
grid (0, 0) and the pixels per grid unit along each axis (y grows south),
as minimapEllipse or worldMapEllipse give for the unit circle at the grid
origin, so the dots share the zone rings' projection. At most
BATTLE_ROYALE_MAX_BOT_DOTS, in the frame's order.
================
*/
export function battleRoyaleBotDots(
	origin: ScreenEllipse,
	positions: BattleRoyaleBotPositions,
	bots: BattleRoyaleBots | undefined,
	clip: UiRect
): BattleRoyaleBotDot[] {
	const names = botNameIndex( bots ), dots: BattleRoyaleBotDot[] = [];
	for ( const bot of positions.bots ) {
		if ( dots.length >= BATTLE_ROYALE_MAX_BOT_DOTS ) break;
		const x = origin.x + bot.x * origin.rx, y = origin.y - bot.z * origin.ry;
		if ( x < clip[0] || x > clip[0] + clip[2] || y < clip[1] || y > clip[1] + clip[3] ) continue;
		dots.push( { gid: bot.gid, name: names?.get( bot.gid ) ?? "", x, y } );
	}
	return dots;
}

/*
================
botDotQuad

A dot as an untextured quad (texture "" is white) in the bot red.
================
*/
export function botDotQuad( dot: BattleRoyaleBotDot, clip: UiRect, mask?: UiQuad["mask"] ): UiQuad {
	const half = BATTLE_ROYALE_BOT_DOT_PX / 2;
	return {
		rect: [ dot.x - half, dot.y - half, BATTLE_ROYALE_BOT_DOT_PX, BATTLE_ROYALE_BOT_DOT_PX ],
		uv: [ 0, 0, 1, 1 ],
		texture: "",
		color: BATTLE_ROYALE_BOT_RGBA,
		clip,
		...(mask ? { mask } : {})
	};
}

/*
================
battleRoyaleAirdropMinimap

The airdrop on the radar, projected through origin (minimapEllipse of
BATTLE_ROYALE_GRID_UNIT, as the bot dots): at its place while it lies
within the rim, else on the rim toward it with the arrow's rotation
(minimapEdge, as the party and quest arrows).
================
*/
export function battleRoyaleAirdropMinimap(
	origin: ScreenEllipse,
	airdrop: Pick<BattleRoyaleLandedAirdrop, "x" | "z">,
	centre: readonly [number, number]
): BattleRoyaleAirdropMark {
	const x = origin.x + airdrop.x * origin.rx - centre[0], y = origin.y - airdrop.z * origin.ry - centre[1];
	if ( Math.hypot( x, y ) < MINIMAP_RIM_PX ) return { x: centre[0] + x, y: centre[1] + y, rim: false, rotation: 0 };
	const edge = minimapEdge( [ x, y ], "quest" );
	return { x: centre[0] + edge.x, y: centre[1] + edge.y, rim: true, rotation: edge.rotation };
}

/*
================
battleRoyaleAirdropWorldMap

The airdrop on the world map, projected through origin (worldMapEllipse of
BATTLE_ROYALE_GRID_UNIT); null when it falls outside clip.
================
*/
export function battleRoyaleAirdropWorldMap(
	origin: ScreenEllipse,
	airdrop: Pick<BattleRoyaleLandedAirdrop, "x" | "z">,
	clip: UiRect
): BattleRoyaleAirdropMark | null {
	const x = origin.x + airdrop.x * origin.rx, y = origin.y - airdrop.z * origin.ry;
	if ( x < clip[0] || x > clip[0] + clip[2] || y < clip[1] || y > clip[1] + clip[3] ) return null;
	return { x, y, rim: false, rotation: 0 };
}

/*
================
airdropDiamond
================
*/
function airdropDiamond(
	mark: BattleRoyaleAirdropMark,
	size: number,
	color: Rgba,
	clip: UiRect,
	mask?: UiQuad["mask"]
): UiQuad {
	return {
		rect: [ mark.x - size / 2, mark.y - size / 2, size, size ],
		uv: [ 0, 0, 1, 1 ],
		texture: "",
		color,
		clip,
		rotation: Math.PI / 4,
		...(mask ? { mask } : {})
	};
}

/*
================
battleRoyaleAirdropQuads

The mark's quads, painted in order: the halo pulsing with nowMs, the dark
outline and the gold diamond of size pixels; on the rim the tinted arrow
alone (its image must be loaded: the caller demands
BATTLE_ROYALE_AIRDROP_ARROW).
================
*/
export function battleRoyaleAirdropQuads(
	mark: BattleRoyaleAirdropMark,
	size: number,
	clip: UiRect,
	nowMs: number,
	mask?: UiQuad["mask"]
): UiQuad[] {
	if ( mark.rim ) {
		const half = BATTLE_ROYALE_AIRDROP_ARROW_PX / 2;
		return [ {
			rect: [ mark.x - half, mark.y - half, BATTLE_ROYALE_AIRDROP_ARROW_PX, BATTLE_ROYALE_AIRDROP_ARROW_PX ],
			uv: [ 0, 0, 1, 1 ],
			texture: BATTLE_ROYALE_AIRDROP_ARROW,
			color: AIRDROP_ARROW_RGBA,
			clip,
			rotation: mark.rotation,
			...(mask ? { mask } : {})
		} ];
	}
	const pulse = .5 - .5 * Math.cos( 2 * Math.PI * (nowMs % AIRDROP_PULSE_MS) / AIRDROP_PULSE_MS );
	const halo: Rgba = [
		BATTLE_ROYALE_AIRDROP_RGBA[0],
		BATTLE_ROYALE_AIRDROP_RGBA[1],
		BATTLE_ROYALE_AIRDROP_RGBA[2],
		AIRDROP_HALO_ALPHA * (1 - pulse)
	];
	return [
		airdropDiamond( mark, size + AIRDROP_HALO_PX * pulse, halo, clip, mask ),
		airdropDiamond( mark, size + AIRDROP_OUTLINE_PX * 2, AIRDROP_OUTLINE_RGBA, clip, mask ),
		airdropDiamond( mark, size, BATTLE_ROYALE_AIRDROP_RGBA, clip, mask )
	];
}

/*
================
battleRoyaleRings

The circles the maps show at nowMs: the lobby to everyone but an alive
participant (who is in the arena), and while a match runs the current zone
(red) and, until the final stage, the circle it shrinks to (white).
================
*/
export function battleRoyaleRings(
	state: BattleRoyaleState | undefined,
	nowMs: number,
	lobby?: BattleRoyaleCircle
): readonly BattleRoyaleRing[] {
	if ( !state ) return lobby ? [ { circle: lobby, color: BATTLE_ROYALE_RED, lobby: true } ] : [];
	const rings: BattleRoyaleRing[] = [];
	if ( state.role !== "alive" && state.lobby.radius <= 0 && lobby ) {
		rings.push( { circle: lobby, color: BATTLE_ROYALE_RED, lobby: true } );
	}
	if ( state.role !== "alive" && state.lobby.radius > 0 ) {
		rings.push( { circle: state.lobby, color: BATTLE_ROYALE_RED, lobby: true } );
	}
	if ( state.phase === "running" ) {
		if ( state.stage !== "final" ) rings.push( { circle: state.next, color: BATTLE_ROYALE_WHITE, lobby: false } );
		rings.push( { circle: battleRoyaleZone( state, nowMs ), color: BATTLE_ROYALE_RED, lobby: false } );
	}
	return rings;
}

/*
================
minimapEllipse

A ring on the minimap around the local pose: the offset from the radar's
centre and the radius at the minimap's pixels per region (y grows south,
as minimapOffset projects). Null in a dungeon.
================
*/
export function minimapEllipse(
	pose: Pick<Pose, "regionId" | "x" | "z">,
	circle: BattleRoyaleCircle,
	zoom: number,
	centre: readonly [number, number]
): ScreenEllipse | null {
	const local = battleRoyaleGridPoint( pose );
	if ( !local ) return null;
	const scale = zoom / REGION_UNITS, radius = circle.radius * scale;
	return {
		x: centre[0] + (circle.x - local.x) * scale,
		y: centre[1] - (circle.z - local.z) * scale,
		rx: radius,
		ry: radius
	};
}

/*
================
ringQuads

A ring as thin rotated quads along its outline (texture "" is white), each
segment tinted and clipped like any HUD quad. Segments wholly outside the
clip are skipped, so a zone far larger than the radar costs only what shows.
================
*/
export function ringQuads(
	ring: ScreenEllipse,
	color: Rgba,
	clip: UiRect,
	thickness: number,
	mask?: UiQuad["mask"]
): UiQuad[] {
	const radius = Math.max( ring.rx, ring.ry );
	if ( !(radius >= RING_MIN_RADIUS_PX) || !Number.isFinite( radius ) ) return [];
	const count = Math.min(
		RING_MAX_SEGMENTS,
		Math.max( RING_MIN_SEGMENTS, Math.ceil( 2 * Math.PI * radius / RING_SEGMENT_PX ) )
	);
	const quads: UiQuad[] = [];
	let px = ring.x + ring.rx, py = ring.y;
	for ( let i = 1; i <= count; i++ ) {
		const angle = 2 * Math.PI * i / count,
			nx = ring.x + ring.rx * Math.cos( angle ),
			ny = ring.y + ring.ry * Math.sin( angle );
		const length = Math.hypot( nx - px, ny - py ) + RING_JOINT_PX,
			mx = (px + nx) / 2,
			my = (py + ny) / 2,
			reach = length / 2 + thickness;
		if (
			mx + reach >= clip[0] && mx - reach <= clip[0] + clip[2] && my + reach >= clip[1] &&
			my - reach <= clip[1] + clip[3]
		) {
			quads.push( {
				rect: [ mx - length / 2, my - thickness / 2, length, thickness ],
				uv: [ 0, 0, 1, 1 ],
				texture: "",
				color,
				clip,
				rotation: Math.atan2( ny - py, nx - px ),
				...(mask ? { mask } : {})
			} );
		}
		px = nx;
		py = ny;
	}
	return quads;
}

/*
================
headlineLayout

Where the headline is laid out at 1x before scaling: centred across the
screen's width divided by the scale, so scaled it is centred on the screen.
================
*/
export function headlineLayout( width: number, lineHeight: number ): UiRect {
	return [ 0, 0, width / HEADLINE_SCALE, lineHeight ];
}

/*
================
scaleHeadline

The headline's 1x glyph quads (laid out in headlineLayout) scaled about
the screen's top left and moved down to top. The runs scale with their
rects (scaleTextRun), as regionBannerQuads does for its title.
================
*/
export function scaleHeadline( laid: readonly UiQuad[], top: number, clip: UiRect ): UiQuad[] {
	return laid.map( q => ({
		...q,
		rect: [
			q.rect[0] * HEADLINE_SCALE,
			top + q.rect[1] * HEADLINE_SCALE,
			q.rect[2] * HEADLINE_SCALE,
			q.rect[3] * HEADLINE_SCALE
		] as UiRect,
		clip,
		...(q.run ? { run: scaleTextRun( q.run, HEADLINE_SCALE ) } : {})
	}) );
}

/*
================
headlineExtent

The headline's scaled width and height for a 1x measured width and line.
================
*/
export function headlineExtent( width: number, lineHeight: number ): readonly [number, number] {
	return [ Math.ceil( width * HEADLINE_SCALE ), Math.ceil( lineHeight * HEADLINE_SCALE ) ];
}

/*
================
tintAlpha

The outside-zone tint's alpha at a moment: a smooth pulse.
================
*/
export function tintAlpha( nowMs: number ): number {
	const wave = .5 + .5 * Math.sin( 2 * Math.PI * nowMs / TINT_PERIOD_MS );
	return TINT_MIN_ALPHA + (TINT_MAX_ALPHA - TINT_MIN_ALPHA) * wave;
}

/*
================
createBattleRoyaleHud
================
*/
export function createBattleRoyaleHud() {
	// Whether the last observed state made the player a lobby member who may
	// pick: becoming one opens the picker once.
	let member = false;
	// The final scoreboard whose results window opened (its sequence).
	let resultsShown = -1;
	// The newest kill feed notice taken, and the lines still showing.
	let feedSequence = 0;
	let feed: { readonly text: string; readonly atMs: number; }[] = [];
	// The redraw clocks last reported (redraw).
	let clockTick = -1, pulseTick = -1, feedTick = -1, dotsTick = -1;
	// The airdrop halo's tick last reported (redrawAirdrop).
	let airdropTick = -1;
	return {
		/*
		================
		observe

		True when the player has just become a lobby member who may pick: the
		picker opens. A member who closed it is not asked again until the role
		changes.
		================
		*/
		observe( state: BattleRoyaleState | undefined ): boolean {
			const pickable = battleRoyaleCanPick( state ), joined = pickable && !member;
			member = pickable;
			return joined;
		},
		/*
		================
		openResults

		True once for each final scoreboard, when admit says the results
		window may open now; a board that arrives while it may not waits for
		the next frame that may.
		================
		*/
		openResults( scoreboard: BattleRoyaleScoreboard | undefined, admit: boolean ): boolean {
			if ( !admit || !scoreboard?.final || scoreboard.sequence === resultsShown ) return false;
			resultsShown = scoreboard.sequence;
			return true;
		},
		/*
		================
		stepFeed

		Takes the kill feed notices newer than the last one taken and drops
		the lines that have shown their time; true when the stack changed.
		================
		*/
		stepFeed( notices: readonly SystemNotice[], nowMs: number ): boolean {
			let changed = false;
			for ( const notice of notices ) {
				if ( !notice.killFeed || notice.sequence === undefined || notice.sequence <= feedSequence ) continue;
				feedSequence = notice.sequence;
				feed = [ ...feed, { text: notice.text ?? "", atMs: nowMs } ].slice( -KILL_FEED_ROWS );
				changed = true;
			}
			if ( feed.some( row => nowMs - row.atMs >= KILL_FEED_SHOW_MS ) ) {
				feed = feed.filter( row => nowMs - row.atMs < KILL_FEED_SHOW_MS );
				changed = true;
			}
			return changed;
		},
		/*
		================
		feed

		The showing lines, oldest first, with their fade.
		================
		*/
		feed( nowMs: number ): readonly KillFeedRow[] {
			return feed.map( row => ({
				text: row.text,
				alpha: Math.min( 1, Math.max( 0, (KILL_FEED_SHOW_MS - (nowMs - row.atMs)) / KILL_FEED_FADE_MS ) )
			}) );
		},
		/*
		================
		redraw

		True when a still world must still be drawn again: a countdown's whole
		second turned, the zone moved a tick, the tint pulsed or a feed line
		faded or the bot dots went stale. simulationMs is the clock the match
		deadlines and the bot positions are on.
		================
		*/
		redraw(
			state: BattleRoyaleState | undefined,
			outside: boolean,
			simulationMs: number,
			nowMs: number,
			positions?: BattleRoyaleBotPositions
		) {
			let clock = -1;
			if ( state?.phase === "countdown" ) {
				clock = Math.ceil( (state.countdownEndsAtMs - simulationMs) / MS_PER_SECOND );
			} else if ( state?.phase === "running" && state.stage === "waiting" ) {
				clock = Math.ceil( (state.stageEndsAtMs - simulationMs) / MS_PER_SECOND );
			} else if ( state?.phase === "running" && state.stage === "shrinking" ) {
				clock = Math.floor( simulationMs / ZONE_TICK_MS );
			} else if ( state?.phase === "finished" ) {
				clock = simulationMs - state.receivedAtMs < BATTLE_ROYALE_RESULT_MS ? 1 : 0;
			}
			const pulse = outside ? Math.floor( nowMs / PULSE_TICK_MS ) : -1,
				fade = feed.length ? Math.floor( nowMs / PULSE_TICK_MS ) : -1;
			const dots = battleRoyaleShowsBots( state, positions, simulationMs ) ? positions.sequence : -1;
			const changed = clock !== clockTick || pulse !== pulseTick || fade !== feedTick || dots !== dotsTick;
			clockTick = clock;
			pulseTick = pulse;
			feedTick = fade;
			dotsTick = dots;
			return changed;
		},
		/*
		================
		redrawAirdrop

		True when a still world must be drawn again for the airdrop: its mark
		or flight line appeared or went, or a tick passed (ten a second, as
		the shrinking zone) for the halo's pulse, the plane on the line and
		the landing countdown. shows is battleRoyaleShowsAirdrop or
		battleRoyaleShowsAirdropLine.
		================
		*/
		redrawAirdrop( shows: boolean, nowMs: number ): boolean {
			const tick = shows ? Math.floor( nowMs / ZONE_TICK_MS ) : -1;
			const changed = tick !== airdropTick;
			airdropTick = tick;
			return changed;
		},
		/*
		================
		reset

		A new session: no remembered membership, results or kill feed lines.
		================
		*/
		reset() {
			member = false;
			resultsShown = -1;
			feedSequence = 0;
			feed = [];
			clockTick =
				pulseTick =
				feedTick =
				dotsTick =
				airdropTick =
					-1;
		}
	};
}
