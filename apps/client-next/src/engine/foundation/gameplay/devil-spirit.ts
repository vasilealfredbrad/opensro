/*
===========================================================================

devil-spirit.ts - the Devil's Spirit's awakening, enhancement and notices

Port-only, not native (owner, 2026-10-18). The Devil's Spirit rows and
their scroll and extension items come from a later client
(apps/server/internal/game/item/devilspirit, SRO_DEVIL_SPIRIT). The
server keeps an instance's later-client state in the item fields every
v1.150 path already carries (state.go there):

  - durability: the awakening's expiry in unix seconds, 0 not awakened,
    0xFFFFFFFF never;
  - variance: bits 0..31 the Awakening Enhancement Scroll's binding expiry,
    32..39 its level, 40..47 the MATTR_NASRUN_UMDU percent;
  - plus: the base OptLevel; the magic options are the server's, derived
    from the effective level.

This module reads that state for the tooltip, words the MATTR_NASRUN_*
options, and turns the server's answers into notices:

  - 0xB5BD [2][code] for the scroll and the extension: 0xDD the failed
    roll (the later server's 0x18DD), 0xDE..0xE1 the port's target
    refusals;
  - 0x3078 [kind][slot]...: kind 2 an extension, kind 3 an expiry (the
    later client's), kind 4 a worn item's state in the 0x3645 layout (the
    port's; the inventory owner applies it to the avatar storage).

The words are the later client's English textdata, which v1.150's
catalogue does not hold.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { SystemNotice } from "./system-notices";

// The Devil's Spirit rows: devilspirit.json's ids, one contiguous run.
export const DEVIL_SPIRIT_FIRST_ID = 24385;
export const DEVIL_SPIRIT_LAST_ID = 24392;
// The later client's avatar-special notification.
export const DEVIL_SPIRIT_STATE_OPCODE = 0x3078;
export const DEVIL_SPIRIT_KIND_EXTENDED = 2;
export const DEVIL_SPIRIT_KIND_EXPIRED = 3;
export const DEVIL_SPIRIT_KIND_STATE = 4;
// The scroll and extension rows (devilspirit.json "etc").
export const DEVIL_SPIRIT_SCROLL_ID = 39738;
export const DEVIL_SPIRIT_EXTENSION_ID = 24395;
// durability: never expires.
const PERMANENT_AWAKE = 0xffffffff;
const BIND_LEVEL_SHIFT = 32n;
const UMDU_SHIFT = 40n;
const BYTE_MASK = 0xffn;
const WORD_MASK = 0xffffffffn;
const SECONDS_PER_MINUTE = 60;
const SECONDS_PER_HOUR = 3600;
const SECONDS_PER_DAY = 86400;
const ITEM_USE_RESPONSE = 0xb5bd;
// The scroll and extension refusals (action/devilspirit.go).
const ERR_FAILED = 0xdd;
const ERR_TARGET = 0xde;
const ERR_NOT_AWAKE = 0xdf;
const ERR_EXPIRED = 0xe0;
const ERR_NOT_FOR_USE = 0xe1;
const NOTICE_COLOR = 0xffff7070;
const STATE_COLOR = 0xffefdaa4;
const OPTION_COLOR = 0xff00eaff;

// The later client's English text (textuisystem / textdata_equip&skill).
const TEXT_NOT_AWAKE = "This devil's spirit is not awaken.";
const TEXT_EXPIRED = "Awaken period is over.";
const TEXT_AWAKE_TIME = "Awaken period.";
const TEXT_RENEWED = "Awaken period is extended.";
const TEXT_FAILED = "You have failed to enhance Awakening Item";
export const DEVIL_SPIRIT_WEAR_EXPIRED_TEXT =
	"You cannot equip the item if the awakening period has expired or it has not been awakened.";
const TEXT_USE_AFTER_AWAKE = "The item can be used after awakening.";
const TEXT_WRONG_TARGET = "Use it on a Devil's Spirit in the inventory.";
const TEXT_PERMANENT = "This Devil's Spirit never expires.";
const TEXT_ENHANCEMENT = "Awakening enhancement";
const TEXT_REMAINING = "Time Remaining:";
const TEXT_UPGRADED = "For the next %d minute(s), the grade of the %s enhancement will be adjusted to %d.";
const TEXT_HP = "HP increase";
const TEXT_MP = "MP increase";
const TEXT_BLOCK = "Blocking rate";
const TEXT_UMDU = "Damage increase (it will apply to Unique monsters only)";

/*
================
DevilSpiritState
================
*/
export interface DevilSpiritState {
	readonly awakeUntil: number;
	readonly bindLevel: number;
	readonly bindUntil: number;
	readonly umdu: number;
}

/*
================
isDevilSpirit
================
*/
export function isDevilSpirit( refObjId: number ): boolean {
	return refObjId >= DEVIL_SPIRIT_FIRST_ID && refObjId <= DEVIL_SPIRIT_LAST_ID;
}

/*
================
devilSpiritState

The state an instance's fields carry (state.go Decode).
================
*/
export function devilSpiritState( item: Pick<InventoryItem, "durability" | "variance"> ): DevilSpiritState {
	let variance = 0n;
	try {
		variance = BigInt( item.variance || "0" );
	} catch {
		variance = 0n;
	}
	return {
		awakeUntil: item.durability >>> 0,
		bindUntil: Number( variance & WORD_MASK ),
		bindLevel: Number( variance >> BIND_LEVEL_SHIFT & BYTE_MASK ),
		umdu: Number( variance >> UMDU_SHIFT & BYTE_MASK )
	};
}

/*
================
devilSpiritExpired
================
*/
export function devilSpiritExpired( state: DevilSpiritState, nowSec: number ): boolean {
	return state.awakeUntil !== 0 && state.awakeUntil !== PERMANENT_AWAKE && state.awakeUntil <= nowSec;
}

/*
================
duration

"Nd Nh Nm" of a remaining time, the largest units only.
================
*/
function duration( seconds: number ): string {
	const days = Math.floor( seconds / SECONDS_PER_DAY ),
		hours = Math.floor( seconds % SECONDS_PER_DAY / SECONDS_PER_HOUR ),
		minutes = Math.floor( seconds % SECONDS_PER_HOUR / SECONDS_PER_MINUTE );
	if ( days ) return `${days}d ${hours}h`;
	if ( hours ) return `${hours}h ${minutes}m`;
	return `${Math.max( 1, minutes )}m`;
}

/*
================
devilSpiritTooltipRows

The awakening and enhancement lines under a Devil's Spirit's options:
not awakened, the remaining period (or over, or never ending), and a
live enhancement's level and time. nowSec is the wall clock; the server
judges expiry by its own.
================
*/
export function devilSpiritTooltipRows(
	item: Pick<InventoryItem, "durability" | "variance" | "plus">,
	nowSec: number
): { value: string; color: number; }[] {
	const state = devilSpiritState( item ), rows: { value: string; color: number; }[] = [];
	if ( state.awakeUntil === 0 ) {
		rows.push( { value: TEXT_NOT_AWAKE, color: NOTICE_COLOR } );
	} else if ( state.awakeUntil === PERMANENT_AWAKE ) {
		rows.push( { value: TEXT_PERMANENT, color: STATE_COLOR } );
	} else if ( devilSpiritExpired( state, nowSec ) ) {
		rows.push( { value: TEXT_EXPIRED, color: NOTICE_COLOR } );
	} else {
		rows.push( {
			value: `${TEXT_AWAKE_TIME} ${duration( state.awakeUntil - nowSec )}`,
			color: STATE_COLOR
		} );
	}
	if ( state.bindLevel && state.bindUntil > nowSec ) {
		rows.push( {
			value: `${TEXT_ENHANCEMENT} +${state.bindLevel} (${TEXT_REMAINING} ${
				duration( state.bindUntil - nowSec )
			})`,
			color: OPTION_COLOR
		} );
	}
	return rows;
}

/*
================
devilSpiritOptionText

A MATTR_NASRUN_* option line, or "".
================
*/
export function devilSpiritOptionText( codename: string, amount: number ): string {
	switch ( codename ) {
		case "MATTR_NASRUN_HPNA":
			return `${TEXT_HP} ${amount}%`;
		case "MATTR_NASRUN_MPNA":
			return `${TEXT_MP} ${amount}%`;
		case "MATTR_NASRUN_BLOCKRATE":
			return `${TEXT_BLOCK} ${amount}`;
		case "MATTR_NASRUN_UMDU":
			return `${TEXT_UMDU} ${amount}%`;
	}
	return "";
}

/*
================
textNotice
================
*/
function textNotice( text: string ): SystemNotice {
	return { key: "", value: 0, text, nativeType: 5, banner: true };
}

/*
================
devilSpiritNotice

The notice a scroll or extension refusal (0xB5BD [2][code]) or a 0x3078
extension or expiry raises, or null for any other frame.
================
*/
export function devilSpiritNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode === ITEM_USE_RESPONSE ) {
		if ( p.length !== 2 || p[0] !== 2 ) return null;
		switch ( p[1] ) {
			case ERR_FAILED:
				return textNotice( TEXT_FAILED );
			case ERR_TARGET:
				return textNotice( TEXT_WRONG_TARGET );
			case ERR_NOT_AWAKE:
				return textNotice( TEXT_USE_AFTER_AWAKE );
			case ERR_EXPIRED:
				return textNotice( TEXT_EXPIRED );
			case ERR_NOT_FOR_USE:
				return textNotice( TEXT_PERMANENT );
		}
		return null;
	}
	if ( opcode !== DEVIL_SPIRIT_STATE_OPCODE ) return null;
	if ( p[0] === DEVIL_SPIRIT_KIND_EXTENDED ) {
		if ( p.length !== 6 ) throw Error( "Invalid Devil's Spirit extension" );
		return textNotice( TEXT_RENEWED );
	}
	if ( p[0] === DEVIL_SPIRIT_KIND_EXPIRED ) {
		if ( p.length !== 2 ) throw Error( "Invalid Devil's Spirit expiry" );
		return textNotice( TEXT_EXPIRED );
	}
	return null;
}

/*
================
devilSpiritUpgradeNotice

The success line when a bag update raised a Devil's Spirit's binding
(the later client's UIIT_MSG_NASRUN_UPGRADE_SUCCESS), or null.
================
*/
export function devilSpiritUpgradeNotice(
	before: InventoryItem | undefined,
	after: InventoryItem | null,
	nowSec: number
): SystemNotice | null {
	if ( !before || !after || before.refObjId !== after.refObjId ) return null;
	const was = devilSpiritState( before ), now = devilSpiritState( after );
	if ( !now.bindLevel || now.bindUntil === was.bindUntil || now.bindUntil <= nowSec ) return null;
	const minutes = Math.round( (now.bindUntil - nowSec) / SECONDS_PER_MINUTE );
	return {
		...textNotice(
			TEXT_UPGRADED.replace( "%d", String( minutes ) ).replace( "%s", after.name ?? "" ).replace(
				"%d",
				String( now.bindLevel )
			)
		),
		banner: undefined
	};
}

/*
================
devilSpiritWornState

The worn slot a 0x3078 kind 4 names and its 0x3645-layout body, or null
for another kind.
================
*/
export function devilSpiritWornState( p: Uint8Array ): Uint8Array | null {
	if ( p[0] !== DEVIL_SPIRIT_KIND_STATE ) return null;
	if ( p.length < 3 ) throw Error( "Invalid Devil's Spirit state" );
	return p.subarray( 1 );
}

// ============================================================================

// 0x3078 kind 5 (port-only): [5][u32 skill id], a devil skill withdrawn.
export const DEVIL_SPIRIT_KIND_FORGET = 5;
// The devil buffs (devilspirit.json "skills": the ECCENTRICDEMON,
// THEDEVILCONFUSION and DESTROYTHEDEVIL rows of each colour) and the two
// Ignore Monster's defense passives.
const DEVIL_BUFF_FIRST = 31146;
const DEVIL_BUFF_STAGE_LAST = 31148;
const DEVIL_PASSIVE_FIRST = 31149;
const DEVIL_PASSIVE_LAST = 31150;
const DEVIL_BUFF_COLOUR_FIRST = 31151;
const DEVIL_BUFF_LAST = 31156;
// quickslots.ts: a skill binding, the four ten-key pages of the under bar
// (slots 1..40 after the common slot 0, F1..F4) and the extension bar
// (CIFExtQuickSlot, quickslots.ts extendedSlot: slots 41..50). A new devil
// buff goes on the extension bar only.
const QUICK_SLOT_SKILL = 0x49;
const SHORTCUT_FIRST = 1;
const SHORTCUT_LAST = 40;
const SHORTCUT_PAGE_SLOTS = 10;
const EXTENSION_FIRST = 41;
const EXTENSION_LAST = 50;
const DEVIL_BUFF_STAGES = 3;
// The enhancement levels each stage spans (devilspirit.json "levels": the
// first skill at +0..+2, the second and a passive at +3..+5, the third and
// the stronger passive at +6..+10).
const TEXT_STAGE_1_LEVELS = "levels 0-2";
const TEXT_STAGE_2_LEVELS = "levels 3-5";
const TEXT_STAGE_3_LEVELS = "levels 6-10";
// The buffs' and passives' reference rows (devilspirit.json "skills"): the
// buffs' cooldown (column 14) and their duration parameter (column 70).
const DEVIL_BUFF_COOLDOWN_MS = 1800000;
const DEVIL_BUFF_DURATION_MS = 600000;
const MS_PER_MINUTE = 60000;
const TEXT_DEVIL_BUFF_DESC = "Able to use Devil's power for limited time.";
const TEXT_DEVIL_PASSIVE_DESC = "Able to ignore monster's defense.";
const TEXT_DEVIL_PASSIVE = "Passive: always on while the Devil's Spirit is worn";
// The skill window's hint while no devil skill is known (owner wording).
export const DEVIL_SKILL_HINT_TEXT = "Wear a Devil's Spirit to get its skills";
export const DEVIL_SKILL_HINT_DETAIL =
	"An awakened Devil's Spirit teaches them; they appear here and on the extension bar.";
// The devil buffs' names by stage (devilspirit.json, the later client's
// SN_SKILL_MALL_NASRUN_* text; "Vallainous" is its spelling).
const TEXT_DEVIL_BUFF_1 = "Malicious Devil Skill";
const TEXT_DEVIL_BUFF_2 = "Wicked Devil Skill";
const TEXT_DEVIL_BUFF_3 = "Vallainous Devil Skill";
const TEXT_DEVIL_SKILL = "Devil skill";

/*
================
isDevilBuff

A devil buff: the skill a worn Devil's Spirit teaches and its wearer casts.
================
*/
export function isDevilBuff( id: number ): boolean {
	return id >= DEVIL_BUFF_FIRST && id <= DEVIL_BUFF_STAGE_LAST ||
		id >= DEVIL_BUFF_COLOUR_FIRST && id <= DEVIL_BUFF_LAST;
}

/*
================
isDevilSkill
================
*/
export function isDevilSkill( id: number ): boolean {
	return isDevilBuff( id ) || id >= DEVIL_PASSIVE_FIRST && id <= DEVIL_PASSIVE_LAST;
}

/*
================
knownDevilSkills

The devil skills among the learned ones, by id: the skill window's
Devil's Spirit tab (port-only) lists them.
================
*/
export function knownDevilSkills( skills: readonly number[] ): number[] {
	return skills.filter( isDevilSkill ).sort( ( a, b ) => a - b );
}

/*
================
devilSkillForgotten

The skill a 0x3078 kind 5 withdraws, or null for another kind.
================
*/
export function devilSkillForgotten( p: Uint8Array ): number | null {
	if ( p[0] !== DEVIL_SPIRIT_KIND_FORGET ) return null;
	if ( p.length !== 5 ) throw Error( "Invalid Devil's Spirit skill withdrawal" );
	const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
	if ( !isDevilSkill( id ) ) throw Error( "Devil's Spirit withdrawal names another skill" );
	return id;
}

/*
================
devilBuffStage

A devil buff's stage, 0..2: each colour run (plain, yellow, blue) lists
the ECCENTRICDEMON, THEDEVILCONFUSION and DESTROYTHEDEVIL rows in order.
================
*/
function devilBuffStage( id: number ): number {
	return id <= DEVIL_BUFF_STAGE_LAST ? id - DEVIL_BUFF_FIRST : (id - DEVIL_BUFF_COLOUR_FIRST) % DEVIL_BUFF_STAGES;
}

/*
================
devilBuffName
================
*/
function devilBuffName( id: number ): string {
	if ( !isDevilBuff( id ) ) return TEXT_DEVIL_SKILL;
	switch ( devilBuffStage( id ) ) {
		case 0:
			return TEXT_DEVIL_BUFF_1;
		case 1:
			return TEXT_DEVIL_BUFF_2;
	}
	return TEXT_DEVIL_BUFF_3;
}

/*
================
devilSkillStage

A devil skill's stage, 0..2: a buff's own, the first passive the second
stage's and the second passive the third's (devilspirit.json "levels").
================
*/
function devilSkillStage( id: number ): number {
	if ( isDevilBuff( id ) ) return devilBuffStage( id );
	return id === DEVIL_PASSIVE_FIRST ? 1 : 2;
}

/*
================
DevilSkillInfo
================
*/
export interface DevilSkillInfo {
	readonly passive: boolean;
	// "Stage 2 – levels 3-5": the Devil's Spirit enhancement that teaches it.
	readonly stageText: string;
	readonly description: string;
	// "Cooldown 30m · Duration 10m", or the passive's line.
	readonly timingText: string;
}

/*
================
devilSkillInfo

The skill window's Devil's Spirit row text for a devil skill, or null for
another skill. The words are the later client's (devilspirit.json); the
timings its reference rows'.
================
*/
export function devilSkillInfo( id: number ): DevilSkillInfo | null {
	if ( !isDevilSkill( id ) ) return null;
	const stage = devilSkillStage( id ), passive = !isDevilBuff( id );
	return {
		passive,
		stageText: `Stage ${stage + 1} – ${
			stage === 0 ? TEXT_STAGE_1_LEVELS : stage === 1 ? TEXT_STAGE_2_LEVELS : TEXT_STAGE_3_LEVELS
		}`,
		description: passive ? TEXT_DEVIL_PASSIVE_DESC : TEXT_DEVIL_BUFF_DESC,
		timingText: passive ?
			TEXT_DEVIL_PASSIVE :
			`Cooldown ${DEVIL_BUFF_COOLDOWN_MS / MS_PER_MINUTE}m · Duration ${DEVIL_BUFF_DURATION_MS / MS_PER_MINUTE}m`
	};
}

/*
================
devilSpiritWorn

Whether the worn avatar records hold a Devil's Spirit: the skill window
shows its Devil's Spirit tab while one is worn, even before its skills
arrive.
================
*/
export function devilSpiritWorn( avatars: readonly { readonly refObjId: number; }[] | undefined ): boolean {
	return avatars?.some( item => isDevilSpirit( item.refObjId ) ) ?? false;
}

/*
================
devilBuffShortcuts

The shortcut bindings a newly learned devil buff takes, standing in for
the later client's "Shortcut" button of its Devil's Spirit window
(UIIT_CTL_NASRUN_SHORTCUT_BUTTON), which no v1.150 window has: every
shortcut still holding a devil buff the player no longer knows (the stage
the enhancement or the removal replaced) takes the new one in place; with
none, and the new buff on no shortcut yet, the first free slot of the
extension bar (41..50) does. The under bar's F1..F4 pages are never
filled, and a shortcut holding anything else is never taken: a full
extension bar places nothing (devilSkillNoRoomNotice says so).
INFERENCE (owner, 2026-10-19): the port's stand-in, so the buff can be
cast without opening the skill window.
================
*/
export function devilBuffShortcuts(
	learned: number,
	skills: readonly number[],
	quickSlots: readonly { readonly slot: number; readonly kind: number; readonly payload: number; }[]
): { slot: number; kind: number; payload: number; }[] {
	if ( !isDevilBuff( learned ) ) return [];
	const stale = quickSlots.filter( row =>
		row.kind === QUICK_SLOT_SKILL && isDevilBuff( row.payload ) && row.payload !== learned &&
		!skills.includes( row.payload )
	);
	if ( stale.length ) return stale.map( row => ({ slot: row.slot, kind: QUICK_SLOT_SKILL, payload: learned }) );
	if ( quickSlots.some( row => row.kind === QUICK_SLOT_SKILL && row.payload === learned ) ) return [];
	for ( let slot = EXTENSION_FIRST; slot <= EXTENSION_LAST; slot++ ) {
		if ( !quickSlots.some( row => row.slot === slot && row.kind !== 0 ) ) {
			return [ { slot, kind: QUICK_SLOT_SKILL, payload: learned } ];
		}
	}
	return [];
}

/*
================
devilBuffUnplaced

Whether a newly learned devil buff is on no shortcut: the extension bar
was full (devilBuffShortcuts placed nothing).
================
*/
export function devilBuffUnplaced(
	learned: number,
	quickSlots: readonly { readonly kind: number; readonly payload: number; }[]
): boolean {
	return isDevilBuff( learned ) &&
		!quickSlots.some( row => row.kind === QUICK_SLOT_SKILL && row.payload === learned );
}

/*
================
devilSkillPlacedNotice

The notice naming where a devil buff was put: the extension bar's slot
(1..10, the numbers the bar draws), or, for an older stage replaced in
place on the under bar, its page (F1..F4 as 1..4) and slot (1..10).
================
*/
export function devilSkillPlacedNotice( learned: number, slot: number ): SystemNotice {
	const name = devilBuffName( learned );
	if ( slot >= EXTENSION_FIRST && slot <= EXTENSION_LAST ) {
		return textNotice( `${name} placed on the extension bar, slot ${slot - EXTENSION_FIRST + 1}` );
	}
	const page = Math.floor( (slot - SHORTCUT_FIRST) / SHORTCUT_PAGE_SLOTS ) + 1,
		key = (slot - SHORTCUT_FIRST) % SHORTCUT_PAGE_SLOTS + 1;
	if ( slot < SHORTCUT_FIRST || slot > SHORTCUT_LAST ) return textNotice( `${name} placed on the quickslot bar` );
	return textNotice( `${name} placed on quickslot page ${page}, slot ${key}` );
}

/*
================
devilSkillNoRoomNotice
================
*/
export function devilSkillNoRoomNotice( learned: number ): SystemNotice {
	return textNotice(
		`The extension bar is full: drag ${devilBuffName( learned )} from the skill window's Devil's Spirit tab`
	);
}

/*
================
devilBuffNewOnExtensionBar

Whether after puts a devil buff on an extension bar slot that before did
not hold it: the UI opens a collapsed extension bar then, so the placed
buff is seen. A first state (before undefined) opens nothing.
================
*/
export function devilBuffNewOnExtensionBar(
	before: readonly { readonly slot: number; readonly kind: number; readonly payload: number; }[] | undefined,
	after: readonly { readonly slot: number; readonly kind: number; readonly payload: number; }[] | undefined
): boolean {
	if ( !before || !after || before === after ) return false;
	return after.some( row =>
		row.slot >= EXTENSION_FIRST && row.slot <= EXTENSION_LAST && row.kind === QUICK_SLOT_SKILL &&
		isDevilBuff( row.payload ) &&
		!before.some( old => old.slot === row.slot && old.kind === row.kind && old.payload === row.payload )
	);
}

/*
================
devilBuffSkill

The devil buff running on gid, or undefined: an attached effect of the
caster's (the later client's charactervisualchange keys the devil form on
these skills, for the caster and every observer). The newest wins.
================
*/
export function devilBuffSkill(
	gid: number,
	effects: readonly { readonly gid: number; readonly skill: number; }[] | undefined
): number | undefined {
	let skill: number | undefined;
	for ( const effect of effects ?? [] ) {
		if ( effect.gid === gid && isDevilBuff( effect.skill ) ) skill = effect.skill;
	}
	return skill;
}

/*
================
devilBuffActive

Whether a devil buff runs on gid (devilBuffSkill).
================
*/
export function devilBuffActive(
	gid: number,
	effects: readonly { readonly gid: number; readonly skill: number; }[] | undefined
): boolean {
	return devilBuffSkill( gid, effects ) !== undefined;
}

/*
================
devilSpiritAvatars

The worn avatar records a character is drawn with. A Devil's Spirit has
no look of its own, only the devil form of the buff (skill) running on its
wearer: while one runs it is kept, tagged with that skill (look), so the
dress catalog's form for it is drawn (equipment-appearance.ts); otherwise
it is left out. Every other record is kept.
================
*/
export function devilSpiritAvatars<T extends { readonly refObjId: number; }>(
	avatars: readonly T[],
	skill: number | undefined
): readonly (T & { readonly look?: number; })[] {
	if ( !avatars.some( record => isDevilSpirit( record.refObjId ) ) ) return avatars;
	if ( skill === undefined ) return avatars.filter( record => !isDevilSpirit( record.refObjId ) );
	return avatars.map( record => isDevilSpirit( record.refObjId ) ? { ...record, look: skill } : record );
}

/*
================
devilFormScale

The devil form's model scale and the seconds it takes to reach it: the
SCT_CHAR_SCALE of the form's ACT_L stage (keep01.efp: 1.2 over 1000 ms),
or undefined when the record has none. v1.150 never reads that script
(91F9xx stores it, 8DCA40 ignores it); the later client enlarges the
devil, so the port applies it for the devil buffs only.
================
*/
export function devilFormScale(
	stages:
		| readonly {
			readonly phase?: string;
			readonly script?: {
				readonly kind: string;
				readonly operation?: string;
				readonly scale?: number;
				readonly durationSeconds?: number;
			};
		}[]
		| undefined
): { readonly scale: number; readonly seconds: number; } | undefined {
	for ( const stage of stages ?? [] ) {
		const script = stage.script;
		if (
			stage.phase === "ACT_L" && script?.kind === "parsed-scale" && script.operation === "SCT_CHAR_SCALE" &&
			script.scale !== undefined && script.scale > 0
		) return { scale: script.scale, seconds: script.durationSeconds ?? 0 };
	}
	return undefined;
}
