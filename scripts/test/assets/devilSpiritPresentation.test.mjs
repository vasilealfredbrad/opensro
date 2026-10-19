/*
===========================================================================

devilSpiritPresentation.test.mjs - the devil skills' cast and devil form rows

Port-only, not native. devilSpiritPresentation.mjs reads a devil skill's
skillaniset, skilleffectset and charactervisualchange rows from the
owner's import and rewrites them into v1.150's layout: the cast keeps its
own stages, the devil form's ACT_* stages are re-keyed onto the skill,
ANI_SHOT takes its port name, and the form's rows give one look (the OR
of their masks, the body first) and a motion set. The effect record table
then joins them as v1.150 rows, under the cast set's port key, and the
equipment records wear the forms' mask. A fixture import stands in for
the later client; the licensed case reads the owner's import.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
	DEVIL_CAST_ANIMATION_SET_KEY,
	devilSpiritEffectSources,
	devilSpiritMotionSets,
	devilSpiritSounds,
	loadDevilSpiritPresentation
} from "../../build/char/devilSpiritPresentation.mjs";
import { ATTACHMENT_AVATAR_SLOT, devilSpiritRecords, loadDevilSpirit } from "../../build/char/devilSpiritData.mjs";
import { buildEffectRecordTable } from "../../build/char/parseSkillEffect.mjs";
import { resolveImportRoot } from "../../build/world/paths.mjs";

const SKILL = "SKILL_MALL_NASRUN_DESTROYTHEDEVIL_01";
const FORM = "AVATAR_MALL_NASRUN_DESTROYTHEDEVIL_01";
const SKILL_ID = 31148;
const TEXTDATA = [ "Media_extracted", "server_dep", "silkroad", "textdata" ];
// skilleffectset: one stage of the later layout (Service, ID, then v1.150's 28).
const STAGE_TAIL = [
	"0",
	"FALSE",
	"none",
	"CHAR_BASE",
	"0",
	"0",
	"0",
	"0",
	"1",
	"0,0",
	"AT_ONE_FOLLOW",
	"MOV_NONE,0,0,0",
	"0,0,0",
	"false,0,0,0,0,false",
	"system\\"
];

/*
================
stage

One later skilleffectset row: phase, effect, script and begin sound.
================
*/
function stage( code, phase, efp, script = "none", sound = "none" ) {
	return [
		"1",
		"5413",
		"name",
		code,
		phase,
		...STAGE_TAIL,
		efp,
		"none",
		"0,0,0",
		"none",
		"0,0,0",
		"none",
		"0",
		script,
		sound,
		"none"
	];
}

/*
================
look

One later charactervisualchange row of the Vallainous form.
================
*/
function look( form, mask, man, woman ) {
	return [
		"1",
		"327",
		"name",
		"3",
		"1",
		"0",
		"3",
		"0",
		"0",
		SKILL,
		form,
		"xxx",
		"xxx",
		String( mask ),
		"512",
		"2",
		"60",
		"0",
		"avatar_nasrun3",
		man,
		woman
	];
}

/*
================
writeRows

A UTF-16LE textdata file, as the later client ships it.
================
*/
function writeRows( file, rows ) {
	mkdirSync( path.dirname( file ), { recursive: true } );
	writeFileSync( file, Buffer.from( "﻿" + rows.map( row => row.join( "\t" ) ).join( "\r\n" ), "utf16le" ) );
}

/*
================
fixtureImport

An import holding the Vallainous Devil Skill's rows, the man's cast and
form clips and its sounds. Returns its root.
================
*/
function fixtureImport() {
	const root = mkdtempSync( path.join( tmpdir(), "devil-import-" ) ), text = path.join( root, ...TEXTDATA );
	writeRows( path.join( text, "skillaniset_2500.txt" ), [
		[
			"1",
			"2401",
			"name",
			SKILL,
			"0",
			"0",
			"0",
			"AVATAR_NASRUN1",
			"ANI_READY01",
			"none",
			"ANI_SHOT",
			...Array( 5 ).fill( "none" ),
			"0",
			"0,0,0,0",
			"ONE",
			...Array( 7 ).fill( "none" ),
			"0",
			"1"
		]
	] );
	writeRows( path.join( text, "skilleffectset_5500.txt" ), [
		stage(
			SKILL,
			"READY",
			"avatar_nasrun01_skill01_ready01.efp",
			"none",
			"Avatar\\Avatar_Nasrun1_Skill1_Ready1.wav"
		),
		stage( SKILL, "SHOT", "avatar_nasrun01_skill01_ready02.efp", "SCT_SHAKECAM1" ),
		stage( FORM, "ACT_S", "avatar_nasrun01_skill01_body01.efp" ),
		stage( FORM, "ACT_L", "avatar_nasrun01_skill01_keep01.efp", "SCT_CHAR_SCALE,1.2,1000" ),
		stage( FORM, "DEACT", "none", "none", "Avatar\\Avatar_Nasrun1_Skill1_End1.wav" ),
		stage( "SKILL_OTHER_01", "SHOT", "other.efp" )
	] );
	writeRows( path.join( text, "charactervisualchange_400.txt" ), [
		look( "xxx", 2561, "res\\item\\avatar\\avatar_m_nasrun_hat.bsr", "res\\item\\avatar\\avatar_w_nasrun_hat.bsr" ),
		look( "xxx", 516, "res\\item\\avatar\\avatar_m_nasrun_3.bsr", "res\\item\\avatar\\avatar_w_nasrun_3.bsr" ),
		look( FORM, 1850, "res\\item\\avatar\\avatar_m_nasrun.bsr", "res\\item\\avatar\\avatar_w_nasrun.bsr" )
	] );
	for ( const clip of [ "ready", "shot", "stand", "run" ] ) {
		const file = path.join( root, "Data_extracted", "prim", "ani", "avatar", `nasrun1_man_${clip}.ban` );
		mkdirSync( path.dirname( file ), { recursive: true } );
		writeFileSync( file, "" );
	}
	for ( const sound of [ "avatar_nasrun1_skill1_ready1.wav", "avatar_nasrun1_skill1_end1.wav" ] ) {
		const file = path.join( root, "Data_extracted", "prim", "snd", "avatar", sound );
		mkdirSync( path.dirname( file ), { recursive: true } );
		writeFileSync( file, "" );
	}
	return root;
}

test("the import's rows become v1.150 rows: the cast, the re-keyed form and its look", t => {
	const root = fixtureImport();
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const presentation = loadDevilSpiritPresentation( loadDevilSpirit(), root );
	assert.equal( presentation.aniSetRows.length, 1 );
	const ani = presentation.aniSetRows[0];
	assert.equal( ani[1], SKILL );
	assert.equal( ani[3], "FALSE" );
	assert.equal( ani[5], "AVATAR_NASRUN1" );
	assert.deepEqual( [ ani[6], ani[8] ], [ "ANI_READY01", "ANI_SKILL_1" ] );
	assert.deepEqual(
		presentation.effectSetRows.map( row => `${row[1]}/${row[2]}` ),
		[ SKILL + "/READY", SKILL + "/SHOT", SKILL + "/ACT_S", SKILL + "/ACT_L", SKILL + "/DEACT" ]
	);
	assert.ok( presentation.effectSetRows.every( row => row.length === 28 ) );
	assert.deepEqual( presentation.skillRows, [ {
		id: SKILL_ID,
		levelName: SKILL,
		animBaseName: "SKILL_MALL_NASRUN_DESTROYTHEDEVIL",
		originalId: 0
	} ] );
	assert.deepEqual( presentation.looks.get( SKILL_ID ), {
		code: SKILL,
		form: FORM,
		mask: 1850 | 2561 | 516,
		priority: 60,
		motion: "avatar_nasrun3",
		models: {
			M: [
				"res/item/avatar/avatar_m_nasrun.bsr",
				"res/item/avatar/avatar_m_nasrun_hat.bsr",
				"res/item/avatar/avatar_m_nasrun_3.bsr"
			],
			W: [
				"res/item/avatar/avatar_w_nasrun.bsr",
				"res/item/avatar/avatar_w_nasrun_hat.bsr",
				"res/item/avatar/avatar_w_nasrun_3.bsr"
			]
		}
	} );
	// The man's clips: the cast's ready (ANI_READY01, 40) and shot (26), the
	// form's stand and run. A woman's clips are absent here, a monster gets none.
	assert.deepEqual( devilSpiritMotionSets( presentation, "CHAR_EU_MAN_ADVENTURER" ), {
		avatar_nasrun1: { 40: "prim/ani/avatar/nasrun1_man_ready.ban", 26: "prim/ani/avatar/nasrun1_man_shot.ban" },
		avatar_nasrun3: { 0: "prim/ani/avatar/nasrun1_man_stand.ban", 7: "prim/ani/avatar/nasrun1_man_run.ban" }
	} );
	assert.deepEqual( devilSpiritMotionSets( presentation, "CHAR_CH_WOMAN_ADVENTURER" ), {} );
	assert.deepEqual( devilSpiritMotionSets( presentation, "MOB_CH_TIGERWOMAN" ), {} );
	assert.deepEqual(
		devilSpiritSounds( presentation ).map( sound => sound.gamePath ),
		[ "prim/snd/avatar/avatar_nasrun1_skill1_ready1.wav", "prim/snd/avatar/avatar_nasrun1_skill1_end1.wav" ]
	);
});

test("the effect record table joins the rows under the cast set's port key", t => {
	const root = fixtureImport(), empty = mkdtempSync( path.join( tmpdir(), "devil-textdata-" ) );
	t.after( () => {
		rmSync( root, { recursive: true, force: true } );
		rmSync( empty, { recursive: true, force: true } );
	} );
	const skillEffect = path.join( empty, "skilleffect.txt" );
	writeFileSync( skillEffect, Buffer.from( "﻿", "utf16le" ) );
	const sources = devilSpiritEffectSources( loadDevilSpiritPresentation( loadDevilSpirit(), root ) );
	const { table, namedTable } = buildEffectRecordTable( empty, skillEffect, null, null, sources );
	const record = table[SKILL_ID];
	assert.ok( record && namedTable[SKILL] );
	assert.equal( record.animSlotKey, DEVIL_CAST_ANIMATION_SET_KEY );
	assert.deepEqual( [ record.animTable0, record.animTable2 ], [ [ "ANI_READY01" ], [ "ANI_SKILL_1" ] ] );
	assert.equal( record.overlap01, false );
	assert.deepEqual(
		record.authoredStages.map( row => row.animationPhase ),
		[ "READY", "SHOT", "ACT_S", "ACT_L", "DEACT" ]
	);
	assert.equal(
		record.authoredStages[0].soundBeginPublicPath,
		"/assets/audio/sfx/prim/snd/avatar/avatar_nasrun1_skill1_ready1.wav"
	);
	assert.equal( record.authoredStages[3].objectResourcePath, "system/avatar_nasrun01_skill01_keep01.efp" );
	// Without the import's rows the table has no devil record.
	assert.equal( buildEffectRecordTable( empty, skillEffect ).table[SKILL_ID], undefined );
	assert.equal( devilSpiritEffectSources( null ), null );
});

test("the equipment records wear the forms' mask and carry a body per form", t => {
	const root = fixtureImport();
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	const data = loadDevilSpirit(), presentation = loadDevilSpiritPresentation( data, root );
	const template = code => ({ code, visualMask: 1280, visualPriority: 90, avatarSlot: ATTACHMENT_AVATAR_SLOT });
	const records = devilSpiritRecords( data, template, presentation.looks );
	for ( const item of data.items ) {
		const record = records[item.id], grants = item.levels.some( level => level.skills.includes( SKILL_ID ) );
		assert.equal( record.visualMask, grants ? 1850 | 2561 | 516 : 1280, item.codename );
		assert.equal( record.visualPriority, grants ? 60 : 90, item.codename );
		assert.deepEqual(
			record.looks,
			grants ? { [SKILL_ID]: { [`CH_${item.sex}`]: null, [`EU_${item.sex}`]: null } } : undefined,
			item.codename
		);
	}
	// No import, no forms: the template's mask.
	assert.equal( devilSpiritRecords( data, template )[data.items[0].id].looks, undefined );
	assert.equal( loadDevilSpiritPresentation( data, null ), null );
});

test("every devil buff has a form, cast and motions in the import (licensed: SRO_IMPORT_ROOT)", t => {
	const root = resolveImportRoot();
	const presentation = root ? loadDevilSpiritPresentation( loadDevilSpirit(), root ) : null;
	if ( !presentation ) {
		t.skip( "SRO_IMPORT_ROOT holds no textdata" );
		return;
	}
	const buffs = loadDevilSpirit().skills.filter( skill => !skill.codename.includes( "MOBDEFENCEIGNORE" ) );
	for ( const skill of buffs ) {
		const form = presentation.looks.get( skill.id );
		assert.ok( form, skill.codename );
		assert.equal( form.mask, 1850 | 2561 | 516, skill.codename );
		assert.equal( form.priority, 60, skill.codename );
		const phases = presentation.effectSetRows.filter( row => row[1] === skill.codename ).map( row => row[2] );
		for ( const phase of [ "READY", "SHOT", "ACT_S", "ACT_L", "DEACT" ] ) {
			assert.ok( phases.includes( phase ), `${skill.codename} ${phase}` );
		}
	}
	for ( const body of [ "CHAR_CH_MAN_ADVENTURER", "CHAR_EU_WOMAN_ADVENTURER" ] ) {
		const sets = devilSpiritMotionSets( presentation, body );
		assert.deepEqual( Object.keys( sets.avatar_nasrun1 ).sort(), [ "26", "40" ], body );
		assert.deepEqual( Object.keys( sets.avatar_nasrun2 ).sort(), [ "0", "7" ], body );
	}
});
