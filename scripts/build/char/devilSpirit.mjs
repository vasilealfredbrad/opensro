/*
===========================================================================

devilSpirit.mjs - the Devil's Spirit's look, from a later client's import

Port-only, not native (owner decision 2026-10-18). The Devil's Spirit rows
(apps/server/internal/game/item/devilspirit/devilspirit.json, the one data
file the server reads too) are not in the v1.150 itemdata, so the equipment
catalog gains one record per row here: a v1.150 avatar attachment (TID
3.1.13.3, avatar slot 2) with the Halloween attachment's visual mask and
priority, drawn on each body of the row's sex.

Its models, textures and icons are never in this repository. They come
from the owner's extraction of a later client named by SRO_IMPORT_ROOT
(Data_extracted/res/item/avatar/avatar_[m|w]_nasrun*.bsr and what they
reference; Media_extracted/icon/item/avatar/mall_avatar_*_nasrun*.ddj).
The import uses the v1.150 formats (JMXVRES 0109, JMXVBMS 0110, JMXVBMT
0102, JMXVDDJ 1000), so the ordinary converters read it: dataAssetPath
falls back to the import, the native texture cache and the image
converter scan it. Without an import every record still exists, with
null bodies, so a worn Devil's Spirit is drawn as nothing rather than
failing the wearer's appearance.

The worn item has no look of its own in the later client: a devil skill
draws its devil form (charactervisualchange, devilSpiritPresentation.mjs)
while its buff runs. With the import's textdata each record therefore
also carries a model per form (looks, by skill id) and the forms' mask
and priority, which hide the worn armour and avatar dress, and the
avatar visual overrides gain the form's motion set.

Run directly, it builds only the Devil's Spirit models (and their native
textures) into the generated tree under the asset lock, the overlay step
of `pnpm assets build`, without touching the published roster.

===========================================================================
*/
import { createHash } from "node:crypto";
import { importDataRoot, importRoot, retailTextdataRoot } from "../world/paths.mjs";
import { buildItemSetGlb, createDonorPool } from "./itemAttachments.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { buildNativeCharacterTextures } from "../shared/nativeCharacterTextures.mjs";
import { staticEnvironmentModifier } from "../shared/characterMaterialState.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { parseJmxBmsStaticMesh, parseJmxBmtMaterialSet, parseJmxResourceBsr } from "../world/objects/formats.mjs";
import { withGeneratedAssetsLock } from "../../rebuildLock.mjs";
import {
	ATTACHMENT_AVATAR_SLOT,
	ATTACHMENT_TYPE,
	devilSpiritRecords,
	itemLooks,
	loadDevilSpirit
} from "./devilSpiritData.mjs";
import { loadDevilSpiritPresentation } from "./devilSpiritPresentation.mjs";

// An environment modifier's scope word (baseWords[1]): the whole resource,
// or the one material whose index baseWords[3] holds.
const ENVIRONMENT_WHOLE_RESOURCE = 1;
const ENVIRONMENT_ONE_MATERIAL = 2;
const ENVIRONMENT_EVERY_MATERIAL = 0xffffffff;

/*
================
meshEnvironment

The environment modifiers one mesh wears. The later client's devil body
scopes its map to one material (baseWords[1] 2, baseWords[3] the BMT
index): that material is the body skin, whose DDJ alpha is the map's
reflection mask, not a cutout (about 9% of its texels reach the 128
reference). AEE6D0 applies the map before material setup and replaces
the 0x200 alpha test with reference 1 (baseWords[5] bit 2) or none, so
without it the 0x341 material cut the body away (the body drew see-
through while the static-mapped shoulders drew whole). A mesh drawn with
the scoped material (its BMS material name is that BMT row's) therefore
wears the map as a whole-resource one, the coverage v1.150 bakes; every
other mesh of the resource wears none of it. materialNames is the BSR's
BMT material names in index order. Other non-static maps (animated) are
returned in dropped.
================
*/
export function meshEnvironment( modifiers, meshMaterialName, materialNames ) {
	const kept = [], dropped = [];
	for ( const modifier of modifiers ) {
		if ( staticEnvironmentModifier( modifier ) ) {
			kept.push( modifier );
			continue;
		}
		const words = modifier.baseWords;
		if ( modifier.kind !== 2 || modifier.stateId !== -1 || words[1] !== ENVIRONMENT_ONE_MATERIAL ) {
			dropped.push( modifier );
			continue;
		}
		if ( materialNames[words[3]] !== meshMaterialName ) continue;
		const whole = [ ...words ];
		whole[1] = ENVIRONMENT_WHOLE_RESOURCE;
		whole[3] = ENVIRONMENT_EVERY_MATERIAL;
		kept.push( { ...modifier, baseWords: whole } );
	}
	return { kept, dropped };
}

/*
================
scopedEnvironment

The buildItemSetGlb environment hook: meshEnvironment for each mesh, from
the piece's BSR material set and the mesh's own material name. warned
names each resource whose dropped maps were reported once.
================
*/
function scopedEnvironment( warned ) {
	const names = new Map();
	return async ( modifiers, itemBsrPath, meshPath ) => {
		if ( modifiers.every( staticEnvironmentModifier ) ) return modifiers;
		let materialNames = names.get( itemBsrPath );
		if ( !materialNames ) {
			const bsr = parseJmxResourceBsr( await loadDataAsset( itemBsrPath ), itemBsrPath );
			materialNames = [];
			for ( const set of bsr.materialPaths ) {
				const parsed = parseJmxBmtMaterialSet( await loadDataAsset( set ), set );
				materialNames.push( ...parsed.materials.map( material => material.name ) );
			}
			names.set( itemBsrPath, materialNames );
		}
		const mesh = parseJmxBmsStaticMesh( await loadDataAsset( meshPath ), meshPath );
		const { kept, dropped } = meshEnvironment( modifiers, mesh.metadata.materialName, materialNames );
		if ( dropped.length && !warned.has( itemBsrPath ) ) {
			warned.add( itemBsrPath );
			console.warn( `[equipment] Devil's Spirit: ${itemBsrPath} drops an animated environment map` );
		}
		return kept;
	};
}

/*
================
publishDevilSpiritEquipment

Adds the records to out (the equipment catalog by id) and, with an import,
builds each body's models: the row's resources (body, horns, shoulders),
and each devil form's, as one item set on the body's donor skeleton.
Rows and forms sharing resources share a model. overrides (the dress's
avatar visual overrides) gains each row's motion set: its first form's,
every form set holding the same clips (devilSpiritMotionSets). Returns
how many models were built.
================
*/
export async function publishDevilSpiritEquipment(
	out,
	{ getDonor, data = loadDevilSpirit(), presentation = loadDevilSpiritPresentation( data ), overrides = null }
) {
	const byCode = new Map( Object.values( out ).map( record => [ record.code, record ] ) );
	const records = devilSpiritRecords( data, code => byCode.get( code ), presentation?.looks );
	for ( const id of Object.keys( records ) ) {
		if ( out[id] ) throw Error( `Devil's Spirit: id ${id} is a v1.150 item` );
	}
	Object.assign( out, records );
	if ( !importDataRoot ) {
		console.log(
			`[equipment] ${Object.keys( records ).length} Devil's Spirit records without models (no SRO_IMPORT_ROOT)`
		);
		return 0;
	}
	const cache = new Map(), environment = scopedEnvironment( new Set() );
	let built = 0;
	const model = async ( body, item, models ) => {
		const key = body + ":" + models.join( "|" );
		let entry = cache.get( key );
		if ( !entry ) {
			const donor = await getDonor( body.slice( 0, 2 ), item.sex );
			if ( !donor ) throw Error( `Devil's Spirit: missing donor ${body}` );
			entry = await buildItemSetGlb( {
				tag: "devil-spirit",
				key: body + "_" +
					createHash( "sha256" ).update( "devil-spirit-2:" + key ).digest( "hex" ).slice( 0, 20 ),
				...donor,
				pieces: models.map( ( itemBsrPath, i ) => ({ part: `EQ${i}`, itemBsrPath }) ),
				outSubdir: "equipment",
				environment
			} );
			if ( !entry ) throw Error( `Devil's Spirit: ${item.codename} has no model in ${importRoot}` );
			cache.set( key, entry );
			built++;
		}
		return entry;
	};
	for ( const item of data.items ) {
		const record = records[item.id], forms = itemLooks( item, presentation?.looks );
		for ( const body of Object.keys( record.bodies ) ) {
			record.bodies[body] = await model( body, item, item.models );
			for ( const [id, look] of forms ) record.looks[id][body] = await model( body, item, look.models[item.sex] );
		}
		if ( overrides && forms.length ) {
			overrides[item.id] = { animation: forms[0][1].motion, priority: forms[0][1].priority, additionalBsr: "" };
		}
	}
	console.log( `[equipment] ${built} Devil's Spirit models from ${importRoot} (port-only, not native)` );
	return built;
}

/*
================
buildDevilSpiritOverlay

The overlay step on its own: the import's native textures, then the
models, against the v1.150 records. Returns the records it would add.
================
*/
export async function buildDevilSpiritOverlay() {
	if ( !importDataRoot ) throw Error( "Devil's Spirit: set SRO_IMPORT_ROOT to the later client's extraction" );
	await buildNativeCharacterTextures( [ importDataRoot ] );
	const records = {};
	for ( const row of loadEquipmentRecords( retailTextdataRoot ).values() ) {
		if ( row.tid.join( "." ) === ATTACHMENT_TYPE ) {
			records[row.id] = {
				code: row.code,
				visualMask: row.visualMask,
				visualPriority: row.visualPriority,
				avatarSlot: ATTACHMENT_AVATAR_SLOT
			};
		}
	}
	const before = new Set( Object.keys( records ) );
	await publishDevilSpiritEquipment( records, {
		getDonor: createDonorPool( resolveRoster( retailTextdataRoot ).resolved )
	} );
	return Object.fromEntries( Object.entries( records ).filter( ( [id] ) => !before.has( id ) ) );
}

if ( isMainScript( import.meta.url ) ) {
	const records = await withGeneratedAssetsLock( "devil spirit overlay", buildDevilSpiritOverlay );
	console.log( JSON.stringify( records, null, "\t" ) );
}
