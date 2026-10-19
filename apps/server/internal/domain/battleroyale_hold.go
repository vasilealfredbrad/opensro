/*
===========================================================================

battleroyale_hold.go - the real character held under a battle royale match

Port-only, not native (SRO_BATTLE_ROYALE, docs/battle-royale.md). While a
player fights a match its character row carries the level-1 match
character; the complete real record is held in this field of the same
row, so one commit swaps the two and no copy of the real character ever
lives in another row. Nil outside a match. A row found with a hold at boot
or at world entry is restored before anything reads it.

===========================================================================
*/
package domain

/*
================
BattleRoyaleHold

The held real record, the build the match character fights with, the
packed match world the row was moved into and when the hold was made.
================
*/
type BattleRoyaleHold struct {
	Real     *Character `json:"real"`
	Build    uint8      `json:"build"`
	World    uint32     `json:"world"`
	HeldAtMs int64      `json:"heldAtMs"`
}

/*
================
cloneBattleRoyaleHold

The hold's deep copy for cloneCharacter.
================
*/
func cloneBattleRoyaleHold(source *BattleRoyaleHold) *BattleRoyaleHold {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Real = cloneCharacter(source.Real)
	return &clone
}

/*
================
CloneCharacter

A detached deep copy of the record as it is, without the enter-world
normalization Snapshot applies. The battle royale hold stores this copy
and restores it byte for byte.
================
*/
func CloneCharacter(source *Character) *Character {
	return cloneCharacter(source)
}
