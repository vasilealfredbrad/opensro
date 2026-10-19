/*
===========================================================================

battleroyale_botbrain.go - what the bots of a match do every tick

Port-only, not native (battleroyale.go; docs/battle-royale.md "Stage 7"
and "Stage 8"). A bot is a real match character with no session
(battleroyale_bots.go), so it acts the only way a player does: through
the commands a client sends. Every match tick (0.5 s) a bot whose think
time came (its difficulty's ThinkMs, the bots staggered over the ticks
so a tick never decides for all of them) gathers its senses and acts on
one decision of its mind (battleroyale.BotMind, which owns the rules):

  - the players and bots it may fight come from one snapshot per tick
    (every living participant's live position), the drops it may want
    from one pass over the layer's ground, and the nearest wolf is only
    searched for when no foe is in sense;
  - a fight is a press: the build's best ready skill on the target (a
    buff that is not up, a heal when hurt, else the strongest attack
    skill the cooldowns, MP, weapon and ammunition admit), or the basic
    attack, through HandleTargetInteract (0x7074) exactly as a client's
    press; the server's own approach, cast, continuation, PvP, death and
    loot rules do the rest;
  - a walk (into the zone, to a roam goal, toward a far target) follows
    a path planned over the walkable ground (battleroyale.BotNav: A* on
    the ground mask, every cell edge probed once with the movement
    clip), one leg at a time as a ground click (0x7738, the movement
    lane's HandleMove through BattleRoyaleSetup.Move), a stuck bot
    re-planning (battleroyale.BotRoute); the match's planner spends at
    most BotNavTickBudget expansions and BotNavTickProbes probes a tick;
  - a drop is a pickup press (0x7074 ground leg) and the pickup's own
    auto-equip seats it when it improves the set; a potion is the item
    use (0x75BD) of a potion in the bag.

Decisions are made under the division lock; the commands they queue run
after it is released (runBattleRoyaleBotCommands), because every handler
takes that lock itself, as it does for a client's packet. The handlers'
public frames are routed like any tick hook's: around the bot to the
sessions observing it.

===========================================================================
*/

package action

import (
	"math"
	"math/rand/v2"
	"sort"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleBotRegoalM: a walk goal that moved farther than this
	// (a chased target) restarts the route.
	battleRoyaleBotRegoalM = 10
	// battleRoyaleBotStraightM is the longest straight leg of a walk the
	// planner gave up on.
	battleRoyaleBotStraightM = 40
	// battleRoyaleBotWalkReach is how near the clip must bring a probed
	// walk to its end for the edge to count as open (0.5 m).
	battleRoyaleBotWalkReach = 5.0
	// battleRoyaleBotHealBelow is the HP share under which a bot presses a
	// heal skill of its build on itself; battleRoyaleBotBuffMP the MP share
	// a buff press needs (the rest is kept for the attack skills).
	battleRoyaleBotHealBelow = 0.6
	battleRoyaleBotBuffMP    = 0.5
	// battleRoyaleBotPotionStock is how many potions of a kind a bot
	// carries before it leaves more on the ground.
	battleRoyaleBotPotionStock = 60
	// battleRoyaleBotLootChecks bounds the equipment drops one decision
	// weighs against the bot's set (nearest first).
	battleRoyaleBotLootChecks = 3
	// The potion codename families a bot drinks and collects.
	battleRoyaleHPPotion  = "ITEM_ETC_HP_POTION_"
	battleRoyaleMPPotion  = "ITEM_ETC_MP_POTION_"
	battleRoyaleAllPotion = "ITEM_ETC_ALL_POTION_"
	battleRoyalePill      = "ITEM_ETC_CURE_ALL_"
)

// battleRoyaleNavBlocked is a probe result that refuses an edge.
const battleRoyaleNavBlocked = monster.NavResultBlocked | monster.NavResultClipped

/*
================
battleRoyaleBotCommandKind
================
*/
type battleRoyaleBotCommandKind int

const (
	botCommandMove battleRoyaleBotCommandKind = iota
	botCommandInteract
	botCommandUse
)

/*
================
battleRoyaleBotCommand

One client command a bot issues, run after the division lock is released
(runBattleRoyaleBotCommands).
================
*/
type battleRoyaleBotCommand struct {
	c       *enterworld.Character
	kind    battleRoyaleBotCommandKind
	payload []byte
}

/*
================
battleRoyaleSight

One participant a bot may see this tick: its target (with its level,
worn gear and HP share), the bot it is when it is one, and the object
its attack holds (aim, 0 none).
================
*/
type battleRoyaleSight struct {
	target battleroyale.BotTarget
	bot    *battleRoyaleBot
	aim    uint32
}

/*
================
battleRoyaleBotSkills

A bot's learned skills as its presses use them: self buffs, heals, and
attack skills strongest first (by the mastery level they need, then
rank).
================
*/
type battleRoyaleBotSkills struct {
	buffs   []enterworld.SkillRow
	heals   []enterworld.SkillRow
	attacks []enterworld.SkillRow
}

//============================================================================

/*
================
advanceBattleRoyaleBots

One brain tick for every living bot (see the file header), then the
bot map. The corpses that lay long enough leave first. The commands the
decisions queue wait in m.botCommands.
================
*/
func (rt *Runtime) advanceBattleRoyaleBots(m *battleRoyaleMatch, nowMs int64) {
	if !m.layered || len(m.bots) == 0 {
		return
	}
	rt.retireDeadBattleRoyaleBots(m, nowMs)
	if m.botNav == nil {
		m.botNav = battleroyale.NewBotNav(rt.battleRoyaleGroundMask())
		m.botNav.SetWalkTest(rt.battleRoyaleBotWalk())
	}
	if m.botRng == nil {
		m.botRng = rand.New(rand.NewPCG(m.seed, battleRoyaleBotStream))
	}
	m.botNav.BeginTick(battleroyale.BotNavTickBudget)
	profile := battleroyale.ProfileFor(m.botDifficulty)
	sights := rt.battleRoyaleSights(m, nowMs)
	aims := battleRoyaleBotAims(sights)
	drops := rt.battleRoyaleDrops(m)
	zone, zoned := m.match.ZonePlan()
	for _, bot := range m.bots {
		if !bot.alive || bot.c == nil || nowMs < bot.nextThinkMs || !enterworld.CharacterAlive(bot.c) {
			continue
		}
		bot.nextThinkMs = nowMs + profile.ThinkMs
		senses := rt.battleRoyaleBotSenses(m, bot, profile, sights, aims, drops, nowMs)
		senses.Zone, senses.Zoned = zone, zoned
		switch bot.mind.Drink(senses, profile, m.botRng) {
		case battleroyale.DrinkHP:
			rt.queueBattleRoyalePotion(m, bot, battleRoyaleHPPotion)
		case battleroyale.DrinkMP:
			rt.queueBattleRoyalePotion(m, bot, battleRoyaleMPPotion)
		}
		intent := bot.mind.Decide(senses, profile, m.botRng)
		if rt.watchBattleRoyaleBot(m, bot, intent, sights, nowMs) {
			continue
		}
		rt.actBattleRoyaleBot(m, bot, senses, intent, nowMs)
	}
	rt.pushBattleRoyaleBotMap(m, nowMs)
}

/*
================
battleRoyaleSights

The tick's snapshot: every living participant of the arena (players and
bots) at its live position with its level, worn gear, HP share and the
object its attack holds; each bot's own position is refreshed.
================
*/
func (rt *Runtime) battleRoyaleSights(m *battleRoyaleMatch, nowMs int64) []battleRoyaleSight {
	out := make([]battleRoyaleSight, 0, len(m.bots)+len(m.participants))
	for _, p := range m.match.Participants() {
		if !p.Alive {
			continue
		}
		name := string(p.ID)
		bot := m.battleRoyaleBotNamed(name)
		var c *enterworld.Character
		if bot != nil {
			c = bot.c
		} else {
			c = rt.findCharacter(m.division, name)
		}
		if c == nil || !battleroyale.Held(c) || !enterworld.CharacterAlive(c) || domain.CharacterWorldInstance(c) != uint32(m.world) {
			continue
		}
		at := battleRoyaleSpawnGrid(rt.liveSpawn(simulation.WorldKey(m.division, name), c, nowMs))
		if bot != nil {
			bot.at = at
		}
		target := battleroyale.BotTarget{GID: enterworld.ObjectIDForCharacter(c), At: at, Player: bot == nil, Level: int(rewardLevel(c)),
			Gear: battleRoyaleWornDegreeSum(c)}
		if maxHP, _, hp, _ := rt.playerKeeperVitals(m.division, c); maxHP > 0 {
			target.HPFrac = float64(hp) / float64(maxHP)
		}
		sight := battleRoyaleSight{target: target, bot: bot}
		if intent, ok := rt.combatIntentFor(m.division, name); ok && !intent.FollowTarget {
			sight.aim = intent.TargetGid
		}
		out = append(out, sight)
	}
	return out
}

/*
================
battleRoyaleBotAims

How many bots' attacks hold each object this tick (the brain's
anti-dogpile consideration).
================
*/
func battleRoyaleBotAims(sights []battleRoyaleSight) map[uint32]int {
	aims := map[uint32]int{}
	for _, sight := range sights {
		if sight.bot != nil && sight.aim != 0 {
			aims[sight.aim]++
		}
	}
	return aims
}

/*
================
battleRoyaleDrops

The layer's ground items a bot may want (match equipment, potions and
pills; never gold), where they lie.
================
*/
func (rt *Runtime) battleRoyaleDrops(m *battleRoyaleMatch) []grounditem.Item {
	if rt.Ground == nil {
		return nil
	}
	var out []grounditem.Item
	for _, item := range rt.Ground.AllInPopulation(m.division, grounditem.Population{World: uint32(m.lease.ID), Generation: m.lease.Generation}) {
		if item.IsGold() {
			continue
		}
		if _, equipment := inventory.EquipSocketForTypeFlags(item.TypeFlags); equipment || battleRoyaleConsumable(item.Codename) {
			out = append(out, item)
		}
	}
	return out
}

/*
================
battleRoyaleConsumable

Whether a codename is a match potion or pill.
================
*/
func battleRoyaleConsumable(codename string) bool {
	for _, family := range []string{battleRoyaleHPPotion, battleRoyaleMPPotion, battleRoyaleAllPotion, battleRoyalePill} {
		if strings.HasPrefix(codename, family) {
			return true
		}
	}
	return false
}

/*
================
battleRoyaleBotSenses

What the bot senses: its level, worn gear, HP and MP shares, the target
its attack holds, the foes within the disengage range
(BotDisengageFactor x the sense range) with whether each attacks it and
how many other bots attack each (aims), the nearest real player, the
nearest drop it wants, and (when its attack holds a monster, or it still
levels) that monster or the nearest wolf within BotFarmRangeM. Stage 8c
searches the wolf whatever foe is in sense: the utility brain weighs
farming against every fight.
================
*/
func (rt *Runtime) battleRoyaleBotSenses(m *battleRoyaleMatch, bot *battleRoyaleBot, profile battleroyale.BotProfile, sights []battleRoyaleSight,
	aims map[uint32]int, drops []grounditem.Item, nowMs int64) battleroyale.BotSenses {
	c := bot.c
	s := battleroyale.BotSenses{NowMs: nowMs, At: bot.at, Level: int(rewardLevel(c)), Gear: battleRoyaleWornDegreeSum(c), HPFrac: 1, MPFrac: 1}
	if maxHP, maxMP, hp, mp := rt.playerKeeperVitals(m.division, c); maxHP > 0 {
		s.HPFrac = float64(hp) / float64(maxHP)
		if maxMP > 0 {
			s.MPFrac = float64(mp) / float64(maxMP)
		}
	}
	if intent, ok := rt.combatIntentFor(m.division, c.Name); ok && !intent.FollowTarget {
		s.Engaged = intent.TargetGid
	}
	reach := profile.SenseRange() * battleroyale.BotDisengageFactor
	own := uint32(0)
	for _, sight := range sights {
		if sight.bot == bot {
			own = sight.aim
		}
	}
	bestPlayer := math.Inf(1)
	for _, sight := range sights {
		if sight.bot == bot {
			continue
		}
		d := battleRoyaleDistance(bot.at, sight.target.At)
		if sight.target.Player && d < bestPlayer {
			bestPlayer, s.Player = d, sight.target
		}
		if d <= reach {
			foe := sight.target
			foe.Attacking = sight.aim == bot.gid
			foe.Attackers = aims[foe.GID]
			if own == foe.GID {
				foe.Attackers--
			}
			s.Foes = append(s.Foes, foe)
		}
	}
	if s.Engaged > domain.MonsterGIDBase && s.Engaged <= domain.MonsterGIDLimit {
		s.Wolf = rt.battleRoyaleBotMonster(m, s.Engaged, nowMs)
	}
	if s.Wolf.GID == 0 && int64(s.Level) < battleroyale.MatchLevelCap {
		s.Wolf = rt.battleRoyaleBotWolf(m, bot, nowMs)
	}
	s.Loot = rt.battleRoyaleBotLoot(m, bot, drops)
	return s
}

/*
================
battleRoyaleBotMonster

A living monster of the layer at its live pose, or the zero target.
================
*/
func (rt *Runtime) battleRoyaleBotMonster(m *battleRoyaleMatch, gid uint32, nowMs int64) battleroyale.BotTarget {
	actor, ok := rt.Monsters.GetInPopulation(m.division, m.lease, gid)
	if !ok || actor.CurrentHP == 0 {
		return battleroyale.BotTarget{}
	}
	at := battleRoyalePoseGrid(monster.Pose{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z})
	if mover, ok := rt.Monsters.Mover(m.division, gid); ok {
		at = battleRoyalePoseGrid(mover.LivePoseAt(nowMs, nil))
	}
	return battleroyale.BotTarget{GID: gid, At: at}
}

/*
================
battleRoyaleBotWolf

The nearest living monster of the layer within BotFarmRangeM (a wolf or
the supply giant), or the zero target.
================
*/
func (rt *Runtime) battleRoyaleBotWolf(m *battleRoyaleMatch, bot *battleRoyaleBot, nowMs int64) battleroyale.BotTarget {
	centre := battleRoyaleSpawnAt(bot.at, 0)
	best, bestD := battleroyale.BotTarget{}, math.Inf(1)
	for _, candidate := range rt.Monsters.CombatCandidatesInPopulation(m.division, m.lease, centre, battleroyale.BotFarmRangeM*battleroyale.UnitsPerMeter, nowMs, true) {
		at := battleRoyalePoseGrid(monster.Pose{RegionID: candidate.Spawn.RegionID, X: candidate.Spawn.X, Z: candidate.Spawn.Z})
		if mover, ok := rt.Monsters.Mover(m.division, candidate.Gid); ok {
			at = battleRoyalePoseGrid(mover.LivePoseAt(nowMs, nil))
		}
		if d := battleRoyaleDistance(bot.at, at); d < bestD {
			best, bestD = battleroyale.BotTarget{GID: candidate.Gid, At: at}, d
		}
	}
	return best
}

/*
================
battleRoyaleBotLoot

The nearest drop within BotLootRangeM the bot may pick up and wants: a
potion or pill while it carries fewer than battleRoyaleBotPotionStock of
that kind, or equipment the set planner would wear (battleroyale.
PlanGear over its gear and the drop, as the pickup's auto-equip will).
Drops reserved for someone else are left alone. The zero target when
none.
================
*/
func (rt *Runtime) battleRoyaleBotLoot(m *battleRoyaleMatch, bot *battleRoyaleBot, drops []grounditem.Item) battleroyale.BotTarget {
	reach := battleroyale.BotLootRangeM * battleroyale.UnitsPerMeter
	type candidate struct {
		item grounditem.Item
		at   battleroyale.Point
		d    float64
	}
	var near []candidate
	for _, item := range drops {
		if item.OwnerJID != 0 && item.OwnerJID != bot.gid {
			continue
		}
		at := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: item.Position.RegionID, X: float64(item.Position.X), Z: float64(item.Position.Z)})
		if d := battleRoyaleDistance(bot.at, at); d <= reach {
			near = append(near, candidate{item: item, at: at, d: d})
		}
	}
	sort.Slice(near, func(i, j int) bool { return near[i].d < near[j].d })
	if len(near) == 0 || !rt.battleRoyaleBagHasRoom(bot.c) {
		return battleroyale.BotTarget{}
	}
	weighed := 0
	for _, n := range near {
		if battleRoyaleConsumable(n.item.Codename) {
			if rt.battleRoyaleCarried(bot.c, n.item.Codename) < battleRoyaleBotPotionStock {
				return battleroyale.BotTarget{GID: n.item.Gid, At: n.at}
			}
			continue
		}
		if weighed == battleRoyaleBotLootChecks {
			continue
		}
		weighed++
		if rt.battleRoyaleWouldWear(bot.c, n.item) {
			return battleroyale.BotTarget{GID: n.item.Gid, At: n.at}
		}
	}
	return battleroyale.BotTarget{}
}

/*
================
battleRoyaleBagHasRoom

Whether the bag has a free slot.
================
*/
func (rt *Runtime) battleRoyaleBagHasRoom(c *enterworld.Character) bool {
	_, free := firstFreeBagSlot(c.MissionInventory, inventory.BagEnd(c))
	return free
}

/*
================
battleRoyaleCarried

How many items of a codename's potion family (its codename without the
degree) the bag holds.
================
*/
func (rt *Runtime) battleRoyaleCarried(c *enterworld.Character, codename string) int64 {
	family := codename
	if cut := strings.LastIndex(codename, "_"); cut > 0 {
		family = codename[:cut+1]
	}
	total := int64(0)
	for _, row := range c.MissionInventory {
		if strings.HasPrefix(row.Codename, family) {
			total += max(row.StackCount, 1)
		}
	}
	return total
}

/*
================
battleRoyaleWouldWear

Whether the set planner (battleroyale.PlanGear, the auto-equip's own)
would wear the ground item next to the bot's gear.
================
*/
func (rt *Runtime) battleRoyaleWouldWear(c *enterworld.Character, item grounditem.Item) bool {
	killer, pieces, _, ok := rt.battleRoyaleGear(c)
	items := rt.deps.ItemReferences()
	if !ok || items == nil {
		return false
	}
	ref, known := items.ItemRefByCodename(item.Codename)
	if !known || ref == nil {
		return false
	}
	piece := battleroyale.GearPiece{Codename: item.Codename, Plus: item.Plus, Blues: len(item.MagicOptions)}
	for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
		value := (item.VarianceBits >> (field * varianceFieldBits)) & battleRoyaleWhiteMax
		piece.WhitePercent += int(value) * whitePercentMax / battleRoyaleWhiteMax
	}
	for _, option := range item.MagicOptions {
		piece.BlueValues += int64(option >> 32)
	}
	candidate := len(pieces)
	for _, index := range battleroyale.PlanGear(killer, append(pieces, piece)).Wear {
		if index == candidate {
			return true
		}
	}
	return false
}

//============================================================================

/*
================
actBattleRoyaleBot

Carries one decision out (see the file header).
================
*/
func (rt *Runtime) actBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot, s battleroyale.BotSenses, intent battleroyale.BotIntent, nowMs int64) {
	// A fight the mind let go ends at once (the client's cancel), whatever
	// comes next: a walk leg may not be due yet, and a press replaces it.
	if s.Engaged != 0 && intent.Target != s.Engaged && intent.Action != battleroyale.BotAttack {
		m.botCommands = append(m.botCommands, battleRoyaleBotCommand{c: bot.c, kind: botCommandInteract,
			payload: wire.TargetInteract{Cancel: true}.Encode()})
	}
	switch intent.Action {
	case battleroyale.BotGoZone, battleroyale.BotRoam, battleroyale.BotChase, battleroyale.BotRetreat:
		rt.travelBattleRoyaleBot(m, bot, intent.Goal, nowMs)
	case battleroyale.BotAttack:
		bot.routing = false
		if payload := rt.battleRoyaleBotPress(m, bot, intent.Target, s, nowMs); payload != nil {
			m.botCommands = append(m.botCommands, battleRoyaleBotCommand{c: bot.c, kind: botCommandInteract, payload: payload})
		}
	case battleroyale.BotPickup:
		bot.routing = false
		if pending, ok := rt.Pending.Peek(grounditem.PendingKey(m.division, bot.name)); ok && pending.ItemGid == intent.Target {
			return
		}
		m.botCommands = append(m.botCommands, battleRoyaleBotCommand{c: bot.c, kind: botCommandInteract,
			payload: wire.TargetInteract{Gid: intent.Target}.Encode()})
	}
}

/*
================
travelBattleRoyaleBot

Walks the bot toward goal on its planned route: a goal that moved more
than battleRoyaleBotRegoalM restarts the route; the next leg is clicked
when the bot is not already walking it. A route the planner gave up on
(unreachable, stuck too often) falls back to one straight leg and lets
the mind pick a new roam goal.
================
*/
func (rt *Runtime) travelBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot, goal battleroyale.Point, nowMs int64) {
	if !bot.routing || battleRoyaleDistance(bot.route.Goal, goal) > battleRoyaleBotRegoalM*battleroyale.UnitsPerMeter {
		bot.route.Reset(goal)
		bot.routing = true
	}
	leg, status := m.botNav.Advance(&bot.route, bot.at, nowMs)
	switch status {
	case battleroyale.RouteArrived, battleroyale.RouteWaiting:
		return
	case battleroyale.RouteFailed:
		bot.routing, bot.mind.NextRoamMs = false, 0
		leg = goal
		if d := battleRoyaleDistance(bot.at, goal); d > battleRoyaleBotStraightM*battleroyale.UnitsPerMeter {
			scale := battleRoyaleBotStraightM * battleroyale.UnitsPerMeter / d
			leg = battleroyale.Point{X: bot.at.X + (goal.X-bot.at.X)*scale, Z: bot.at.Z + (goal.Z-bot.at.Z)*scale}
		}
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(m.division, bot.name), func() simulation.WorldState { return simulation.SeedWorldState(bot.c) })
	if leg == bot.leg && world.MovingAt(nowMs) {
		return
	}
	bot.leg = leg
	at, _ := rt.battleRoyaleSurface(leg)
	payload := simulation.EncodeClientMovementRequest(simulation.MovementRequest{Mode: simulation.MovementAckDestinationMode, RegionID: at.RegionID,
		X: at.X, Y: at.Y, Z: at.Z})
	m.botCommands = append(m.botCommands, battleRoyaleBotCommand{c: bot.c, kind: botCommandMove, payload: payload})
}

/*
================
battleRoyaleBotPress

The press on target, as the client's 0x7074 body: a self heal when hurt,
a buff of the build that is not up while the MP pool is at least
battleRoyaleBotBuffMP full, the strongest attack skill ready,
else the basic attack unless the attack already holds the target (nil:
nothing to press). Readiness is the server's own command admission
(skillAdmission, mask admitCommand: cooldown, MP, weapon, ammunition),
so a press is never one the handler would refuse for those.
================
*/
func (rt *Runtime) battleRoyaleBotPress(m *battleRoyaleMatch, bot *battleRoyaleBot, target uint32, s battleroyale.BotSenses, nowMs int64) []byte {
	snapshot := rt.characterSnapshot(m.division, bot.c)
	if snapshot == nil {
		return nil
	}
	skills := rt.battleRoyaleBotSkillsOf(snapshot)
	ready := func(row enterworld.SkillRow) bool {
		return rt.skillAdmission(m.division, snapshot, row, nowMs, nil, nil, admitCommand) == 0
	}
	if s.HPFrac < battleRoyaleBotHealBelow {
		for _, row := range skills.heals {
			if ready(row) {
				return wire.SkillAction{ActionId: row.ID, HasTarget: row.TargetRequired, TargetGid: bot.gid}.Encode()
			}
		}
	}
	active := map[uint32]bool{}
	for _, effect := range rt.effects.Snapshot(m.division, bot.name) {
		active[effect.SkillGroup] = true
	}
	for _, row := range skills.buffs {
		// A buff spends MP an attack needs: only from a well-filled pool
		// (stage 8c: five fire buffs at level 20 emptied 568 MP and the
		// bot fought on with its basic attack).
		if s.MPFrac >= battleRoyaleBotBuffMP && !active[row.Group] && ready(row) {
			return wire.SkillAction{ActionId: row.ID}.Encode()
		}
	}
	for _, row := range skills.attacks {
		if ready(row) {
			return wire.SkillAction{ActionId: row.ID, HasTarget: row.TargetRequired, TargetGid: target}.Encode()
		}
	}
	if intent, ok := rt.combatIntentFor(m.division, bot.name); ok && intent.TargetGid == target {
		return nil
	}
	return wire.BasicAttackEngage{TargetGid: target}.Encode()
}

/*
================
battleRoyaleBotSkillsOf

The character's learned skills sorted into the press lists (see
battleRoyaleBotSkills): routeSkillCommand's owners decide what a skill
is, exactly as they will run it. Base attacks, untargeted area or status
casts the bot cannot aim, and every other shape are left out.
================
*/
func (rt *Runtime) battleRoyaleBotSkillsOf(c *enterworld.Character) battleRoyaleBotSkills {
	var out battleRoyaleBotSkills
	source := rt.deps.SkillData()
	if source == nil {
		return out
	}
	for _, id := range c.Skills {
		row, ok := source.SkillByID(id)
		if !ok || isPinnedBaseAttack(c, row.Codename) {
			continue
		}
		switch route := routeSkillCommand(row); {
		case row.InstantSelfEffectPinned || row.Imbue.Pinned || route == skillRouteTimedSelf || route == skillRoutePartyBuff:
			out.buffs = append(out.buffs, row)
		case route == skillRouteSupport && row.Heal.Present && row.TargetRequired:
			out.heals = append(out.heals, row)
		case route == skillRouteOffensive && row.TargetRequired && SkillPressHandled(source, row),
			route == skillRouteTimedTarget && row.TargetRequired:
			out.attacks = append(out.attacks, row)
		}
	}
	sort.SliceStable(out.attacks, func(i, j int) bool {
		a, b := out.attacks[i], out.attacks[j]
		if a.Masteries[0].Level != b.Masteries[0].Level {
			return a.Masteries[0].Level > b.Masteries[0].Level
		}
		if a.Level != b.Level {
			return a.Level > b.Level
		}
		return a.ID > b.ID
	})
	return out
}

/*
================
queueBattleRoyalePotion

Queues the item use of the bot's potion of a family (an all-round vigor
potion when it has none), the highest degree it carries.
================
*/
func (rt *Runtime) queueBattleRoyalePotion(m *battleRoyaleMatch, bot *battleRoyaleBot, family string) {
	best, found := domain.InventoryRow{}, false
	for _, prefix := range []string{family, battleRoyaleAllPotion} {
		for _, row := range bot.c.MissionInventory {
			if !inventory.InBag(bot.c, row.Slot) || row.StackCount < 1 || !strings.HasPrefix(row.Codename, prefix) {
				continue
			}
			if !found || row.Codename > best.Codename {
				best, found = row, true
			}
		}
		if found {
			break
		}
	}
	if !found {
		return
	}
	bot.potions++
	payload := wire.NewWriter(3).U8(uint8(best.Slot)).U16(best.TypeFlags).Payload()
	m.botCommands = append(m.botCommands, battleRoyaleBotCommand{c: bot.c, kind: botCommandUse, payload: payload})
}

//============================================================================

/*
================
runBattleRoyaleBotCommands

Runs the commands the bots queued, in order, through the handlers a
client's packets reach; the caller holds no division lock. The public
frames of each result go around their objects (the tick routes a
SourceGID batch to the sessions observing it) and its recipients' frames
to them; the bot's own frames have no session to go to.
================
*/
func (rt *Runtime) runBattleRoyaleBotCommands(division string, commands []battleRoyaleBotCommand) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, command := range commands {
		var result OpResult
		switch command.kind {
		case botCommandMove:
			if rt.battleRoyale != nil && rt.battleRoyale.setup.Move != nil {
				rt.battleRoyale.setup.Move(division, command.c, command.payload)
			}
			continue
		case botCommandInteract:
			result = rt.HandleTargetInteract(division, command.c, command.payload)
		case botCommandUse:
			result = rt.HandleItemUse(division, command.c, command.payload)
		}
		out = append(out, battleRoyaleBotPublication(division, command.c, result)...)
	}
	return out
}

/*
================
battleRoyaleBotPublication

A headless actor's result as tick batches: Broadcast around the actor
(its observers; the actor has no session), Around around each named
object, Recipients to each character.
================
*/
func battleRoyaleBotPublication(division string, c *enterworld.Character, result OpResult) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	if len(result.Broadcast) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, SourceGID: enterworld.ObjectIDForCharacter(c),
			ExceptCharacterID: c.ID, Frames: battleRoyaleSimulationFrames(result.Broadcast)})
	}
	out = append(out, observedDivisionFrames(division, result.Around)...)
	for _, recipient := range result.Recipients {
		if recipient.CharacterID != c.ID && len(recipient.Frames) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: recipient.CharacterID,
				Frames: battleRoyaleSimulationFrames(recipient.Frames)})
		}
	}
	return out
}

/*
================
battleRoyaleSimulationFrames
================
*/
func battleRoyaleSimulationFrames(frames []wire.Frame) []simulation.Frame {
	out := make([]simulation.Frame, len(frames))
	for i, f := range frames {
		out[i] = simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope}
	}
	return out
}

/*
================
battleRoyalePoseGrid
================
*/
func battleRoyalePoseGrid(pose monster.Pose) battleroyale.Point {
	return worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z})
}

/*
================
battleRoyaleSpawnGrid
================
*/
func battleRoyaleSpawnGrid(at simulation.Spawn) battleroyale.Point {
	return worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
}

/*
================
battleRoyaleMonsterPose
================
*/
func battleRoyaleMonsterPose(at simulation.Spawn) monster.Pose {
	return monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}
}
