/*
===========================================================================

botprofile.go - bot difficulty profiles and the gear a bot collects

Port-only, not native (see zone.go; docs/battle-royale.md "Stage 7").
The GM picks a difficulty with the bot switch (0x7FB4 v2); everything a
difficulty changes is one row of plain data here, so the brain, the
wiring and the economy simulation read the same numbers:

	           sense  damage  HP    strike  level  potion  thinks  hunts
	Easy        40 m   x0.6   x0.8   1.8 s   x0.8    30 %   1.0 s    20 %
	Normal      70 m   x1.0   x1.0   1.3 s   x1.0    70 %   1.0 s    40 %
	Hard       110 m   x1.5   x1.3   0.9 s   x1.3   100 %   0.5 s    60 %

"thinks" is how often a bot of the difficulty decides (the match tick is
0.5 s; bots are staggered across it), "hunts" the share of its roam
goals it takes toward the nearest real player. Stage 8c adds the
utility brain's three personality numbers (botbrain.go):

	           aggression  bravery    retreats below  alert at  alert decay
	Easy          x0.8     -3 levels       15 % HP       0.9       10 s
	Normal        x1.0      0              30 % HP       0.6       20 s
	Hard          x1.2     +3 levels       35 % HP       0.4       30 s

aggression multiplies every fight's score, bravery is added to the bot's
own power when it weighs a foe, and a bot under its retreat share that
is losing a fight walks away from it (an Easy bot fights almost to the
death; a Hard bot breaks off sooner, drinks and comes back).

A bot also gears up (owner requirement 2). Every kill it makes rolls the
match loot table at its level (LootPlan, as a player's kill would) for
the build it was dealt; the bot keeps the best weapon of its build's
kinds and the best armor or shield it has seen, ranked as the auto-equip
ranks weapons (2 x degree, +1 for Seal of Sun). Its strength follows:

	attack  x (1 + GearAttackPerRank  x (weapon rank - StartWeaponRank))
	defence x (1 + GearDefencePerRank x  armor rank)

so a bot that reached a degree-4 Sun weapon (rank 9) hits x1.48 and one
in degree-4 Sun armor (rank 9) takes hits / 1.54. Every bot starts with
the players' kit weapon, a degree-1 Seal of Sun (rank 3), and no armor.

===========================================================================
*/
package battleroyale

import (
	"strconv"
	"strings"
)

/*
================
BotDifficulty

The switch's difficulty byte: 0 easy, 1 normal, 2 hard.
================
*/
type BotDifficulty uint8

const (
	BotEasy BotDifficulty = iota
	BotNormal
	BotHard
	botDifficulties
)

const (
	// DefaultBotCount is the bot count of a stage-5 switch (one byte) and
	// of a lobby nobody configured.
	DefaultBotCount = 20
	// MaxBotCount is the most bots a match may hold (0x7FB4 v2 count).
	MaxBotCount = 200

	// BotHealBelow is the HP share under which a bot reaches for a potion,
	// BotPotionCooldownMs how long it waits before the next attempt.
	BotHealBelow        = 0.4
	BotPotionCooldownMs = 8000
	// StartWeaponRank is the kit weapon's rank (degree-1 Sun).
	StartWeaponRank = 2*1 + int(SealSun)
	// GearAttackPerRank and GearDefencePerRank are the gear formula's
	// slopes (file header).
	GearAttackPerRank  = 0.08
	GearDefencePerRank = 0.06
	// botBaseStrikeMs is the strike interval the stage-5 numbers were tuned
	// at (Normal): a faster profile hits players proportionally harder.
	botBaseStrikeMs = 1300
)

/*
================
BotProfile

One difficulty's numbers. SenseM is how far (metres) a bot notices a
player or another bot; DamageScale and HPScale multiply what it deals
and what its bar holds; StrikeMs is its strike interval; LevelPace scales
its level-up pace; PotionChance is the chance (percent) a potion attempt
succeeds; RoamTowardPlayers is the chance (percent) an idle bot roams
toward the nearest real player instead of a random point of the zone;
ThinkMs is how often it decides. Aggression, Bravery and RetreatBelow
weigh the utility brain's fights, AlertThreshold and AlertDecayMs its
alert meter (file header).
================
*/
type BotProfile struct {
	SenseM            float64
	DamageScale       float64
	HPScale           float64
	StrikeMs          int64
	LevelPace         float64
	PotionChance      int
	RoamTowardPlayers int
	ThinkMs           int64
	Aggression        float64
	Bravery           float64
	RetreatBelow      float64
	AlertThreshold    float64
	AlertDecayMs      int64
}

/*
================
botProfiles

The three profiles (file header table), indexed by BotDifficulty.
================
*/
func botProfiles() [botDifficulties]BotProfile {
	return [botDifficulties]BotProfile{
		BotEasy: {SenseM: 40, DamageScale: 0.6, HPScale: 0.8, StrikeMs: 1800, LevelPace: 0.8, PotionChance: 30, RoamTowardPlayers: 20, ThinkMs: 1000,
			Aggression: 0.8, Bravery: -3, RetreatBelow: 0.15, AlertThreshold: 0.9, AlertDecayMs: 10_000},
		BotNormal: {SenseM: 70, DamageScale: 1.0, HPScale: 1.0, StrikeMs: 1300, LevelPace: 1.0, PotionChance: 70, RoamTowardPlayers: 40, ThinkMs: 1000,
			Aggression: 1.0, Bravery: 0, RetreatBelow: 0.3, AlertThreshold: 0.6, AlertDecayMs: 20_000},
		BotHard: {SenseM: 110, DamageScale: 1.5, HPScale: 1.3, StrikeMs: 900, LevelPace: 1.3, PotionChance: 100, RoamTowardPlayers: 60, ThinkMs: 500,
			Aggression: 1.2, Bravery: 3, RetreatBelow: 0.35, AlertThreshold: 0.4, AlertDecayMs: 30_000},
	}
}

/*
================
ProfileFor

The profile of a difficulty; an unknown value is Normal.
================
*/
func ProfileFor(d BotDifficulty) BotProfile {
	if d >= botDifficulties {
		d = BotNormal
	}
	return botProfiles()[d]
}

/*
================
ValidBotDifficulty
================
*/
func ValidBotDifficulty(d BotDifficulty) bool {
	return d < botDifficulties
}

/*
================
BotProfile.SenseRange

The sense range in world units.
================
*/
func (p BotProfile) SenseRange() float64 {
	return p.SenseM * UnitsPerMeter
}

/*
================
BotProfile.PlayerHitScale

What multiplies a bot's native hits on a player beside its gear: the
difficulty's damage, and its strike pace against the Normal pace (the
native attack keeps the row's own cadence, so a faster profile hits
harder instead).
================
*/
func (p BotProfile) PlayerHitScale() float64 {
	return p.DamageScale * float64(botBaseStrikeMs) / float64(max(p.StrikeMs, 1))
}

//============================================================================

/*
================
BotGear

The best gear a bot has looted: the weapon rank (2 x degree + 1 for Sun)
of its build's kinds and the armor rank of any armor or shield row. The
zero value is no loot yet; NewBotGear is the kit.
================
*/
type BotGear struct {
	WeaponRank int
	ArmorRank  int
}

/*
================
NewBotGear

A fresh bot's gear: the kit weapon, no armor.
================
*/
func NewBotGear() BotGear {
	return BotGear{WeaponRank: StartWeaponRank}
}

/*
================
BotGear.Absorb

Keeps the better of the gear and every equipment drop of a kill for the
bot's build; returns whether anything improved.
================
*/
func (g *BotGear) Absorb(build Build, drops []Drop) bool {
	improved := false
	for _, drop := range drops {
		if rank := WeaponRank(build, drop.Codename); rank > g.WeaponRank {
			g.WeaponRank, improved = rank, true
			continue
		}
		if rank := ArmorRank(drop.Codename); rank > g.ArmorRank {
			g.ArmorRank, improved = rank, true
		}
	}
	return improved
}

/*
================
BotGear.WeaponDegree

The degree (1-4) of the best weapon.
================
*/
func (g BotGear) WeaponDegree() int {
	return max(g.WeaponRank/2, 1)
}

/*
================
BotGear.ArmorDegree

The degree (0 none, 1-4) of the best armor.
================
*/
func (g BotGear) ArmorDegree() int {
	return g.ArmorRank / 2
}

/*
================
BotGear.AttackScale

The gear's attack multiplier (file header).
================
*/
func (g BotGear) AttackScale() float64 {
	return 1 + GearAttackPerRank*float64(max(g.WeaponRank-StartWeaponRank, 0))
}

/*
================
BotGear.DefenceScale

The gear's defence divisor (file header): hits taken are divided by it.
================
*/
func (g BotGear) DefenceScale() float64 {
	return 1 + GearDefencePerRank*float64(max(g.ArmorRank, 0))
}

/*
================
ArmorRank

The rank (2 x degree, +1 for Sun) of a Moon/Sun armor or shield
codename (ITEM_{CH|EU}_..._{HEAVY|LIGHT|CLOTHES|SHIELD}..._0d..._{B|C}_RARE),
0 for anything else (weapons, accessories, consumables).
================
*/
func ArmorRank(codename string) int {
	seal := SealMoon
	base, ok := strings.CutSuffix(codename, "_"+SealMoon.suffix())
	if !ok {
		base, ok = strings.CutSuffix(codename, "_"+SealSun.suffix())
		seal = SealSun
	}
	if !ok {
		return 0
	}
	words := strings.Split(base, "_")
	armor := false
	for _, word := range words {
		switch word {
		case "HEAVY", "LIGHT", "CLOTHES", "SHIELD":
			armor = true
		}
	}
	if !armor {
		return 0
	}
	for _, word := range words {
		if degree, err := strconv.Atoi(word); err == nil && len(word) == 2 && degree >= 1 && degree <= maxDegree {
			return 2*degree + int(seal)
		}
	}
	return 0
}

//============================================================================

/*
================
BotLevelPaced

A bot's level after elapsedMs alive with the given kills at a
difficulty's pace: BotLevel's time and wolf terms scaled by pace (the
participant kills keep their two levels each), 1 to MatchLevelCap.
================
*/
func BotLevelPaced(elapsedMs int64, monsterKills, participantKills int, pace float64) int {
	earned := pace * (float64(max(elapsedMs, 0))/botLevelMs + float64(max(monsterKills, 0))/botMonsterKillsPerLevel)
	level := 1 + int64(earned) + int64(max(participantKills, 0)*botPlayerKillLevels)
	return int(min(level, MatchLevelCap))
}

/*
================
BotScaledMaxHP

The HP a bot of level holds at a difficulty: BotMaxHP x HPScale.
================
*/
func BotScaledMaxHP(level int, p BotProfile) int64 {
	return max(int64(float64(BotMaxHP(level))*p.HPScale), 1)
}

/*
================
PotionHeal

What one of a level's HP potions heals (itemdata 120/220/370/570/820 for
tiers 1-5, PotionTier): the potion a bot drinks.
================
*/
func PotionHeal(level int64) int64 {
	return [...]int64{120, 220, 370, 570, 820}[PotionTier(level)-1]
}
