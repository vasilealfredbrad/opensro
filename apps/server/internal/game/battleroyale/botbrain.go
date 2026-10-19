/*
===========================================================================

botbrain.go - what a bot does next (stages 7, 8 and 8c)

Port-only, not native (see zone.go; docs/battle-royale.md "Stage 7",
"Stage 8" and "Stage 8c"). A bot is a real match character (stage 8): it
fights with the ordinary player commands (a skill press or the basic
attack on a target, which the server's own approach and continuation
carry out), walks with ground clicks, picks up drops and drinks potions.
The wiring gathers what it senses (BotSenses: where it stands, its
level, worn gear, HP and MP, the zone, the players and bots near it with
their level, gear, HP and whom they attack, the target its attack holds,
the nearest wolf, the nearest wanted drop, the nearest real player) and
asks its BotMind for one decision (BotIntent). The mind is plain data
and the decision a pure function of the senses, the difficulty profile
and a seeded draw, so the rules below are tested here without a world.

Stage 8c replaces the stage-7/8 priority list with a utility brain (the
"infinite axis" shape: every candidate action gets a score that is its
stage weight times its considerations, each a curve in 0..1, and the
best score wins). The live matches showed why: with the nearest player
or bot in sense always first, bots dropped 20 m apart fought each other
from the first seconds and died at level 1 without ever farming.

Zone first, absolutely: outside the circle the zone closes to (at least
BotMinZoneM), or inside it but within botZoneLeadMs of the current edge
reaching it, the bot goes there, dropping any fight. Every other
candidate is scored:

	farm       Farm x distance                          (below the level cap)
	loot       Loot x distance
	player     Player x aggression x distance x threat x ownHP x foeHP
	bot        Bot x aggression x distance x threat x ownHP x foeHP
	           / (1 + bots already attacking it)        (no dogpiles)
	retaliate  Retaliate replaces Player/Bot for a foe attacking the bot
	retreat    0.8-1.0 under RetreatBelow HP, a healthier foe attacking
	roam       botRoamUtility, the floor

The stage weights follow the zone's phase (BotStageOf):

	           farm  loot  player  bot   retaliate
	early      0.70  0.75  1.10    0.05  1.20   phases 0-1: level and gear
	mid        0.50  0.60  1.10    0.40  1.20   phases 2-3
	late       0.20  0.45  1.20    0.85  1.20   phase 4 on: hunt

Distance is 1 within BotEngageM and falls linearly to botDistanceFloor
at the candidate's range (the sense range for a foe, 1.5 x it for the
foe the attack holds or one attacking the bot, BotFarmRangeM for a wolf,
BotLootRangeM for a drop). Threat is botFightFloor + (1 - botFightFloor)
x logistic((own power - foe power + bravery) / botThreatSlope), power =
level + gear / botGearPerLevel (gear: the summed degrees of the worn
equipment). OwnHP is 0.3 + 0.7 x the HP share; foeHP raises a wounded
foe's score by up to botLowTargetBonus. Real players are always
candidates in sense; early bots almost never pick another bot (0.05)
unless it attacks them. The profile scales aggression, bravery and the
retreat share (botprofile.go).

Alert meter (the "aggression" escalation of shooter bots): every
decision the meter decays by exp(-elapsed / AlertDecayMs) and rises by
botAlertHit per foe attacking the bot, botAlertWound x the HP share lost,
botAlertPlayer x distance per real player in sense and botAlertFight per
bot fight in sense. At or above the profile's AlertThreshold the bot is
aggressive: Player x botAggroPlayer, Farm x botAggroFarm, Loot x
botAggroLoot, Bot at least botAggroBotFloor. A real player near a bot
therefore turns it on him within a decision or two (Normal: threshold
0.6, two decisions in reach), and it calms down and farms again some
AlertDecayMs after the fight.

Hysteresis: the candidate chosen last decision (or the target the
attack already holds) gets botMomentum on top of its score, and a
choice holds botCommitMs unless the zone, a retreat or the loss of its
target ends it, so a bot does not flip between two wolves or a wolf and
a drop.

A target farther than BotEngageM is walked to on the planned path
(BotChase); nearer, the bot presses an attack (BotAttack) at most once
per StrikeMs of its profile, the difficulty's reaction time: the cast
times, cooldowns and MP of its skills pace the rest exactly as they pace
a player. A retreat walks botRetreatM away from the threat, kept inside
the circle (BotRetreat). Potions are a separate draw (BotMind.Drink):
under BotHealBelow of its HP (or BotManaBelow of its MP), once per
BotPotionCooldownMs, with the profile's PotionChance.

===========================================================================
*/
package battleroyale

import (
	"math"
	"math/rand/v2"
)

const (
	// BotEngageM is the farthest a target may stand for an attack press:
	// the server's own approach walks the rest straight, so a farther
	// target is first reached on the planned path.
	BotEngageM = 25
	// BotLootRangeM bounds the drops a bot walks to.
	BotLootRangeM = 30
	// BotManaBelow is the MP share under which a bot reaches for an MP
	// potion.
	BotManaBelow = 0.25
	// BotReachM is a melee strike's reach: the economy simulation's bots
	// strike their wolves from it.
	BotReachM = 4
	// BotFarmRangeM bounds the wolf search.
	BotFarmRangeM = 80
	// BotMinZoneM is the least circle a bot keeps to, so the last phase's
	// point still leaves room to fight.
	BotMinZoneM = 10
	// BotDisengageFactor x the sense range is where a fight is dropped.
	BotDisengageFactor = 1.5
	// botZoneLeadMs: a bot inside the next circle still leaves for its
	// centre when the current edge would reach it this soon.
	botZoneLeadMs = 60_000
	// botZoneEdgeSpeed bounds how fast the edge comes (4 m/s, schedule.go):
	// a bot farther than lead x speed inside the current circle is safe
	// without sampling the zone.
	botZoneEdgeSpeed = 4 * UnitsPerMeter / 1000.0
	// botZoneSampleMs is the step of the edge-reach sampling.
	botZoneSampleMs = 2000
	// botZoneGoal places a fleeing bot's goal this far into the circle
	// (a share of its radius from the centre), on its own side.
	botZoneGoal = 0.6
	// botRoamMargin keeps a random roam goal this far inside the circle;
	// botRoamMs is the longest a roam goal lasts; botRoamArriveM is how
	// near counts as arrived; botRoamPlayerSpreadM scatters a hunt goal
	// around the player.
	botRoamMargin        = 0.7
	botRoamMs            = 20_000
	botRoamArriveM       = 6
	botRoamPlayerSpreadM = 30

	// The utility brain (file header). botRoamUtility is the floor every
	// other candidate must beat; botMomentum the last choice's bonus and
	// botCommitMs how long a choice holds.
	botRoamUtility = 0.1
	botMomentum    = 0.15
	botCommitMs    = 2000
	// botDistanceFloor is what the distance consideration leaves at the
	// edge of a candidate's range.
	botDistanceFloor = 0.5
	// The threat consideration: power = level + summed worn degrees /
	// botGearPerLevel; botThreatSlope levels of advantage are one unit of
	// the logistic; botFightFloor is the least it leaves of a fight.
	botGearPerLevel = 2.0
	botThreatSlope  = 4.0
	botFightFloor   = 0.4
	// botOwnHPFloor is what the own-HP consideration leaves of a fight at
	// no HP; botLowTargetBonus what a foe at no HP adds.
	botOwnHPFloor     = 0.3
	botLowTargetBonus = 0.3
	// A retreat scores botRetreatBase .. botRetreatBase + botRetreatSpan,
	// walks botRetreatM from the threat and lasts at least botRetreatMs.
	botRetreatBase = 0.8
	botRetreatSpan = 0.2
	botRetreatM    = 40
	botRetreatMs   = 4000

	// The alert meter (file header): what raises it per decision, its
	// ceiling, and what the aggressive mode does to the stage weights.
	botAlertHit      = 0.5
	botAlertPlayer   = 0.35
	botAlertFight    = 0.1
	botAlertWound    = 1.0
	botAlertMax      = 2.0
	botAggroPlayer   = 1.5
	botAggroFarm     = 0.5
	botAggroLoot     = 0.7
	botAggroBotFloor = 0.3
)

/*
================
BotStage

The part of the match a bot plays: early (levelling and gearing), mid,
late (hunting), by the zone's phase (BotStageOf).
================
*/
type BotStage int

const (
	BotEarly BotStage = iota
	BotMid
	BotLate
	botStages
)

/*
================
BotStageOf

The stage of a zone phase: 0-1 early, 2-3 mid, 4 on (and the final
point, -1) late. Phase 1 ends at 9.5 min, about when the economy
simulation's participants reach level 30 (stage 6).
================
*/
func BotStageOf(phase int) BotStage {
	switch {
	case phase < 0:
		return BotLate
	case phase <= 1:
		return BotEarly
	case phase <= 3:
		return BotMid
	}
	return BotLate
}

/*
================
botWeights

One stage's base utility of each candidate kind (file header table).
================
*/
type botWeights struct {
	Farm, Loot, Player, Bot, Retaliate float64
}

/*
================
botStageWeights
================
*/
func botStageWeights() [botStages]botWeights {
	return [botStages]botWeights{
		BotEarly: {Farm: 0.70, Loot: 0.75, Player: 1.10, Bot: 0.05, Retaliate: 1.2},
		BotMid:   {Farm: 0.50, Loot: 0.60, Player: 1.10, Bot: 0.40, Retaliate: 1.2},
		BotLate:  {Farm: 0.20, Loot: 0.45, Player: 1.20, Bot: 0.85, Retaliate: 1.2},
	}
}

/*
================
BotAction

The kind of a decision.
================
*/
type BotAction int

const (
	// BotHold keeps doing what the bot does (an attack in progress, a leg
	// in flight, a press cooling down).
	BotHold BotAction = iota
	// BotGoZone walks to Goal inside the zone.
	BotGoZone
	// BotAttack presses an attack on Target (a player, a bot or a
	// monster) standing at Goal.
	BotAttack
	// BotChase walks toward Target standing at Goal.
	BotChase
	// BotPickup picks up the drop Target lying at Goal.
	BotPickup
	// BotRoam walks to Goal.
	BotRoam
	// BotRetreat walks to Goal, away from the foe Target (stage 8c).
	BotRetreat
)

/*
================
BotTarget

A participant, monster or drop a bot may go for: its world object id,
where it stands and whether it is a real player. A participant also
carries its level, the summed degrees of its worn equipment, its HP
share (0: unknown, read as full), whether its attack holds the bot that
senses it (Attacking) and how many other bots' attacks hold it
(Attackers).
================
*/
type BotTarget struct {
	GID       uint32
	At        Point
	Player    bool
	Level     int
	Gear      int
	HPFrac    float64
	Attacking bool
	Attackers int
}

/*
================
BotSenses

What the wiring tells the mind for one decision. Foes are the living
players and other bots within BotDisengageFactor x the sense range;
Engaged is the target the bot's attack holds (0 none, any kind); Wolf
the nearest living wolf within BotFarmRangeM (or the monster the attack
holds), Loot the nearest wanted drop within BotLootRangeM and Player the
nearest living real player anywhere (GID 0 when none). Gear is the
summed degrees of the bot's worn equipment.
================
*/
type BotSenses struct {
	NowMs   int64
	At      Point
	Level   int
	Gear    int
	HPFrac  float64
	MPFrac  float64
	Zone    Zone
	Zoned   bool
	Engaged uint32
	Foes    []BotTarget
	Wolf    BotTarget
	Loot    BotTarget
	Player  BotTarget
}

/*
================
BotIntent

One decision: the action, its goal and target, and whether the attack
the bot holds must be released first.
================
*/
type BotIntent struct {
	Action  BotAction
	Goal    Point
	Target  uint32
	Release bool
}

/*
================
botKind

What a utility candidate is (the hysteresis compares kind and target).
================
*/
type botKind int

const (
	botKindNone botKind = iota
	botKindRoam
	botKindFarm
	botKindLoot
	botKindFight
	botKindRetreat
)

/*
================
botCandidate

One scored candidate of a decision.
================
*/
type botCandidate struct {
	kind   botKind
	target BotTarget
	score  float64
}

/*
================
BotMind

A bot's memory between decisions: the press, roam and potion clocks, the
roam goal, the utility brain's last choice (kind and target), how long
it holds, the retreat in progress, and the alert meter (its value, when
it was last decayed, and the HP share it last saw).
================
*/
type BotMind struct {
	NextPressMs  int64
	NextRoamMs   int64
	NextPotionMs int64
	NextManaMs   int64
	Roam         Point
	roaming      bool
	choice       botKind
	choiceGID    uint32
	commitMs     int64
	retreatMs    int64
	retreatGoal  Point
	retreatFrom  uint32
	alert        float64
	alertMs      int64
	lastHP       float64
}

/*
================
BotKeepCircle

The circle a bot keeps to: the one the zone closes to, at least
BotMinZoneM.
================
*/
func BotKeepCircle(state ZoneState) Circle {
	return Circle{Center: state.Target.Center, Radius: max(state.Target.Radius, BotMinZoneM*UnitsPerMeter)}
}

/*
================
BotPower

What a participant weighs in a fight: its level plus its worn degrees
over botGearPerLevel (a full degree-4 set, about 48 degrees, is worth 24
levels).
================
*/
func BotPower(level, gear int) float64 {
	return float64(level) + float64(gear)/botGearPerLevel
}

/*
================
BotMind.Decide

One decision (see the file header).
================
*/
func (mind *BotMind) Decide(s BotSenses, p BotProfile, rng *rand.Rand) BotIntent {
	keep := Circle{Center: s.At, Radius: math.Inf(1)}
	stage := BotEarly
	if s.Zoned {
		state := CircleAt(s.Zone, s.NowMs)
		keep = BotKeepCircle(state)
		stage = BotStageOf(state.Phase)
		if Outside(keep, s.At) || edgeComing(s.Zone, state, s.At, s.NowMs) {
			mind.roaming, mind.choice, mind.retreatMs = false, botKindNone, 0
			return BotIntent{Action: BotGoZone, Goal: zoneGoal(keep, s.At), Release: s.Engaged != 0}
		}
	}
	best := mind.choose(s, p, stage, keep)
	if best.kind != mind.choice || best.target.GID != mind.choiceGID {
		mind.choice, mind.choiceGID = best.kind, best.target.GID
		mind.commitMs = s.NowMs + botCommitMs
	}
	release := s.Engaged != 0 && s.Engaged != best.target.GID
	switch best.kind {
	case botKindRetreat:
		mind.roaming = false
		return BotIntent{Action: BotRetreat, Goal: mind.retreatGoal, Target: best.target.GID, Release: s.Engaged != 0}
	case botKindFight, botKindFarm:
		mind.roaming = false
		return mind.attack(best.target, s, p)
	case botKindLoot:
		mind.roaming = false
		return BotIntent{Action: BotPickup, Goal: best.target.At, Target: best.target.GID, Release: release}
	}
	if !s.Zoned {
		return BotIntent{Action: BotHold, Release: release}
	}
	if !mind.roaming || s.NowMs >= mind.NextRoamMs || distance(s.At, mind.Roam) <= botRoamArriveM*UnitsPerMeter {
		mind.Roam, mind.roaming = roamGoal(keep, s, p, rng), true
		mind.NextRoamMs = s.NowMs + botRoamMs
	}
	return BotIntent{Action: BotRoam, Goal: mind.Roam, Release: release}
}

/*
================
BotMind.choose

The utility pass (file header): scores every candidate, adds the
momentum of the last choice, keeps the last choice while it is committed
and still a candidate (a retreat breaks any commitment), and returns the
best. The roam floor wins when nothing scores above it.
================
*/
func (mind *BotMind) choose(s BotSenses, p BotProfile, stage BotStage, keep Circle) botCandidate {
	if retreat, ok := mind.retreat(s, p, keep); ok {
		return retreat
	}
	w := botStageWeights()[stage]
	if mind.alarm(s, p) {
		w.Player *= botAggroPlayer
		w.Farm *= botAggroFarm
		w.Loot *= botAggroLoot
		w.Bot = max(w.Bot, botAggroBotFloor)
	}
	best := botCandidate{kind: botKindRoam, score: botRoamUtility}
	current, held := botCandidate{}, false
	consider := func(c botCandidate) {
		if c.kind == mind.choice && c.target.GID == mind.choiceGID {
			c.score += botMomentum
			current, held = c, true
		} else if c.target.GID != 0 && c.target.GID == s.Engaged {
			// The fight the attack already holds: finish it first.
			c.score += botMomentum
		}
		if c.score > best.score {
			best = c
		}
	}
	sense := p.SenseRange()
	for _, foe := range s.Foes {
		reach := sense
		if foe.GID == s.Engaged || foe.Attacking {
			reach = sense * BotDisengageFactor
		}
		d := distance(s.At, foe.At)
		if d > reach {
			continue
		}
		base := w.Bot
		switch {
		case foe.Attacking:
			base = w.Retaliate
		case foe.Player:
			base = w.Player
		}
		score := base * p.Aggression * distanceConsideration(d, reach) * threatConsideration(s, foe, p) *
			(botOwnHPFloor + (1-botOwnHPFloor)*clamp01(s.HPFrac)) * (1 + botLowTargetBonus*(1-knownHP(foe.HPFrac)))
		if !foe.Player && !foe.Attacking {
			score /= float64(1 + max(foe.Attackers, 0))
		}
		consider(botCandidate{kind: botKindFight, target: foe, score: score})
	}
	farm := BotFarmRangeM * UnitsPerMeter
	if d := distance(s.At, s.Wolf.At); s.Wolf.GID != 0 && d <= farm && (s.Level < int(MatchLevelCap) || s.Engaged == s.Wolf.GID) {
		consider(botCandidate{kind: botKindFarm, target: s.Wolf, score: w.Farm * distanceConsideration(d, farm)})
	}
	loot := BotLootRangeM * UnitsPerMeter
	if d := distance(s.At, s.Loot.At); s.Loot.GID != 0 && d <= loot {
		consider(botCandidate{kind: botKindLoot, target: s.Loot, score: w.Loot * distanceConsideration(d, loot)})
	}
	if held && best != current && s.NowMs < mind.commitMs {
		return current
	}
	return best
}

/*
================
BotMind.retreat

The retreat candidate: under the profile's RetreatBelow HP share, with a
foe in reach whose attack holds the bot (or that the bot's attack holds)
and whose HP share is above the bot's, the bot walks botRetreatM away
from it (toward the keep circle's centre when that point lies outside
it) for at least botRetreatMs. A retreat in progress continues until its
time is up while the bot is still under the share plus botMomentum and
the foe is still sensed.
================
*/
func (mind *BotMind) retreat(s BotSenses, p BotProfile, keep Circle) (botCandidate, bool) {
	if s.NowMs < mind.retreatMs && s.HPFrac < p.RetreatBelow+botMomentum {
		if foe, ok := findTarget(s.Foes, mind.retreatFrom); ok {
			return botCandidate{kind: botKindRetreat, target: foe, score: botRetreatBase}, true
		}
	}
	if s.HPFrac >= p.RetreatBelow {
		return botCandidate{}, false
	}
	reach := p.SenseRange() * BotDisengageFactor
	for _, foe := range s.Foes {
		if !(foe.Attacking || foe.GID == s.Engaged) || knownHP(foe.HPFrac) <= s.HPFrac || distance(s.At, foe.At) > reach {
			continue
		}
		goal := awayFrom(s.At, foe.At, botRetreatM*UnitsPerMeter)
		if Outside(keep, goal) {
			goal = zoneGoal(keep, s.At)
		}
		mind.retreatMs, mind.retreatGoal, mind.retreatFrom = s.NowMs+botRetreatMs, goal, foe.GID
		score := botRetreatBase + botRetreatSpan*(1-s.HPFrac/max(p.RetreatBelow, 1e-6))
		return botCandidate{kind: botKindRetreat, target: foe, score: score}, true
	}
	return botCandidate{}, false
}

/*
================
BotMind.alarm

The alert meter (file header): decays by exp(-elapsed / AlertDecayMs)
since the last decision, then rises by botAlertHit for every foe whose
attack holds the bot, botAlertWound x the HP share lost since the last
decision, botAlertPlayer x distance for every real player in sense (so
a player who comes near makes the bot fight him within a decision or
two) and botAlertFight for every foe in sense already fought by bots; at
most botAlertMax. Returns whether the bot is aggressive: the meter at
or above the profile's AlertThreshold.
================
*/
func (mind *BotMind) alarm(s BotSenses, p BotProfile) bool {
	if mind.alertMs != 0 && p.AlertDecayMs > 0 {
		mind.alert *= math.Exp(-float64(max(s.NowMs-mind.alertMs, 0)) / float64(p.AlertDecayMs))
	}
	mind.alertMs = s.NowMs
	if mind.lastHP > s.HPFrac {
		mind.alert += botAlertWound * (mind.lastHP - s.HPFrac)
	}
	mind.lastHP = s.HPFrac
	sense := p.SenseRange()
	for _, foe := range s.Foes {
		d := distance(s.At, foe.At)
		switch {
		case foe.Attacking:
			mind.alert += botAlertHit
		case d > sense:
		case foe.Player:
			mind.alert += botAlertPlayer * distanceConsideration(d, sense)
		case foe.Attackers > 0:
			mind.alert += botAlertFight
		}
	}
	mind.alert = min(mind.alert, botAlertMax)
	return mind.alert >= p.AlertThreshold
}

/*
================
BotMind.Alert

The alert meter as it stood at the last decision (telemetry, tests).
================
*/
func (mind *BotMind) Alert() float64 {
	return mind.alert
}

/*
================
distanceConsideration

1 within BotEngageM, falling linearly to botDistanceFloor at reach.
================
*/
func distanceConsideration(d, reach float64) float64 {
	engage := BotEngageM * UnitsPerMeter
	if d <= engage || reach <= engage {
		return 1
	}
	return 1 - (1-botDistanceFloor)*min((d-engage)/(reach-engage), 1)
}

/*
================
threatConsideration

How willing the bot is to fight foe: botFightFloor .. 1 by the logistic
of its power advantage (BotPower, plus the profile's bravery) over
botThreatSlope. A foe whose level is unknown (0) weighs as the bot.
================
*/
func threatConsideration(s BotSenses, foe BotTarget, p BotProfile) float64 {
	own := BotPower(s.Level, s.Gear)
	their := own
	if foe.Level > 0 {
		their = BotPower(foe.Level, foe.Gear)
	}
	advantage := own - their + p.Bravery
	return botFightFloor + (1-botFightFloor)/(1+math.Exp(-advantage/botThreatSlope))
}

/*
================
knownHP

A target's HP share, an unknown one (0) read as full.
================
*/
func knownHP(frac float64) float64 {
	if frac <= 0 {
		return 1
	}
	return clamp01(frac)
}

/*
================
awayFrom

The point length from at, directly away from threat (east when they
stand on one spot).
================
*/
func awayFrom(at, threat Point, length float64) Point {
	dx, dz := at.X-threat.X, at.Z-threat.Z
	d := math.Hypot(dx, dz)
	if d == 0 {
		return Point{X: at.X + length, Z: at.Z}
	}
	return Point{X: at.X + dx/d*length, Z: at.Z + dz/d*length}
}

/*
================
clamp01
================
*/
func clamp01(v float64) float64 {
	return min(max(v, 0), 1)
}

/*
================
BotMind.attack

Walk to target when it stands past BotEngageM; nearer, press an attack
on it, at most once per the profile's StrikeMs (between presses the
fight goes on: hold).
================
*/
func (mind *BotMind) attack(target BotTarget, s BotSenses, p BotProfile) BotIntent {
	if distance(s.At, target.At) > BotEngageM*UnitsPerMeter {
		return BotIntent{Action: BotChase, Goal: target.At, Target: target.GID}
	}
	if s.NowMs < mind.NextPressMs {
		return BotIntent{Action: BotHold, Target: target.GID}
	}
	mind.NextPressMs = s.NowMs + p.StrikeMs
	return BotIntent{Action: BotAttack, Goal: target.At, Target: target.GID}
}

/*
================
BotDrink

Which potion a bot reaches for.
================
*/
type BotDrink int

const (
	DrinkNone BotDrink = iota
	DrinkHP
	DrinkMP
)

/*
================
BotMind.Drink

The potion the bot drinks now: HP under BotHealBelow, else MP under
BotManaBelow, each at most once per BotPotionCooldownMs and with the
profile's PotionChance.
================
*/
func (mind *BotMind) Drink(s BotSenses, p BotProfile, rng *rand.Rand) BotDrink {
	if s.HPFrac < BotHealBelow && s.NowMs >= mind.NextPotionMs {
		mind.NextPotionMs = s.NowMs + BotPotionCooldownMs
		if rng.IntN(100) < p.PotionChance {
			return DrinkHP
		}
		return DrinkNone
	}
	if s.MPFrac < BotManaBelow && s.NowMs >= mind.NextManaMs {
		mind.NextManaMs = s.NowMs + BotPotionCooldownMs
		if rng.IntN(100) < p.PotionChance {
			return DrinkMP
		}
	}
	return DrinkNone
}

/*
================
edgeComing

Whether the current circle's edge reaches at within botZoneLeadMs while
at is inside it: only sampled (every botZoneSampleMs) when at stands
nearer the edge than the edge can travel in the lead time.
================
*/
func edgeComing(zone Zone, state ZoneState, at Point, nowMs int64) bool {
	if Outside(state.Current, at) {
		return true
	}
	if state.Current.Radius-distance(state.Current.Center, at) > botZoneEdgeSpeed*botZoneLeadMs {
		return false
	}
	for dt := int64(botZoneSampleMs); dt <= botZoneLeadMs; dt += botZoneSampleMs {
		if Outside(CircleAt(zone, nowMs+dt).Current, at) {
			return true
		}
	}
	return false
}

/*
================
zoneGoal

Where a bot heading into keep walks: botZoneGoal of the radius from the
centre, on the bot's side (the centre itself for a bot standing on it).
================
*/
func zoneGoal(keep Circle, at Point) Point {
	d := distance(keep.Center, at)
	if d <= keep.Radius*botZoneGoal || d == 0 {
		return keep.Center
	}
	scale := keep.Radius * botZoneGoal / d
	return Point{X: keep.Center.X + (at.X-keep.Center.X)*scale, Z: keep.Center.Z + (at.Z-keep.Center.Z)*scale}
}

/*
================
roamGoal

A new roam goal: near the nearest real player inside keep for
RoamTowardPlayers percent of the draws, else a random point of keep's
inner botRoamMargin.
================
*/
func roamGoal(keep Circle, s BotSenses, p BotProfile, rng *rand.Rand) Point {
	if s.Player.GID != 0 && !Outside(keep, s.Player.At) && rng.IntN(100) < p.RoamTowardPlayers {
		angle := rng.Float64() * 2 * math.Pi
		r := rng.Float64() * botRoamPlayerSpreadM * UnitsPerMeter
		return Point{X: s.Player.At.X + r*math.Cos(angle), Z: s.Player.At.Z + r*math.Sin(angle)}
	}
	return NextCircle(Circle{Center: keep.Center, Radius: keep.Radius * botRoamMargin}, 0, rng).Center
}

/*
================
findTarget
================
*/
func findTarget(targets []BotTarget, gid uint32) (BotTarget, bool) {
	for _, t := range targets {
		if t.GID == gid {
			return t, true
		}
	}
	return BotTarget{}, false
}
