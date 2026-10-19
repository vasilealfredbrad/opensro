/*
===========================================================================

gm-panel-hud.ts - the GM panel's state and the console lines it composes

Port-only, not native. The v1.150 client gives a GM only the slash
console (Shift+`); this window puts the same commands behind searchable
lists and buttons. It sends nothing the console could not: every action
becomes one gm-command line that gm-command.ts encodes and the server
authorises against the character's GM privilege, so the panel grants no
new power. ui.ts owns this module, draws the window and routes its events.

===========================================================================
*/
import type { GmCatalogKind, GmCatalogRequest, GmCatalogRow } from "@/engine/foundation/gameplay/gm-catalog";

export const GM_PANEL_TABS = [ "Items", "Monsters", "Players", "Self" ] as const;
export type GmPanelTab = (typeof GM_PANEL_TABS)[number];

// The edit fields the window publishes; ui.ts routes their edit events here.
export const GM_PANEL_FIELDS = [
	"gm-panel-query",
	"gm-panel-amount",
	"gm-panel-count",
	"gm-panel-target",
	"gm-panel-silk",
	"gm-panel-speed",
	"gm-panel-hour"
] as const;
export type GmPanelField = (typeof GM_PANEL_FIELDS)[number];

export const GM_PANEL_VISIBLE_ROWS = 18;
// /LOADMONSTER's optional type word; "" sends the monster's own type.
export const GM_MONSTER_TYPES = [ "", "NORMAL", "CHAMP", "GIANT" ] as const;
export type GmMonsterType = (typeof GM_MONSTER_TYPES)[number];

// Mirrors of gm-command.ts's accepted ranges, so a button never composes
// a line the encoder would refuse.
const MAX_PLUS = 12;
const MAX_STACK_AMOUNT = 255;
const MAX_SPAWN_COUNT = 255;
const MAX_SILK = 1_000_000;
const MIN_SPEED = 10;
const MAX_SPEED = 1000;
const MAX_NAME_LENGTH = 64;

/*
================
GmPanelState
================
*/
export interface GmPanelState {
	readonly tab: GmPanelTab;
	// Per catalogue: the browsed category, its filter, page and selection.
	readonly browse: Readonly<Record<GmCatalogKind, GmPanelBrowse>>;
	readonly offset: number;
	readonly fields: Readonly<Record<GmPanelField, string>>;
	readonly monsterType: GmMonsterType;
}

/*
================
GmPanelBrowse
================
*/
export interface GmPanelBrowse {
	readonly category: string;
	readonly query: string;
	readonly page: number;
	readonly selected: string;
}

/*
================
catalogOf

The catalogue a list tab browses, or undefined for the button tabs.
================
*/
export function catalogOf( tab: GmPanelTab ): GmCatalogKind | undefined {
	return tab === "Items" ? "item" : tab === "Monsters" ? "monster" : undefined;
}

/*
================
digits
================
*/
function digits( value: string ): number | undefined {
	return /^[0-9]{1,9}$/.test( value.trim() ) ? Number( value.trim() ) : undefined;
}

/*
================
characterName

A typed character name with no whitespace (the encoder refuses one for
/SILK, and no character name holds a space).
================
*/
function characterName( value: string ): string | undefined {
	const name = value.trim();
	return name && name.length <= MAX_NAME_LENGTH && !/\s/.test( name ) ? name : undefined;
}

/*
================
createGmPanelHud
================
*/
export function createGmPanelHud() {
	const fresh: GmPanelBrowse = { category: "all", query: "", page: 0, selected: "" };
	let state: GmPanelState = {
		tab: "Items",
		browse: { item: fresh, monster: fresh },
		offset: 0,
		fields: {
			"gm-panel-query": "",
			"gm-panel-amount": "0",
			"gm-panel-count": "1",
			"gm-panel-target": "",
			"gm-panel-silk": "1000",
			"gm-panel-speed": "200",
			"gm-panel-hour": "12"
		},
		monsterType: ""
	};
	/*
	================
	browse

	Changes the current tab's browse state; a new page starts at its top.
	================
	*/
	const browse = ( change: Partial<GmPanelBrowse> ) => {
		const kind = catalogOf( state.tab );
		if ( !kind ) return;
		state = { ...state, offset: 0, browse: { ...state.browse, [kind]: { ...state.browse[kind], ...change } } };
	};
	return {
		/*
		================
		state
		================
		*/
		state(): GmPanelState {
			return state;
		},
		/*
		================
		request

		The catalogue page the current tab shows, or undefined for the button
		tabs. ui.ts sends it after every change that can alter the page.
		================
		*/
		request(): GmCatalogRequest | undefined {
			const kind = catalogOf( state.tab );
			if ( !kind ) return undefined;
			const browse = state.browse[kind];
			return { kind, category: browse.category, query: browse.query, page: browse.page };
		},
		/*
		================
		selectTab
		================
		*/
		selectTab( tab: GmPanelTab ) {
			const kind = catalogOf( tab );
			state = {
				...state,
				tab,
				offset: 0,
				fields: { ...state.fields, "gm-panel-query": kind ? state.browse[kind].query : "" }
			};
		},
		/*
		================
		selectCategory
		================
		*/
		selectCategory( category: string ) {
			browse( { category, page: 0 } );
		},
		/*
		================
		turnPage
		================
		*/
		turnPage( delta: number, pages: number ) {
			const kind = catalogOf( state.tab );
			if ( !kind ) return;
			browse( { page: Math.min( Math.max( 0, pages - 1 ), Math.max( 0, state.browse[kind].page + delta ) ) } );
		},
		/*
		================
		edit

		Stores a field's text; the query field also filters the catalogue.
		================
		*/
		edit( field: GmPanelField, value: string ) {
			state = { ...state, fields: { ...state.fields, [field]: value } };
			if ( field === "gm-panel-query" ) browse( { query: value, page: 0 } );
		},
		/*
		================
		select
		================
		*/
		select( codename: string ) {
			const kind = catalogOf( state.tab );
			if ( kind ) {
				state = {
					...state,
					browse: { ...state.browse, [kind]: { ...state.browse[kind], selected: codename } }
				};
			}
		},
		/*
		================
		scroll
		================
		*/
		scroll( delta: number, rows: number ) {
			const last = Math.max( 0, rows - GM_PANEL_VISIBLE_ROWS );
			state = { ...state, offset: Math.min( last, Math.max( 0, state.offset + delta ) ) };
		},
		/*
		================
		selectMonsterType
		================
		*/
		selectMonsterType( type: GmMonsterType ) {
			state = { ...state, monsterType: type };
		},
		/*
		================
		makeItem

		/MAKEITEM for the selected row: a plus level (0..12) for equipment, a
		count for a stackable item.
		================
		*/
		makeItem( row: GmCatalogRow | undefined ): string | undefined {
			const amount = digits( state.fields["gm-panel-amount"] );
			if ( !row || amount === undefined ) return undefined;
			const limit = row.stackable ? Math.min( row.maxStack, MAX_STACK_AMOUNT ) : MAX_PLUS;
			return `/MAKEITEM ${row.codename} ${Math.min( limit, row.stackable ? Math.max( 1, amount ) : amount )}`;
		},
		/*
		================
		loadMonster
		================
		*/
		loadMonster( row: GmCatalogRow | undefined ): string | undefined {
			const count = digits( state.fields["gm-panel-count"] );
			if ( !row || count === undefined || count < 1 ) return undefined;
			const type = state.monsterType ? " " + state.monsterType : "";
			return `/LOADMONSTER ${row.codename} ${Math.min( MAX_SPAWN_COUNT, count )}${type}`;
		},
		/*
		================
		named

		A command that takes the target character's name.
		================
		*/
		named( command: "/FINDUSER" | "/MOVETOUSER" | "/RECALLUSER" | "/TOTOWN" | "/BAN" ): string | undefined {
			const name = characterName( state.fields["gm-panel-target"] );
			return name ? `${command} ${name}` : undefined;
		},
		/*
		================
		silk
		================
		*/
		silk(): string | undefined {
			const name = characterName( state.fields["gm-panel-target"] ),
				amount = digits( state.fields["gm-panel-silk"] );
			if ( !name || amount === undefined || amount < 1 || amount > MAX_SILK ) return undefined;
			return `/SILK ${name} ${amount}`;
		},
		/*
		================
		speed

		/SETSPEED with the typed percent; reset sends the bare command.
		================
		*/
		speed( reset = false ): string | undefined {
			if ( reset ) return "/SETSPEED";
			const percent = digits( state.fields["gm-panel-speed"] );
			if ( percent === undefined || percent < MIN_SPEED || percent > MAX_SPEED ) return undefined;
			return `/SETSPEED ${percent}`;
		},
		/*
		================
		time
		================
		*/
		time(): string | undefined {
			const hour = digits( state.fields["gm-panel-hour"] );
			return hour !== undefined && hour < 24 ? `/SETTIME ${hour}` : undefined;
		}
	};
}
