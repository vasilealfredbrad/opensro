/*
===========================================================================

lootrandom.go - random match equipment and skins (stage 9)

Port-only, not native (docs/battle-royale.md "Stage 9", item 1 and 4).
The stage-8 set bias and pity rule are gone: an equipment draw of the
match table is random across every build, race and gender. Its rows are
the union of what every build of the table wears (equipmentRows over
Builds() and both bodies): the weapon kinds of every build, the Chinese
and European shields, the heavy/light armor of both Chinese bodies and
the heavy/light/clothes armor of both European bodies, and both races'
earrings, necklaces and rings, Moon or Sun by rollSeal. The level window
is kept: only rows whose required level lies in [level - LootWindow,
level] of the KILLER drop. A player builds its set from what it finds;
the auto-equip wears what its build can use (autoequip.go).

Skins (item 4): skinChance in skinDraws equipment draws (about 1 %)
drop a random avatar of SkinCodenames instead, any gender, at any
level. The wiring gives it at least two blue options of its native
avatar pool (loot.AvatarMagic).

LootTuning holds the two knobs the stage-9 set report weighs (docs
"Server notes (stage 9)"): equipment-only stacks added to every kill and
a level-cap window over the whole top degree. The live table runs
LiveLootTuning (owner choice 2026-10-18: five extra equipment stacks a
kill and the level-30 window over the top degree); the zero tuning is the
stage-9 table the economy simulation compares against.

The row table is built once (sync.OnceValue) and never written after:
immutable data, like a compiled pattern.

===========================================================================
*/
package battleroyale

import (
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
)

const (
	// A skin replaces skinChance in skinDraws equipment draws (1 %).
	skinChance = 1
	skinDraws  = 100
	// liveExtraEquipment is the live table's equipment-only stacks a
	// kill (LiveLootTuning).
	liveExtraEquipment = 5
)

/*
================
LootTuning

Port-only knobs of the random loot; the zero value is the stage-9
table, LiveLootTuning the live one.
ExtraEquipment adds that many equipment-only stacks to every kill (a
family drawn by the table's equipment weights). TopDegreeAtCap widens
the window of a killer at MatchLevelCap to every row of the top degree
(required level 24-29), which a level-30 character wears but the
[level-3, level] window stops offering at 27.
================
*/
type LootTuning struct {
	ExtraEquipment int
	TopDegreeAtCap bool
}

/*
================
LiveLootTuning

The tuning every match kill rolls (LootPlan): liveExtraEquipment
equipment-only stacks and the top-degree window at the level cap. The
stage-9 set report measured it: about half the players of a 5- or
16-player match hold a complete set at phase 5, against none under the
zero tuning.
================
*/
func LiveLootTuning() LootTuning {
	return LootTuning{ExtraEquipment: liveExtraEquipment, TopDegreeAtCap: true}
}

/*
================
lootUniverse

Every Moon/Sun row a build of the table wears, once each, in a fixed
order (build order, male first, then equipmentRows' order), and the
window rows per killer level (1..MatchLevelCap) and family.
================
*/
type lootUniverse struct {
	rows   []equipmentRow
	window [MatchLevelCap + 1][lootShield + 1][]equipmentRow
}

var randomLoot = sync.OnceValue(buildLootUniverse)

/*
================
buildLootUniverse
================
*/
func buildLootUniverse() *lootUniverse {
	u := &lootUniverse{}
	seen := map[string]bool{}
	for _, build := range Builds() {
		for _, female := range []bool{false, true} {
			for _, row := range equipmentRows(Killer{Build: build, Female: female}) {
				if seen[row.base] {
					continue
				}
				seen[row.base] = true
				u.rows = append(u.rows, row)
			}
		}
	}
	for level := int64(1); level <= MatchLevelCap; level++ {
		for _, row := range u.rows {
			if InLootWindow(level, row.level) {
				u.window[level][row.family] = append(u.window[level][row.family], row)
			}
		}
	}
	return u
}

/*
================
lootUniverse.windowRows

The rows of family (or of every family when anyFamily) a killer of
level may drop under tuning.
================
*/
func (u *lootUniverse) windowRows(level int64, family lootEntry, anyFamily bool, tuning LootTuning) []equipmentRow {
	topDegree := tuning.TopDegreeAtCap && level >= MatchLevelCap
	if level >= 1 && level <= MatchLevelCap && !anyFamily && !topDegree {
		return u.window[level][family]
	}
	var out []equipmentRow
	for _, row := range u.rows {
		inWindow := InLootWindow(level, row.level) || topDegree && row.degree == maxDegree
		if inWindow && (anyFamily || row.family == family) {
			out = append(out, row)
		}
	}
	return out
}

/*
================
equipmentDraw

One equipment draw of family at a killer level: a skin at skinChance in
skinDraws, else a row of the family drawn uniformly from the window
(every family's rows when the family has none there), Moon or Sun by
rollSeal; false when the window holds no row at all (levels 5-7).
================
*/
func equipmentDraw(level int64, family lootEntry, tuning LootTuning, rng *rand.Rand) (Drop, bool) {
	if rng.IntN(skinDraws) < skinChance {
		skins := SkinCodenames()
		return Drop{Codename: skins[rng.IntN(len(skins))], Count: 1}, true
	}
	u := randomLoot()
	rows := u.windowRows(level, family, false, tuning)
	if len(rows) == 0 {
		rows = u.windowRows(level, family, true, tuning)
	}
	if len(rows) == 0 {
		return Drop{}, false
	}
	row := rows[rng.IntN(len(rows))]
	return Drop{Codename: row.base + "_" + rollSeal(rng).suffix(), Count: 1}, true
}

/*
================
equipmentFamilyDraw

A family drawn by the combined table's equipment weights (weapon 14,
armor 18, accessory 10, shield 3 of 45): what an extra equipment-only
stack (LootTuning.ExtraEquipment) rolls.
================
*/
func equipmentFamilyDraw(rng *rand.Rand) lootEntry {
	weights := lootWeights()
	total := 0
	for family := lootWeapon; family <= lootShield; family++ {
		total += weights[family]
	}
	roll := rng.IntN(total)
	for family := lootWeapon; family < lootShield; family++ {
		if roll < weights[family] {
			return family
		}
		roll -= weights[family]
	}
	return lootShield
}

/*
================
LootRowCount

How many distinct Moon/Sun rows (without the seal) the random match
loot can drop over every level.
================
*/
func LootRowCount() int {
	return len(randomLoot().rows)
}

/*
================
LootWindowCodenames

Every Moon and Sun codename the random match loot drops for a killer of
level (live tuning), in row order: what a fixture or a tool must know of
itemdata to stand a kill's equipment.
================
*/
func LootWindowCodenames(level int64) []string {
	var out []string
	for _, row := range randomLoot().windowRows(level, lootWeapon, true, LootTuning{}) {
		out = append(out, row.base+"_"+SealMoon.suffix(), row.base+"_"+SealSun.suffix())
	}
	return out
}

/*
================
IsLootRow

Whether a Moon or Sun codename is a row of the random match loot.
================
*/
func IsLootRow(codename string) bool {
	base, _ := lootBaseOf(codename)
	return slices.ContainsFunc(randomLoot().rows, func(row equipmentRow) bool { return row.base == base })
}

//============================================================================

/*
================
SkinCodenames

The avatar rows a skin drop draws from (itemdata TID 3/1/13, TID4 1 hat,
2 dress, 3 attachment; both genders; the licensed test checks every
row): the item-mall avatar sets of v1.150. The event and job-flag
avatars (TID4 4) and the MABT hats (MaxMagic 0, no blue room) are left
out. A fresh slice on every call.
================
*/
func SkinCodenames() []string {
	var out []string
	for _, set := range []string{"EU_WEDDING", "HALLOWEEN", "PIRATE", "FUR", "KNIGHT"} {
		for _, body := range []string{"M", "W"} {
			out = append(out, "ITEM_MALL_AVATAR_"+body+"_"+set+"_HAT", "ITEM_MALL_AVATAR_"+body+"_"+set)
		}
	}
	for _, dress := range []string{"ANGEL_WING_DRESS", "DEVIL_WING_DRESS", "HALLOWEEN_ATTACH"} {
		for _, body := range []string{"M", "W"} {
			out = append(out, "ITEM_MALL_AVATAR_"+body+"_"+dress)
		}
	}
	return out
}

/*
================
IsSkin

Whether a codename is one of SkinCodenames.
================
*/
func IsSkin(codename string) bool {
	return strings.HasPrefix(codename, "ITEM_MALL_AVATAR_") && slices.Contains(SkinCodenames(), codename)
}
