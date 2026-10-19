/*
===========================================================================

devilSpiritImport.mjs - devilspirit.json against the later client's data

Port-only, not native. devilspirit.json carries values of a later client
the v1.150 data does not have; this helper reads them again from the
owner's import (SRO_IMPORT_ROOT, its UTF-16LE textdata) so the table
cannot drift from its source:

  - every row's eleven levels: _RefAbilityByItemOptLevel joins
    (RefItemID, OptLevel) to a link whose _RefMagicOptByItemOptLevel rows
    give MATTR_NASRUN_HPNA / _MPNA / _BLOCKRATE and whose
    _RefSkillByItemOptLevel rows give the skills;
  - the four MATTR_NASRUN_* magicoption rows (id, tag, range words);
  - AVATAR_SPECIAL_OPTLV_RATIO_00..10 (refshardcontentconfig.txt);
  - every devil skill row, column for column (but the Korean name);
  - the rows' Param1 (28 days) and the scroll's and extension's ids,
    codenames and Param1.

===========================================================================
*/
import { existsSync, readdirSync } from "node:fs";
import path from "node:path";
import assert from "node:assert/strict";
import { readTextDataRowsSync } from "../../build/shared/textDataIo.mjs";
import { devilSpiritSkillRows } from "../../build/char/devilSpiritData.mjs";

// The import's textdata folder.
const TEXTDATA = [ "Media_extracted", "server_dep", "silkroad", "textdata" ];
// itemdata columns: id, codename, Param1.
const ITEM_ID = 1;
const ITEM_CODENAME = 2;
const ITEM_PARAM1 = 118;
// magicoption columns: id, codename, tag, range words.
const OPTION_ID = 1;
const OPTION_CODENAME = 2;
const OPTION_TAG = 7;
const OPTION_WORDS = 8;
const OPTION_WORD_COUNT = 3;
// skilldata: the (Korean) name column the data leaves out.
const SKILL_NAME_COLUMN = 4;

/*
================
rows

Every row of the textdata files whose lower-case name matches pattern.
================
*/
function rows( dir, pattern ) {
	return readdirSync( dir ).filter( name => pattern.test( name.toLowerCase() ) ).flatMap( name =>
		readTextDataRowsSync( path.join( dir, name ) )
	);
}

/*
================
packTag
================
*/
function packTag( tag ) {
	return [ ...tag ].reduce( ( packed, letter ) => packed * 256 + letter.charCodeAt( 0 ), 0 );
}

/*
================
checkDevilSpiritAgainstImport

Asserts every value; returns how many rows it checked, or null when the
import has no textdata.
================
*/
export function checkDevilSpiritAgainstImport( data, importRoot ) {
	const dir = path.join( importRoot, ...TEXTDATA );
	if ( !existsSync( dir ) ) return null;
	let checked = 0;

	const options = new Map(
		rows( dir, /^magicoption(_\d+)?\.txt$/ ).map( row => [ Number( row[OPTION_ID] ), row ] )
	);
	for ( const option of data.magicOptions ) {
		const row = options.get( option.id );
		assert.ok( row, `magicoption ${option.id}` );
		assert.equal( row[OPTION_CODENAME], option.codename );
		assert.equal( Number( row[OPTION_TAG] ), packTag( option.tag ) );
		assert.deepEqual(
			row.slice( OPTION_WORDS, OPTION_WORDS + OPTION_WORD_COUNT ).map( Number ),
			option.rangeWords,
			option.codename
		);
		checked++;
	}
	const optionByID = new Map( data.magicOptions.map( option => [ option.id, option.tag ] ) );

	const abilities = rows( dir, /^refabilitybyitemoptlevel/ );
	const magic = rows( dir, /^refmagicoptbyitemoptlevel/ );
	const skills = rows( dir, /^refskillbyitemoptlevel/ );
	for ( const item of data.items ) {
		const levels = abilities.filter( row => Number( row[2] ) === item.id ).sort( ( a, b ) => a[3] - b[3] );
		assert.deepEqual(
			levels.map( row => Number( row[3] ) ),
			item.levels.map( ( _, level ) => level ),
			item.codename
		);
		levels.forEach( ( ability, level ) => {
			const link = ability[1], values = { hpna: 0, mpna: 0, brna: 0 };
			for ( const row of magic.filter( row => row[0] === link ) ) {
				values[optionByID.get( Number( row[1] ) )] = Number( row[2] );
			}
			const granted = skills.filter( row => row[0] === link ).map( row => Number( row[1] ) ).sort( ( a, b ) =>
				a - b
			);
			assert.deepEqual(
				{ hp: values.hpna, mp: values.mpna, block: values.brna, skills: granted },
				{ ...item.levels[level], skills: [ ...item.levels[level].skills ].sort( ( a, b ) => a - b ) },
				`${item.codename} level ${level}`
			);
			checked++;
		} );
	}

	const ratios = new Map(
		rows( dir, /^refshardcontentconfig\.txt$/ ).map( row => [ row[2], Number( row[4] ) ] )
	);
	data.upgradeRatios.forEach( ( ratio, level ) => {
		assert.equal( ratios.get( `AVATAR_SPECIAL_OPTLV_RATIO_${String( level ).padStart( 2, "0" )}` ), ratio );
		checked++;
	} );

	const skillRows = new Map(
		rows( dir, /^skilldata_\d+\.txt$/ ).filter( row => row[0] === "1" ).map( row => [ Number( row[1] ), row ] )
	);
	devilSpiritSkillRows( data ).forEach( ( row, index ) => {
		const skill = data.skills[index], source = skillRows.get( skill.id );
		assert.ok( source, `skilldata ${skill.id}` );
		assert.deepEqual(
			row.filter( ( _, column ) => column !== SKILL_NAME_COLUMN ),
			source.filter( ( _, column ) => column !== SKILL_NAME_COLUMN ),
			skill.codename
		);
		checked++;
	} );

	const items = new Map( rows( dir, /^itemdata(_\d+)?\.txt$/ ).map( row => [ Number( row[ITEM_ID] ), row ] ) );
	for ( const item of data.items ) {
		const row = items.get( item.id );
		assert.equal( row?.[ITEM_CODENAME], item.codename );
		assert.equal( Number( row[ITEM_PARAM1] ), data.awakeSeconds, item.codename );
		checked++;
	}
	for ( const etc of data.etc ) {
		const row = items.get( etc.id );
		assert.equal( row?.[ITEM_CODENAME], etc.codename );
		assert.equal( Number( row[ITEM_PARAM1] ), etc.seconds, etc.codename );
		checked++;
	}
	return checked;
}
