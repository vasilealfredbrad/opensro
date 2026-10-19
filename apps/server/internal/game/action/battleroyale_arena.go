/*
===========================================================================

battleroyale_arena.go - the arena's monsters and the match character's gear

Port-only, not native (battleroyale.go). What fills the arena and what a
participant carries: the drop's monsters, the zone supply (stage 6:
the wolves left outside the circle retired, the next circle restocked
every 15 s and at every phase start), every phase's supply-drop elite
and the layer's ground-item lifetime and cap, placed on walkable ground
(battleroyale/loot.go and economy.go plan them); the starter kit written into the match character at the hold; and
the auto-equip that seats a better build weapon through the ordinary
inventory move, so the client and every observer get the native move,
visual and stat frames. The caller of every function holds the division
lock.

===========================================================================
*/

package action

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// battleRoyaleSupplyBanner is the channel-7 notice of every phase
	// start: the airdrop's flight line (stage 8b, stage 9).
	battleRoyaleSupplyBanner = battleroyale.AirdropBanner
	// battleRoyaleGroundSweepMs is the cadence of the layer's ground
	// policy (sweepBattleRoyaleGround), the fixture sweep's 5 s.
	battleRoyaleGroundSweepMs = 5000
	// battleRoyaleLocalStream seeds the local supply's draws.
	battleRoyaleLocalStream = 0x4c6f6361 // "Loca"
)

/*
================
spawnBattleRoyaleMonsters

The drop's wolves over the walkable Roc ground of the arena; a point
whose exact spot is not walkable is redrawn on the ground, and a spawn
the population refuses is skipped. Returns how many stood.
================
*/
func (rt *Runtime) spawnBattleRoyaleMonsters(m *battleRoyaleMatch, arena battleroyale.Circle) int {
	rng := rand.New(rand.NewPCG(m.seed, battleRoyaleMonsterStream))
	ground := rt.battleRoyaleGroundMask().Within(arena)
	return rt.spawnBattleRoyalePlan(m, battleroyale.MonsterPlan(ground, rng), ground, rng, m.startedMs)
}

/*
================
spawnBattleRoyalePlan

Spawns each planned monster at its point, redrawing a point the ground
refuses on ground (battleRoyaleRedraw).
================
*/
func (rt *Runtime) spawnBattleRoyalePlan(m *battleRoyaleMatch, plan []battleroyale.MonsterSpawn, ground battleroyale.GroundCells, rng *rand.Rand, nowMs int64) int {
	spawned := 0
	for _, monster := range plan {
		at, ok := rt.battleRoyaleGround(monster.At)
		if !ok {
			at, ok = rt.battleRoyaleRedraw(ground, rng)
		}
		if !ok {
			continue
		}
		if rt.Monsters.SpawnQuestMonster(simulation.QuestMonsterSpawn{Division: m.division, Population: m.lease,
			Codename: monster.Codename, Position: at, FixedPosition: true, NowMs: nowMs, Rarity: monster.Rarity}) {
			spawned++
		}
	}
	return spawned
}

/*
================
battleRoyaleRedraw

A walkable point drawn on ground, up to battleRoyaleDropAttempts draws.
================
*/
func (rt *Runtime) battleRoyaleRedraw(ground battleroyale.GroundCells, rng *rand.Rand) (simulation.Spawn, bool) {
	for range battleRoyaleDropAttempts {
		point, found := ground.Sample(rng)
		if !found {
			break
		}
		if at, ok := rt.battleRoyaleGround(point); ok {
			return at, true
		}
	}
	return simulation.Spawn{}, false
}

/*
================
battleRoyaleRocRegions

Every Roc Mountain region id (battleroyale.InRoc): where the census
(battleRoyaleCensus) counts the layer's living monsters.
================
*/
func battleRoyaleRocRegions() []uint16 {
	var out []uint16
	for y := battleroyale.RocStripSectorY; y <= battleroyale.RocMaxSectorY; y++ {
		for x := battleroyale.RocMinSectorX; x <= battleroyale.RocMaxSectorX; x++ {
			centre := battleroyale.Point{X: (float64(x) + 0.5) * worldgeom.OutdoorRegionSize, Z: (float64(y) + 0.5) * worldgeom.OutdoorRegionSize}
			if battleroyale.InRoc(centre) {
				out = append(out, worldgeom.RegionIDForSectors(x, y))
			}
		}
	}
	return out
}

/*
================
replenishBattleRoyaleArena

A zone phase began its wait: a zone supply pass at once (see
supplyBattleRoyaleZone), the last phase's airdrop removed and the new
one announced with its flight line across the next circle and the
banner; it lands battleroyale.AirdropLeadMs later, its guardian (the
supply-drop giant wolf, whose rarity the loot hook recognizes:
battleroyale.IsSupplyElite) at the landing point (stage 8b and 9,
battleroyale_airdrop.go).
================
*/
func (rt *Runtime) replenishBattleRoyaleArena(m *battleRoyaleMatch, e battleroyale.ZonePhase, nowMs int64) {
	if !m.layered || rt.Monsters == nil {
		return
	}
	target := e.State.Target
	supplied, retired := rt.supplyBattleRoyaleZone(m, e.State, nowMs)
	rt.expireBattleRoyaleAirdrop(m, nowMs)
	rt.announceBattleRoyaleAirdrop(m, e.State.Phase, target, nowMs)
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "zone_replenished", Division: m.division, Match: m.id, Fields: map[string]string{
		"phase": strconv.Itoa(e.State.Phase), "monsters": strconv.Itoa(supplied), "retired": strconv.Itoa(retired),
		"targetRadius": strconv.FormatFloat(target.Radius, 'f', 0, 64)}})
}

/*
================
advanceBattleRoyaleEconomy

The running match's economy cadences (stage 6, battleroyale/economy.go):
every battleRoyaleGroundSweepMs the layer's drops past their lifetime or
over the cap vanish (the 0x36AB despawns are returned) and every living
player wears its best set and drops the rest (stage 8,
battleroyale_autoequip.go), every
SupplyIntervalMs a zone supply pass, every LocalSupplyIntervalMs a local
supply pass.
================
*/
func (rt *Runtime) advanceBattleRoyaleEconomy(m *battleRoyaleMatch, nowMs int64) []simulation.DivisionFrames {
	if !m.layered || rt.Monsters == nil {
		return nil
	}
	var out []simulation.DivisionFrames
	if nowMs >= m.nextGroundSweepMs {
		m.nextGroundSweepMs = nowMs + battleRoyaleGroundSweepMs
		out = rt.sweepBattleRoyaleGround(m, nowMs)
		rt.battleRoyaleGearPasses(m)
	}
	zone, ok := m.match.ZoneAt(nowMs)
	if !ok {
		return out
	}
	if nowMs >= m.nextSupplyMs {
		rt.supplyBattleRoyaleZone(m, zone, nowMs)
	}
	if nowMs >= m.nextLocalSupplyMs {
		rt.localBattleRoyaleSupply(m, zone, nowMs)
	}
	rt.recordBattleRoyaleSupply(m, zone, nowMs)
	return out
}

/*
================
battleRoyaleWolf

One living match wolf of the census: its gid, where it stands now and
whether it is untouched (full HP).
================
*/
type battleRoyaleWolf struct {
	gid       uint32
	at        battleroyale.Point
	untouched bool
}

/*
================
battleRoyaleCensus

The layer's living monsters (everything the live cap counts) and, of
them, the ordinary match wolves at their live pose (supply elites and
bots are live, never stock).
================
*/
func (rt *Runtime) battleRoyaleCensus(m *battleRoyaleMatch, nowMs int64) ([]battleRoyaleWolf, int) {
	var wolves []battleRoyaleWolf
	live := 0
	for _, actor := range rt.Monsters.PopulationInstances(m.division, m.lease, battleRoyaleRocRegions()) {
		if actor.CurrentHP == 0 {
			continue
		}
		live++
		if actor.Ref.Codename != battleroyale.MatchMonsterCodename || m.battleRoyaleBotOf(actor.Gid) != nil ||
			actor.Nest.HasRarityOverride && battleroyale.IsSupplyElite(actor.Ref.Codename, actor.Nest.RarityOverride) {
			continue
		}
		at := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z})
		if mover, ok := rt.Monsters.Mover(m.division, actor.Gid); ok {
			at = battleRoyalePoseGrid(mover.LivePoseAt(nowMs, nil))
		}
		wolves = append(wolves, battleRoyaleWolf{gid: actor.Gid, at: at, untouched: actor.CurrentHP >= actor.EffectiveMaxHP()})
	}
	// The bots are characters from stage 8: they still count against the
	// live cap, so wolves + bots stay under MaxLiveMonsters.
	for _, bot := range m.bots {
		if bot.alive && bot.c != nil {
			live++
		}
	}
	return wolves, live
}

/*
================
supplyBattleRoyaleZone

One zone supply pass (battleroyale/economy.go): the untouched wolves
standing past the current circle's margin are retired through the
monster death path (Defeat, as a native lifetime ends), then the next
circle is topped up to battleroyale.SupplyTarget on its walkable ground
(kept off its edge, 50 m around a point target; a bot weighs
ZoneWolvesPerBot, stage 7), never past battleroyale.MaxLiveMonsters
living monsters in the layer (the bots among them). Returns the
wolves spawned and retired; the next pass is due SupplyIntervalMs later.
================
*/
func (rt *Runtime) supplyBattleRoyaleZone(m *battleRoyaleMatch, zone battleroyale.ZoneState, nowMs int64) (int, int) {
	m.nextSupplyMs = nowMs + battleroyale.SupplyIntervalMs
	wolves, live := rt.battleRoyaleCensus(m, nowMs)
	inside, retired := 0, 0
	for _, wolf := range wolves {
		if wolf.untouched && battleroyale.RetireOutside(zone.Current, wolf.at) && rt.Monsters.Defeat(m.division, wolf.gid, time.UnixMilli(nowMs)) {
			live--
			retired++
			continue
		}
		if !battleroyale.Outside(zone.Target, wolf.at) {
			inside++
		}
	}
	mask := rt.battleRoyaleGroundMask()
	target := zone.Target
	area := mask.Within(battleroyale.Circle{Center: target.Center, Radius: max(target.Radius, battleroyale.SupplyMinRadius)}).Area()
	ground := mask.Within(battleroyale.Circle{Center: target.Center, Radius: max(target.Radius*battleroyale.SupplyEdge, battleroyale.SupplyMinRadius)})
	rng := rand.New(rand.NewPCG(m.seed^uint64(nowMs), battleRoyaleMonsterStream))
	players, bots := battleRoyaleAliveCounts(m)
	supply := battleroyale.Supply{Participants: players, Bots: bots, Inside: inside, Live: live, GroundM2: area}
	plan := battleroyale.SupplyPlan(ground, supply, rng)
	spawned := rt.spawnBattleRoyalePlan(m, plan, ground, rng, nowMs)
	m.supply.zone += spawned
	m.supply.retired += retired
	m.supply.inside, m.supply.target, m.supply.live = inside, battleroyale.SupplyTarget(supply), live+spawned
	return spawned, retired
}

/*
================
localBattleRoyaleSupply

One local supply pass (battleroyale.LocalSupplyPlan): every living
participant below the level cap standing in the current circle (players
at their live position, bots at their body's pose) keeps
battleroyale.LocalWolves wolves within LocalSupplyRadiusM, on the zone's
side of it, never past battleroyale.MaxLiveMonsters. The wolves a
leveller's share stands count for the levellers after it (stage 8c: two
bots standing together each stood a full share, the overshoot filled the
live cap and starved the zone supply). Returns the wolves spawned; the
next pass is due LocalSupplyIntervalMs later.
================
*/
func (rt *Runtime) localBattleRoyaleSupply(m *battleRoyaleMatch, zone battleroyale.ZoneState, nowMs int64) int {
	m.nextLocalSupplyMs = nowMs + battleroyale.LocalSupplyIntervalMs
	wolves, live := rt.battleRoyaleCensus(m, nowMs)
	mask := rt.battleRoyaleGroundMask()
	rng := rand.New(rand.NewPCG(m.seed^uint64(nowMs), battleRoyaleLocalStream))
	spawned := 0
	_, bots := battleRoyaleAliveCounts(m)
	for _, leveller := range rt.battleRoyaleLevellers(m, nowMs) {
		at, want := leveller.at, battleroyale.LocalWolves
		if leveller.bot {
			want = battleroyale.BotLocalWolves(bots)
		}
		room := battleroyale.MaxLiveMonsters - live
		if room <= 0 {
			break
		}
		reach := battleroyale.Circle{Center: at, Radius: battleroyale.LocalSupplyRadiusM * battleroyale.UnitsPerMeter}
		nearby := 0
		for _, wolf := range wolves {
			if !battleroyale.Outside(reach, wolf.at) {
				nearby++
			}
		}
		ground := mask.Within(reach)
		plan := battleroyale.LocalSupplyPlanFor(ground, at, zone, want, nearby, rng)
		plan = plan[:min(len(plan), room)]
		stood := rt.spawnBattleRoyalePlan(m, plan, ground, rng, nowMs)
		// The planned points stand for the new wolves (a redrawn one lands
		// elsewhere on the same ground, rarely).
		for _, spawn := range plan[:min(stood, len(plan))] {
			wolves = append(wolves, battleRoyaleWolf{at: spawn.At, untouched: true})
		}
		live += stood
		spawned += stood
	}
	m.supply.local += spawned
	return spawned
}

/*
================
battleRoyaleAliveCounts

The real players and the bots alive in the match.
================
*/
func battleRoyaleAliveCounts(m *battleRoyaleMatch) (players, bots int) {
	for _, p := range m.match.Participants() {
		switch {
		case !p.Alive:
		case p.Bot:
			bots++
		default:
			players++
		}
	}
	return players, bots
}

/*
================
battleRoyaleLeveller

A participant the local supply serves: where it stands and whether it
is a bot.
================
*/
type battleRoyaleLeveller struct {
	at  battleroyale.Point
	bot bool
}

/*
================
battleRoyaleLevellers

Where every living participant below the level cap stands: the players
in the arena at their live position first, then the bots at their
body's pose, each in participant order.
================
*/
func (rt *Runtime) battleRoyaleLevellers(m *battleRoyaleMatch, nowMs int64) []battleRoyaleLeveller {
	var out, bots []battleRoyaleLeveller
	for _, p := range m.match.Participants() {
		if !p.Alive {
			continue
		}
		if p.Bot {
			// A bot is a match character (stage 8): its own level and live
			// position.
			bot := m.battleRoyaleBotNamed(string(p.ID))
			if bot == nil || !bot.alive || bot.c == nil || rewardLevel(bot.c) >= battleroyale.MatchLevelCap {
				continue
			}
			live := rt.liveSpawn(simulation.WorldKey(m.division, bot.name), bot.c, nowMs)
			bots = append(bots, battleRoyaleLeveller{at: battleRoyaleSpawnGrid(live), bot: true})
			continue
		}
		c := rt.findCharacter(m.division, string(p.ID))
		if c == nil || !battleroyale.Held(c) || !enterworld.CharacterAlive(c) || domain.CharacterWorldInstance(c) != uint32(m.world) ||
			rewardLevel(c) >= battleroyale.MatchLevelCap {
			continue
		}
		live := rt.liveSpawn(simulation.WorldKey(m.division, string(p.ID)), c, nowMs)
		out = append(out, battleRoyaleLeveller{at: worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: live.RegionID, X: live.X, Z: live.Z})})
	}
	return append(out, bots...)
}

/*
================
sweepBattleRoyaleGround

The match layer's ground policy: drops older than
battleroyale.GroundItemLifetimeMs vanish, and past
battleroyale.MaxGroundItems the oldest go first; each leaves with the
0x36AB despawn the fixture sweep sends.
================
*/
func (rt *Runtime) sweepBattleRoyaleGround(m *battleRoyaleMatch, nowMs int64) []simulation.DivisionFrames {
	if rt.Ground == nil {
		return nil
	}
	population := grounditem.Population{World: uint32(m.lease.ID), Generation: m.lease.Generation}
	expired := rt.Ground.ExpirePopulation(m.division, population, time.UnixMilli(nowMs),
		battleroyale.GroundItemLifetimeMs*time.Millisecond, battleroyale.MaxGroundItems)
	out := make([]simulation.DivisionFrames, 0, len(expired))
	for _, item := range expired {
		out = append(out, simulation.DivisionFrames{DivisionID: m.division, SourceGID: item.Gid, Frames: []simulation.Frame{{
			Opcode:  wire.OpObjectDespawn,
			Scope:   []domain.ObjectScopeChange{{GID: item.Gid}},
			Payload: wire.ObjectDespawn{Gid: item.Gid}.Encode(),
		}}})
	}
	return out
}

//============================================================================

/*
================
dressBattleRoyaleKit

Writes the build's starter kit into a freshly held match character (inside
the hold's door, before the re-entry publishes the record, so the client
and its peers see the gear in the spawn itself): the Sun weapon replaces
the creation weapon in socket 6, the shield or ammunition whatever socket
7 held, the potions take the first free bag slots. A codename itemdata
lacks is skipped with a warning; the character keeps its creation gear
there.
================
*/
func (rt *Runtime) dressBattleRoyaleKit(c *enterworld.Character, build battleroyale.Build) {
	items := rt.deps.ItemReferences()
	if items == nil {
		return
	}
	rows := c.MissionInventory
	for _, item := range build.StarterKit() {
		ref, ok := items.ItemRefByCodename(item.Codename)
		if !ok || ref == nil {
			log.WithField("codename", item.Codename).Warn("battle royale: kit item missing from itemdata")
			continue
		}
		slot := int64(item.Slot)
		if item.Slot == battleroyale.KitBagSlot {
			free, found := firstFreeBagSlot(rows, inventory.BagEnd(c))
			if !found {
				continue
			}
			slot = free
		}
		rows = withoutSlot(rows, slot)
		rows = append(rows, rt.battleRoyaleKitRow(ref, slot, item.Count))
	}
	c.MissionInventory = rows
}

/*
================
battleRoyaleKitRow

A fresh row of ref: equipment takes the dropped-equipment variance roll
when the drop source is wired, else the itemdata minimum durability.
================
*/
func (rt *Runtime) battleRoyaleKitRow(ref *enterworld.ItemRef, slot, count int64) domain.InventoryRow {
	row := domain.InventoryRow{Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: count, VarianceBits: "0"}
	if ref.TypeIDs[1] == 1 {
		variance, durability, ok := rt.rollDroppedEquipmentVariance(ref)
		if !ok {
			variance, durability = 0, combat.DurabilityFromVariance(ref, 0)
		}
		row.VarianceBits, row.Durability = strconv.FormatUint(variance, 10), int64(durability)
	}
	return row
}

/*
================
firstFreeBagSlot

The lowest bag slot (13..bagEnd-1) no row occupies.
================
*/
func firstFreeBagSlot(rows []domain.InventoryRow, bagEnd uint8) (int64, bool) {
	used := map[int64]bool{}
	for _, row := range rows {
		used[row.Slot] = true
	}
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(bagEnd); slot++ {
		if !used[slot] {
			return slot, true
		}
	}
	return 0, false
}

/*
================
withoutSlot
================
*/
func withoutSlot(rows []domain.InventoryRow, slot int64) []domain.InventoryRow {
	out := rows[:0:0]
	for _, row := range rows {
		if row.Slot != slot {
			out = append(out, row)
		}
	}
	return out
}

//============================================================================

/*
================
battleRoyaleGearPasses

The periodic gear pass (battleroyale_autoequip.go) of every living,
connected player of the match.
================
*/
func (rt *Runtime) battleRoyaleGearPasses(m *battleRoyaleMatch) {
	for _, p := range m.match.Participants() {
		if !p.Alive || !p.Connected || p.Bot {
			continue
		}
		if c := rt.findCharacter(m.division, string(p.ID)); c != nil && battleroyale.Held(c) {
			rt.battleRoyaleGearPass(m.division, c)
		}
	}
}

/*
================
pushBattleRoyaleEquip

Delivers an auto-equip made outside a request (the tick after a level
gain): the actor's frames to it, the visuals to its peers.
================
*/
func (rt *Runtime) pushBattleRoyaleEquip(division string, c *enterworld.Character, result OpResult) {
	if rt.PushCharacterFrames != nil && len(result.Frames) > 0 {
		rt.PushCharacterFrames(division, c.Name, result.Frames)
	}
	if rt.PushDivisionPeerFrames != nil && len(result.Broadcast) > 0 {
		rt.PushDivisionPeerFrames(division, c.Name, result.Broadcast)
	}
}

/*
================
battleRoyaleKillBanner

The streak banners of a kill count: 3 "is on a rampage", 5 "is
unstoppable", each once per participant.
================
*/
func (rt *Runtime) battleRoyaleKillBanner(m *battleRoyaleMatch, killer string, kills int) {
	var text string
	switch {
	case kills >= battleRoyaleStreakUnstoppable && m.streaks[killer] < battleRoyaleStreakUnstoppable:
		text, m.streaks[killer] = fmt.Sprintf("%s is unstoppable", killer), battleRoyaleStreakUnstoppable
	case kills >= battleRoyaleStreakRampage && m.streaks[killer] < battleRoyaleStreakRampage:
		text, m.streaks[killer] = fmt.Sprintf("%s is on a rampage", killer), battleRoyaleStreakRampage
	default:
		return
	}
	rt.battleRoyaleBanner(m, m.participants, text)
}
