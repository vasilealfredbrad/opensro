/*
===========================================================================

uniquemonster_test.go - tests for uniquemonster.go and the NASRUN options

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
nasrunStats

A level-1 player with the given keeper writes.
================
*/
func nasrunStats(t *testing.T, writes []paramkeeper.Write) Stats {
	t.Helper()
	c := &domain.Character{Level: pointer(1), Strength: pointer(20), Intellect: pointer(20)}
	s, _, err := PlayerStatsWithModifiers(c, Catalogs{Items: itemRefs{}}, writes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

/*
================
TestUniqueMonsterDamageAddsItsPercentOnlyToUniques

Port-only: an attacker with 10 in the unique-monster parameter deals 10 %
more to a unique (grade 3, also as a party unique) and nothing more to any
other monster or player; a blocked or zero impact stays as it is; an
attacker without the parameter changes nothing.
================
*/
func TestUniqueMonsterDamageAddsItsPercentOnlyToUniques(t *testing.T) {
	writes, err := magicOptionWrites("ITEM_MALL_AVATAR_M_NASRUN", []magicOption{{tag: optionTag("umdu"), value: 10}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	attacker, plain := nasrunStats(t, writes), nasrunStats(t, nil)
	unique, err := MonsterStats(monster.MonsterRef{CombatPinned: true, MonsterType: monsterGradeUnique})
	if err != nil {
		t.Fatal(err)
	}
	partyUnique := unique
	partyUnique.Rarity = 0x10 | monsterGradeUnique
	normal := unique
	normal.Rarity = 0
	player := nasrunStats(t, nil)
	hit := Result{Damage: 1005}
	for _, c := range []struct {
		name               string
		in                 Result
		attacker, defender Stats
		want               uint32
	}{
		{"unique", hit, attacker, unique, 1105},
		{"party unique", hit, attacker, partyUnique, 1105},
		{"normal monster", hit, attacker, normal, 1005},
		{"player", hit, attacker, player, 1005},
		{"no stat", hit, plain, unique, 1005},
		{"blocked", Result{Damage: 1005, Blocked: true}, attacker, unique, 1005},
		{"zero", Result{}, attacker, unique, 0},
	} {
		if got := UniqueMonsterDamage(c.in, c.attacker, c.defender).Damage; got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

/*
================
TestNasrunOptionsWriteTheirParameters

hpna and mpna raise the maximum HP and MP by their percent, brna adds a
flat block rate, umdu fills the unique-monster parameter.
================
*/
func TestNasrunOptionsWriteTheirParameters(t *testing.T) {
	writes, err := magicOptionWrites("ITEM_MALL_AVATAR_M_NASRUN", []magicOption{
		{tag: optionTag("hpna"), value: 20}, {tag: optionTag("mpna"), value: 30},
		{tag: optionTag("brna"), value: 3}, {tag: optionTag("umdu"), value: 6},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	worn, bare := nasrunStats(t, writes), nasrunStats(t, nil)
	hp, _ := worn.Param(3)
	bareHP, _ := bare.Param(3)
	mp, _ := worn.Param(4)
	bareMP, _ := bare.Param(4)
	if int(hp) != int(bareHP*1.2) || int(mp) != int(bareMP*1.3) {
		t.Fatalf("HP %v (bare %v), MP %v (bare %v)", hp, bareHP, mp, bareMP)
	}
	if worn.BlockRate != bare.BlockRate+3 {
		t.Fatalf("block rate %v, bare %v", worn.BlockRate, bare.BlockRate)
	}
	if v, ok := worn.Param(paramUniqueMonsterDamage); !ok || v != 6 {
		t.Fatalf("unique-monster parameter %v %v", v, ok)
	}
}
