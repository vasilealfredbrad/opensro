/*
===========================================================================

battleroyale_match.go - a match's life in the world: drop, zone, exit

Port-only, not native (battleroyale.go). The core decides when a match
starts, who is out and who won; this file does what each decision means
in the world: a layer of world 29 and its monsters, the record swap and
relocation of every participant (hold at the drop, restore on the way
out), the zone's damage, the winner's prize and the layer's release. The
caller of every function holds the division lock.

===========================================================================
*/

package action

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
handleBattleRoyaleEvent

One core event, in the order Tick reported them.
================
*/
func (rt *Runtime) handleBattleRoyaleEvent(m *battleRoyaleMatch, event battleroyale.Event, nowMs int64) {
	switch e := event.(type) {
	case battleroyale.CountdownStarted:
		rt.battleRoyaleBanner(m, m.members, fmt.Sprintf("Battle Royale starts in %d s", battleroyale.DefaultCountdownMs/1000))
		rt.pushBattleRoyaleStates(m, m.members, nowMs)
	case battleroyale.CountdownTick:
		if e.SecondsLeft <= 5 || e.SecondsLeft == 10 {
			rt.battleRoyaleBanner(m, m.members, fmt.Sprintf("Battle Royale starts in %d s", e.SecondsLeft))
		}
	case battleroyale.CountdownCancelled:
		rt.battleRoyaleBanner(m, m.members, "Battle Royale cancelled: not enough players")
		rt.pushBattleRoyaleStates(m, m.members, nowMs)
	case battleroyale.MatchStarted:
		rt.beginBattleRoyale(m, e, nowMs)
	case battleroyale.ZonePhase:
		rt.announceBattleRoyaleZone(m, e)
		if e.State.Stage == battleroyale.ZoneWaiting {
			rt.replenishBattleRoyaleArena(m, e, nowMs)
		}
		rt.pushBattleRoyaleStates(m, m.participants, nowMs)
	case battleroyale.PlayerEliminated:
		rt.eliminateBattleRoyalePlayer(m, e, nowMs)
	case battleroyale.MatchEnded:
		rt.endBattleRoyale(m, e, nowMs)
	case battleroyale.MatchCancelled:
		m.endedMs = nowMs
	}
}

//============================================================================

/*
================
beginBattleRoyale

The drop: allocate a layer of the arena world, fill it with the match
monsters, then hold and relocate every participant. A participant the
world cannot admit is out before it starts.
================
*/
func (rt *Runtime) beginBattleRoyale(m *battleRoyaleMatch, started battleroyale.MatchStarted, nowMs int64) {
	m.startedMs = nowMs
	m.nextSupplyMs, m.nextLocalSupplyMs, m.nextGroundSweepMs = nowMs+battleroyale.SupplyIntervalMs, nowMs, nowMs
	// m.participants are the players who receive the match's frames; the
	// bots have no session.
	m.participants = map[string]bool{}
	for _, p := range started.Participants {
		if !p.Bot {
			m.participants[string(p.ID)] = true
		}
	}
	if !rt.allocateBattleRoyaleLayer(m) {
		log.Warn("battle royale: no arena layer; the match is cancelled")
		rt.battleRoyaleBanner(m, m.participants, "Battle Royale cancelled: no arena available")
		_ = m.match.Cancel("no arena layer", nowMs)
		m.endedMs = nowMs
		return
	}
	spawned := rt.spawnBattleRoyaleMonsters(m, started.Arena)
	rng := rand.New(rand.NewPCG(m.seed, battleRoyaleMonsterStream))
	builds := make([]string, 0, len(started.Participants))
	for _, p := range started.Participants {
		if p.Bot {
			continue
		}
		builds = append(builds, string(p.ID)+":"+p.Build)
		if !rt.dropBattleRoyalePlayer(m, p, started.Arena, rng, nowMs) {
			_ = m.match.Eliminate(p.ID, "", battleroyale.EliminatedLeftArena, nowMs)
		}
	}
	// The bots stand after the players, in their ring around the drops.
	bots := 0
	if m.match.BotsEnabled() {
		bots = rt.spawnBattleRoyaleBots(m, started, nowMs)
	}
	rt.battleRoyaleBanner(m, m.participants, "Battle Royale: fight to the last player")
	rt.pushBattleRoyaleStates(m, m.participants, nowMs)
	if len(m.bots) > 0 {
		rt.pushBattleRoyaleRoster(m, m.participants)
	}
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "match_started", Division: m.division, Match: m.id, Fields: map[string]string{
		"players": strconv.Itoa(len(m.participants)), "bots": strconv.Itoa(bots),
		"botDifficulty": strconv.Itoa(int(m.botDifficulty)), "botCount": strconv.Itoa(m.botCount), "monsters": strconv.Itoa(spawned), "builds": strings.Join(builds, ","),
		"world": fmt.Sprintf("%08x", uint32(m.world)), "arenaRadius": strconv.FormatFloat(started.Arena.Radius, 'f', 0, 64),
		"seed": strconv.FormatUint(m.seed, 10)}})
}

/*
================
allocateBattleRoyaleLayer

The first free layer of the arena world.
================
*/
func (rt *Runtime) allocateBattleRoyaleLayer(m *battleRoyaleMatch) bool {
	if rt.Monsters == nil {
		return false
	}
	for layer := uint16(1); layer <= battleRoyaleMaxLayer; layer++ {
		id := instance.Pack(battleRoyaleWorld, layer)
		if _, allocated := rt.Monsters.PopulationLease(m.division, id); allocated {
			continue
		}
		lease, status := rt.Monsters.AllocatePopulation(m.division, id)
		switch status {
		case instance.Success:
			m.lease, m.world, m.layered = lease, id, true
			return true
		case instance.AllocationFull, instance.InvalidLayer:
			return false
		}
	}
	return false
}

/*
================
battleRoyaleSpawnAt

A grid point as a spawn in its own region.
================
*/
func battleRoyaleSpawnAt(point battleroyale.Point, y float64) simulation.Spawn {
	region := worldgeom.RegionIDForSectors(int(point.X/worldgeom.OutdoorRegionSize), int(point.Z/worldgeom.OutdoorRegionSize))
	local := worldgeom.LocalFromGrid(region, point)
	return simulation.Spawn{RegionID: local.RegionID, X: local.X, Y: y, Z: local.Z}
}

/*
================
battleRoyaleGround

The walkable ground at a grid point a match may use: inside the main
walkable component of Roc (battleRoyaleGroundMask, stage 4) and walkable
at the point itself, when the world can say.
================
*/
func (rt *Runtime) battleRoyaleGround(point battleroyale.Point) (simulation.Spawn, bool) {
	if !rt.battleRoyaleGroundMask().Contains(point) {
		return battleRoyaleSpawnAt(point, 0), false
	}
	return rt.battleRoyaleSurface(point)
}

/*
================
battleRoyaleGroundMask

The walkable Roc mask, built once from battleRoyaleSurface on first use
(about 90 000 ground tests over the Roc grid; the navmesh does not change
while the server runs). A runtime without the mode builds a throwaway
mask.
================
*/
func (rt *Runtime) battleRoyaleGroundMask() *battleroyale.GroundMask {
	br := rt.battleRoyale
	if br == nil {
		return battleroyale.BuildGroundMask(func(p battleroyale.Point) bool { _, ok := rt.battleRoyaleSurface(p); return ok })
	}
	br.groundMu.Lock()
	defer br.groundMu.Unlock()
	if br.ground == nil {
		br.ground = battleroyale.BuildGroundMask(func(p battleroyale.Point) bool { _, ok := rt.battleRoyaleSurface(p); return ok })
		log.WithField("km2", br.ground.Area()/1e6).Info("battle royale: walkable Roc ground mask built")
	}
	return br.ground
}

/*
================
battleRoyaleSurface

The walkable ground at a grid point, when the world can say (the region
is available and the navmesh stands it).
================
*/
func (rt *Runtime) battleRoyaleSurface(point battleroyale.Point) (simulation.Spawn, bool) {
	at := battleRoyaleSpawnAt(point, 0)
	if rt.SpawnRegionAvailable != nil && !rt.SpawnRegionAvailable(at.RegionID) {
		return at, false
	}
	if rt.CompanionSurfaceHeight == nil {
		return at, true
	}
	y, ok := rt.CompanionSurfaceHeight(at.RegionID, at.X, at.Y, at.Z)
	if !ok {
		return at, false
	}
	at.Y = y
	return at, true
}

/*
================
dropBattleRoyalePlayer

Holds one participant's real record, dresses the match character and
relocates it to its drop point (a drawn point off walkable ground is
redrawn), all inside the relocation's own door. A relocation that fails
after the swap is undone at once.
================
*/
func (rt *Runtime) dropBattleRoyalePlayer(m *battleRoyaleMatch, p battleroyale.Participant, arena battleroyale.Circle, rng *rand.Rand, nowMs int64) bool {
	name := string(p.ID)
	c := rt.findCharacter(m.division, name)
	if c == nil || !rt.battleRoyaleAdmitted(m.division, name) {
		return false
	}
	drop, ok := rt.battleRoyaleGround(p.Drop)
	if !ok {
		drop, ok = rt.battleRoyaleRedraw(rt.battleRoyaleGroundMask().Within(arena), rng)
	}
	if !ok {
		return false
	}
	build := battleroyale.BuildByName(p.Build)
	rt.retireBattleRoyaleActor(m.division, name)
	var pets []uint32
	var petFrames []wire.Frame
	relocated := rt.relocateCharacter(m.division, c, "battle-royale-hold", func() (travelPoint, bool) {
		if !enterworld.CharacterAlive(c) || c.DeletePending || battleroyale.Held(c) {
			return travelPoint{}, false
		}
		public, _ := rt.releaseRiddenVehicleInDoor(m.division, c, nowMs)
		petFrames = append(petFrames, public...)
		for _, pet := range c.Companions() {
			if pet.Summoned {
				pet.Summoned = false
				pet.StateFlags &^= cosStateSummoned
				pets = append(pets, pet.GID)
			}
		}
		if err := battleroyale.Hold(c, build, uint32(m.world), nowMs, rt.battleRoyale.setup.Seeders); err != nil {
			return travelPoint{}, false
		}
		rt.learnBattleRoyaleSkills(c, build)
		rt.dressBattleRoyaleKit(c, build)
		return travelPoint{spawn: drop, world: m.world}, true
	})
	for _, gid := range pets {
		rt.retireCosRuntime(m.division, c, gid)
		petFrames = append(petFrames, wire.Frame{Opcode: wire.OpObjectDespawn, Payload: wire.ObjectDespawn{Gid: gid}.Encode()})
	}
	if len(petFrames) > 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(m.division, "", petFrames)
	}
	if !relocated {
		if battleroyale.Held(c) {
			rt.restoreBattleRoyaleRecord(m.division, c)
		}
		return false
	}
	return true
}

/*
================
battleRoyaleAdmitted

Whether the character holds a live world membership (is in the world).
================
*/
func (rt *Runtime) battleRoyaleAdmitted(division, name string) bool {
	_, admitted := rt.characterAdmissions.Load(simulation.WorldKey(division, name))
	return admitted && rt.PushCharacterFrames != nil
}

/*
================
retireBattleRoyaleActor

What neither record may inherit from the other across a swap: the live
effects (a logout would checkpoint them into the record), the pulse
sources and the abnormal block.
================
*/
func (rt *Runtime) retireBattleRoyaleActor(division, name string) {
	rt.effects.Forget(division, name)
	rt.periodicEffects.StopSource(division, name)
	rt.storePlayerAbnormal(division, name, nil)
	rt.ClearCombatIntent(division, name)
	rt.clearSkillFinalizes(division, name)
}

//============================================================================

/*
================
advanceBattleRoyaleArena

The running match's world inputs: a participant outside the arena world
is out; one inside takes the zone's damage for the time since its last
sample. Bots included (stage 8: a bot is a match character); their brain
is battleroyale_botbrain.go.
================
*/
func (rt *Runtime) advanceBattleRoyaleArena(m *battleRoyaleMatch, nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, p := range m.match.Participants() {
		// A bot is a match character too (stage 8): the zone hurts it
		// through the same door.
		if !p.Alive || !p.Connected {
			continue
		}
		name := string(p.ID)
		c := rt.findCharacter(m.division, name)
		if c == nil {
			_ = m.match.Disconnect(p.ID, nowMs)
			continue
		}
		if !battleroyale.Held(c) || domain.CharacterWorldInstance(c) != uint32(m.world) {
			_ = m.match.Eliminate(p.ID, "", battleroyale.EliminatedLeftArena, nowMs)
			continue
		}
		if !enterworld.CharacterAlive(c) {
			continue
		}
		if m.equipPending[name] {
			delete(m.equipPending, name)
			if result, equipped := rt.battleRoyaleAutoEquip(m.division, c); equipped {
				rt.pushBattleRoyaleEquip(m.division, c, result)
			}
		}
		at := rt.liveSpawn(simulation.WorldKey(m.division, name), c, nowMs)
		pos := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: at.RegionID, X: at.X, Z: at.Z})
		maxHP, _, _, _ := rt.playerKeeperVitals(m.division, c)
		damage, err := m.match.ZoneDamage(p.ID, pos, maxHP, nowMs)
		if err != nil || damage <= 0 {
			continue
		}
		out = append(out, rt.applyBattleRoyaleZoneDamage(m.division, c, damage, nowMs)...)
	}
	return out
}

/*
================
applyBattleRoyaleZoneDamage

The zone's HP debit in the character's door; at zero HP the shared death
settles there too (settlePlayerDeathInDoor), which records the
elimination through the death hook (battleroyale_combat.go). The frames
follow the damage-over-time publication: the victim's vitals, the
observers' HP, and on death the effects, the zero baseline and the dead
state.
================
*/
func (rt *Runtime) applyBattleRoyaleZoneDamage(division string, c *enterworld.Character, damage, nowMs int64) []simulation.DivisionFrames {
	var effects, progression []wire.Frame
	fatal := false
	if !rt.deps.UpdateDeferred(c, "battle-royale-zone", func() bool {
		if c.DeletePending || !enterworld.CharacterAlive(c) {
			return false
		}
		_, _, hp, _ := rt.playerKeeperVitals(division, c)
		hp = max(hp-damage, 0)
		c.CurrentHP = &hp
		if hp == 0 {
			fatal = true
			effects, progression = rt.settlePlayerDeathInDoor(division, c, deathKiller{}, nowMs)
		}
		return true
	}) {
		return nil
	}
	gid := enterworld.ObjectIDForCharacter(c)
	frames := playerAbnormalFrames{sourceGID: gid, exceptActor: true}
	values := rt.publishedVitals(division, c)
	frames.actor = append(frames.actor, wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceFlags(abnormalStatusVitalsSource), values)})
	frames.public = append(frames.public, wire.Frame{Opcode: simulation.OpVitalsUpdate,
		Payload: simulation.HPRefreshPayload(gid, simulation.VitalsSourceFlags(abnormalStatusVitalsSource), values.CurrentHP)})
	if fatal {
		rt.bindResidentRegion(simulation.WorldKey(division, c.Name), nowMs)
		frames.public = append(frames.public, effects...)
		life := beginFatalLifePublication(gid)
		baseline := life.publishDeathBaseline()
		dead := life.publishDead()
		frames.public = append(frames.public, wire.Frame{Opcode: baseline.opcode, Payload: baseline.payload}, wire.Frame{Opcode: dead.opcode, Payload: dead.payload})
		frames.actor = append(frames.actor, progression...)
	}
	return playerAbnormalDivisionFrames(division, c.ID, frames)
}

//============================================================================

/*
================
eliminateBattleRoyalePlayer

A participant is out: the kill feed, the restore and the return to the
lobby, its frame and the telemetry. A bot's elimination re-sends the
bot roster instead of a restore.
================
*/
func (rt *Runtime) eliminateBattleRoyalePlayer(m *battleRoyaleMatch, e battleroyale.PlayerEliminated, nowMs int64) {
	victim := string(e.Player)
	var feed string
	switch {
	case e.Killer != "":
		feed = fmt.Sprintf("%s eliminated %s", e.Killer, victim)
	case e.Reason == battleroyale.EliminatedZone:
		feed = fmt.Sprintf("%s died outside the zone", victim)
	case e.Reason == battleroyale.EliminatedLeftArena:
		feed = fmt.Sprintf("%s left the arena", victim)
	case e.Reason == battleroyale.EliminatedDisconnected:
		feed = fmt.Sprintf("%s disconnected", victim)
	default:
		feed = fmt.Sprintf("%s died", victim)
	}
	rt.battleRoyaleFeed(m, feed)
	if e.AliveLeft > 1 {
		rt.battleRoyaleFeed(m, fmt.Sprintf("%d players left", e.AliveLeft))
	}
	if e.Killer != "" {
		if killer, ok := m.match.Participant(e.Killer); ok {
			rt.battleRoyaleKillBanner(m, string(e.Killer), killer.Kills)
		}
	}
	if m.match.IsBot(e.Player) {
		m.battleRoyaleBotEliminated(victim, nowMs)
		rt.pushBattleRoyaleRoster(m, m.participants)
	} else {
		rt.restoreBattleRoyalePlayer(m, victim, nowMs)
	}
	rt.pushBattleRoyaleStates(m, m.participants, nowMs)
	rt.pushBattleRoyaleScoreboard(m, false)
	fields := map[string]string{"killer": string(e.Killer), "reason": e.Reason.String(), "placement": strconv.Itoa(e.Placement),
		"alive": strconv.Itoa(e.AliveLeft), "elapsedMs": strconv.FormatInt(nowMs-m.startedMs, 10)}
	if e.Killer != "" && m.match.IsBot(e.Killer) {
		fields["killerBot"] = "1"
	}
	if bot := m.battleRoyaleBotNamed(victim); bot != nil {
		kills := 0
		if p, ok := m.match.Participant(e.Player); ok {
			kills = p.Kills
		}
		battleRoyaleBotFields(bot, kills, fields)
	}
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "player_eliminated", Division: m.division, Match: m.id, Character: victim, Fields: fields})
}

/*
================
restoreBattleRoyalePlayer

Puts a participant's real record back. A player in the world is
relocated to the lobby with the swap inside the relocation's door; one
who is offline, or whose relocation fails, gets the record alone and
enters at the lobby next time.
================
*/
func (rt *Runtime) restoreBattleRoyalePlayer(m *battleRoyaleMatch, name string, nowMs int64) {
	c := rt.findCharacter(m.division, name)
	if c == nil || !battleroyale.Held(c) {
		return
	}
	m.restored[name] = true
	if !rt.battleRoyaleAdmitted(m.division, name) {
		rt.restoreBattleRoyaleRecord(m.division, c)
		return
	}
	rt.retireBattleRoyaleActor(m.division, name)
	home := rt.battleRoyaleHomeSpawn()
	relocated := rt.relocateCharacter(m.division, c, "battle-royale-restore", func() (travelPoint, bool) {
		if c.DeletePending {
			return travelPoint{}, false
		}
		if _, ok := battleroyale.Restore(c, BattleRoyaleHome()); !ok {
			return travelPoint{}, false
		}
		return travelPoint{spawn: home, world: instance.ID(domain.DefaultWorldInstance)}, true
	})
	if !relocated && battleroyale.Held(c) {
		log.WithFields(log.Fields{"division": m.division, "character": name}).Warn("battle royale: relocation refused; restoring the record only")
		rt.restoreBattleRoyaleRecord(m.division, c)
	}
	rt.storePlayerAbnormal(m.division, name, nil)
}

/*
================
restoreBattleRoyaleRecord

The record-only restore: the real character, standing at the lobby in the
default world, committed in one door.
================
*/
func (rt *Runtime) restoreBattleRoyaleRecord(division string, c *enterworld.Character) bool {
	return rt.deps.Update(c, "battle-royale-restore", func() bool {
		_, ok := battleroyale.Restore(c, BattleRoyaleHome())
		return ok
	})
}

/*
================
battleRoyaleHomeSpawn

The lobby stand as a live spawn, lifted to the ground when the world can.
================
*/
func (rt *Runtime) battleRoyaleHomeSpawn() simulation.Spawn {
	at := simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Z: battleRoyaleLobbyZ}
	if rt.CompanionSurfaceHeight != nil {
		if y, ok := rt.CompanionSurfaceHeight(at.RegionID, at.X, at.Y, at.Z); ok {
			at.Y = y
		}
	}
	return at
}

//============================================================================

/*
================
endBattleRoyale

The result: the winner's shard announcement and silk, or the draw; the
participants' final frame. The arena lingers battleRoyaleLingerMs so the
winner sees it, then closeBattleRoyale restores and releases.
================
*/
func (rt *Runtime) endBattleRoyale(m *battleRoyaleMatch, e battleroyale.MatchEnded, nowMs int64) {
	m.endedMs, m.winner, m.draw = nowMs, string(e.Winner), e.Draw
	fields := map[string]string{"winner": m.winner, "draw": strconv.FormatBool(e.Draw), "players": strconv.Itoa(len(m.participants)),
		"durationMs": strconv.FormatInt(nowMs-m.startedMs, 10), "standings": strconv.Itoa(len(e.Standings))}
	switch {
	case e.Draw || m.winner == "":
		rt.announceBattleRoyaleShard(m.division, "The Battle Royale ended in a draw")
	case m.match.IsBot(e.Winner):
		// A bot wins the match, never silk.
		rt.announceBattleRoyaleShard(m.division, fmt.Sprintf("%s won the Battle Royale!", m.winner))
		fields["bot"] = "true"
	default:
		rt.announceBattleRoyaleShard(m.division, fmt.Sprintf("%s won the Battle Royale!", m.winner))
		if _, err := rt.GrantSilk(m.division, "battle-royale", "battleroyale", m.winner, battleRoyaleWinnerSilk); err != nil {
			log.WithError(err).WithField("character", m.winner).Warn("battle royale: winner silk refused")
			fields["silk"] = "refused"
		} else {
			fields["silk"] = strconv.FormatUint(uint64(battleRoyaleWinnerSilk), 10)
		}
	}
	rt.pushBattleRoyaleStates(m, m.participants, nowMs)
	rt.pushBattleRoyaleScoreboard(m, true)
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "match_ended", Division: m.division, Match: m.id, Character: m.winner, Fields: fields})
}

/*
================
closeBattleRoyale

Restores whoever is still held, purges the arena's ground items, releases
the layer and reopens the lobby.
================
*/
func (rt *Runtime) closeBattleRoyale(m *battleRoyaleMatch, nowMs int64) {
	for _, name := range sortedNames(m.participants) {
		rt.restoreBattleRoyalePlayer(m, name, nowMs)
	}
	// The bots leave the world and the store before their layer does.
	rt.forgetBattleRoyaleBots(m)
	rt.releaseBattleRoyaleLayer(m)
	involved := m.participants
	rt.openBattleRoyaleLobby(m, nowMs)
	rt.pushBattleRoyaleStates(m, involved, nowMs)
}

/*
================
releaseBattleRoyaleLayer
================
*/
func (rt *Runtime) releaseBattleRoyaleLayer(m *battleRoyaleMatch) {
	if !m.layered || rt.Monsters == nil {
		return
	}
	rt.Ground.RemovePopulation(m.division, grounditem.Population{World: uint32(m.lease.ID), Generation: m.lease.Generation})
	rt.Monsters.BeginPopulationRetirement(m.division, m.lease)
	rt.Monsters.ReleasePopulation(m.division, m.lease)
	m.layered = false
}

//============================================================================

/*
================
battleRoyaleDisconnect

A session closed: a lobby member leaves, a participant starts its grace.
The caller holds the division lock (forgetCharacterLocked).
================
*/
func (rt *Runtime) battleRoyaleDisconnect(division, name string, nowMs int64) {
	m := rt.battleRoyaleMatchOf(division)
	if m == nil {
		return
	}
	_ = m.match.Disconnect(battleroyale.PlayerID(name), nowMs)
}

/*
================
battleRoyaleReconnect

A session opened: a participant within its grace is back. The caller
holds the division lock (AdmitCharacterSession).
================
*/
func (rt *Runtime) battleRoyaleReconnect(division, name string, nowMs int64) {
	m := rt.battleRoyaleMatchOf(division)
	if m == nil || m.match.State() != battleroyale.StateRunning {
		return
	}
	_ = m.match.Reconnect(battleroyale.PlayerID(name), nowMs)
}

/*
================
BattleRoyaleEntryRestore

World entry found a held record (enterworld.Deps.RestoreHeldCharacter).
A live participant inside its disconnect grace keeps its match character
and rejoins the running match; everyone else is restored before the
character spawns, so the entry never targets a released layer.
================
*/
func (rt *Runtime) BattleRoyaleEntryRestore(division string, c *enterworld.Character) error {
	unlock := rt.lockDivision(division)
	defer unlock()
	if c == nil || !battleroyale.Held(c) {
		return nil
	}
	nowMs := rt.Now().UnixMilli()
	if m := rt.battleRoyaleMatchOf(division); m != nil && m.layered && m.match.State() == battleroyale.StateRunning &&
		domain.CharacterWorldInstance(c) == uint32(m.world) {
		if p, ok := m.match.Participant(battleroyale.PlayerID(c.Name)); ok && p.Alive &&
			(p.Connected || nowMs < p.DisconnectedAtMs+battleroyale.DefaultDisconnectGraceMs) {
			return nil
		}
	}
	if !rt.restoreBattleRoyaleRecord(division, c) {
		return fmt.Errorf("battle royale: held record of %s could not be restored", c.Name)
	}
	return nil
}

/*
================
RestoreHeldCharacters

Boot recovery, whatever the flag says: every row of every division still
holding a real character (the process died mid-match) gets it back,
standing at the lobby in the default world, through the store's own
door. Returns the names restored, in division and name order.
================
*/
func RestoreHeldCharacters(divisions []string, characters domain.CharacterSource, mutate func(c *domain.Character, label string, fn func())) []string {
	var restored []string
	for _, division := range divisions {
		for _, c := range characters.CharactersForDivision(division) {
			if c == nil || !battleroyale.Held(c) {
				continue
			}
			mutate(c, "battle-royale-restore", func() {
				if _, ok := battleroyale.Restore(c, BattleRoyaleHome()); ok {
					restored = append(restored, division+":"+c.Name)
				}
			})
		}
	}
	return restored
}

/*
================
sortedNames
================
*/
func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
