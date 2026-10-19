/*
===========================================================================

botbrain_test.go - the utility brain's rules (stage 8c)

Port-only, not native (docs/battle-royale.md "Stage 8c"). Every rule of
BotMind.Decide on hand-made senses: the zone first, a fight walked to
and pressed at the profile's cadence, early farming over bot fights, a
real player always fought, self-defence, the late hunt, no dogpiles, the
threat of a stronger foe, the retreat by difficulty, the hysteresis that
keeps a choice, the drop, the wolf, the cap and the roam. Time is
injected.

===========================================================================
*/
package battleroyale

import (
	"math/rand/v2"
	"testing"
)

/*
================
brainStagedZone

A five-phase zone whose circles never shrink (every phase ends at the
same 200 m circle), so a bot at its centre is never sent into the zone
and the phase alone selects the stage: phase i runs from i x 2 s.
================
*/
func brainStagedZone() Zone {
	c := Point{X: 210000, Z: 175000}
	var schedule Schedule
	var targets []Circle
	for range 5 {
		schedule.Phases = append(schedule.Phases, Phase{WaitMs: 1000, ShrinkMs: 1000, EndRadius: 2000, DamagePctPerSec: 1})
		targets = append(targets, Circle{Center: c, Radius: 2000})
	}
	return Zone{Schedule: schedule, Start: Circle{Center: c, Radius: 2000}, Targets: targets}
}

/*
================
TestBotStageOf
================
*/
func TestBotStageOf(t *testing.T) {
	for phase, want := range map[int]BotStage{-1: BotLate, 0: BotEarly, 1: BotEarly, 2: BotMid, 3: BotMid, 4: BotLate, 5: BotLate} {
		if got := BotStageOf(phase); got != want {
			t.Fatalf("phase %d: stage %d, want %d", phase, got, want)
		}
	}
	zone := brainStagedZone()
	if CircleAt(zone, 8500).Phase != 4 || CircleAt(zone, 500).Phase != 0 {
		t.Fatal("the staged zone's phases moved")
	}
}

/*
================
TestBotBrainRules

The zone, the engaged fight (chase, press cadence, release), the drop,
the wolf, the cap and the roam.
================
*/
func TestBotBrainRules(t *testing.T) {
	p := ProfileFor(BotNormal)
	rng := rand.New(rand.NewPCG(5, 6))
	zone := brainZone()
	centre := zone.Start.Center
	senses := func(at Point) BotSenses {
		return BotSenses{NowMs: 1000, At: at, Level: 5, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true}
	}
	m := func(dx float64) Point { return Point{X: centre.X + dx, Z: centre.Z} }
	engage := BotEngageM * UnitsPerMeter

	// Outside the next circle: go in, releasing a fight.
	var mind BotMind
	s := senses(m(2500))
	s.Engaged = 7
	s.Foes = []BotTarget{{GID: 7, At: m(2510), Player: true}}
	if got := mind.Decide(s, p, rng); got.Action != BotGoZone || !got.Release || Outside(zone.Targets[0], got.Goal) {
		t.Fatalf("outside the next circle: %+v", got)
	}
	// Inside the circle a bot keeps but the edge comes within the lead.
	final := zone
	final.Targets = []Circle{{Center: centre, Radius: 50}}
	final.Schedule.Phases[0].EndRadius = 50
	s = senses(m(90))
	s.Zone, s.NowMs = final, final.Schedule.Phases[0].WaitMs+150_000
	if got := mind.Decide(s, p, rng); got.Action != BotGoZone {
		t.Fatalf("edge coming: %+v", got)
	}
	s.NowMs = 1000
	if got := mind.Decide(s, p, rng); got.Action == BotGoZone {
		t.Fatalf("edge far away: %+v", got)
	}
	// The engaged player within 1.5 x sense keeps the fight: walked to
	// past the engage range, pressed on near, at the profile's cadence.
	mind = BotMind{}
	s = senses(m(0))
	s.Engaged = 7
	s.Foes = []BotTarget{{GID: 7, At: m(p.SenseRange() * 1.4), Player: true}}
	if got := mind.Decide(s, p, rng); got.Action != BotChase || got.Target != 7 {
		t.Fatalf("engaged player past sense: %+v", got)
	}
	s.Foes = []BotTarget{{GID: 7, At: m(engage / 2), Player: true}}
	if got := mind.Decide(s, p, rng); got.Action != BotAttack || got.Target != 7 {
		t.Fatalf("engaged player near: %+v", got)
	}
	if got := mind.Decide(s, p, rng); got.Action != BotHold || got.Release {
		t.Fatalf("press before its cadence: %+v", got)
	}
	s.NowMs += p.StrikeMs
	if got := mind.Decide(s, p, rng); got.Action != BotAttack {
		t.Fatalf("press after its cadence: %+v", got)
	}
	s.Foes = nil
	s.Zoned = false
	if got := mind.Decide(s, p, rng); got.Action != BotHold || !got.Release {
		t.Fatalf("lost foe not released: %+v", got)
	}
	// The wolf the attack holds is finished before a drop; with no fight
	// the drop is picked up; none beyond the loot range.
	mind = BotMind{}
	s = senses(m(0))
	s.Engaged = 4
	s.Wolf = BotTarget{GID: 4, At: m(engage / 2)}
	s.Loot = BotTarget{GID: 300100, At: m(100)}
	if got := mind.Decide(s, p, rng); got.Action != BotAttack || got.Target != 4 {
		t.Fatalf("engaged wolf: %+v", got)
	}
	mind = BotMind{}
	s.Engaged, s.Wolf = 0, BotTarget{}
	if got := mind.Decide(s, p, rng); got.Action != BotPickup || got.Target != 300100 || got.Goal != s.Loot.At {
		t.Fatalf("drop: %+v", got)
	}
	s.Loot.At = m(BotLootRangeM*UnitsPerMeter + 10)
	// A wolf below the cap (walked to past the engage range); none at
	// the cap.
	mind = BotMind{}
	s.Wolf = BotTarget{GID: 4, At: m(300)}
	if got := mind.Decide(s, p, rng); got.Action != BotChase || got.Target != 4 {
		t.Fatalf("wolf: %+v", got)
	}
	s.Level = int(MatchLevelCap)
	s.NowMs += botCommitMs
	if got := mind.Decide(s, p, rng); got.Action != BotRoam {
		t.Fatalf("capped bot farmed: %+v", got)
	}
	// Roam: a goal inside the circle that lasts until arrival.
	mind = BotMind{}
	first := mind.Decide(s, p, rng)
	if first.Action != BotRoam || Outside(zone.Targets[0], first.Goal) {
		t.Fatalf("roam %+v", first)
	}
	if again := mind.Decide(s, p, rng); again.Goal != first.Goal {
		t.Fatal("the roam goal changed before arrival")
	}
}

/*
================
TestBotUtilityByStage

Early, a near wolf beats a near bot that leaves the bot alone, but a
near real player beats the wolf, and a bot attacking the bot is fought
back. Late, the near bot beats the wolf. A bot at the level cap hunts
bots from mid-match on.
================
*/
func TestBotUtilityByStage(t *testing.T) {
	p := ProfileFor(BotNormal)
	rng := rand.New(rand.NewPCG(7, 8))
	zone := brainStagedZone()
	centre := zone.Start.Center
	m := func(dx float64) Point { return Point{X: centre.X + dx, Z: centre.Z} }
	near := BotEngageM * UnitsPerMeter / 2
	decide := func(nowMs int64, level int, foes []BotTarget) BotIntent {
		var mind BotMind
		s := BotSenses{NowMs: nowMs, At: centre, Level: level, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Foes: foes,
			Wolf: BotTarget{GID: 4, At: m(near)}}
		return mind.Decide(s, p, rng)
	}
	bot := BotTarget{GID: 9, At: m(-near), Level: 5}
	if got := decide(500, 5, []BotTarget{bot}); got.Target != 4 {
		t.Fatalf("early: the bot fought a bot over a wolf: %+v", got)
	}
	player := BotTarget{GID: 8, At: m(-near), Level: 5, Player: true}
	if got := decide(500, 5, []BotTarget{bot, player}); got.Target != 8 {
		t.Fatalf("early: a near real player was not fought: %+v", got)
	}
	attacker := bot
	attacker.Attacking = true
	if got := decide(500, 5, []BotTarget{attacker}); got.Target != 9 {
		t.Fatalf("early: the bot did not defend itself: %+v", got)
	}
	if got := decide(8500, 5, []BotTarget{bot}); got.Target != 9 {
		t.Fatalf("late: the bot farmed instead of fighting: %+v", got)
	}
	if got := decide(4500, int(MatchLevelCap), []BotTarget{bot}); got.Target != 9 {
		t.Fatalf("mid: a capped bot did not hunt: %+v", got)
	}
}

/*
================
TestBotUtilityNoDogpileAndThreat

Of two equal bots, the one already attacked by two others is left
alone; a far stronger player at the sense edge loses to a wolf, an equal
one in reach does not.
================
*/
func TestBotUtilityNoDogpileAndThreat(t *testing.T) {
	p := ProfileFor(BotNormal)
	rng := rand.New(rand.NewPCG(9, 10))
	zone := brainStagedZone()
	centre := zone.Start.Center
	m := func(dx float64) Point { return Point{X: centre.X + dx, Z: centre.Z} }
	var mind BotMind
	s := BotSenses{NowMs: 8500, At: centre, Level: 20, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Foes: []BotTarget{
		{GID: 9, At: m(100), Level: 20, Attackers: 2},
		{GID: 10, At: m(-120), Level: 20},
	}}
	if got := mind.Decide(s, p, rng); got.Target != 10 {
		t.Fatalf("dogpiled bot picked: %+v", got)
	}
	strong := BotTarget{GID: 8, At: m(p.SenseRange() * 0.95), Level: 30, Gear: 40, Player: true}
	mind = BotMind{}
	s = BotSenses{NowMs: 500, At: centre, Level: 5, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Foes: []BotTarget{strong},
		Wolf: BotTarget{GID: 4, At: m(300)}}
	if got := mind.Decide(s, p, rng); got.Target != 4 {
		t.Fatalf("a far stronger player beat the wolf: %+v", got)
	}
}

/*
================
TestBotUtilityRetreat

A Normal bot at 20 % HP fought by a healthier foe walks away from it (a
goal farther from the foe, inside the circle) and keeps walking for the
retreat's time; an Easy bot (retreats under 15 %) fights back.
================
*/
func TestBotUtilityRetreat(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	zone := brainStagedZone()
	centre := zone.Start.Center
	foe := BotTarget{GID: 9, At: Point{X: centre.X + 50, Z: centre.Z}, Level: 5, HPFrac: 0.8, Attacking: true}
	s := BotSenses{NowMs: 500, At: centre, Level: 5, HPFrac: 0.2, MPFrac: 1, Zone: zone, Zoned: true, Engaged: 9, Foes: []BotTarget{foe}}
	var mind BotMind
	got := mind.Decide(s, ProfileFor(BotNormal), rng)
	if got.Action != BotRetreat || !got.Release || distance(got.Goal, foe.At) <= distance(centre, foe.At) || Outside(zone.Targets[0], got.Goal) {
		t.Fatalf("no retreat: %+v", got)
	}
	s.NowMs += botRetreatMs / 2
	s.HPFrac = 0.35
	if again := mind.Decide(s, ProfileFor(BotNormal), rng); again.Action != BotRetreat || again.Goal != got.Goal {
		t.Fatalf("the retreat stopped early: %+v", again)
	}
	var easy BotMind
	s.NowMs, s.HPFrac = 500, 0.2
	if got := easy.Decide(s, ProfileFor(BotEasy), rng); got.Action == BotRetreat || got.Target != 9 {
		t.Fatalf("an Easy bot retreated: %+v", got)
	}
}

/*
================
TestBotUtilityHysteresis

A fight chosen holds against a slightly better newcomer (momentum and
commitment) and is dropped when its target goes.
================
*/
func TestBotUtilityHysteresis(t *testing.T) {
	p := ProfileFor(BotNormal)
	rng := rand.New(rand.NewPCG(13, 14))
	zone := brainStagedZone()
	centre := zone.Start.Center
	m := func(dx float64) Point { return Point{X: centre.X + dx, Z: centre.Z} }
	a := BotTarget{GID: 21, At: m(300), Level: 5, Player: true}
	b := BotTarget{GID: 22, At: m(-100), Level: 5, Player: true}
	var mind BotMind
	s := BotSenses{NowMs: 500, At: centre, Level: 5, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Foes: []BotTarget{a}}
	if got := mind.Decide(s, p, rng); got.Target != 21 {
		t.Fatalf("first choice %+v", got)
	}
	s.Foes = []BotTarget{a, b}
	for i := range 6 {
		s.NowMs += 500
		if got := mind.Decide(s, p, rng); got.Target != 21 {
			t.Fatalf("decision %d flipped to %+v", i, got)
		}
	}
	s.Foes = []BotTarget{b}
	s.NowMs += 500
	if got := mind.Decide(s, p, rng); got.Target != 22 {
		t.Fatalf("lost target kept: %+v", got)
	}
}

/*
================
TestBotAlertMeter

A real player at 60 m loses to a near wolf on the first decision, but
his presence raises the Normal bot's alert meter past its threshold
within a few decisions and the bot turns on him; once he is gone the
meter decays and the bot farms again. An Easy bot (threshold 0.9) takes
longer than a Hard one (0.4); a foe attacking the bot raises the meter
at once.
================
*/
func TestBotAlertMeter(t *testing.T) {
	zone := brainStagedZone()
	centre := zone.Start.Center
	m := func(dx float64) Point { return Point{X: centre.X + dx, Z: centre.Z} }
	wolf := BotTarget{GID: 4, At: m(100)}
	player := BotTarget{GID: 8, At: m(-600), Level: 5, Player: true}
	rng := rand.New(rand.NewPCG(15, 16))
	turns := func(d BotDifficulty) int {
		var mind BotMind
		s := BotSenses{NowMs: 500, At: centre, Level: 5, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Wolf: wolf,
			Foes: []BotTarget{player}}
		for i := 1; i <= 20; i++ {
			s.NowMs += 1000
			if mind.Decide(s, ProfileFor(d), rng).Target == player.GID {
				return i
			}
		}
		return 0
	}
	easy, normal, hard := turns(BotEasy), turns(BotNormal), turns(BotHard)
	if normal == 0 || normal > 3 || hard == 0 || hard > normal || (easy != 0 && easy < normal) {
		t.Fatalf("decisions until the bot turns on the player: easy %d, normal %d, hard %d", easy, normal, hard)
	}
	var mind BotMind
	p := ProfileFor(BotNormal)
	s := BotSenses{NowMs: 500, At: centre, Level: 5, HPFrac: 1, MPFrac: 1, Zone: zone, Zoned: true, Wolf: wolf, Foes: []BotTarget{player}}
	for range normal {
		s.NowMs += 1000
		mind.Decide(s, p, rng)
	}
	if mind.Alert() < p.AlertThreshold {
		t.Fatalf("alert %.2f", mind.Alert())
	}
	s.Foes = nil
	s.NowMs += 4 * p.AlertDecayMs
	if got := mind.Decide(s, p, rng); got.Target != wolf.GID || mind.Alert() >= p.AlertThreshold {
		t.Fatalf("the bot did not calm down: %+v alert %.2f", got, mind.Alert())
	}
	var hit BotMind
	attacker := BotTarget{GID: 9, At: m(300), Level: 5, Attacking: true}
	s.Foes = []BotTarget{attacker}
	hit.Decide(s, p, rng)
	if hit.Alert() < botAlertHit {
		t.Fatalf("a hit raised the alert to %.2f", hit.Alert())
	}
}
