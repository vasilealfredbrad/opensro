/*
===========================================================================

portcombat.go - a port-only combat projection for scripted actors

PORT-ONLY, NOT NATIVE: no v1.150 monster fights with another row's
numbers. The battle royale bots (SRO_BATTLE_ROYALE, docs/battle-royale.md
stage 5) keep one visible reference for their whole life, so every client
derives the same level, model and HP bar from it, while their combat
strength follows the bot's level: the combat projection
(combat.MonsterInstanceStats) reads Ref instead of the visible reference
and scales the hits the actor takes and deals. A nil PortCombat (every
native actor) changes nothing.

===========================================================================
*/

package monster

/*
================
PortCombat

Ref is the reference whose attack, defence, hit and evasion the actor
fights with. DamageTaken multiplies every hit it receives and DamageDealt
every hit it deals, on top of the native lane factors; 0 is no scale.
The value is immutable once installed: writers replace the pointer.
================
*/
type PortCombat struct {
	Ref         MonsterRef
	DamageTaken float32
	DamageDealt float32
}
