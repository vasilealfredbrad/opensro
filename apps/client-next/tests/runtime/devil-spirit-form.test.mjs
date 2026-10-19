/*
===========================================================================

devil-spirit-form.test.mjs - the devil skills' devil form on the client

Port-only, not native. A devil buff draws its devil form on its caster
for every observer: the worn Devil's Spirit, tagged with the running
buff, wears that buff's form from the dress catalog (looks) at the
forms' mask and priority, which hide the worn armour and avatar dress;
untagged it draws and hides nothing. The cast's animation set
(avatar_nasrun1) has its own port key, and the form's ACT_L
SCT_CHAR_SCALE is the model scale the effect owner eases to.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const ds = await import( sourceFileUrl( "src/engine/foundation/gameplay/devil-spirit.ts" ).href );
const { assembleEquipmentAppearance } = await import(
	sourceFileUrl( "src/engine/foundation/animation/equipment-appearance.ts" ).href
);
const { skillMotionRole } = await import( sourceFileUrl( "src/engine/foundation/animation/skill-motion.ts" ).href );
const { effectScript } = await import( sourceFileUrl( "src/engine/foundation/animation/effect-script.ts" ).href );

const DEVIL = 24385;
const VALLAINOUS = 31148;
const ARMOR = 100;
const DRESS = 200;
// charactervisualchange rows 307 / 317 / 327: 2561 | 516 | 1850, priority 60.
const FORM_MASK = 3903;
const FORM_PRIORITY = 60;
// The port key of the cast's animation set (devilSpiritPresentation.mjs).
const DEVIL_CAST_SET_KEY = 0x00ccce2c;
const BODY = "EU_M";

/*
================
entry

One published set entry drawn as one part.
================
*/
function entry( glb ) {
	return { glb, parts: [ "EQ0" ] };
}

/*
================
dress

A catalog with a chest armour, an avatar dress and the Devil's Spirit,
whose own body differs from its Vallainous form.
================
*/
function dress() {
	return {
		equipment: {
			[ARMOR]: {
				slot: 1,
				armorClass: 3,
				thiefSuit: false,
				visualMask: 2,
				visualPriority: 150,
				model: "armor",
				source: null,
				bodies: { [BODY]: entry( "/assets/char/equipment/armor.glb" ) }
			},
			[DRESS]: {
				slot: null,
				avatarSlot: 1,
				armorClass: 0,
				thiefSuit: false,
				visualMask: 574,
				visualPriority: 70,
				model: "dress",
				source: null,
				bodies: { [BODY]: entry( "/assets/char/equipment/dress.glb" ) }
			},
			[DEVIL]: {
				slot: null,
				avatarSlot: 2,
				armorClass: 0,
				thiefSuit: false,
				visualMask: FORM_MASK,
				visualPriority: FORM_PRIORITY,
				model: "devil",
				source: null,
				bodies: { [BODY]: entry( "/assets/char/equipment/devil-item.glb" ) },
				looks: { [VALLAINOUS]: { [BODY]: entry( "/assets/char/equipment/devil-form.glb" ) } }
			}
		}
	};
}

/*
================
models

The models a wearer is drawn with.
================
*/
function models( avatars ) {
	return assembleEquipmentAppearance( {
		resource: { codename: "CHAR_EU_MAN_ADVENTURER" },
		dress: dress(),
		equipment: [ { slot: 1, refObjId: ARMOR, plus: 0 } ],
		avatars,
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: false
	} ).parts.map( part => part.model ).sort();
}

test("a running devil buff draws its form over the armour and the avatar dress", () => {
	const worn = [ { refObjId: DRESS }, { refObjId: DEVIL } ];
	const effects = [ { gid: 9, skill: VALLAINOUS } ];
	assert.deepEqual(
		models( ds.devilSpiritAvatars( worn, ds.devilBuffSkill( 9, effects ) ) ),
		[ "/assets/char/equipment/devil-form.glb" ]
	);
});

test("without a devil buff the Devil's Spirit draws and hides nothing", () => {
	const worn = [ { refObjId: DRESS }, { refObjId: DEVIL } ];
	assert.deepEqual(
		models( ds.devilSpiritAvatars( worn, ds.devilBuffSkill( 9, [] ) ) ),
		[ "/assets/char/equipment/dress.glb" ]
	);
	// A preview's untagged record (the dock, the mall) wears no form either.
	assert.deepEqual( models( worn ), [ "/assets/char/equipment/dress.glb" ] );
	assert.deepEqual( models( [ { refObjId: DEVIL } ] ), [ "/assets/char/equipment/armor.glb" ] );
});

test("a buff with no published form falls back to the item's own body", () => {
	assert.deepEqual(
		models( [ { refObjId: DEVIL, look: 31146 } ] ),
		[ "/assets/char/equipment/devil-item.glb" ]
	);
});

test("the cast's animation set has the port key after the native sets", () => {
	assert.equal( skillMotionRole( DEVIL_CAST_SET_KEY, "ANI_READY01" ), "native:avatar_nasrun1:40" );
	assert.equal( skillMotionRole( DEVIL_CAST_SET_KEY, "ANI_SKILL_1" ), "native:avatar_nasrun1:26" );
	assert.equal( skillMotionRole( 0x00cccdbc, "ANI_SKILL_1" ), "native:dual_axe:26" );
	assert.throws( () => skillMotionRole( DEVIL_CAST_SET_KEY + 28, "ANI_READY01" ), /Unknown skill animation set/ );
});

test("the devil form scales to its ACT_L SCT_CHAR_SCALE", () => {
	const stages = [
		{ phase: "ACT_S", script: effectScript( [] ) },
		{ phase: "ACT_L", script: effectScript( [] ) },
		{ phase: "ACT_L", script: effectScript( [ "SCT_CHAR_SCALE", "1.2", "1000" ] ) }
	];
	assert.deepEqual( ds.devilFormScale( stages ), { scale: Math.fround( 1.2 ), seconds: 1 } );
	// The cast's camera shake or an ACT_S scale is no form scale.
	assert.equal( ds.devilFormScale( [ { phase: "SHOT", script: effectScript( [ "SCT_SHAKECAM1" ] ) } ] ), undefined );
	assert.equal(
		ds.devilFormScale( [ { phase: "ACT_S", script: effectScript( [ "SCT_CHAR_SCALE", "1.2", "1000" ] ) } ] ),
		undefined
	);
	assert.equal( ds.devilFormScale( undefined ), undefined );
});
