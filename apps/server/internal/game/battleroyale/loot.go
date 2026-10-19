/*
===========================================================================

loot.go - the match monsters and what they drop

Port-only, not native (see zone.go). A match layer holds one monster
only: the level-5 Baroi wolf (characterdata 5854 MOB_EU_BARUSWOLF_CLON,
119 HP). The drop spreads ArenaMonsters of them uniformly over the
walkable Roc ground (groundmask.go; no nests, no respawn); the zone
supply (economy.go) keeps the circle stocked afterwards, never past
MaxLiveMonsters alive at once, and every phase stands one supply-drop
elite, a giant of the same wolf, at the next circle's centre. Every plan
draws from a caller-owned *rand.Rand, so a seeded match places and drops
the same things.

Loot is one combined match table rolled per kill (LootPlan, stage 6):
three stacks a kill and a fourth at extraStackChance, each a weighted
draw of HP, MP and vigor potions, pills and Seal of Moon or Seal of Sun
equipment (Moon two draws in three, Sun one in three), then the live
tuning's five equipment-only stacks (LiveLootTuning); never the
monster's own table and never any other equipment. Everything follows
the KILLER's level at the kill, never the monster's:

  - Equipment (stage 9, lootrandom.go) is random across every build,
    race and gender: any Moon/Sun row some build of the table wears
    (every build's weapon kinds, both shields, the armor of every
    race, gender and build class, both races' accessories) whose
    itemdata required level lies in [level - LootWindow, level] (at
    the level cap, any row of the top degree: LiveLootTuning); the
    killer's own build plays no part. About one equipment draw in a
    hundred is a skin (an avatar hat, dress or attachment) instead.
    The required level of every Moon/Sun row of degrees 1-4 is
    RequiredLevel: degree 1 is level 1, degree d > 1 is 8 (d - 1) plus
    the part's offset (weapons, shields, rings, hands 0, shoulders 1,
    feet and earrings 2, head and chest 3, legs 4 and necklaces 4, body
    5); the licensed test checks that against every itemdata row. A
    level-20 player therefore never sees a level-1 item. Levels 5-7 have
    no row in their window (degree 1 is 1, degree 2 starts at 8): their
    equipment draws fall back to potions.
  - HP, MP and vigor potions by PotionTier: the largest itemdata tier
    (heal 120/220/370/570/820) that restores at most 30 % of the match
    character's max HP at that level, so herbs only at 1-4 and the
    special potion from 26. Pills (cure-all 1-4) by the weapon degree
    bands.

The blue options and white stats of the dropped equipment are the
wiring's (action/battleroyale_loot.go): at least three blues and every
white stat at 50 % or more; a skin carries at least two blues of its
native avatar pool.

===========================================================================
*/
package battleroyale

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
)

const (
	// MatchMonsterCodename is the only monster of a match: the Baroi
	// wolf, level 5 (characterdata 5854).
	MatchMonsterCodename = "MOB_EU_BARUSWOLF_CLON"
	// MatchMonsterHP is that row's max HP (characterdata column 59).
	MatchMonsterHP = 119

	// The drop's wolves: a fixed count over the fixed all-Roc arena,
	// about one wolf per 3 300 m2 of its 5 km2 walkable ground (stage 6:
	// 2000 before; the local supply carries the farming).
	ArenaMonsters = 1500
	// MaxLiveMonsters bounds the monsters alive in one match layer at
	// once (wolves, elites and bots): the supply stops at it (the host
	// has little memory to spare). The stage-6 simulation peaks well
	// below it.
	MaxLiveMonsters = 3000

	// The supply-drop elite: a giant (monster/gradescale.go gradeGiant,
	// three times the body) of the match wolf.
	supplyEliteRarity = 4

	// A kill drops lootStacks stacks, one more at extraStackChance
	// percent (3.5 on average; the owner's "at least 3").
	lootStacks       = 3
	extraStackChance = 50
	// Potions drop two or three at a time.
	potionsMin = 2
	potionsMax = 3

	// LootWindow: equipment drops only from rows whose required level is
	// within LootWindow levels at or below the killer's.
	LootWindow = 3

	// The supply drop: a Moon or Sun weapon of the window, five HP and
	// MP potions and three vigor potions, then a Moon or Sun armor piece
	// and accessory of the window at supplyExtraChance each: 3 to 6
	// stacks.
	supplyPotions     = 5
	supplyVigor       = 3
	supplyExtraChance = 50

	// The weapon degree bands by level (itemdata ReqLevel of the weapons,
	// shields and rings: 1, 8, 16, 24); a degree above 1 starts at
	// degreeLevelStep x (degree - 1).
	degree2Level    = 8
	degree3Level    = 16
	degree4Level    = 24
	degreeLevelStep = 8
	maxDegree       = 4
	// Pills exist in degrees 1-4 (ITEM_ETC_CURE_ALL_01..04).
	maxPillDegree = 4

	percentRoll = 100

	// The seal of an equipment drop: Moon moonDraws in sealDraws, Sun the
	// rest (stage 4: Sun a bit rarer than Moon, 2/3 to 1/3).
	sealDraws = 3
	moonDraws = 2
)

/*
================
Seal

The rarity row of a match equipment drop: Seal of Moon (the _B_RARE
itemdata rows) or Seal of Sun (_C_RARE).
================
*/
type Seal int

const (
	SealMoon Seal = iota
	SealSun
)

/*
================
Seal.suffix

The itemdata codename suffix of a seal.
================
*/
func (s Seal) suffix() string {
	if s == SealSun {
		return "C_RARE"
	}
	return "B_RARE"
}

/*
================
rollSeal

Moon at moonDraws in sealDraws, else Sun.
================
*/
func rollSeal(rng *rand.Rand) Seal {
	if rng.IntN(sealDraws) < moonDraws {
		return SealMoon
	}
	return SealSun
}

/*
================
MonsterSpawn

One monster to spawn into the match layer: its characterdata codename,
its point in the arena and its spawn rarity byte (0 ordinary).
================
*/
type MonsterSpawn struct {
	Codename string
	At       Point
	Rarity   uint8
}

/*
================
MonsterPlan

The drop's monsters: ArenaMonsters wolves uniform over ground (the
walkable Roc cells of the arena); none when ground is empty.
================
*/
func MonsterPlan(ground GroundCells, rng *rand.Rand) []MonsterSpawn {
	return planWolves(ground, ArenaMonsters, rng)
}

/*
================
planWolves
================
*/
func planWolves(ground GroundCells, count int, rng *rand.Rand) []MonsterSpawn {
	if ground.Len() == 0 {
		return nil
	}
	out := make([]MonsterSpawn, 0, count)
	for range count {
		at, _ := ground.Sample(rng)
		out = append(out, MonsterSpawn{Codename: MatchMonsterCodename, At: at})
	}
	return out
}

/*
================
SupplyElite

The supply-drop elite of a zone phase: a giant match wolf at the target
circle's centre.
================
*/
func SupplyElite(target Circle) MonsterSpawn {
	return MonsterSpawn{Codename: MatchMonsterCodename, At: target.Center, Rarity: supplyEliteRarity}
}

/*
================
IsSupplyElite

Whether a spawned match monster is a supply-drop elite: the codename and
the giant rarity together (an ordinary wolf is not one).
================
*/
func IsSupplyElite(codename string, rarity uint8) bool {
	return codename == MatchMonsterCodename && rarity == supplyEliteRarity
}

//============================================================================

/*
================
Drop

One dropped stack: an itemdata codename and its count.
================
*/
type Drop struct {
	Codename string
	Count    int64
}

/*
================
Killer

What the match code reads of a character: its build, its level and
whether its body is female (the armor rows are gendered). The loot
table reads the level only (stage 9: random loot); the build and the
body decide what the character can wear (lootset.go SlotOf, the
auto-equip) and the ultimate weapon's kind.
================
*/
type Killer struct {
	Build  Build
	Level  int64
	Female bool
}

/*
================
lootEntry

One row of the combined match table: its weight out of percentRoll.
================
*/
type lootEntry int

const (
	lootHP lootEntry = iota
	lootMP
	lootVigor
	lootPill
	lootWeapon
	lootArmor
	lootAccessory
	lootShield
)

/*
================
lootWeights

The combined table's weights, summing to percentRoll (stage 4): weapons
14 % of the stacks, armor 18 %, accessories 10 %, shields 3 %:
equipment 45 %, consumables 55 %.
A fresh table on every call.
================
*/
func lootWeights() [lootShield + 1]int {
	return [lootShield + 1]int{
		lootHP: 20, lootMP: 17, lootVigor: 10, lootPill: 8,
		lootWeapon: 14, lootArmor: 18, lootAccessory: 10, lootShield: 3,
	}
}

/*
================
LootPlan

What a match wolf drops for killer: lootStacks stacks of the combined
table, one more at extraStackChance, then the live tuning's
equipment-only stacks (LiveLootTuning: five), every equipment row in
the window or, at the level cap, of the top degree. Only the killer's
level counts: two killers of the same level draw from the same table
(stage 9).
================
*/
func LootPlan(killer Killer, rng *rand.Rand) []Drop {
	return LootPlanTuned(killer, LiveLootTuning(), rng)
}

/*
================
LootPlanTuned

LootPlan under a LootTuning (lootrandom.go): the zero tuning is the
stage-9 table; ExtraEquipment equipment-only stacks follow the table's
stacks.
================
*/
func LootPlanTuned(killer Killer, tuning LootTuning, rng *rand.Rand) []Drop {
	count := lootStacks
	if rng.IntN(percentRoll) < extraStackChance {
		count++
	}
	out := make([]Drop, 0, count+tuning.ExtraEquipment)
	for range count {
		out = append(out, rollLoot(killer.Level, tuning, rng))
	}
	for range tuning.ExtraEquipment {
		if drop, ok := equipmentDraw(killer.Level, equipmentFamilyDraw(rng), tuning, rng); ok {
			out = append(out, drop)
		}
	}
	return out
}

/*
================
rollLoot

One weighted draw of the combined table at the killer's level. An
equipment entry (weapon, armor, accessory, shield: 45 %) draws a random
row of its family in the window (lootrandom.go equipmentDraw); an
equipment draw with no row in the window (levels 5-7) drops an HP
potion instead.
================
*/
func rollLoot(level int64, tuning LootTuning, rng *rand.Rand) Drop {
	roll, entry := rng.IntN(percentRoll), lootHP
	for i, weight := range lootWeights() {
		if roll < weight {
			entry = lootEntry(i)
			break
		}
		roll -= weight
	}
	tier := PotionTier(level)
	potions := int64(potionsMin + rng.IntN(potionsMax-potionsMin+1))
	switch entry {
	case lootHP:
		return potionDrops(tier, potions)[0]
	case lootMP:
		return potionDrops(tier, potions)[1]
	case lootVigor:
		return vigorDrop(tier, 1)
	case lootPill:
		return pillDrop(level, 1)
	}
	if drop, ok := equipmentDraw(level, entry, tuning, rng); ok {
		return drop
	}
	return potionDrops(tier, potions)[0]
}

/*
================
SupplyDropLoot

The supply-drop elite's loot for its killer: a random Moon or Sun weapon
of the window (any equipment of the window when no weapon row is in
it), five HP and MP potions and three vigor potions of its tier, then a
random armor piece and accessory of the window at supplyExtraChance
each: 3 to 6 stacks. Like every match drop it ignores the killer's
build (stage 9); the airdrop's ultimate weapon is the build's
(airdrop.go). Its window is the live one (a level-30 killer draws the
top degree), without the extra stacks.
================
*/
func SupplyDropLoot(killer Killer, rng *rand.Rand) []Drop {
	tier := PotionTier(killer.Level)
	window := LootTuning{TopDegreeAtCap: LiveLootTuning().TopDegreeAtCap}
	var out []Drop
	if drop, ok := equipmentDraw(killer.Level, lootWeapon, window, rng); ok {
		out = append(out, drop)
	}
	out = append(out, potionDrops(tier, supplyPotions)...)
	out = append(out, vigorDrop(tier, supplyVigor))
	for _, family := range []lootEntry{lootArmor, lootAccessory} {
		if rng.IntN(percentRoll) >= supplyExtraChance {
			continue
		}
		if drop, ok := equipmentDraw(killer.Level, family, window, rng); ok {
			out = append(out, drop)
		}
	}
	return out
}

/*
================
potionDrops, vigorDrop, pillDrop

The consumable rows of each family.
================
*/
func potionDrops(tier int, count int64) []Drop {
	return []Drop{
		{Codename: fmt.Sprintf("ITEM_ETC_HP_POTION_%02d", tier), Count: count},
		{Codename: fmt.Sprintf("ITEM_ETC_MP_POTION_%02d", tier), Count: count},
	}
}

func vigorDrop(tier int, count int64) Drop {
	return Drop{Codename: fmt.Sprintf("ITEM_ETC_ALL_POTION_%02d", tier), Count: count}
}

func pillDrop(level int64, count int64) Drop {
	return Drop{Codename: fmt.Sprintf("ITEM_ETC_CURE_ALL_%02d", min(DegreeForLevel(level), maxPillDegree)), Count: count}
}

/*
================
potionTierLevels

The first level of each HP/MP/vigor potion tier (itemdata
ITEM_ETC_*_POTION_01..05, heal 120/220/370/570/820, column 61 the tier):
the largest heal that is at most 30 % of the match character's max HP
(BotMaxHP, the x2.5 curve). TestPotionTiers re-derives the table.
================
*/
func potionTierLevels() [5]int64 {
	return [5]int64{1, 5, 12, 19, 26}
}

/*
================
PotionTier

The HP, MP and vigor potion tier (1-5) a killer level drops.
================
*/
func PotionTier(level int64) int {
	tier := 1
	for i, first := range potionTierLevels() {
		if level >= first {
			tier = i + 1
		}
	}
	return tier
}

//============================================================================

/*
================
equipmentRow

One Moon/Sun equipment row of a build without its seal suffix, its
required level, its table family, its set slot and its degree.
================
*/
type equipmentRow struct {
	base   string
	level  int64
	family lootEntry
	slot   SetSlot
	degree int
}

/*
================
armorPartOffsets, accessoryOffsets

The required-level offset of each armor part and accessory word over the
degree's base level (itemdata: hands AA 8/16/24, shoulders SA 9/17/25,
feet FA 10/18/26, head HA and crown CA 11/19/27, legs LA 12/20/28, chest
BA 13/21/29; rings 8, earrings 10, necklaces 12 at degree 2). Fresh
maps: no package state.
================
*/
func armorPartOffsets() map[string]int64 {
	return map[string]int64{"HA": 3, "CA": 3, "SA": 1, "BA": 5, "LA": 4, "AA": 0, "FA": 2}
}

func accessoryOffsets() map[string]int64 {
	return map[string]int64{"EARRING": 2, "NECKLACE": 4, "RING": 0}
}

/*
================
armorParts

The itemdata codename suffixes of the seven armor rows of a degree in a
fixed order: HA helm and CA crown (both TID4 1, the head socket), SA
shoulders, BA chest, LA legs, AA hands, FA feet. Six sockets, seven
rows: lootset.go maps them to the set slots.
================
*/
func armorParts() []string {
	return []string{"HA", "CA", "SA", "BA", "LA", "AA", "FA"}
}

/*
================
degreeLevel

The required level of a degree's row with offset: 1 at degree 1 (every
degree-1 row needs level 1), degreeLevelStep x (degree - 1) + offset
above.
================
*/
func degreeLevel(degree int, offset int64) int64 {
	if degree <= 1 {
		return 1
	}
	return int64(degree-1)*degreeLevelStep + offset
}

/*
================
equipmentRows

Every Moon/Sun row of degrees 1-4 the killer's build may drop: its weapon
kinds, its shield (shield builds), the armor of its race, gender and
class, and the race's earrings, necklaces and rings.
================
*/
func equipmentRows(killer Killer) []equipmentRow {
	race := racePrefix(killer.Build.Race)
	gender := "M"
	if killer.Female {
		gender = "W"
	}
	parts, accessories := armorPartOffsets(), accessoryOffsets()
	partSlots, wordSlots := armorPartSlots(), accessoryWordSlots()
	var out []equipmentRow
	for degree := 1; degree <= maxDegree; degree++ {
		for _, kind := range killer.Build.WeaponKinds {
			if name, ok := SealWeaponCodename(killer.Build.Race, kind, degree, SealMoon); ok {
				out = append(out, equipmentRow{strings.TrimSuffix(name, "_"+SealMoon.suffix()), degreeLevel(degree, 0), lootWeapon,
					SlotWeapon, degree})
			}
		}
		if killer.Build.Shield {
			out = append(out, equipmentRow{fmt.Sprintf("ITEM_%s_SHIELD_%02d", race, degree), degreeLevel(degree, 0), lootShield,
				SlotShield, degree})
		}
		for _, part := range armorParts() {
			out = append(out, equipmentRow{fmt.Sprintf("ITEM_%s_%s_%s_%02d_%s", race, gender, killer.Build.Armor.word(), degree, part),
				degreeLevel(degree, parts[part]), lootArmor, partSlots[part], degree})
		}
		for _, word := range []string{"EARRING", "NECKLACE", "RING"} {
			out = append(out, equipmentRow{fmt.Sprintf("ITEM_%s_%s_%02d", race, word, degree), degreeLevel(degree, accessories[word]),
				lootAccessory, wordSlots[word], degree})
		}
	}
	return out
}

/*
================
InLootWindow

Whether a row requiring required may drop for a killer of level:
level - LootWindow <= required <= level.
================
*/
func InLootWindow(level, required int64) bool {
	return required <= level && required >= level-LootWindow
}

/*
================
RequiredLevel

The required level of a Moon or Sun equipment codename of degrees 1-4
(ITEM_{CH|EU}_..._0d[_part]_{B|C}_RARE), as itemdata gates it (the EU
armor's mastery gate, trained to the character level, holds the same
value); false for any other codename.
================
*/
func RequiredLevel(codename string) (int64, bool) {
	base, ok := strings.CutSuffix(codename, "_"+SealMoon.suffix())
	if !ok {
		base, ok = strings.CutSuffix(codename, "_"+SealSun.suffix())
	}
	if !ok || (!strings.HasPrefix(base, "ITEM_CH_") && !strings.HasPrefix(base, "ITEM_EU_")) {
		return 0, false
	}
	words := strings.Split(base, "_")
	for i, word := range words {
		degree, err := strconv.Atoi(word)
		if err != nil || len(word) != 2 || degree < 1 || degree > maxDegree {
			continue
		}
		offset := int64(0)
		if i+1 < len(words) {
			offset = armorPartOffsets()[words[i+1]]
		}
		offset += accessoryOffsets()[words[i-1]]
		return degreeLevel(degree, offset), true
	}
	return 0, false
}

/*
================
DegreeForLevel

The weapon degree band of a level (1 at 1-7, 2 at 8-15, 3 at 16-23, 4
at 24-30): the pill tier, and the degree the auto-equip can wear.
================
*/
func DegreeForLevel(level int64) int {
	switch {
	case level >= degree4Level:
		return maxDegree
	case level >= degree3Level:
		return 3
	case level >= degree2Level:
		return 2
	}
	return 1
}

/*
================
SunWeaponCodename

The ITEM_{CH|EU}_{KIND}_0d_C_RARE codename of a Seal of Sun weapon, or
false for a kind the race does not wield.
================
*/
func SunWeaponCodename(race Race, kind uint8, degree int) (string, bool) {
	return SealWeaponCodename(race, kind, degree, SealSun)
}

/*
================
SealWeaponCodename

The ITEM_{CH|EU}_{KIND}_0d_{B|C}_RARE codename of a Seal of Moon or Sun
weapon, or false for a kind the race does not wield.
================
*/
func SealWeaponCodename(race Race, kind uint8, degree int, seal Seal) (string, bool) {
	name, ok := weaponKindNames()[kind]
	if !ok || degree < 1 || degree > maxDegree {
		return "", false
	}
	chinese := kind <= WeaponBow
	if chinese != (race == RaceChinese) {
		return "", false
	}
	return fmt.Sprintf("ITEM_%s_%s_%02d_%s", racePrefix(race), name, degree, seal.suffix()), true
}

/*
================
WeaponRank

How good a Seal of Moon or Sun weapon of the build's kinds is: 2 x its
degree, plus one for Sun (a higher degree always wins, Sun beats Moon of
the same degree), or 0 for any other codename. What the auto-equip
compares.
================
*/
func WeaponRank(build Build, codename string) int {
	for _, kind := range build.WeaponKinds {
		for degree := 1; degree <= maxDegree; degree++ {
			for _, seal := range []Seal{SealMoon, SealSun} {
				if name, ok := SealWeaponCodename(build.Race, kind, degree, seal); ok && name == codename {
					return 2*degree + int(seal)
				}
			}
		}
	}
	return 0
}

/*
================
weaponKindNames

The itemdata codename word of each weapon TID4 kind.
================
*/
func weaponKindNames() map[uint8]string {
	return map[uint8]string{
		WeaponSword: "SWORD", WeaponBlade: "BLADE", WeaponSpear: "SPEAR", WeaponGlaive: "TBLADE", WeaponBow: "BOW",
		WeaponOneHandEU: "SWORD", WeaponTwoHandEU: "TSWORD", WeaponAxe: "AXE", WeaponDarkStaff: "DARKSTAFF",
		WeaponTwoHandStaff: "TSTAFF", WeaponCrossbow: "CROSSBOW", WeaponDagger: "DAGGER", WeaponHarp: "HARP", WeaponStaff: "STAFF",
	}
}

/*
================
racePrefix
================
*/
func racePrefix(race Race) string {
	if race == RaceChinese {
		return "CH"
	}
	return "EU"
}
