/*
===========================================================================

gm-catalog.ts - the GM panel's item and monster browser

Port-only, not native. The native GM console takes codenames only; the GM
panel lets a GM browse every /MAKEITEM item and /LOADMONSTER monster by
category, optionally filtered by English name or codename. The worker owns
the catalogue (the reference file's item command rows and monster rows)
and publishes only one bounded page of matches, so the UI thread never
holds the whole catalogue.

===========================================================================
*/
import type { GmItemReference } from "./gm-command";

// One published page: enough to scroll, small enough for every view.
export const GM_CATALOG_PAGE_ROWS = 200;
export const GM_CATALOG_QUERY_BYTES = 64;
// The typeFlags bits gm-command.ts reads as "stackable" (50A838).
const STACKABLE_FLAGS = 0x60;
// The unique monster type byte (gmmonster.go GMSpawnRarity, 520D90).
const UNIQUE_MONSTER_TYPE = 3;

export type GmCatalogKind = "item" | "monster";

/*
================
GmCatalogCategory

A browsable group the panel lists; gmItemInCategory and
gmMonsterInCategory decide membership by its id.
================
*/
export interface GmCatalogCategory {
	readonly id: string;
	readonly label: string;
}

/*
================
gmItemCategories

The item groups, in the order the panel lists them. "Other" takes every
row no named group claims, so each item is reachable without a search.
================
*/
export function gmItemCategories(): readonly GmCatalogCategory[] {
	return [
		{ id: "all", label: "All items" },
		{ id: "ch-weapon", label: "CH Weapons" },
		{ id: "eu-weapon", label: "EU Weapons" },
		{ id: "shield", label: "Shields" },
		{ id: "ch-garment", label: "CH Garment" },
		{ id: "ch-protector", label: "CH Protector" },
		{ id: "ch-armor", label: "CH Armor" },
		{ id: "eu-robe", label: "EU Robe" },
		{ id: "eu-light", label: "EU Light armor" },
		{ id: "eu-heavy", label: "EU Heavy armor" },
		{ id: "accessory", label: "Accessories" },
		{ id: "job", label: "Job suits" },
		{ id: "avatar", label: "Avatars" },
		{ id: "pet", label: "Pets" },
		{ id: "potion", label: "Potions & pills" },
		{ id: "scroll", label: "Scrolls & mounts" },
		{ id: "alchemy", label: "Alchemy" },
		{ id: "quest", label: "Quest items" },
		{ id: "special", label: "Special & mall" },
		{ id: "other", label: "Other" }
	];
}

/*
================
itemGroup

The named group an item belongs to, from TypeID2 and TypeID3 of its
command row (ItemRef.TypeFlags packs tid1<<2 | tid2<<5 | tid3<<7 |
tid4<<11), or "other".
================
*/
function itemGroup( item: GmItemReference ): string {
	const tid2 = (item.typeFlags >> 5) & 3, tid3 = (item.typeFlags >> 7) & 15;
	if ( tid2 === 2 ) return "pet";
	if ( tid2 === 1 ) {
		switch ( tid3 ) {
			case 1:
				return "ch-garment";
			case 2:
				return "ch-protector";
			case 3:
				return "ch-armor";
			case 4:
				return "shield";
			case 5:
			case 12:
				return "accessory";
			case 6:
				return item.codename.startsWith( "ITEM_CH_" ) ? "ch-weapon" : "eu-weapon";
			case 7:
				return "job";
			case 9:
				return "eu-robe";
			case 10:
				return "eu-light";
			case 11:
				return "eu-heavy";
			case 13:
				return "avatar";
		}
		return "other";
	}
	if ( tid2 === 3 ) {
		switch ( tid3 ) {
			case 1:
			case 2:
				return "potion";
			case 3:
				return "scroll";
			case 9:
				return "quest";
			case 10:
			case 11:
				return "alchemy";
			case 13:
				return "special";
		}
	}
	return "other";
}

/*
================
gmItemInCategory
================
*/
export function gmItemInCategory( category: string, item: GmItemReference ): boolean {
	return category === "all" || itemGroup( item ) === category;
}

/*
================
GmCatalogMonster
================
*/
export interface GmCatalogMonster {
	readonly codename: string;
	readonly name: string;
	readonly level: number;
	readonly monsterType: number;
}

/*
================
gmMonsterCategories
================
*/
export function gmMonsterCategories(): readonly GmCatalogCategory[] {
	return [
		{ id: "all", label: "All monsters" },
		{ id: "unique", label: "Uniques" },
		{ id: "lv1", label: "Level 1-20" },
		{ id: "lv21", label: "Level 21-40" },
		{ id: "lv41", label: "Level 41-60" },
		{ id: "lv61", label: "Level 61-80" },
		{ id: "lv81", label: "Level 81+" },
		{ id: "quest", label: "Quest monsters" },
		{ id: "fortress", label: "Fortress" }
	];
}

/*
================
monsterGroup

Uniques, quest spawns and fortress structures by themselves; every other
monster by its level band.
================
*/
function monsterGroup( monster: GmCatalogMonster ): string {
	if ( monster.monsterType === UNIQUE_MONSTER_TYPE ) return "unique";
	if ( monster.codename.startsWith( "MOB_QT_" ) ) return "quest";
	if ( monster.codename.startsWith( "MOB_FW_" ) ) return "fortress";
	return monster.level <= 20 ?
		"lv1" :
		monster.level <= 40 ?
		"lv21" :
		monster.level <= 60 ?
		"lv41" :
		monster.level <= 80 ?
		"lv61" :
		"lv81";
}

/*
================
gmMonsterInCategory
================
*/
export function gmMonsterInCategory( category: string, monster: GmCatalogMonster ): boolean {
	return category === "all" || monsterGroup( monster ) === category;
}

/*
================
GmCatalogRow
================
*/
export interface GmCatalogRow {
	readonly codename: string;
	readonly name: string;
	readonly level: number;
	// Items only: a stackable item's amount is a count, else a plus level.
	readonly stackable: boolean;
	readonly maxStack: number;
}

/*
================
GmCatalogRequest

What the panel asks for: one page of one category, optionally filtered.
================
*/
export interface GmCatalogRequest {
	readonly kind: GmCatalogKind;
	readonly category: string;
	readonly query: string;
	readonly page: number;
}

/*
================
GmCatalog

One answered request. revision orders answers; total counts every match,
of which rows holds page `page` (GM_CATALOG_PAGE_ROWS per page).
================
*/
export interface GmCatalog extends GmCatalogRequest {
	readonly revision: number;
	readonly rows: readonly GmCatalogRow[];
	readonly total: number;
}

/*
================
gmCatalogQuery

The bounded, trimmed form of a typed query.
================
*/
export function gmCatalogQuery( value: string ): string {
	return value.trim().slice( 0, GM_CATALOG_QUERY_BYTES );
}

/*
================
gmCatalogSearch

Rows of the category whose name or codename holds every typed word
(case-insensitive); an empty query keeps the whole category. An exact name
match sorts first, then lower levels, then names. Returns the requested
page, clamped to the last one, and the match count.
================
*/
export function gmCatalogSearch(
	request: GmCatalogRequest,
	items: Iterable<GmItemReference>,
	monsters: readonly GmCatalogMonster[]
): { rows: GmCatalogRow[]; total: number; page: number; } {
	const words = gmCatalogQuery( request.query ).toLowerCase().split( /\s+/ ).filter( Boolean );
	const matches: GmCatalogRow[] = [];
	const consider = ( row: GmCatalogRow ) => {
		const text = (row.name + " " + row.codename).toLowerCase();
		if ( words.every( word => text.includes( word ) ) ) matches.push( row );
	};
	if ( request.kind === "item" ) {
		for ( const item of items ) {
			if ( !gmItemInCategory( request.category, item ) ) continue;
			consider( {
				codename: item.codename,
				name: item.name ?? "",
				level: item.level ?? 0,
				stackable: (item.typeFlags & STACKABLE_FLAGS) === STACKABLE_FLAGS,
				maxStack: item.maxStack
			} );
		}
	} else {
		for ( const monster of monsters ) {
			if ( !gmMonsterInCategory( request.category, monster ) ) continue;
			consider( {
				codename: monster.codename,
				name: monster.name,
				level: monster.level,
				stackable: false,
				maxStack: 1
			} );
		}
	}
	const phrase = words.join( " " );
	const rank = ( row: GmCatalogRow ) => phrase && row.name.toLowerCase() === phrase ? 0 : 1;
	matches.sort( ( a, b ) =>
		rank( a ) - rank( b ) || a.level - b.level || a.name.localeCompare( b.name ) ||
		a.codename.localeCompare( b.codename )
	);
	const pages = Math.max( 1, Math.ceil( matches.length / GM_CATALOG_PAGE_ROWS ) );
	const page = Math.min( pages - 1, Math.max( 0, Math.floor( request.page ) || 0 ) );
	return {
		rows: matches.slice( page * GM_CATALOG_PAGE_ROWS, (page + 1) * GM_CATALOG_PAGE_ROWS ),
		total: matches.length,
		page
	};
}
