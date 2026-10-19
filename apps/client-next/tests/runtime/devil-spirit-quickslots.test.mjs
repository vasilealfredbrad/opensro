/*
===========================================================================

devil-spirit-quickslots.test.mjs - where a learned devil buff is put, and
the skill window's Devil's Spirit tab

Port-only, not native (owner, 2026-10-19). A devil buff the server teaches
goes to the first free slot of the extension bar (slots 41..50), never to
the F1..F4 pages, and a notice names the slot; an older stage's shortcut
is replaced in place and a shortcut holding another skill is never taken.
A collapsed extension bar opens when a devil buff lands on it. The skill
window shows a Devil's Spirit tab at the right end of its tab strip while
a Devil's Spirit is worn or a devil skill is known: a hint before the
skills arrive, then a row per skill (name, stage, description, timings)
whose icon drags to the bar and casts by right-click.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const ds = await import( "../../src/engine/foundation/gameplay/devil-spirit.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const SKILL = 0x49;
const WICKED_BLUE = 31155;
const IGNORE_DEFENSE = 31149;
const OTHER_SKILL = 7;
const BLUE_SPIRIT = 24389;

/*
================
bound

Skill shortcuts of OTHER_SKILL on each slot of slots.
================
*/
function bound( slots ) {
	return slots.map( slot => ({ slot, kind: SKILL, payload: OTHER_SKILL }) );
}

/*
================
range
================
*/
function range( first, last ) {
	return Array.from( { length: last - first + 1 }, ( _, i ) => first + i );
}

/*
================
u32
================
*/
function u32( n ) {
	const b = Buffer.alloc( 4 );
	b.writeUInt32LE( n );
	return [ ...b ];
}

/*
================
skillRow
================
*/
function skillRow( id, group ) {
	const req = [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ];
	return {
		id,
		group,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: "Authored skill",
			spCost: 0,
			trainable: false,
			targetRequired: false,
			cooldownMs: 0,
			masteries: req,
			prerequisites: [ ...req, { ID: 0, Level: 0 } ]
		}
	};
}

test("a learned devil buff takes the first free extension bar slot, never an F1..F4 page", () => {
	// The under bar is empty: the extension bar still gets it.
	assert.deepEqual( ds.devilBuffShortcuts( WICKED_BLUE, [ WICKED_BLUE ], [] ), [ {
		slot: 41,
		kind: SKILL,
		payload: WICKED_BLUE
	} ] );
	// Its first slots taken: the next free one.
	assert.deepEqual( ds.devilBuffShortcuts( WICKED_BLUE, [ WICKED_BLUE ], bound( [ 41, 42, 44 ] ) ), [ {
		slot: 43,
		kind: SKILL,
		payload: WICKED_BLUE
	} ] );
	// Already on a shortcut: nothing more.
	assert.deepEqual(
		ds.devilBuffShortcuts( WICKED_BLUE, [ WICKED_BLUE ], [ { slot: 5, kind: SKILL, payload: WICKED_BLUE } ] ),
		[]
	);
});

test("a learned devil buff never takes another skill's shortcut", () => {
	const full = bound( range( 41, 50 ) );
	assert.deepEqual( ds.devilBuffShortcuts( WICKED_BLUE, [ WICKED_BLUE ], full ), [], "a full extension bar" );
	assert.equal( ds.devilBuffUnplaced( WICKED_BLUE, full ), true );
	assert.equal( ds.devilBuffUnplaced( IGNORE_DEFENSE, [] ), false, "a passive is never placed" );
	// An older stage's shortcut is replaced in place, wherever it is.
	for ( const slot of [ 37, 45 ] ) {
		const stale = [ ...bound( range( 41, 44 ) ), { slot, kind: SKILL, payload: 31154 } ];
		assert.deepEqual( ds.devilBuffShortcuts( WICKED_BLUE, [ WICKED_BLUE ], stale ), [ {
			slot,
			kind: SKILL,
			payload: WICKED_BLUE
		} ] );
	}
});

test("the notices name the extension bar slot, an in-place page slot, or a full bar", () => {
	assert.equal(
		ds.devilSkillPlacedNotice( WICKED_BLUE, 41 ).text,
		"Wicked Devil Skill placed on the extension bar, slot 1"
	);
	assert.equal(
		ds.devilSkillPlacedNotice( 31153, 50 ).text,
		"Vallainous Devil Skill placed on the extension bar, slot 10"
	);
	assert.equal(
		ds.devilSkillPlacedNotice( 31146, 11 ).text,
		"Malicious Devil Skill placed on quickslot page 2, slot 1"
	);
	assert.match(
		defined( ds.devilSkillNoRoomNotice( WICKED_BLUE ).text ),
		/extension bar is full.*Wicked Devil Skill/
	);
	assert.deepEqual( ds.knownDevilSkills( [ 31155, 3, 31149, 31157 ] ), [ 31149, 31155 ] );
});

test("each devil skill row says its stage, description and timings", () => {
	assert.equal( ds.devilSkillInfo( OTHER_SKILL ), null );
	const buff = defined( ds.devilSkillInfo( WICKED_BLUE ) );
	assert.equal( buff.stageText, "Stage 2 – levels 3-5" );
	assert.equal( buff.timingText, "Cooldown 30m · Duration 10m" );
	assert.equal( buff.passive, false );
	assert.equal( defined( ds.devilSkillInfo( 31146 ) ).stageText, "Stage 1 – levels 0-2" );
	assert.equal( defined( ds.devilSkillInfo( 31156 ) ).stageText, "Stage 3 – levels 6-10" );
	const passive = defined( ds.devilSkillInfo( IGNORE_DEFENSE ) );
	assert.equal( passive.passive, true );
	assert.equal( passive.stageText, "Stage 2 – levels 3-5" );
	assert.equal( defined( ds.devilSkillInfo( 31150 ) ).stageText, "Stage 3 – levels 6-10" );
	assert.equal( ds.devilSpiritWorn( [ { refObjId: BLUE_SPIRIT } ] ), true );
	assert.equal( ds.devilSpiritWorn( [ { refObjId: 3 } ] ), false );
	assert.equal( ds.devilSpiritWorn( undefined ), false );
});

test("only a devil buff newly on the extension bar opens it", () => {
	const placed = [ { slot: 42, kind: SKILL, payload: WICKED_BLUE } ];
	assert.equal( ds.devilBuffNewOnExtensionBar( [], placed ), true );
	assert.equal( ds.devilBuffNewOnExtensionBar( placed, [ ...placed ] ), false, "already there" );
	assert.equal( ds.devilBuffNewOnExtensionBar( undefined, placed ), false, "the first state" );
	assert.equal( ds.devilBuffNewOnExtensionBar( [], [ { slot: 3, kind: SKILL, payload: WICKED_BLUE } ] ), false );
	assert.equal( ds.devilBuffNewOnExtensionBar( [], bound( [ 42 ] ) ), false, "another skill" );
});

/*
================
learnedWorker

A worker whose bar holds OTHER_SKILL on slots and that has just learned
WICKED_BLUE; returns its state and sent frames.
================
*/
function learnedWorker( slots ) {
	const frames = [], g = createGameplay( f => frames.push( f ) );
	g.bootstrap( {
		character: { skills: [ OTHER_SKILL ], quickSlots: bound( slots ) },
		refSkillSnapshot: [ skillRow( OTHER_SKILL, 10 ), skillRow( WICKED_BLUE, 20 ) ]
	} );
	g.worldReady();
	g.seed( /** @type {any} */ ({ gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }) );
	g.receive( { opcode: 0xb2cb, payload: Uint8Array.from( [ 1, ...u32( WICKED_BLUE ) ] ) }, 1 );
	const state = defined( g.take() );
	g.dispose();
	return { state, frames };
}

test("the worker places a learned devil buff on the extension bar and says where", () => {
	const { state, frames } = learnedWorker( [ ...range( 1, 10 ), 41 ] ), slots = defined( state.quickSlots );
	assert.ok( slots.some( row => row.slot === 42 && row.payload === WICKED_BLUE ) );
	assert.ok( !slots.some( row => row.slot <= 40 && row.payload === WICKED_BLUE ), "no F1..F4 page" );
	assert.equal( slots.filter( row => row.payload === OTHER_SKILL ).length, 11, "no shortcut taken" );
	assert.deepEqual( [ ...frames.filter( f => f.opcode === 0x7541 ).at( -1 ).payload ], [
		1,
		42,
		SKILL,
		...u32( WICKED_BLUE )
	] );
	assert.ok(
		defined( state.notices ).some( notice =>
			notice.text === "Wicked Devil Skill placed on the extension bar, slot 2"
		)
	);
});

test("the worker says so when the extension bar is full", () => {
	const { state, frames } = learnedWorker( range( 41, 50 ) );
	assert.ok( !defined( state.quickSlots ).some( row => row.payload === WICKED_BLUE ) );
	assert.equal( frames.filter( f => f.opcode === 0x7541 ).length, 0 );
	assert.ok( defined( state.notices ).some( notice => /extension bar is full/.test( notice.text ?? "" ) ) );
});

/*
================
skillMeta
================
*/
function skillMeta( id, name, nameSymbol, icon ) {
	const req = [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ];
	return {
		id,
		group: id,
		level: 1,
		name,
		nameSymbol,
		icon,
		spCost: 0,
		trainable: false,
		targetRequired: false,
		cooldownMs: 0,
		masteries: req,
		prerequisites: [ ...req, { ID: 0, Level: 0 } ]
	};
}

/*
================
skillWindow

A HUD with the skill window open; the returned draw() steps it and
returns the newest scene.
================
*/
function skillWindow( f, size = { width: 1600, height: 900 } ) {
	Object.assign( f.state, size );
	f.state.gameplay = {
		...f.state.gameplay,
		skills: [ OTHER_SKILL ],
		skillCatalog: [
			skillMeta(
				WICKED_BLUE,
				"Wicked Devil Skill",
				"SN_SKILL_MALL_NASRUN_THEDEVILCONFUSION",
				"skill\\etc\\mall_nasrun_thedevilconfusion.ddj"
			),
			skillMeta(
				IGNORE_DEFENSE,
				"Ignore Monster's defense",
				"SN_SKILL_MALL_NASRUN_MOBDEFENCEIGNORE",
				"skill\\etc\\mall_nasrun_mobdeffenceignore01.ddj"
			)
		],
		quickSlots: [],
		progression: { level: 10, skillPoints: 0, masteries: [ { id: 257, level: 1 } ] }
	};
	/** @type {any} */
	let scene;
	let now = 1000;
	const step = () => scene = f.ui.step( { ...f.state }, now += 100 ) ?? scene;
	for ( let i = 0; i < 12; i++ ) step();
	f.ui.event( { kind: "key", code: "KeyS" } );
	step();
	return () => {
		step();
		return scene;
	};
}

/*
================
overlaps
================
*/
function overlaps( a, b ) {
	return a[0] < b[0] + b[2] && b[0] < a[0] + a[2] && a[1] < b[1] + b[3] && b[1] < a[1] + a[3];
}

test("the Devil's Spirit tab shows while a spirit is worn, with a hint before its skills", () => {
	const f = uiFixture();
	try {
		const draw = skillWindow( f );
		let scene = draw();
		assert.ok( scene.controls.some( c => c.id === "skill-tab:0" ), "the skill window is open" );
		assert.ok( !scene.controls.some( c => c.id === "skill-tab:-1" ), "no spirit, no devil skill: no tab" );
		f.state.gameplay = {
			...f.state.gameplay,
			avatarInventory: [ { slot: 4, refObjId: BLUE_SPIRIT, typeFlags: 0, name: "Devil's Spirit" } ]
		};
		scene = draw();
		const tab = defined( scene.controls.find( c => c.id === "skill-tab:-1" ) ),
			mastery = defined( scene.controls.find( c => c.id === "skill-tab:0" ) );
		assert.equal( tab.label, "Devil's Spirit" );
		assert.ok( tab.rect[0] > mastery.rect[0] + mastery.rect[2], "right of the mastery tabs" );
		assert.ok( !overlaps( tab.rect, mastery.rect ) );
		assert.equal( tab.rect[1], mastery.rect[1], "on the same strip" );
		f.ui.event( { kind: "activate", id: "skill-tab:-1" } );
		draw();
		draw();
		assert.ok( f.hasText( "Wear a Devil's Spirit to get its skills" ), "the hint" );
		// Taken off with no skill known: the tab goes, the mastery tab returns.
		f.state.gameplay = { ...f.state.gameplay, avatarInventory: [] };
		scene = draw();
		assert.ok( !scene.controls.some( c => c.id === "skill-tab:-1" ) );
		assert.ok( defined( scene.controls.find( c => c.id === "skill-tab:0" ) ).selected );
	} finally {
		f.dispose();
	}
});

test("the Devil's Spirit tab lists the known skills, which drag to the extension bar and cast", () => {
	/** @type {any[]} */
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const draw = skillWindow( f );
		f.state.gameplay = { ...f.state.gameplay, skills: [ OTHER_SKILL, IGNORE_DEFENSE, WICKED_BLUE ] };
		let scene = draw();
		assert.ok( scene.controls.some( c => c.id === "skill-tab:-1" ), "a known devil skill alone shows it" );
		f.ui.event( { kind: "activate", id: "skill-tab:-1" } );
		draw();
		scene = draw();
		const buff = defined( scene.controls.find( c => c.id === "skill:" + WICKED_BLUE ) ),
			passive = defined( scene.controls.find( c => c.id === "skill:" + IGNORE_DEFENSE ) );
		assert.equal( buff.label, "Wicked Devil Skill" );
		assert.equal( buff.draggable, true );
		assert.equal( buff.rightActivate, true );
		assert.equal( passive.label, "Ignore Monster's defense", "the passive is listed too" );
		assert.ok( f.hasText( "Wicked Devil Skill" ), "the name is drawn" );
		assert.ok( f.hasText( "Stage 2 – levels 3-5" ), "the stage is drawn" );
		assert.ok( f.hasText( "Cooldown 30m · Duration 10m" ), "the timings are drawn" );
		const slot = defined( scene.controls.find( c => c.id === "hotbar:41" ) ).rect;
		sent.length = 0;
		f.ui.event( { kind: "drag", id: buff.id, dx: 40, dy: 300 } );
		f.ui.event( { kind: "drag-end", id: buff.id, x: slot[0] + 4, y: slot[1] + 4 } );
		assert.deepEqual( sent, [ {
			kind: "gameplay",
			command: { kind: "quickslot-set", binding: { slot: 41, kind: SKILL, payload: WICKED_BLUE } }
		} ] );
		sent.length = 0;
		f.ui.event( { kind: "right-activate", id: buff.id } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "skill", skillId: WICKED_BLUE } } ] );
	} finally {
		f.dispose();
	}
});

test("a devil buff placed on a collapsed extension bar opens it", () => {
	const f = uiFixture();
	try {
		const draw = skillWindow( f );
		f.ui.event( { kind: "activate", id: "ext-open" } );
		let scene = draw();
		assert.ok( !scene.controls.some( c => c.id === "hotbar:42" ), "collapsed" );
		f.state.gameplay = {
			...f.state.gameplay,
			skills: [ OTHER_SKILL, WICKED_BLUE ],
			quickSlots: [ { slot: 42, kind: SKILL, payload: WICKED_BLUE } ]
		};
		draw();
		scene = draw();
		assert.ok( scene.controls.some( c => c.id === "hotbar:42" ), "opened" );
		// Collapsed again by the player, it stays so while the buff stays put.
		f.ui.event( { kind: "activate", id: "ext-open" } );
		draw();
		scene = draw();
		assert.ok( !scene.controls.some( c => c.id === "hotbar:42" ) );
	} finally {
		f.dispose();
	}
});

test("the Devil's Spirit tab fits a compact screen beside the mastery tabs", () => {
	const f = uiFixture();
	try {
		const draw = skillWindow( f, { width: 640, height: 480 } );
		f.state.gameplay = { ...f.state.gameplay, skills: [ OTHER_SKILL, WICKED_BLUE ] };
		const scene = draw(),
			tab = defined( scene.controls.find( c => c.id === "skill-tab:-1" ) ),
			mastery = defined( scene.controls.find( c => c.id === "skill-tab:0" ) );
		assert.ok( !overlaps( tab.rect, mastery.rect ) );
		assert.ok( tab.rect[0] >= 0 && tab.rect[0] + tab.rect[2] <= 640, "on screen" );
	} finally {
		f.dispose();
	}
});
