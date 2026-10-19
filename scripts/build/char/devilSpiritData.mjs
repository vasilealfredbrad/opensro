/*
===========================================================================

devilSpiritData.mjs - the Devil's Spirit rows the asset build reads

Port-only, not native. Reads and validates devilspirit.json, the one data
file the server (game/item/devilspirit) and the asset build share, and
maps its rows onto equipment records and icon references. No file I/O
beyond that table, so light owners (the interface images) can import it.
The models are built by devilSpirit.mjs.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";
import { rebuildRoot } from "../world/paths.mjs";
import { normalizeAssetPath } from "../shared/assetPaths.mjs";

export const DEVIL_SPIRIT_DATA = path.join(
	rebuildRoot,
	"apps",
	"server",
	"internal",
	"game",
	"item",
	"devilspirit",
	"devilspirit.json"
);

const DATA_FORMAT = "opensro-devil-spirit-v1";
export const ATTACHMENT_TYPE = "3.1.13.3";
// avatarSlot is TID4 - 1 (buildEquipmentVisuals.mjs).
export const ATTACHMENT_AVATAR_SLOT = 2;
const RACES = [ "CH", "EU" ];
// skilldata: its width and the name, icon and text symbol columns.
const SKILL_COLUMNS = 118;
const SKILL_NAME_COLUMN = 4;
const SKILL_ICON_COLUMN = 61;
const SKILL_NAME_SYMBOL_COLUMN = 62;
const SKILL_DESCRIPTION_SYMBOL_COLUMN = 64;
const SEXES = new Set( [ "M", "W" ] );

/*
================
loadDevilSpirit

The validated data file. A bad row is a build defect.
================
*/
export function loadDevilSpirit( file = DEVIL_SPIRIT_DATA ) {
	const data = JSON.parse( readFileSync( file, "utf8" ) );
	if ( data.format !== DATA_FORMAT ) throw Error( `Devil's Spirit: format ${data.format}` );
	if ( data.portType?.join( "." ) !== ATTACHMENT_TYPE ) throw Error( "Devil's Spirit: not an avatar attachment" );
	const ids = new Set();
	for ( const item of data.items ?? [] ) {
		if ( !Number.isSafeInteger( item.id ) || ids.has( item.id ) ) throw Error( `Devil's Spirit: id ${item.id}` );
		if ( !SEXES.has( item.sex ) || !data.templates?.[item.sex] ) {
			throw Error( `Devil's Spirit: ${item.codename} sex` );
		}
		if ( !item.models?.length || item.models.some( model => !/^res\/[a-z0-9_/]+\.bsr$/.test( model ) ) ) {
			throw Error( `Devil's Spirit: ${item.codename} models` );
		}
		if ( !/\.ddj$/i.test( item.icon ?? "" ) ) throw Error( `Devil's Spirit: ${item.codename} icon` );
		ids.add( item.id );
	}
	if ( !ids.size ) throw Error( "Devil's Spirit: no rows" );
	return data;
}

/*
================
devilSpiritIconReferences

The icon paths the interface images publish: the rows'
(icon/item/avatar/...ddj), the scroll's and extension's
(icon/item/etc/...ddj) and the devil skills' (icon/skill/etc/...ddj).
================
*/
export function devilSpiritIconReferences( data = loadDevilSpirit() ) {
	const icons = [
		...[ ...data.items, ...(data.etc ?? []) ].map( row => row.icon ),
		...devilSpiritSkillRows( data ).map( row => row[SKILL_ICON_COLUMN] )
	];
	return [ ...new Set( icons.map( icon => `icon/${normalizeAssetPath( icon )}` ) ) ].sort();
}

/*
================
devilSpiritSkillRows

The devil skills as skilldata rows (game/item/devilspirit/skills.go
SkillRows, the same rule): the 118 columns, "0" where the data names
none, the codename in the (Korean) name column.
================
*/
export function devilSpiritSkillRows( data = loadDevilSpirit() ) {
	return (data.skills ?? []).map( skill => {
		const row = Array.from( { length: SKILL_COLUMNS }, () => "0" );
		for ( const [key, value] of Object.entries( skill.columns ) ) {
			const column = Number( key );
			if ( !Number.isInteger( column ) || column < 0 || column >= SKILL_COLUMNS ) {
				throw Error( `Devil's Spirit: ${skill.codename} column ${key}` );
			}
			row[column] = value;
		}
		row[SKILL_NAME_COLUMN] = skill.codename;
		if ( row[1] !== String( skill.id ) || row[3] !== skill.codename ) {
			throw Error( `Devil's Spirit: ${skill.codename} columns name another skill` );
		}
		return row;
	} );
}

/*
================
devilSpiritTextEntries

The devil skills' name and description symbols (skilldata columns 62 and
64) with the later client's English text, and the scroll's and
extension's descriptions, for the client's SN_* text catalogue
(textdataname.en.json), which v1.150's textdata does not fill.
================
*/
export function devilSpiritTextEntries( data = loadDevilSpirit() ) {
	const entries = {};
	for ( const skill of data.skills ?? [] ) {
		entries[skill.columns[SKILL_NAME_SYMBOL_COLUMN]] = skill.name;
		entries[skill.columns[SKILL_DESCRIPTION_SYMBOL_COLUMN]] = skill.description;
	}
	// The scroll's and extension's tooltip text (devilspirit.go
	// EtcDescriptionSymbol).
	for ( const etc of data.etc ?? [] ) entries[`SN_${etc.codename}_TT_DESC`] = etc.description;
	return entries;
}

/*
================
itemLooks

The devil forms (devilSpiritPresentation.mjs looks, by skill id) of the
skills a row grants at any level, in skill id order.
================
*/
export function itemLooks( item, looks ) {
	const skills = new Set( item.levels.flatMap( level => level.skills ) );
	return [ ...(looks ?? new Map()) ].filter( ( [id] ) => skills.has( id ) ).sort( ( a, b ) => a[0] - b[0] );
}

/*
================
devilSpiritRecords

One equipment record per row, keyed by id, every body of the row's sex
null until a model is built (publishDevilSpiritEquipment). template(code)
returns the template row's published record, whose mask and priority the
row wears. With the import's devil forms (looks, by skill id) the row
wears theirs instead (charactervisualchange: one mask and priority for
every form) and has a body per form, looks[skill id][body], also null
until built.
================
*/
export function devilSpiritRecords( data, template, looks = null ) {
	const records = {};
	for ( const item of data.items ) {
		const base = template( data.templates[item.sex] );
		if ( !base || base.avatarSlot !== ATTACHMENT_AVATAR_SLOT ) {
			throw Error( `Devil's Spirit: template ${data.templates[item.sex]} is no avatar attachment` );
		}
		const forms = itemLooks( item, looks ),
			bodies = () => Object.fromEntries( RACES.map( race => [ `${race}_${item.sex}`, null ] ) );
		const visuals = new Set( forms.map( ( [, look] ) => `${look.mask}:${look.priority}` ) );
		if ( visuals.size > 1 ) throw Error( `Devil's Spirit: ${item.codename} forms disagree on their mask` );
		records[item.id] = {
			code: item.codename,
			slot: null,
			armorClass: 0,
			thiefSuit: false,
			visualMask: forms.length ? forms[0][1].mask : base.visualMask,
			visualPriority: forms.length ? forms[0][1].priority : base.visualPriority,
			source: null,
			model: item.models[0],
			avatarSlot: ATTACHMENT_AVATAR_SLOT,
			bodies: bodies(),
			...(forms.length ? { looks: Object.fromEntries( forms.map( ( [id] ) => [ id, bodies() ] ) ) } : {})
		};
	}
	return records;
}
