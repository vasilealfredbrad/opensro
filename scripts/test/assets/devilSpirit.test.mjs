/*
===========================================================================

devilSpirit.test.mjs - the Devil's Spirit rows in the equipment catalog

Port-only, not native. The shared data file maps onto v1.150 avatar
attachment records (one per row, bodies of its sex only, the template's
mask and priority), its icons onto icon references, and the import root
resolves only as an absolute path.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import {
	ATTACHMENT_AVATAR_SLOT,
	devilSpiritIconReferences,
	devilSpiritRecords,
	loadDevilSpirit
} from "../../build/char/devilSpiritData.mjs";
import { meshEnvironment, publishDevilSpiritEquipment } from "../../build/char/devilSpirit.mjs";
import { characterMaterialState } from "../../build/shared/characterMaterialState.mjs";
import { resolveImportRoot } from "../../build/world/paths.mjs";
import { checkDevilSpiritAgainstImport } from "./devilSpiritImport.mjs";

// The Halloween attachment's published visual mask and priority.
const TEMPLATE_MASK = 1280;
const TEMPLATE_PRIORITY = 90;

/*
================
template
================
*/
function template( code ) {
	return code.includes( "HALLOWEEN_ATTACH" ) ?
		{ code, visualMask: TEMPLATE_MASK, visualPriority: TEMPLATE_PRIORITY, avatarSlot: ATTACHMENT_AVATAR_SLOT } :
		undefined;
}

test("every row is an avatar attachment for both races of its sex", () => {
	const data = loadDevilSpirit();
	const records = devilSpiritRecords( data, template );
	assert.equal( Object.keys( records ).length, data.items.length );
	for ( const item of data.items ) {
		const record = records[item.id];
		assert.equal( record.code, item.codename );
		assert.equal( record.avatarSlot, ATTACHMENT_AVATAR_SLOT );
		assert.equal( record.slot, null );
		assert.equal( record.visualMask, TEMPLATE_MASK );
		assert.equal( record.visualPriority, TEMPLATE_PRIORITY );
		assert.deepEqual( Object.keys( record.bodies ).sort(), [ `CH_${item.sex}`, `EU_${item.sex}` ] );
	}
});

test("a template that is no attachment refuses the table", () => {
	assert.throws( () => devilSpiritRecords( loadDevilSpirit(), () => undefined ), /template/ );
});

test("without an import the records exist and draw nothing", async () => {
	const out = {
		24154: template( "ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH" ),
		24155: template( "ITEM_MALL_AVATAR_W_HALLOWEEN_ATTACH" )
	};
	if ( resolveImportRoot() ) return;
	const built = await publishDevilSpiritEquipment( out, { getDonor: () => assert.fail( "no model is built" ) } );
	assert.equal( built, 0 );
	for ( const item of loadDevilSpirit().items ) {
		assert.ok( Object.values( out[item.id].bodies ).every( body => body === null ) );
	}
});

test("a v1.150 id refuses the table", async () => {
	const data = loadDevilSpirit();
	const out = {
		24154: template( "ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH" ),
		24155: template( "ITEM_MALL_AVATAR_W_HALLOWEEN_ATTACH" ),
		[data.items[0].id]: { code: "RETAIL" }
	};
	await assert.rejects( publishDevilSpiritEquipment( out, { getDonor: () => null, data } ), /v1\.150 item/ );
});

test("icons are icon-tree references", () => {
	const icons = devilSpiritIconReferences();
	assert.ok( icons.length > 0 );
	for ( const icon of icons ) {
		assert.match(
			icon,
			/^icon\/(item\/(avatar\/mall_avatar_[mw]_nasrun[a-z_]*|etc\/[a-z_]*nasrun[a-z_]*)|skill\/etc\/mall_nasrun_[a-z0-9_]+)\.ddj$/
		);
	}
	assert.ok( icons.includes( "icon/item/etc/nasrun_up_scroll.ddj" ) );
	assert.ok( icons.includes( "icon/item/etc/mall_nasrun_extension.ddj" ) );
	assert.ok( icons.includes( "icon/skill/etc/mall_nasrun_eccentricdemon.ddj" ) );
});

test("the import root is absolute or absent", () => {
	assert.equal( resolveImportRoot( {} ), null );
	assert.equal( resolveImportRoot( { SRO_IMPORT_ROOT: " " } ), null );
	const root = path.resolve( "/import/client" );
	assert.equal( resolveImportRoot( { SRO_IMPORT_ROOT: root } ), root );
	assert.throws( () => resolveImportRoot( { SRO_IMPORT_ROOT: "relative/client" } ), /absolute/ );
});

test("the tables are the later client's (licensed: SRO_IMPORT_ROOT)", t => {
	const root = resolveImportRoot();
	if ( !root ) {
		t.skip( "SRO_IMPORT_ROOT is not set" );
		return;
	}
	const checked = checkDevilSpiritAgainstImport( loadDevilSpirit(), root );
	if ( checked === null ) {
		t.skip( "the import holds no textdata" );
		return;
	}
	assert.ok( checked > 0 );
});

// The devil body's material flags (0x341: alpha test at 128) and its
// environment maps: the later client's body scopes one to BMT material 0.
const BODY_FLAGS = 0x341;
const SCOPED_MAP = { kind: 2, stateId: -1, baseWords: [ 1056964608, 2, 256, 0, 0, 3 ] };
const STATIC_MAP = { kind: 2, stateId: -1, baseWords: [ 1056964608, 1, 256, 0xffffffff, 0, 1 ] };
const ANIMATED_MAP = { kind: 2, stateId: 7, baseWords: [ 1056964608, 1, 256, 0xffffffff, 0, 1 ] };

test("the body skin wears its material-scoped map, so its reflection mask is no cutout", () => {
	const names = [ "avatar_m_nasrun", "avatar_m_nasrun02" ];
	const skin = meshEnvironment( [ SCOPED_MAP, STATIC_MAP ], "avatar_m_nasrun", names );
	assert.deepEqual( skin.dropped, [] );
	assert.deepEqual( skin.kept.map( map => map.baseWords[1] ), [ 1, 1 ] );
	assert.equal( skin.kept[0].baseWords[3], 0xffffffff );
	// AEE6D0: the map replaces the 0x200 test with reference 1, not 128.
	const body = meshEnvironment( [ SCOPED_MAP ], "avatar_m_nasrun", names ).kept;
	assert.equal( characterMaterialState( BODY_FLAGS, body ).alphaCutoff, 1 / 255 );
	assert.equal( characterMaterialState( BODY_FLAGS, [] ).alphaCutoff, 128 / 255 );
	// The resource's other material keeps its own alpha test and only the static map.
	const other = meshEnvironment( [ SCOPED_MAP, STATIC_MAP ], "avatar_m_nasrun02", names );
	assert.deepEqual( other.kept, [ STATIC_MAP ] );
	// An animated map has no baked form.
	assert.deepEqual( meshEnvironment( [ ANIMATED_MAP ], "avatar_m_nasrun", names ).dropped, [ ANIMATED_MAP ] );
	assert.equal( SCOPED_MAP.baseWords[1], 2, "the source modifier is not rewritten in place" );
});
