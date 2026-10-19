/*
===========================================================================

battleroyale_botwatch.go - a bot's counters and its stuck relocation

Port-only, not native (battleroyale.go; docs/battle-royale.md "Stage
8c"). Two things the owner asked for after the live matches, beside the
brain itself (battleroyale_botbrain.go):

  - Counters. What a bot did, for the elimination telemetry
    (battleRoyaleBotFields): the skills and buffs it cast (counted where
    every cast starts, startSkillCast) and the drops it picked up
    (counted where a pickup lands, pickup.go). Both paths run outside
    the division lock, so the counters are atomics reached through the
    mode's gid index (battleRoyaleRuntime.botStats).
  - Stuck relocation. The planner re-plans a stuck route (BotRoute,
    5 s) and gives a goal up after eight stucks, but a bot in a pocket
    of the navmesh, or one the movement clip left off the walkable
    ground, can still stand forever. A bot whose decision is a walk and
    that has not come battleRoyaleBotProgressM from where it stood for
    battleRoyaleBotStuckMs, or that stands off the walkable ground mask
    for battleRoyaleBotOffGroundMs, is relocated to a random walkable
    point of the inner part of the circle the zone closes to, out of
    every real player's view when such a point is found (so no viewer
    sees it vanish and appear: the peer visibility despawns it where it
    stood and spawns it where it lands). The relocation is the in-layer
    form of the relocation door (relocation.go) for a character with no
    session: the world plane's stand and the record's world under the
    character's door, the pending pickup, combat intent and queued
    finalizes cleared, the resident region re-bound. Each relocation is
    counted and recorded (battle_royale_bot_relocated).

The caller of every watch function holds the division lock.

===========================================================================
*/

package action

import (
	"strconv"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleBotStuckMs is how long a walking bot may stay within
	// battleRoyaleBotProgressM of where it stood before it is relocated
	// (the owner's ~10 s: two route re-plans of BotStuckMs).
	battleRoyaleBotStuckMs     = 10_000
	battleRoyaleBotProgressM   = 5
	battleRoyaleBotOffGroundMs = 3000
	// battleRoyaleBotRefugeShare is the inner share of the keep circle a
	// relocation lands in; battleRoyaleBotRefugeDraws how many points it
	// draws looking for one out of every player's view.
	battleRoyaleBotRefugeShare = 0.7
	battleRoyaleBotRefugeDraws = 24
)

/*
================
noteBattleRoyaleBotCast

A cast starts (startSkillCast): a bot's cast of a skill that is not its
basic attack counts as a buff (the self and party buff routes, imbues,
instant self effects) or a skill. Nil and a non-bot are native.
================
*/
func (rt *Runtime) noteBattleRoyaleBotCast(c *enterworld.Character, skill enterworld.SkillRow) {
	stats := rt.battleRoyaleBotStatsOf(c)
	if stats == nil || isPinnedBaseAttack(c, skill.Codename) {
		return
	}
	stats.lastSkill.Store(skill.ID)
	switch route := routeSkillCommand(skill); {
	case skill.InstantSelfEffectPinned || skill.Imbue.Pinned || route == skillRouteTimedSelf || route == skillRoutePartyBuff:
		stats.buffs.Add(1)
	default:
		stats.skills.Add(1)
	}
}

/*
================
noteBattleRoyaleBotPickup

A pickup landed in a bot's bag.
================
*/
func (rt *Runtime) noteBattleRoyaleBotPickup(c *enterworld.Character) {
	if stats := rt.battleRoyaleBotStatsOf(c); stats != nil {
		stats.picked.Add(1)
	}
}

/*
================
battleRoyaleBotStatsOf

The counters of a standing bot's character, nil for every other.
================
*/
func (rt *Runtime) battleRoyaleBotStatsOf(c *enterworld.Character) *battleRoyaleBotStats {
	if rt.battleRoyale == nil || c == nil {
		return nil
	}
	value, ok := rt.battleRoyale.botStats.Load(enterworld.ObjectIDForCharacter(c))
	if !ok {
		return nil
	}
	return value.(*battleRoyaleBotStats)
}

/*
================
battleRoyaleWornDegreeSum

The summed degrees of every worn match item (armor, weapon, shield,
accessories; never the job suit or ammunition): the gear the brain
weighs (battleroyale.BotPower).
================
*/
func battleRoyaleWornDegreeSum(c *enterworld.Character) int {
	sum := 0
	for _, row := range c.MissionInventory {
		if row.Slot < 0 || row.Slot > int64(inventory.SocketRingSecond) || row.Slot == int64(inventory.SocketSpecialDress) {
			continue
		}
		if _, equipment := inventory.EquipSocketForTypeFlags(row.TypeFlags); !equipment {
			continue
		}
		sum += battleRoyaleItemDegree(row.Codename)
	}
	return sum
}

//============================================================================

/*
================
battleRoyaleBotWalks

Whether a decision walks the bot (the stuck watch only times walks: a
bot fighting or holding stands still on purpose).
================
*/
func battleRoyaleBotWalks(action battleroyale.BotAction) bool {
	switch action {
	case battleroyale.BotGoZone, battleroyale.BotRoam, battleroyale.BotChase, battleroyale.BotRetreat, battleroyale.BotPickup:
		return true
	}
	return false
}

/*
================
watchBattleRoyaleBot

The stuck watch of one decision (file header). Returns whether the bot
was relocated (the decision is then dropped: the bot thinks again from
its new stand next time).
================
*/
func (rt *Runtime) watchBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot, intent battleroyale.BotIntent, sights []battleRoyaleSight, nowMs int64) bool {
	onGround := rt.battleRoyaleGroundMask().Contains(bot.at)
	if onGround {
		bot.offGroundMs = 0
	} else if bot.offGroundMs == 0 {
		bot.offGroundMs = nowMs
	}
	progress := battleRoyaleBotProgressM * battleroyale.UnitsPerMeter
	// A walk whose goal is already within reach (a bot standing at its
	// zone goal or on its drop) waits on purpose.
	walking := battleRoyaleBotWalks(intent.Action) && battleRoyaleDistance(bot.at, intent.Goal) > progress
	if !walking || battleRoyaleDistance(bot.at, bot.progressAt) > progress {
		bot.progressAt, bot.progressMs = bot.at, nowMs
	}
	reason := ""
	switch {
	case !onGround && nowMs-bot.offGroundMs >= battleRoyaleBotOffGroundMs:
		reason = "off-ground"
	case nowMs-bot.progressMs >= battleRoyaleBotStuckMs:
		reason = "no-progress"
	default:
		return false
	}
	to, ok := rt.battleRoyaleBotRefuge(m, sights, nowMs)
	if !ok {
		// Nowhere to go now: try again after another full wait.
		bot.progressAt, bot.progressMs, bot.offGroundMs = bot.at, nowMs, 0
		return false
	}
	return rt.relocateBattleRoyaleBot(m, bot, to, reason, nowMs)
}

/*
================
battleRoyaleBotRefuge

Where a stuck bot goes: a walkable point (battleRoyaleGround) of the
inner battleRoyaleBotRefugeShare of the circle the zone closes to, the
first of battleRoyaleBotRefugeDraws draws out of every living real
player's interest (worldgeom.InterestVisible), else the first walkable
draw.
================
*/
func (rt *Runtime) battleRoyaleBotRefuge(m *battleRoyaleMatch, sights []battleRoyaleSight, nowMs int64) (simulation.Spawn, bool) {
	zone, ok := m.match.ZoneAt(nowMs)
	if !ok {
		return simulation.Spawn{}, false
	}
	keep := battleroyale.BotKeepCircle(zone)
	ground := rt.battleRoyaleGroundMask().Within(battleroyale.Circle{Center: keep.Center, Radius: keep.Radius * battleRoyaleBotRefugeShare})
	var fallback simulation.Spawn
	found := false
	for range battleRoyaleBotRefugeDraws {
		point, drawn := ground.Sample(m.botRng)
		if !drawn {
			break
		}
		at, walkable := rt.battleRoyaleGround(point)
		if !walkable {
			continue
		}
		if !battleRoyaleSeenByPlayer(at, sights) {
			return at, true
		}
		if !found {
			fallback, found = at, true
		}
	}
	return fallback, found
}

/*
================
battleRoyaleSeenByPlayer

Whether any living real player of the tick's sights has at in its
interest.
================
*/
func battleRoyaleSeenByPlayer(at simulation.Spawn, sights []battleRoyaleSight) bool {
	object := worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z}
	for _, sight := range sights {
		if !sight.target.Player {
			continue
		}
		viewer := battleRoyaleSpawnAt(sight.target.At, 0)
		if worldgeom.InterestVisible(worldgeom.RegionXZ{RegionID: viewer.RegionID, X: viewer.X, Z: viewer.Z}, object) {
			return true
		}
	}
	return false
}

/*
================
relocateBattleRoyaleBot

The in-layer relocation of a session-less bot (file header): the stand
moves under the character's door, the transient action state is
cleared, the brain's route and the watch restart, and the relocation is
counted and recorded.
================
*/
func (rt *Runtime) relocateBattleRoyaleBot(m *battleRoyaleMatch, bot *battleRoyaleBot, to simulation.Spawn, reason string, nowMs int64) bool {
	c := bot.c
	key := simulation.WorldKey(m.division, bot.name)
	from := bot.at
	if !rt.deps.Update(c, "battle-royale-bot-relocate", func() bool {
		state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
			w.Spawn, w.MoveSegment, w.Sitting, w.PostureTransitionUntilMs = to, nil, false, 0
			w.SpawnSet, w.MovementSourceSeeded = true, true
		})
		writeBackWorld(c, state)
		c.World.MoveSegment = nil
		return true
	}) {
		return false
	}
	rt.bindResidentRegion(key, nowMs)
	rt.Pending.Clear(grounditem.PendingKey(m.division, bot.name))
	rt.ClearCombatIntent(m.division, bot.name)
	rt.clearSkillFinalizes(m.division, bot.name)
	bot.at = battleRoyaleSpawnGrid(to)
	bot.routing, bot.leg = false, battleroyale.Point{}
	bot.progressAt, bot.progressMs, bot.offGroundMs = bot.at, nowMs, 0
	bot.mind.NextRoamMs = 0
	bot.relocations++
	log.WithFields(log.Fields{"division": m.division, "bot": bot.name, "reason": reason}).Debug("battle royale: stuck bot relocated")
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "bot_relocated", Division: m.division, Match: m.id, Character: bot.name, Fields: map[string]string{
		"reason": reason, "relocations": strconv.Itoa(bot.relocations), "elapsedMs": strconv.FormatInt(nowMs-m.startedMs, 10),
		"fromX": strconv.FormatFloat(from.X, 'f', 0, 64), "fromZ": strconv.FormatFloat(from.Z, 'f', 0, 64),
		"toX": strconv.FormatFloat(bot.at.X, 'f', 0, 64), "toZ": strconv.FormatFloat(bot.at.Z, 'f', 0, 64)}})
	return true
}
