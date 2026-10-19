/*
===========================================================================

devil-spirit.test.mjs - a worn Devil's Spirit on the character

Port-only, not native (owner, 2026-10-18). The Devil's Spirit rows
(apps/server/internal/game/item/devilspirit/devilspirit.json, which the
server and the asset build both read) take the v1.150 avatar attachment
socket but, as in the later client they come from, need no avatar dress:
the bag's double-click or drag sends the avatar move with or without
one, while a retail attachment still needs a dress and says so. Worn,
the Devil's Spirit's model is drawn, with a dress or alone; published
without a model (no import), it draws nothing instead of failing the
wearer's appearance.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { assembleEquipmentAppearance } = await import(
	sourceFileUrl( "src/engine/foundation/animation/equipment-appearance.ts" ).href
);
const { DEVIL_SPIRIT_FIRST_ID, DEVIL_SPIRIT_LAST_ID, avatarEquipVerdict, isDevilSpirit } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/avatar-equip.ts" ).href
);
const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

const DATA = JSON.parse(
	readFileSync(
		new URL( "../../../server/internal/game/item/devilspirit/devilspirit.json", import.meta.url ),
		"utf8"
	)
);
// ITEM_MALL_AVATAR_M_HALLOWEEN, the dress the attachment needs.
const DRESS = 23438;
const DRESS_GLB = "/assets/char/equipment/dress.glb";
const DEVIL_GLB = "/assets/char/equipment/devil.glb";

/*
================
dress

The catalog as the visuals build publishes it for the Chinese man: the
Halloween dress and every male Devil's Spirit, modelled or not.
================
*/
function dress( modelled ) {
	const equipment = {
		[DRESS]: {
			slot: null,
			avatarSlot: 1,
			armorClass: 0,
			thiefSuit: false,
			visualMask: 574,
			visualPriority: 70,
			model: null,
			source: null,
			bodies: { CH_M: { glb: DRESS_GLB, parts: [ "EQ0" ], covers: {} } }
		}
	};
	for ( const item of DATA.items.filter( row => row.sex === "M" ) ) {
		equipment[item.id] = {
			code: item.codename,
			slot: null,
			avatarSlot: 2,
			armorClass: 0,
			thiefSuit: false,
			visualMask: 1280,
			visualPriority: 90,
			model: item.models[0],
			source: null,
			bodies: { CH_M: modelled ? { glb: DEVIL_GLB, parts: [ "EQ0", "EQ1", "EQ2" ], covers: {} } : null }
		};
	}
	return { equipment };
}

/*
================
wornModels
================
*/
function wornModels( catalog, refObjId, dressed = true ) {
	const assembly = assembleEquipmentAppearance( {
		resource: { codename: "CHAR_CH_MAN_ADVENTURER", cover: {} },
		dress: catalog,
		equipment: [],
		avatars: dressed ? [ { refObjId: DRESS }, { refObjId } ] : [ { refObjId } ],
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: true
	} );
	return assembly.parts.map( part => part.model );
}

test("every male Devil's Spirit is drawn over its dress", () => {
	for ( const item of DATA.items.filter( row => row.sex === "M" ) ) {
		const models = wornModels( dress( true ), item.id );
		assert.ok( models.includes( DRESS_GLB ) && models.includes( DEVIL_GLB ), `${item.codename}: ${models}` );
	}
});

test("a Devil's Spirit published without a model draws nothing and does not fail", () => {
	const models = wornModels( dress( false ), DATA.items.find( row => row.sex === "M" ).id );
	assert.ok( models.includes( DRESS_GLB ) );
	assert.ok( !models.includes( DEVIL_GLB ) );
});

test("a Devil's Spirit worn without a dress is drawn alone", () => {
	for ( const item of DATA.items.filter( row => row.sex === "M" ) ) {
		const models = wornModels( dress( true ), item.id, false );
		assert.ok( models.includes( DEVIL_GLB ) && !models.includes( DRESS_GLB ), `${item.codename}: ${models}` );
	}
});

// The bag records the avatar gate reads (TID 3.1.13.x; TID4 2 dress, 3 attachment).
const ATTACHMENT_FLAGS = 0x1eac;
const DRESS_FLAGS = 0x16ac;
const RETAIL_ATTACHMENT = 23440;

test("the client's Devil's Spirit ids are the server's rows", () => {
	const ids = Array.from(
		{ length: DEVIL_SPIRIT_LAST_ID - DEVIL_SPIRIT_FIRST_ID + 1 },
		( _, i ) => DEVIL_SPIRIT_FIRST_ID + i
	);
	assert.deepEqual( ids, DATA.items.map( row => row.id ).sort( ( a, b ) => a - b ) );
	assert.ok( !isDevilSpirit( DEVIL_SPIRIT_FIRST_ID - 1 ) && !isDevilSpirit( DEVIL_SPIRIT_LAST_ID + 1 ) );
});

test("a Devil's Spirit needs no dress; a retail attachment still does", () => {
	const dressed = [ { slot: 0, refObjId: DRESS, typeFlags: DRESS_FLAGS } ];
	for ( const item of DATA.items ) {
		const spirit = { slot: 13, refObjId: item.id, typeFlags: ATTACHMENT_FLAGS };
		assert.deepEqual( avatarEquipVerdict( spirit, [] ), { kind: "send", destination: 0 } );
		assert.deepEqual( avatarEquipVerdict( spirit, dressed ), { kind: "send", destination: 1 } );
	}
	const retail = { slot: 13, refObjId: RETAIL_ATTACHMENT, typeFlags: ATTACHMENT_FLAGS };
	assert.deepEqual( avatarEquipVerdict( retail, [] ), { kind: "needs-dress" } );
	assert.deepEqual( avatarEquipVerdict( retail, dressed ), { kind: "send", destination: 1 } );
	// One attachment socket: a worn Devil's Spirit refuses a second attachment.
	const worn = [ { slot: 0, refObjId: DATA.items[0].id, typeFlags: ATTACHMENT_FLAGS } ];
	assert.deepEqual( avatarEquipVerdict( retail, worn ), { kind: "occupied" } );
});

/*
================
inventoryWindow

The production HUD with the inventory open over the given bag and worn
avatars; sent collects the gameplay commands.
================
*/
function inventoryWindow( inventory, sent ) {
	const f = uiFixture( c => sent.push( c.command ) );
	const state = {
		...f.state,
		gameplay: {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory,
			avatarInventory: []
		}
	};
	let now = 0;
	for ( ; now < 1200; now += 100 ) f.ui.step( state, now );
	f.ui.event( { kind: "key", code: "KeyI" } );
	f.ui.step( state, ++now );
	return { f, state, next: () => f.ui.step( state, ++now ) };
}

test("double-clicking a Devil's Spirit in the bag wears it without a dress", () => {
	const sent = [];
	const { f } = inventoryWindow( [ {
		slot: 13,
		refObjId: DATA.items[0].id,
		typeFlags: ATTACHMENT_FLAGS,
		quantity: 1
	} ], sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( sent, [ { kind: "avatar-move", equip: true, source: 13, destination: 0 } ] );
	} finally {
		f.dispose();
	}
});

test("dragging a Devil's Spirit onto the avatar window wears it without a dress", () => {
	const sent = [];
	const { f, next } = inventoryWindow(
		[ { slot: 13, refObjId: DATA.items[0].id, typeFlags: ATTACHMENT_FLAGS, quantity: 1 } ],
		sent
	);
	try {
		f.ui.event( { kind: "activate", id: "equipment-view" } );
		const controls = next()?.controls ?? [];
		const avatar = controls.find( c => c.id.startsWith( "avatar:" ) )?.rect;
		assert.ok( avatar, "no avatar slot on screen" );
		f.ui.event( { kind: "drag", id: "slot:13", dx: 36, dy: 0 } );
		f.ui.event( { kind: "drag-end", id: "slot:13", x: avatar[0] + 8, y: avatar[1] + 8 } );
		assert.deepEqual( sent, [ { kind: "avatar-move", equip: true, source: 13, destination: 0 } ] );
	} finally {
		f.dispose();
	}
});

test("a retail attachment without a dress stays in the bag and says why", () => {
	const sent = [];
	const { f, next } = inventoryWindow( [ {
		slot: 13,
		refObjId: RETAIL_ATTACHMENT,
		typeFlags: ATTACHMENT_FLAGS,
		quantity: 1
	} ], sent );
	try {
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		next();
		assert.deepEqual( sent, [] );
		assert.ok( f.hasText( "Can only be equipped when avatar dress is worn." ) );
	} finally {
		f.dispose();
	}
});
