/*
===========================================================================

devil-spirit-state.test.mjs - the Devil's Spirit's awakening on the client

Port-only, not native (owner, 2026-10-18). The server keeps a Devil's
Spirit's later-client state in its item fields (durability the
awakening's expiry, variance the enhancement and UMDU); the client reads
them for the tooltip, words the MATTR_NASRUN_* options, refuses to send
an expired one to the avatar storage, turns the scroll's and extension's
answers and the 0x3078 notifications into notices, and applies a worn
item's state. The scroll, extension and option ids are pinned to the
server's devilspirit.json.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const ds = await import( sourceFileUrl( "src/engine/foundation/gameplay/devil-spirit.ts" ).href );
const { avatarEquipVerdict } = await import( sourceFileUrl( "src/engine/foundation/gameplay/avatar-equip.ts" ).href );
const { itemStateDelta } = await import( sourceFileUrl( "src/engine/foundation/gameplay/item-state-delta.ts" ).href );
const { itemTooltipMagic } = await import( sourceFileUrl( "src/engine/foundation/ui/item-tooltip-magic.ts" ).href );

const DATA = JSON.parse(
	readFileSync(
		new URL( "../../../server/internal/game/item/devilspirit/devilspirit.json", import.meta.url ),
		"utf8"
	)
);
const NOW = 1_800_000_000;
const DEVIL = 24385;
// TID 3.1.13.3, the attachment the rows are cloned from.
const ATTACHMENT_FLAGS = 0x1eac;

/*
================
variance

The packed state as the server writes it (state.go encode).
================
*/
function variance( bindUntil, bindLevel, umdu ) {
	return String( BigInt( bindUntil ) | BigInt( bindLevel ) << 32n | BigInt( umdu ) << 40n );
}

test("the scroll and extension ids are the server's", () => {
	const ids = Object.fromEntries( DATA.etc.map( row => [ row.kind, row.id ] ) );
	assert.equal( ds.DEVIL_SPIRIT_SCROLL_ID, ids.upgrade );
	assert.equal( ds.DEVIL_SPIRIT_EXTENSION_ID, ids.extension );
});

test("the state decodes from the item fields", () => {
	const state = ds.devilSpiritState( { durability: NOW + 60, variance: variance( NOW + 600, 7, 8 ) } );
	assert.deepEqual( state, { awakeUntil: NOW + 60, bindUntil: NOW + 600, bindLevel: 7, umdu: 8 } );
	assert.equal( ds.devilSpiritExpired( state, NOW ), false );
	assert.equal( ds.devilSpiritExpired( state, NOW + 60 ), true );
	assert.equal( ds.devilSpiritExpired( { ...state, awakeUntil: 0 }, NOW ), false );
	assert.equal( ds.devilSpiritExpired( { ...state, awakeUntil: 0xffffffff }, NOW * 2 ), false );
});

test("the tooltip shows the awakening and a live enhancement", () => {
	const rows = item => ds.devilSpiritTooltipRows( item, NOW ).map( row => row.value );
	assert.deepEqual( rows( { durability: 0, variance: "0", plus: 0 } ), [ "This devil's spirit is not awaken." ] );
	assert.deepEqual( rows( { durability: NOW - 1, variance: "0", plus: 0 } ), [ "Awaken period is over." ] );
	assert.deepEqual(
		rows( { durability: NOW + 2 * 86400 + 3 * 3600, variance: variance( NOW + 5400, 6, 0 ), plus: 0 } ),
		[ "Awaken period. 2d 3h", "Awakening enhancement +6 (Time Remaining: 1h 30m)" ]
	);
	assert.deepEqual( rows( { durability: NOW + 600, variance: variance( NOW - 1, 6, 0 ), plus: 0 } ), [
		"Awaken period. 10m"
	] );
});

test("the NASRUN options are worded", () => {
	const refs = DATA.magicOptions.map( row => ({
		paramId: row.id,
		optionName: row.codename,
		paramName: "+",
		degree: 1,
		rangeWords: row.rangeWords
	}) );
	const option = ( codename, value ) =>
		String( BigInt( value ) << 32n | BigInt( DATA.magicOptions.find( r => r.codename === codename ).id ) );
	const rows = itemTooltipMagic( {
		typeFlags: ATTACHMENT_FLAGS,
		magic: [
			option( "MATTR_NASRUN_UMDU", 8 ),
			option( "MATTR_NASRUN_HPNA", 21 ),
			option( "MATTR_NASRUN_MPNA", 21 ),
			option( "MATTR_NASRUN_BLOCKRATE", 2 )
		],
		magicReferences: refs
	}, symbol => symbol ).map( row => row.value );
	assert.deepEqual( rows, [
		"Damage increase (it will apply to Unique monsters only) 8%",
		"HP increase 21%",
		"MP increase 21%",
		"Blocking rate 2"
	] );
});

test("an expired Devil's Spirit stays in the bag", () => {
	const item = { slot: 20, refObjId: DEVIL, typeFlags: ATTACHMENT_FLAGS };
	assert.equal( avatarEquipVerdict( { ...item, durability: NOW - 1 }, [], NOW ).kind, "expired" );
	assert.equal( avatarEquipVerdict( { ...item, durability: NOW + 1 }, [], NOW ).kind, "send" );
	assert.equal( avatarEquipVerdict( { ...item, durability: 0 }, [], NOW ).kind, "send" );
	assert.equal( avatarEquipVerdict( { ...item, refObjId: 23440, durability: 1 }, [], NOW ).kind, "needs-dress" );
});

test("the scroll, extension and expiry answers are notices", () => {
	const text = ( opcode, bytes ) => ds.devilSpiritNotice( opcode, Uint8Array.from( bytes ) )?.text ?? null;
	assert.equal( text( 0xb5bd, [ 2, 0xdd ] ), "You have failed to enhance Awakening Item" );
	assert.equal( text( 0xb5bd, [ 2, 0xdf ] ), "The item can be used after awakening." );
	assert.equal( text( 0xb5bd, [ 2, 0x84 ] ), null );
	assert.equal( text( 0xb5bd, [ 1, 25, 1, 0, 0xec, 0x46 ] ), null );
	assert.equal( text( 0x3078, [ 2, 21, 0, 0, 0, 1 ] ), "Awaken period is extended." );
	assert.equal( text( 0x3078, [ 3, 1 ] ), "Awaken period is over." );
	assert.equal( text( 0x3078, [ 4, 1, 0 ] ), null );
	assert.throws( () => text( 0x3078, [ 3 ] ), /expiry/ );
});

test("a raised enhancement says so once", () => {
	const before = { refObjId: DEVIL, durability: NOW + 600, variance: "0", name: "Crimson Devil's Spirit" };
	const after = { ...before, variance: variance( NOW + 10800, 6, 0 ) };
	assert.equal(
		ds.devilSpiritUpgradeNotice( before, after, NOW ).text,
		"For the next 180 minute(s), the grade of the Crimson Devil's Spirit enhancement will be adjusted to 6."
	);
	assert.equal( ds.devilSpiritUpgradeNotice( after, after, NOW ), null );
	assert.equal( ds.devilSpiritUpgradeNotice( before, before, NOW ), null );
});

test("a worn item's state applies in the 0x3645 layout", () => {
	const body = new Uint8Array( 2 + 1 + 8 + 4 + 1 );
	const view = new DataView( body.buffer );
	body[0] = 1;
	body[1] = 2 | 4 | 16 | 32;
	body[2] = 3;
	view.setBigUint64( 3, BigInt( variance( 0, 0, 4 ) ), true );
	view.setUint32( 11, 0xffffffff, true );
	body[15] = 0;
	const frame = Uint8Array.of( 4, ...body );
	const worn = new Map( [ [ 1, {
		slot: 1,
		refObjId: DEVIL,
		typeFlags: ATTACHMENT_FLAGS,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [ "1" ]
	} ] ] );
	const next = itemStateDelta( ds.devilSpiritWornState( frame ), worn, new Map(), new Map() );
	assert.equal( next.slot, 1 );
	assert.deepEqual( [ next.item.plus, next.item.durability, next.item.variance, next.item.magic ], [
		3,
		0xffffffff,
		variance( 0, 0, 4 ),
		[]
	] );
	assert.equal( ds.devilSpiritWornState( Uint8Array.of( 3, 1 ) ), null );
});

test("the devil skills are the server's", () => {
	const buffs = DATA.skills.filter( skill => !skill.codename.includes( "MOBDEFENCEIGNORE" ) ).map( skill =>
		skill.id
	);
	for ( const skill of DATA.skills ) {
		assert.equal( ds.isDevilSkill( skill.id ), true, skill.codename );
		assert.equal( ds.isDevilBuff( skill.id ), buffs.includes( skill.id ), skill.codename );
	}
	assert.equal( ds.isDevilSkill( 31145 ), false );
	assert.equal( ds.isDevilSkill( 31157 ), false );
});

test("a withdrawn devil skill is read from 0x3078 kind 5", () => {
	assert.equal( ds.devilSkillForgotten( Uint8Array.of( 5, 0xaa, 0x79, 0, 0 ) ), 31146 );
	assert.equal( ds.devilSkillForgotten( Uint8Array.of( 4, 1, 0 ) ), null );
	assert.throws( () => ds.devilSkillForgotten( Uint8Array.of( 5, 1, 0, 0, 0 ) ), /another skill/ );
});

test("a learned devil buff takes the shortcut of the one it replaced, or a free extension slot", () => {
	const skill = payload => ({ slot: 3, kind: 0x49, payload });
	// The stage-1 buff withdrawn: the stage-3 buff takes its shortcut.
	assert.deepEqual( ds.devilBuffShortcuts( 31148, [ 31148 ], [ skill( 31146 ) ] ), [ skill( 31148 ) ] );
	// Already bound: nothing.
	assert.deepEqual( ds.devilBuffShortcuts( 31146, [ 31146 ], [ skill( 31146 ) ] ), [] );
	// None bound: the first free extension bar slot (41..50), not a page slot.
	assert.deepEqual( ds.devilBuffShortcuts( 31146, [ 31146 ], [ { slot: 41, kind: 0x49, payload: 1 } ] ), [ {
		slot: 42,
		kind: 0x49,
		payload: 31146
	} ] );
	// A passive or another skill takes none.
	assert.deepEqual( ds.devilBuffShortcuts( 31149, [ 31149 ], [] ), [] );
	assert.deepEqual( ds.devilBuffShortcuts( 1, [ 1 ], [] ), [] );
});

test("the Devil's Spirit is drawn only while its devil buff runs, as that buff's form", () => {
	const worn = [ { refObjId: DEVIL }, { refObjId: 23438 } ];
	const effects = [ { gid: 7, skill: 31152 }, { gid: 8, skill: 1 } ];
	assert.equal( ds.devilBuffActive( 7, effects ), true );
	assert.equal( ds.devilBuffActive( 8, effects ), false );
	assert.equal( ds.devilBuffActive( 7, undefined ), false );
	assert.equal( ds.devilBuffSkill( 7, effects ), 31152 );
	assert.equal( ds.devilBuffSkill( 8, effects ), undefined );
	assert.deepEqual( ds.devilSpiritAvatars( worn, 31152 ), [ { refObjId: DEVIL, look: 31152 }, { refObjId: 23438 } ] );
	assert.deepEqual( ds.devilSpiritAvatars( worn, undefined ), [ { refObjId: 23438 } ] );
	const plain = [ { refObjId: 23438 } ];
	assert.equal( ds.devilSpiritAvatars( plain, undefined ), plain );
	assert.equal( ds.devilSpiritAvatars( plain, 31148 ), plain );
});
