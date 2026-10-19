/*
===========================================================================

uniquemonster.go - the extra damage a player deals to unique monsters

Port-only, not native to v1.150. The later server keeps a parameter
(0x103) that a Devil's Spirit's MATTR_NASRUN_UMDU option ("Damage
increase (it will apply to Unique monsters only)") writes, and adds that
percent of a hit's damage when the victim's base grade is unique (the
rarity byte's low nibble 3). v1.150 writes the parameter from nothing,
so with SRO_DEVIL_SPIRIT off (no umdu row, game/item/devilspirit) every
attacker reads 0 and no damage changes.

===========================================================================
*/
package combat

import "opensro.online/server/internal/game/item/wire"

const (
	// paramUniqueMonsterDamage is the later server's unique-monster damage
	// percent (its CParam definition: 0..100).
	paramUniqueMonsterDamage uint16 = 0x103

	// monsterGradeMask / monsterGradeUnique are the rarity byte's base
	// grade (CGObjMob_GetBaseGrade, vtable +0x12C) and its unique value.
	monsterGradeMask   = 0x0f
	monsterGradeUnique = 3

	// percentDivisor turns the parameter into a fraction.
	percentDivisor = 100
)

/*
================
UniqueMonsterDamage

A player's impact on a monster: when the defender is a unique monster,
the attacker's unique-monster percent of the damage is added (truncated,
at most the wire's damage ceiling). A blocked, covered or zero impact,
a defender that is no unique monster, or an attacker without the stat is
returned unchanged.
================
*/
func UniqueMonsterDamage(r Result, attacker, defender Stats) Result {
	if r.Blocked || r.Slain || r.Damage == 0 || !defender.Monster || defender.Rarity&monsterGradeMask != monsterGradeUnique {
		return r
	}
	percent, ok := attacker.Param(paramUniqueMonsterDamage)
	if !ok || percent <= 0 {
		return r
	}
	extra := uint64(r.Damage) * uint64(percent) / percentDivisor
	r.Damage = uint32(min(uint64(r.Damage)+extra, uint64(wire.MaxSkillActionDamage)))
	return r
}
