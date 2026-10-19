/*
===========================================================================

battleroyale_supplylog.go - the wolf supply's telemetry

Port-only, not native (battleroyale.go; docs/battle-royale.md "Stage
8c"). The live match reported zone_replenished monsters=0 at every phase
start and the owner read it as "no wolves". It was the zone pass at the
phase start only: the 15 s passes before it had already stocked the
next circle to its target, and the local supply, which feeds the
levellers, was never reported at all. So every battleRoyaleSupplyLogMs
the match records battle_royale_wolf_supply: what both supplies stood
and the zone pass retired since the match began, the last zone pass's
stock inside the next circle against its target and the live count
(wolves and bots, against MaxLiveMonsters), and how many levelling bots
have no wolf within BotFarmRangeM (the brain's farm range) at that
moment.

===========================================================================
*/

package action

import (
	"strconv"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/world/simulation"
)

// battleRoyaleSupplyLogMs is the cadence of battle_royale_wolf_supply.
const battleRoyaleSupplyLogMs = 60_000

/*
================
battleRoyaleSupplyLog

The supply's sums of one match (file header) and the next record's due
time.
================
*/
type battleRoyaleSupplyLog struct {
	zone, local, retired int
	inside, target, live int
	nextMs               int64
}

/*
================
recordBattleRoyaleSupply

Every battleRoyaleSupplyLogMs of a running match: the supply record
(file header).
================
*/
func (rt *Runtime) recordBattleRoyaleSupply(m *battleRoyaleMatch, zone battleroyale.ZoneState, nowMs int64) {
	if m.supply.nextMs == 0 {
		m.supply.nextMs = nowMs + battleRoyaleSupplyLogMs
	}
	if nowMs < m.supply.nextMs {
		return
	}
	m.supply.nextMs = nowMs + battleRoyaleSupplyLogMs
	levelling, starved := rt.battleRoyaleStarvedBots(m, nowMs)
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "wolf_supply", Division: m.division, Match: m.id, Fields: map[string]string{
		"phase": strconv.Itoa(zone.Phase), "elapsedMs": strconv.FormatInt(nowMs-m.startedMs, 10),
		"zoneSpawned": strconv.Itoa(m.supply.zone), "localSpawned": strconv.Itoa(m.supply.local), "retired": strconv.Itoa(m.supply.retired),
		"inside": strconv.Itoa(m.supply.inside), "target": strconv.Itoa(m.supply.target), "live": strconv.Itoa(m.supply.live),
		"levellingBots": strconv.Itoa(levelling), "botsWithoutWolf": strconv.Itoa(starved)}})
}

/*
================
battleRoyaleStarvedBots

The living bots below the level cap, and how many of them have no
living match wolf within BotFarmRangeM.
================
*/
func (rt *Runtime) battleRoyaleStarvedBots(m *battleRoyaleMatch, nowMs int64) (levelling, starved int) {
	wolves, _ := rt.battleRoyaleCensus(m, nowMs)
	reach := battleroyale.BotFarmRangeM * battleroyale.UnitsPerMeter
	for _, bot := range m.bots {
		if !bot.alive || bot.c == nil || rewardLevel(bot.c) >= battleroyale.MatchLevelCap {
			continue
		}
		levelling++
		at := battleRoyaleSpawnGrid(rt.liveSpawn(simulation.WorldKey(m.division, bot.name), bot.c, nowMs))
		near := false
		for _, wolf := range wolves {
			if battleRoyaleDistance(at, wolf.at) <= reach {
				near = true
				break
			}
		}
		if !near {
			starved++
		}
	}
	return levelling, starved
}
