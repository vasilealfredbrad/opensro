/*
===========================================================================

economy.go - the match economy: wolf supply inside the zone, ground items

Port-only, not native (see zone.go; docs/battle-royale.md "Server notes
(stage 6: economy)"). Stage 6 replaces the stage-4 phase top-ups (40
wolves per player alive at each phase start) with two supplies, sized by
the whole-match simulation (economy_sim_test.go): wolves spread
uniformly cannot give a 4.5-6 s kill under the live cap (2 000 over
5 km2 put the nearest wolf 25 m away and players at about 13 s a kill),
and the wolves left outside the shrinking circle only held the cap.

The zone supply runs every SupplyIntervalMs and at every phase start.
Each pass first retires the untouched wolves standing more than
retireMarginM outside the CURRENT circle (nobody farms there; they only
hold the live cap), then tops the NEXT circle (the one the zone closes
to) up to SupplyTarget:

	target = max(ZoneWolvesPerParticipant x players alive
	             + ZoneWolvesPerBot x bots alive,
	             walkable m2 of the next circle / ZoneM2PerWolf)
	         at most walkable m2 / zoneMinM2PerWolf (no crowding of a
	         small circle) and MaxLiveMonsters

and never adds more than maxSupplyBatch at once or past MaxLiveMonsters
living monsters in the layer. The proof that every participant can reach
level 30 before phase 5 on this supply is the simulation; its threshold,
MinZoneWolvesPerParticipant living wolves inside the current circle per
participant alive, is what it asserts at every second before phase 5.

The local supply runs every LocalSupplyIntervalMs: every participant
still below the level cap, standing in the current circle, keeps
LocalWolves wolves within LocalSupplyRadiusM, the new ones at least
localSupplyMinM away and on the zone's side of it (Inward), so a player
levelling finds its next wolf a few seconds away and farms its way into
the zone. A capped participant gets none (the bots also stop hunting).

Stage 7 lets a GM add up to MaxBotCount bots. The bots are living
monsters of the layer, so the live cap already counts them (wolves +
bots never pass MaxLiveMonsters); what changes is how much of the cap
they may claim: a bot weighs ZoneWolvesPerBot in the zone target instead
of a player's ZoneWolvesPerParticipant, and the local supply gives the
bots BotLocalWolves(bots) wolves each, a shared botLocalBudget spread
over them (LocalWolves each up to 20 bots, at least botLocalMin), the
players first. The simulation's floor counts MinZoneWolvesPerBot for a
bot.

Ground items of the match layer vanish GroundItemLifetimeMs after they
fall, and the layer never holds more than MaxGroundItems (the oldest go
first): a 16-player match drops about 11 stacks a second.

===========================================================================
*/
package battleroyale

import "math/rand/v2"

const (
	// SupplyIntervalMs is the cadence of the zone supply (and of the
	// retirement of the wolves left outside).
	SupplyIntervalMs = 15_000
	// ZoneWolvesPerParticipant is the per-participant stock the supply
	// keeps inside the next circle: about 45 s of farming for a player at
	// one kill every 5 s, three supply passes of margin.
	ZoneWolvesPerParticipant = 16
	// ZoneM2PerWolf is the density floor of the supply: one wolf per
	// 3 000 m2 of walkable ground in the next circle (about the drop's
	// density, ArenaMonsters over the 5 km2 ground mask): the shared
	// stock between the local supplies.
	ZoneM2PerWolf = 3000.0
	// zoneMinM2PerWolf caps the density: one wolf per 400 m2, so the
	// small last circles are not packed solid.
	zoneMinM2PerWolf = 400.0
	// maxSupplyBatch bounds one pass's spawns (the drop already stands
	// ArenaMonsters in one tick).
	maxSupplyBatch = 600
	// MinZoneWolvesPerParticipant is the simulation's floor: living wolves
	// inside the current circle per player alive (MinZoneWolvesPerBot per
	// bot), at every second before phase 5 starts.
	MinZoneWolvesPerParticipant = 8
	MinZoneWolvesPerBot         = 2
	// ZoneWolvesPerBot is a bot's weight in the zone target (stage 7).
	ZoneWolvesPerBot = 4
	// botLocalBudget is the local supply all bots share; botLocalMin the
	// least a bot gets (BotLocalWolves).
	botLocalBudget = 200
	botLocalMin    = 2
	// SupplyEdge keeps the zone supply off the next circle's edge (a
	// share of its radius); SupplyMinRadius is the least ground it
	// spreads over (50 m), so a point target still gets wolves around it.
	SupplyEdge      = 0.9
	SupplyMinRadius = 50 * UnitsPerMeter
	// retireMarginM keeps a wolf just outside the circle (where a player
	// may still be finishing it) from being retired.
	retireMarginM = 30

	// The local supply (LocalSupplyPlan): every LocalSupplyIntervalMs each
	// participant below the level cap inside the current circle keeps
	// LocalWolves living wolves within LocalSupplyRadiusM, new ones never
	// nearer than localSupplyMinM (no wolf appears on top of anyone).
	LocalSupplyIntervalMs = 5_000
	LocalWolves           = 10
	LocalSupplyRadiusM    = 35
	localSupplyMinM       = 12
	localSupplyAttempts   = 8

	// GroundItemLifetimeMs is how long a drop lies in the match layer.
	GroundItemLifetimeMs = 75_000
	// MaxGroundItems bounds the drops of one match layer; past it the
	// oldest vanish first.
	MaxGroundItems = 2000
)

/*
================
Supply

What the zone supply reads: the real players and the bots alive, the
living match wolves inside the next circle (supply elites and bots not
counted), the living monsters of the layer (everything the live cap
counts, the bots included) and the walkable ground of the next circle
in square metres.
================
*/
type Supply struct {
	Participants int
	Bots         int
	Inside       int
	Live         int
	GroundM2     float64
}

/*
================
SupplyTarget

The stock the supply keeps inside the next circle (see the file header).
================
*/
func SupplyTarget(s Supply) int {
	target := max(ZoneWolvesPerParticipant*max(s.Participants, 0)+ZoneWolvesPerBot*max(s.Bots, 0), int(s.GroundM2/ZoneM2PerWolf))
	return min(target, int(s.GroundM2/zoneMinM2PerWolf), MaxLiveMonsters)
}

/*
================
SupplyCount

How many wolves one supply pass adds: up to SupplyTarget, at most
maxSupplyBatch, never past MaxLiveMonsters living monsters.
================
*/
func SupplyCount(s Supply) int {
	return max(min(SupplyTarget(s)-s.Inside, maxSupplyBatch, MaxLiveMonsters-s.Live), 0)
}

/*
================
SupplyPlan

The wolves one supply pass adds over ground (the walkable cells of the
next circle, kept off its edge by the caller): SupplyCount of them.
================
*/
func SupplyPlan(ground GroundCells, s Supply, rng *rand.Rand) []MonsterSpawn {
	return planWolves(ground, SupplyCount(s), rng)
}

/*
================
LocalSupplyPlan

The local supply around one participant below the level cap standing at
at inside the current circle of zone (economy.go header): LocalWolves
minus the living wolves already within LocalSupplyRadiusM (nearby), each
drawn on ground (the mask cells within LocalSupplyRadiusM of at) no
nearer than localSupplyMinM, on the zone's side of at (Inward), so
farming the local wolves walks a player into the zone. A draw that finds
no such point in localSupplyAttempts tries is dropped.
================
*/
func LocalSupplyPlan(ground GroundCells, at Point, zone ZoneState, nearby int, rng *rand.Rand) []MonsterSpawn {
	return LocalSupplyPlanFor(ground, at, zone, LocalWolves, nearby, rng)
}

/*
================
BotLocalWolves

The local supply's stock around one bot of a match of bots bots:
botLocalBudget shared among them, LocalWolves at most, botLocalMin at
least.
================
*/
func BotLocalWolves(bots int) int {
	if bots <= 0 {
		return LocalWolves
	}
	return min(max(botLocalBudget/bots, botLocalMin), LocalWolves)
}

/*
================
LocalSupplyPlanFor

LocalSupplyPlan keeping want wolves (a player LocalWolves, a bot
BotLocalWolves) instead of LocalWolves.
================
*/
func LocalSupplyPlanFor(ground GroundCells, at Point, zone ZoneState, want, nearby int, rng *rand.Rand) []MonsterSpawn {
	count := max(want-nearby, 0)
	if ground.Len() == 0 || count == 0 || Outside(zone.Current, at) {
		return nil
	}
	inner := Circle{Center: at, Radius: localSupplyMinM * UnitsPerMeter}
	outer := Circle{Center: at, Radius: LocalSupplyRadiusM * UnitsPerMeter}
	out := make([]MonsterSpawn, 0, count)
	for range count {
		for range localSupplyAttempts {
			p, _ := ground.Sample(rng)
			if Outside(inner, p) && !Outside(outer, p) && Inward(zone, at, p) {
				out = append(out, MonsterSpawn{Codename: MatchMonsterCodename, At: p})
				break
			}
		}
	}
	return out
}

/*
================
Inward

Whether p lies on the zone's side of at: inside the current circle, and
inside the next circle when at is in it, else nearer the next circle's
centre than at. The local supply spawns there; the simulation's players
farm there.
================
*/
func Inward(zone ZoneState, at, p Point) bool {
	if Outside(zone.Current, p) {
		return false
	}
	if !Outside(zone.Target, at) {
		return !Outside(zone.Target, p)
	}
	c := zone.Target.Center
	return (p.X-c.X)*(p.X-c.X)+(p.Z-c.Z)*(p.Z-c.Z) < (at.X-c.X)*(at.X-c.X)+(at.Z-c.Z)*(at.Z-c.Z)
}

/*
================
RetireOutside

Whether the supply retires an untouched wolf standing at p: more than
retireMarginM outside the current circle.
================
*/
func RetireOutside(current Circle, p Point) bool {
	return Outside(Circle{Center: current.Center, Radius: current.Radius + retireMarginM*UnitsPerMeter}, p)
}

/*
================
GroundCells.Area

The cells' ground in square metres.
================
*/
func (g GroundCells) Area() float64 {
	cell := GroundMaskStep / UnitsPerMeter
	return float64(len(g.cells)) * cell * cell
}
