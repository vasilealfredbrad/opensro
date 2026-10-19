/*
===========================================================================

devil-spirit-skills.test.mjs - the devil skills in the client's skill data

Port-only, not native (owner, 2026-10-18). The asset build adds the
Devil's Spirit's devil skills (rows of a later client in v1.150's own
skilldata layout, devilSpiritData.mjs) to skillData.json, and their names
to the SN_* text catalogue. The client's own decoder must admit those rows
as it admits every shipped one. Licensed: the published skillData.json
gives the projection's columns; without it the test skips.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import {
	devilSpiritSkillRows,
	devilSpiritTextEntries,
	loadDevilSpirit
} from "../../../../scripts/build/char/devilSpiritData.mjs";

const { decodeTooltipSkills } = await import( "../../src/engine/foundation/ui/skill-tooltip-catalog.ts" );
const ds = await import( "../../src/engine/foundation/gameplay/devil-spirit.ts" );
const SKILL_DATA = CLIENT_PUBLIC_ROOT + "/assets/data/skillData.json";

test("the client's decoder admits every devil skill row", t => {
	if ( !existsSync( SKILL_DATA ) ) {
		t.skip( "no published skillData.json" );
		return;
	}
	const source = JSON.parse( readFileSync( SKILL_DATA, "utf8" ) );
	const rows = devilSpiritSkillRows().map( row => source.columns.map( column => row[column] ).join( "\t" ) );
	const catalog = decodeTooltipSkills( { ...source, rows } );
	const data = loadDevilSpirit();
	assert.equal( catalog.size, data.skills.length );
	for ( const skill of data.skills ) {
		assert.ok( catalog.get( skill.id ), skill.codename );
		assert.equal( ds.isDevilSkill( skill.id ), true );
	}
});

test("the devil skills' names are in the text catalogue", () => {
	const entries = devilSpiritTextEntries();
	assert.equal( entries.SN_SKILL_MALL_NASRUN_ECCENTRICDEMON, "Malicious Devil Skill" );
	assert.equal( entries.SN_SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_TT_DESC, "able to ignore monster's defense" );
});
