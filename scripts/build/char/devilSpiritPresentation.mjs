/*
===========================================================================

devilSpiritPresentation.mjs - the devil skills' cast and devil form, from a
later client's import

Port-only, not native. The later client draws a devil skill (the
SKILL_MALL_NASRUN_* rows devilspirit.json carries) with textdata v1.150 has
no copy of; this module reads it from the owner's import (SRO_IMPORT_ROOT,
its UTF-16LE textdata) for the skills devilspirit.json names:

  - skillaniset (rows 2399..2407): the cast. AniGroup AVATAR_NASRUN1,
    ANI_READY01 then ANI_SHOT: nasrun1_<sex>_ready.ban (166 ms, the
    skill's PreparingTime) and nasrun1_<sex>_shot.ban (800 ms, its
    ActionDuration);
  - skilleffectset (rows 5397..5460): the cast's READY and SHOT stages
    (avatar_nasrun01_skill01_ready01 / ready02.efp, a camera shake, two
    sounds) and the devil form's ACT_S / ACT_L / DEACT stages, authored
    under the form's own code (charactervisualchange column 10,
    AVATAR_MALL_NASRUN_*): body01.efp on Bip01, keep01.efp with
    SCT_CHAR_SCALE 1.2 over 1000 ms, keep02.efp on both hands, the end
    sound;
  - charactervisualchange (rows 305..333): the form's look per skill, a
    body (mask 1850), horns (2561) and shoulders (516) at priority 60, and
    its motion set (avatar_nasrun2 / avatar_nasrun3, the column the angel
    wings fill with avatar_wing).

v1.150 joins stages to a skill by the skill's own code only, so the form's
stages are re-keyed onto the skill's code: a devil buff's attached effect
then plays them as any buff plays its ACT_* rows. The rows are rewritten
into v1.150's layout (no Service and ID columns) and vocabulary (below);
the asset build parses them with the v1.150 parsers. Without an import
every function returns nothing.

===========================================================================
*/
import { existsSync, readdirSync } from "node:fs";
import path from "node:path";
import { SKILL_EFFECT_ANIMATION_ID_BY_NAME } from "./native/skillEffectAnimationRegistry.ts";
import { readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { normalizeAssetPath } from "../shared/assetPaths.mjs";
import { importRoot } from "../world/paths.mjs";
import { loadDevilSpirit } from "./devilSpiritData.mjs";

// The import's textdata folder.
const TEXTDATA = [ "Media_extracted", "server_dep", "silkroad", "textdata" ];
// Both tables prepend Service and ID to v1.150's skillaniset2 /
// skilleffectset layout.
const LATER_LEADING_COLUMNS = 2;
// skillaniset (v1.150 layout, Service dropped): code, AniTRUE, AniGroup and
// the three phase animation columns.
const ANI_CODE = 1;
const ANI_TRUE = 3;
const ANI_GROUP = 5;
const ANI_PHASES = [ [ 6, "ready" ], [ 7, "wait" ], [ 8, "shot" ] ];
// skilleffectset (v1.150 layout): SkillEffectID and the sound columns.
const STAGE_CODE = 1;
const STAGE_SOUNDS = [ 26, 27 ];
// charactervisualchange: Service, the skill and form codes, mask, priority,
// motion set and the man's and woman's resources.
const LOOK_SERVICE = 0;
const LOOK_SKILL = 9;
const LOOK_FORM = 10;
const LOOK_MASK = 13;
const LOOK_PRIORITY = 16;
const LOOK_MOTION = 18;
const LOOK_MAN = 19;
const LOOK_WOMAN = 20;
const NONE = "xxx";
// skilldata: the base (animation set) name column.
const SKILL_BASE_COLUMN = "5";

// The later client's ANI_SHOT is not in v1.150's data_ccd620 animation
// registry, which drops unknown names (91DEF3). The port names it
// ANI_SKILL_1: the state id it is published under in the cast's own
// animation set (devilSpiritMotionSets), so no v1.150 set is touched.
const PORT_ANIMATION_NAMES = new Map( [ [ "ANI_SHOT", "ANI_SKILL_1" ] ] );
// The cast's animation set has no v1.150 global string (BBDB70). Its key is
// the next slot after the thirteen native ones (0xCCCCC0 + 13 * 0x1C); the
// client's skill-motion.ts names the same slot.
export const DEVIL_CAST_ANIMATION_SET_KEY = 0x00ccce2c;
const DEVIL_CAST_ANIMATION_SET = "avatar_nasrun1";
// INFERENCE: the form's motion set binds the import's stand and run clips
// to states 0 and 7, as v1.150's avatar_wing set binds man_avatar_fly.ban
// to run (7). The later client's character BSRs that bind them are not in
// the import; its file names (nasrun1_<sex>_stand / _run) are the evidence.
const FORM_STATES = [ [ 0, "stand" ], [ 7, "run" ] ];
const MOTION_DIRECTORY = "prim/ani/avatar";

/*
================
rows

The enabled rows of the textdata shards whose lower-case name matches.
================
*/
function rows( dir, pattern ) {
	return readdirSync( dir ).filter( name => pattern.test( name.toLowerCase() ) ).sort().flatMap( name =>
		readTextDataRowsSync( path.join( dir, name ) )
	).filter( row => row[0] === "1" );
}

/*
================
lookOf

One skill's devil form: the OR of its rows' masks (one priority for all),
its motion set, its form code (the body row's) and its resources, the body
first. Refuses rows that disagree.
================
*/
function lookOf( code, lookRows ) {
	const own = lookRows.filter( row => row[LOOK_SKILL] === code ).sort( ( a, b ) =>
		Number( b[LOOK_FORM] !== NONE ) - Number( a[LOOK_FORM] !== NONE )
	);
	if ( !own.length ) return null;
	const priorities = new Set( own.map( row => Number( row[LOOK_PRIORITY] ) ) ),
		motions = new Set( own.map( row => row[LOOK_MOTION] ) ),
		forms = own.filter( row => row[LOOK_FORM] !== NONE );
	if ( priorities.size !== 1 || motions.size !== 1 || forms.length !== 1 ) {
		throw Error( `Devil's Spirit: ${code} has no single devil form` );
	}
	const resource = column => own.map( row => normalizeAssetPath( row[column] ) );
	return {
		code,
		form: forms[0][LOOK_FORM],
		mask: own.reduce( ( mask, row ) => mask | Number( row[LOOK_MASK] ), 0 ),
		priority: [ ...priorities ][0],
		motion: [ ...motions ][0].toLowerCase(),
		models: { M: resource( LOOK_MAN ), W: resource( LOOK_WOMAN ) }
	};
}

/*
================
aniSetRow

A later skillaniset row in v1.150's layout and vocabulary: the AniTRUE
flag the later client writes 0 / 1 is FALSE / TRUE (91DAE0's boolean
lookup), and the animation names v1.150 lacks take their port names.
================
*/
function aniSetRow( row ) {
	const c = row.slice( LATER_LEADING_COLUMNS );
	c[ANI_TRUE] = c[ANI_TRUE] === "0" ? "FALSE" : c[ANI_TRUE] === "1" ? "TRUE" : c[ANI_TRUE];
	for ( const [column] of ANI_PHASES ) {
		c[column] = c[column].split( "," ).map( name => PORT_ANIMATION_NAMES.get( name ) ?? name ).join( "," );
	}
	return c;
}

/*
================
loadDevilSpiritPresentation

The rows of every devil skill with an animation set, keyed in v1.150's
layout, and the devil form of every skill that has one (looks, by skill
id). Null without an import's textdata.
================
*/
export function loadDevilSpiritPresentation( data = loadDevilSpirit(), root = importRoot ) {
	const dir = root ? path.join( root, ...TEXTDATA ) : null;
	if ( !dir || !existsSync( dir ) ) return null;
	const aniRows = new Map(
		rows( dir, /^skillaniset_\d+\.txt$/ ).map( row => [ row[LATER_LEADING_COLUMNS + ANI_CODE], row ] )
	);
	const stageRows = rows( dir, /^skilleffectset_\d+\.txt$/ ),
		lookRows = rows( dir, /^charactervisualchange_\d+\.txt$/ );
	const presentation = {
		root,
		looks: new Map(),
		aniSetRows: [],
		effectSetRows: [],
		skillRows: []
	};
	const stagesOf = ( code, owner ) =>
		stageRows.filter( row => row[LATER_LEADING_COLUMNS + STAGE_CODE] === code ).map( row => {
			const c = row.slice( LATER_LEADING_COLUMNS );
			c[STAGE_CODE] = owner;
			return c;
		} );
	for ( const skill of data.skills ) {
		const ani = aniRows.get( skill.codename );
		if ( !ani ) continue;
		presentation.aniSetRows.push( aniSetRow( ani ) );
		presentation.skillRows.push( {
			id: skill.id,
			levelName: skill.codename,
			animBaseName: skill.columns[SKILL_BASE_COLUMN] ?? "",
			originalId: 0
		} );
		presentation.effectSetRows.push( ...stagesOf( skill.codename, skill.codename ) );
		const look = lookOf( skill.codename, lookRows );
		if ( !look ) continue;
		presentation.looks.set( skill.id, look );
		presentation.effectSetRows.push( ...stagesOf( look.form, skill.codename ) );
	}
	return presentation;
}

/*
================
devilSpiritEffectSources

The rows buildEffectRecordTable adds (parseSkillEffect.mjs): the skills'
animation sets and stages, their skilldata joins, the cast set's key and
the import's sound root. Null without a presentation.
================
*/
export function devilSpiritEffectSources( presentation ) {
	if ( !presentation ) return null;
	return {
		aniSetRows: presentation.aniSetRows,
		effectSetRows: presentation.effectSetRows,
		skillRows: presentation.skillRows,
		animationSetKeys: new Map( [ [ DEVIL_CAST_ANIMATION_SET, DEVIL_CAST_ANIMATION_SET_KEY ] ] ),
		soundRoot: path.join( presentation.root, "Data_extracted", "prim", "snd" )
	};
}

/*
================
devilSpiritSounds

Every stage sound the import holds, as { gamePath, source } (prim/snd/...).
================
*/
export function devilSpiritSounds( presentation ) {
	if ( !presentation ) return [];
	const soundRoot = path.join( presentation.root, "Data_extracted" ), out = new Map();
	for ( const row of presentation.effectSetRows ) {
		for ( const column of STAGE_SOUNDS ) {
			const value = row[column]?.trim();
			if ( !value || value.toLowerCase() === "none" ) continue;
			const gamePath = normalizeAssetPath( `prim/snd/${value}` ),
				source = path.join( soundRoot, ...gamePath.split( "/" ) );
			if ( existsSync( source ) ) out.set( gamePath, source );
		}
	}
	return [ ...out ].map( ( [gamePath, source] ) => ({ gamePath, source }) );
}

/*
================
devilSpiritMotionSets

A player body's devil motions, { set: { stateId: game path } }: the cast's
set (its rows' animation names, ready and shot clips) and each form's set
(stand and run). A clip the import lacks is left out; a body that is no
player (CHAR_<CH|EU>_<MAN|WOMAN>_) gets none.
================
*/
export function devilSpiritMotionSets( presentation, codename ) {
	const body = /^CHAR_(?:CH|EU)_(MAN|WOMAN)_/.exec( codename );
	if ( !presentation || !body ) return {};
	const stem = DEVIL_CAST_ANIMATION_SET.replace( /^avatar_/, "" ), sex = body[1].toLowerCase();
	const clip = name => {
		const gamePath = `${MOTION_DIRECTORY}/${stem}_${sex}_${name}.ban`;
		return existsSync( path.join( presentation.root, "Data_extracted", ...gamePath.split( "/" ) ) ) ?
			gamePath :
			null;
	};
	const sets = {};
	const add = ( set, stateId, name ) => {
		const gamePath = clip( name );
		if ( stateId === undefined || !gamePath ) return;
		(sets[set] ??= {})[stateId] = gamePath;
	};
	for ( const row of presentation.aniSetRows ) {
		if ( row[ANI_GROUP].toLowerCase() !== DEVIL_CAST_ANIMATION_SET ) continue;
		for ( const [column, name] of ANI_PHASES ) {
			for ( const token of row[column].split( "," ) ) {
				add( DEVIL_CAST_ANIMATION_SET, SKILL_EFFECT_ANIMATION_ID_BY_NAME.get( token ), name );
			}
		}
	}
	for ( const look of presentation.looks.values() ) {
		for ( const [stateId, name] of FORM_STATES ) add( look.motion, stateId, name );
	}
	return sets;
}
