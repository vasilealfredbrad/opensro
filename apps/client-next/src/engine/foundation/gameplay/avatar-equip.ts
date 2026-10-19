/*
===========================================================================

avatar-equip.ts - whether a bag item may go into the avatar storage

The inventory's avatar view (double-click or drag of a bag avatar, the
avatar window's slots) judges a bag avatar before it sends the 0x7034
bag-to-avatar move, as the server judges it again
(action/avatartransfer.go):

  - only an avatar item (TID 3.1.13.x) moves there;
  - one item per part: a worn hat, dress, attachment or flag refuses a
    second of its part;
  - an attachment (TID4 3) is worn only over a worn avatar dress. The
    refusal tells the player so (UIIT_STT_AVATAR_MAIL_ONLY_WEAR, "Can
    only be equipped when avatar dress is worn."); the original
    refusal's text is not traced, so the client's own string for this
    very rule stands in for it (inference).

Port-only, not native: the Devil's Spirit rows (game/item/devilspirit
on the server, ids above every v1.150 item) take the attachment socket
but, as in the later client they come from, need no dress. Their ids
are pinned to the server's devilspirit.json by
tests/runtime/devil-spirit.test.mjs. With SRO_DEVIL_SPIRIT off the
server has no such row, so no bag item can carry one.

===========================================================================
*/

import { devilSpiritExpired, devilSpiritState, isDevilSpirit } from "./devil-spirit";

export { DEVIL_SPIRIT_FIRST_ID, DEVIL_SPIRIT_LAST_ID, isDevilSpirit } from "./devil-spirit";

// (typeFlags & AVATAR_TYPE_MASK) === AVATAR_TYPE_BITS: TID 3.1.13.x.
const AVATAR_TYPE_MASK = 0x7fe;
const AVATAR_TYPE_BITS = 0x6ac;
// typeFlags >>> AVATAR_PART_SHIFT is the TID4 part.
const AVATAR_PART_SHIFT = 11;
const AVATAR_PART_DRESS = 2;
const AVATAR_PART_ATTACHMENT = 3;
// The avatar storage holds four records.
export const AVATAR_STORAGE_SLOTS = 4;
// The text of the dress refusal.
export const AVATAR_NEEDS_DRESS_TEXT = "UIIT_STT_AVATAR_MAIL_ONLY_WEAR";

export interface AvatarRecord {
	readonly slot: number;
	readonly refObjId: number;
	readonly typeFlags: number;
	// The Devil's Spirit's awakening expiry (devil-spirit.ts); other rows ignore it.
	readonly durability?: number;
}

export type AvatarEquipVerdict =
	| { readonly kind: "send"; readonly destination: number; }
	| { readonly kind: "not-avatar"; }
	| { readonly kind: "occupied"; }
	| { readonly kind: "needs-dress"; }
	| { readonly kind: "expired"; };

/*
================
isAvatarItem
================
*/
export function isAvatarItem( typeFlags: number ): boolean {
	return (typeFlags & AVATAR_TYPE_MASK) === AVATAR_TYPE_BITS;
}

/*
================
avatarNeedsDress

An attachment needs a worn dress; a Devil's Spirit does not.
================
*/
export function avatarNeedsDress( item: AvatarRecord ): boolean {
	return item.typeFlags >>> AVATAR_PART_SHIFT === AVATAR_PART_ATTACHMENT && !isDevilSpirit( item.refObjId );
}

/*
================
avatarEquipVerdict

The judgement of moving a bag item into the avatar storage that holds
the worn records: the free storage record it goes to, or why not.
================
*/
export function avatarEquipVerdict(
	item: AvatarRecord,
	worn: readonly AvatarRecord[],
	nowSec = Math.floor( Date.now() / 1000 )
): AvatarEquipVerdict {
	if ( !isAvatarItem( item.typeFlags ) ) return { kind: "not-avatar" };
	// Port-only: an expired Devil's Spirit stays in the bag, as the server
	// refuses it (action/avatartransfer.go).
	if (
		isDevilSpirit( item.refObjId ) &&
		devilSpiritExpired( devilSpiritState( { durability: item.durability ?? 0, variance: "0" } ), nowSec )
	) return { kind: "expired" };
	const part = item.typeFlags >>> AVATAR_PART_SHIFT;
	if ( worn.some( record => record.typeFlags >>> AVATAR_PART_SHIFT === part ) ) return { kind: "occupied" };
	if (
		avatarNeedsDress( item ) &&
		!worn.some( record => record.typeFlags >>> AVATAR_PART_SHIFT === AVATAR_PART_DRESS )
	) return { kind: "needs-dress" };
	for ( let slot = 0; slot < AVATAR_STORAGE_SLOTS; slot++ ) {
		if ( !worn.some( record => record.slot === slot ) ) return { kind: "send", destination: slot };
	}
	return { kind: "occupied" };
}
