/*
===========================================================================

footprints.ts - authored toe contacts and their decal lifetime

Owns the footprint list and sequence for one character presentation owner.
Actor callbacks admit real toe sockets; the parent expires and publishes the
list at its existing post-actor position, and clears it during teardown.
The port-only Battle Royale rings (ground-rings.ts) are the other ground
decals the character presentation publishes; they step and clear with the
footprints at the same positions.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import { MOVEMENT_MODE_SEATED, type EntityState } from "@/engine/contracts/world";
import type { Footprint } from "@/engine/contracts/footprint";
import type { Pose } from "@/engine/contracts/gameplay";
import type { Renderer } from "@/engine/contracts/runtime";
import type { GameplayState } from "@/engine/contracts/gameplay";
import { createGroundRings } from "./ground-rings";

/*
================
createFootprints
================
*/
export function createFootprints(
	renderer: Pick<Renderer, "characterSocket" | "setFootprints" | "setGroundRings" | "setZoneFog">,
	soundSurface: ( pose: Pose ) => string | undefined
) {
	let footprints: Footprint[] = [], footprintSequence = 0;
	const groundRings = createGroundRings( renderer );
	/*
	================
	footContact
	================
	*/
	function footContact(
		entity: EntityState,
		actors: readonly CharacterActor[],
		actor: CharacterActor,
		pose: Pose,
		right: boolean,
		seconds: number
	) {
		if (
			(entity.kind !== "player" && entity.kind !== "local-player") || entity.movementMode === MOVEMENT_MODE_SEATED
		) return;
		const surface = soundSurface( pose );
		if ( surface !== "SAND" && surface !== "SNOW" ) return;
		const socket = renderer.characterSocket( actors, entity.gid, right ? "Bip01 R Toe0" : "Bip01 L Toe0", [
			0,
			0,
			0
		] );
		if ( socket ) {
			footprints.push( {
				id: ++footprintSequence,
				pose: socket,
				yaw: Math.fround( Math.PI - actor.pose.yaw + (right ? -.07853981852531433 : .07853981852531433) ),
				right,
				surface,
				started: seconds
			} );
		}
	}
	/*
	================
	clearFootprints
	================
	*/
	function clearFootprints() {
		if ( footprints.length ) {
			footprints = [];
			renderer.setFootprints( footprints );
		}
		footprintSequence = 0;
		groundRings.clear();
	}
	return {
		footContact,
		clearFootprints,
		/*
		================
		step

		Expire and publish after actor callbacks have admitted this frame's toes,
		then lay the Battle Royale rings of this frame's match, if any.
		================
		*/
		step( seconds: number, gameplay?: GameplayState | null ) {
			if ( footprints.length ) {
				footprints = footprints.filter( row => seconds < row.started + 20 );
				renderer.setFootprints( footprints );
			}
			groundRings.step( gameplay?.battleRoyale, seconds, gameplay?.battleRoyaleLobby, gameplay?.pose );
		}
	};
}
