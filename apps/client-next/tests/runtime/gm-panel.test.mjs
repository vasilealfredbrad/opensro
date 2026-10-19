/*
===========================================================================

gm-panel.test.mjs - the GM panel's catalogue browser and composed lines

Port-only, not native: every line the panel composes must be one the GM
console encoder (gm-command.ts) accepts, so the panel can never send a
command a GM could not type.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { gmRequest, gmItemReferences } = await import( "../../src/engine/foundation/gameplay/gm-command.ts" );
const { gmCatalogSearch, gmItemCategories, gmMonsterCategories, GM_CATALOG_PAGE_ROWS } = await import(
	"../../src/engine/foundation/gameplay/gm-catalog.ts"
);
const { createGmPanelHud } = await import( "../../src/engine/runtime/ui/hud/gm-panel-hud.ts" );

/*
================
typeFlags

ItemRef.TypeFlags for an item (tid1 = 3).
================
*/
function typeFlags( tid2, tid3, tid4 = 0 ) {
	return (3 << 2) | (tid2 << 5) | (tid3 << 7) | (tid4 << 11);
}

const STAFF = typeFlags( 1, 6, 9 ), POTION = typeFlags( 3, 1 ) | 0x60;
const ITEMS = gmItemReferences( {
	itemCommandReferences: [
		{ refObjId: 1, codename: "ITEM_EU_TSTAFF_09_C", typeFlags: STAFF, maxStack: 1, name: "Staff", level: 85 },
		{ refObjId: 2, codename: "ITEM_EU_TSTAFF_01_A", typeFlags: STAFF, maxStack: 1, name: "Staff", level: 1 },
		{ refObjId: 3, codename: "ITEM_CH_SWORD_01_A", typeFlags: STAFF, maxStack: 1, name: "Sword", level: 1 },
		{
			refObjId: 4,
			codename: "ITEM_ETC_GNGWC_WHAN",
			typeFlags: POTION,
			maxStack: 1,
			name: "Berserk Regeneration Potion"
		},
		{ refObjId: 5, codename: "ITEM_ETC_HP_POTION_05", typeFlags: POTION, maxStack: 50, name: "HP Recovery Potion" },
		{ refObjId: 6, codename: "ITEM_ETC_ODD", typeFlags: typeFlags( 3, 15 ), maxStack: 1, name: "Odd" }
	]
} );
const MONSTERS = [
	{ codename: "MOB_CH_TIGERWOMAN", name: "Tiger Girl", level: 20, monsterType: 3 },
	{ codename: "MOB_CH_TIGER", name: "Tiger", level: 8, monsterType: 0 },
	{ codename: "MOB_QT_01_TAHOMET", name: "Mine shaitan", level: 40, monsterType: 0 }
];
const MONSTER_REFS = new Map( [ [ "MOB_CH_TIGERWOMAN", { refObjId: 9, monsterType: 3 } ] ] );

/*
================
search
================
*/
function search( kind, category, query = "", page = 0 ) {
	return gmCatalogSearch( { kind, category, query, page }, ITEMS.values(), MONSTERS );
}

test("a category lists its rows without a query, lower levels first", () => {
	assert.deepEqual( search( "item", "eu-weapon" ).rows.map( row => row.codename ), [
		"ITEM_EU_TSTAFF_01_A",
		"ITEM_EU_TSTAFF_09_C"
	] );
	assert.deepEqual( search( "item", "ch-weapon" ).rows.map( row => row.codename ), [ "ITEM_CH_SWORD_01_A" ] );
	assert.equal( search( "item", "potion" ).total, 2 );
	assert.equal( search( "item", "all" ).total, ITEMS.size );
	assert.deepEqual( search( "monster", "unique" ).rows.map( row => row.codename ), [ "MOB_CH_TIGERWOMAN" ] );
	assert.deepEqual( search( "monster", "quest" ).rows.map( row => row.codename ), [ "MOB_QT_01_TAHOMET" ] );
	assert.deepEqual( search( "monster", "lv1" ).rows.map( row => row.codename ), [ "MOB_CH_TIGER" ] );
});

test("every item falls in exactly one named category or Other", () => {
	const named = gmItemCategories().map( category => category.id ).filter( id => id !== "all" );
	for ( const item of ITEMS.values() ) {
		const homes = named.filter( id => search( "item", id ).rows.some( row => row.codename === item.codename ) );
		assert.equal( homes.length, 1, item.codename + " in " + homes.join( "," ) );
	}
	assert.deepEqual( search( "item", "other" ).rows.map( row => row.codename ), [ "ITEM_ETC_ODD" ] );
	assert.ok( gmMonsterCategories().some( category => category.id === "unique" ) );
});

test("a query filters the category by every word of the name or codename", () => {
	assert.deepEqual( search( "item", "all", "berserk potion" ).rows.map( row => row.codename ), [
		"ITEM_ETC_GNGWC_WHAN"
	] );
	assert.equal( search( "item", "eu-weapon", "sword" ).total, 0 );
	assert.deepEqual( search( "monster", "all", "Tiger" ).rows.map( row => row.codename ), [
		"MOB_CH_TIGER",
		"MOB_CH_TIGERWOMAN"
	] );
});

test("a search publishes one bounded page, clamped to the last", () => {
	const many = Array.from( { length: GM_CATALOG_PAGE_ROWS + 5 }, ( _, i ) => ({
		codename: "MOB_X_" + i,
		name: "Wolf",
		level: 1,
		monsterType: 0
	}) );
	const first = gmCatalogSearch( { kind: "monster", category: "all", query: "", page: 0 }, [], many );
	assert.equal( first.rows.length, GM_CATALOG_PAGE_ROWS );
	assert.equal( first.total, GM_CATALOG_PAGE_ROWS + 5 );
	const last = gmCatalogSearch( { kind: "monster", category: "all", query: "", page: 9 }, [], many );
	assert.equal( last.page, 1 );
	assert.equal( last.rows.length, 5 );
});

/*
================
composed lines
================
*/
test("item and monster lines encode through the console's own encoder", () => {
	const panel = createGmPanelHud();
	const [staff] = search( "item", "eu-weapon", "09" ).rows;
	panel.edit( "gm-panel-amount", "40" );
	// Equipment takes a plus level, clamped to 12.
	assert.equal( panel.makeItem( staff ), "/MAKEITEM ITEM_EU_TSTAFF_09_C 12" );
	const [hp] = search( "item", "potion", "hp" ).rows;
	// A stackable item takes a count, clamped to its stack.
	assert.equal( panel.makeItem( hp ), "/MAKEITEM ITEM_ETC_HP_POTION_05 40" );
	assert.ok( gmRequest( panel.makeItem( hp ) ?? "", ITEMS ) );

	panel.selectTab( "Monsters" );
	panel.edit( "gm-panel-count", "3" );
	panel.selectMonsterType( "GIANT" );
	const [girl] = search( "monster", "unique" ).rows;
	const line = panel.loadMonster( girl );
	assert.equal( line, "/LOADMONSTER MOB_CH_TIGERWOMAN 3 GIANT" );
	assert.ok( gmRequest( line, ITEMS, 0, codename => MONSTER_REFS.get( codename ) ) );
	assert.equal( panel.makeItem( undefined ), undefined );
});

test("player and self lines refuse what the encoder would refuse", () => {
	const panel = createGmPanelHud();
	assert.equal( panel.named( "/RECALLUSER" ), undefined );
	panel.edit( "gm-panel-target", " Gina " );
	assert.equal( panel.named( "/RECALLUSER" ), "/RECALLUSER Gina" );
	panel.edit( "gm-panel-silk", "5000" );
	assert.equal( panel.silk(), "/SILK Gina 5000" );
	assert.ok( gmRequest( panel.silk() ?? "" ) );
	panel.edit( "gm-panel-target", "two words" );
	assert.equal( panel.silk(), undefined );
	panel.edit( "gm-panel-speed", "5" );
	assert.equal( panel.speed(), undefined );
	panel.edit( "gm-panel-speed", "300" );
	assert.equal( panel.speed(), "/SETSPEED 300" );
	assert.equal( panel.speed( true ), "/SETSPEED" );
	panel.edit( "gm-panel-hour", "24" );
	assert.equal( panel.time(), undefined );
	panel.edit( "gm-panel-hour", "7" );
	assert.ok( gmRequest( panel.time() ?? "" ) );
});

test("each catalogue tab keeps its own category, filter and page", () => {
	const panel = createGmPanelHud();
	assert.deepEqual( panel.request(), { kind: "item", category: "all", query: "", page: 0 } );
	panel.selectCategory( "potion" );
	panel.turnPage( 1, 3 );
	panel.edit( "gm-panel-query", "hp" );
	assert.deepEqual( panel.request(), { kind: "item", category: "potion", query: "hp", page: 0 } );
	panel.turnPage( 5, 3 );
	assert.equal( panel.request()?.page, 2 );
	panel.selectTab( "Monsters" );
	assert.deepEqual( panel.request(), { kind: "monster", category: "all", query: "", page: 0 } );
	panel.selectTab( "Self" );
	assert.equal( panel.request(), undefined );
	panel.selectTab( "Items" );
	assert.deepEqual( panel.request(), { kind: "item", category: "potion", query: "hp", page: 2 } );
	assert.equal( panel.state().fields["gm-panel-query"], "hp" );
});
