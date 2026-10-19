/*
===========================================================================

battleroyale_airdrop.go - the phase airdrop: flight line, landing,
guardian and the ultimate weapon

Port-only, not native (stage 8b and stage 9 item 5, docs/battle-royale.md;
the rules are battleroyale/airdrop.go). At every phase start
replenishBattleRoyaleArena removes the last phase's airdrop
(expireBattleRoyaleAirdrop, 0x3FB6 state 0) and announces the new one
(announceBattleRoyaleAirdrop): battleroyale.PlanFlightLine draws its
flight line across the next circle and its landing point on walkable
ground (battleRoyaleGround), every participant gets 0x3FB6 v2 state 1
(the line and the time to landing) and the channel-7 banner. Every
running tick landBattleRoyaleAirdrop checks the clock: AirdropLeadMs
after the announcement the guardian (the supply-drop giant wolf)
stands at the landing point, the airdrop is remembered by its gid
(battleRoyaleRuntime.airdrops, for the loot hook, which has no
division), its ultimate weapon is rolled, and every participant gets
state 2 (the landing point, the line kept for reference). The guardian's
death (the loot hook, takeBattleRoyaleAirdrop) or the next phase start
sends state 0; an expired airdrop's guardian keeps its loot.

The ultimate weapon (prepareBattleRoyaleUltimate): the Sun weapon
battleroyale.UltimateWeaponCodename names, at battleroyale.UltimatePlus
(the item's Plus byte, as an enhanced item carries it everywhere), every
white-stat field at 31 (100 %) and loot.EquipmentMagicMaximum's blue
options (the most its native pool allows, at their top values). Its
finder is named shard-wide on channel 7.

===========================================================================
*/

package action

import (
	"math/rand/v2"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// battleRoyaleAirdropStream seeds an airdrop's flight line and landing.
const battleRoyaleAirdropStream = 0x41697244 // "AirD"

/*
================
battleRoyaleAirdrop

One phase's airdrop: its match and phase, its flight line and landing
time, its 0x3FB6 state (none, incoming, landed), and once landed the
guardian's gid and grid position and whether its loot holds the
ultimate weapon.
================
*/
type battleRoyaleAirdrop struct {
	division, match string
	phase           int
	line            battleroyale.FlightLine
	landsMs         int64
	state           uint8
	gid             uint32
	at              battleroyale.Point
	ultimate        bool
}

/*
================
announceBattleRoyaleAirdrop

A phase began: the phase's airdrop takes its flight line across target
(the circle the phase closes to) and shows it to every participant with
the banner; it lands battleroyale.AirdropLeadMs later
(landBattleRoyaleAirdrop). A target whose landing circle has no
walkable point on any drawn line keeps the last line drawn and lets the
guardian's spawn find ground near its landing point.
================
*/
func (rt *Runtime) announceBattleRoyaleAirdrop(m *battleRoyaleMatch, phase int, target battleroyale.Circle, nowMs int64) {
	rng := rand.New(rand.NewPCG(m.seed^uint64(phase+1), battleRoyaleAirdropStream))
	line, ok := battleroyale.PlanFlightLine(target, func(p battleroyale.Point) bool {
		_, walkable := rt.battleRoyaleGround(p)
		return walkable
	}, rng)
	if !ok {
		log.WithFields(log.Fields{"division": m.division, "phase": phase}).Warn("battle royale: no walkable landing on the flight line")
		line, _ = battleroyale.PlanFlightLine(target, nil, rng)
	}
	a := &battleRoyaleAirdrop{division: m.division, match: m.id, phase: phase, line: line,
		landsMs: nowMs + battleroyale.AirdropLeadMs, state: wire.BattleRoyaleAirdropIncoming}
	m.airdrop = a
	rt.pushBattleRoyaleAirdrop(m, a, nowMs)
	rt.battleRoyaleBanner(m, m.participants, battleRoyaleSupplyBanner)
}

/*
================
landBattleRoyaleAirdrop

The running tick: an incoming airdrop whose time has come lands. The
guardian stands at the landing point (walkable ground nearby when the
point is refused), the airdrop is registered by the guardian's gid (the
layer's living supply elite no airdrop holds yet), its ultimate weapon
is rolled (battleroyale.RollAirdropUltimate) and every participant gets
the landed marker at the guardian. Airdrops of earlier matches of the
division are forgotten.
================
*/
func (rt *Runtime) landBattleRoyaleAirdrop(m *battleRoyaleMatch, nowMs int64) {
	a := m.airdrop
	if a == nil || a.state != wire.BattleRoyaleAirdropIncoming || nowMs < a.landsMs || !m.layered || rt.Monsters == nil {
		return
	}
	br := rt.battleRoyale
	br.airdrops.Range(func(key, value any) bool {
		if old := value.(*battleRoyaleAirdrop); old.division == m.division && old.match != m.id {
			br.airdrops.Delete(key)
		}
		return true
	})
	rng := rand.New(rand.NewPCG(m.seed^uint64(a.phase+1)^uint64(nowMs), battleRoyaleAirdropStream))
	near := rt.battleRoyaleGroundMask().Within(battleroyale.Circle{Center: a.line.Landing, Radius: battleroyale.SupplyMinRadius})
	rt.spawnBattleRoyalePlan(m, []battleroyale.MonsterSpawn{battleroyale.SupplyElite(battleroyale.Circle{Center: a.line.Landing})}, near, rng, nowMs)
	phases := 0
	if zone, ok := m.match.ZonePlan(); ok {
		phases = len(zone.Schedule.Phases)
	}
	a.ultimate = battleroyale.RollAirdropUltimate(a.phase, phases, rng)
	for _, actor := range rt.Monsters.PopulationInstances(m.division, m.lease, battleRoyaleRocRegions()) {
		if actor.CurrentHP == 0 || !actor.Nest.HasRarityOverride || !battleroyale.IsSupplyElite(actor.Ref.Codename, actor.Nest.RarityOverride) {
			continue
		}
		if _, held := br.airdrops.Load(actor.Gid); held {
			continue
		}
		// The guardian stands at its fixed spawn (FixedPosition).
		a.gid = actor.Gid
		a.at = worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z})
		break
	}
	if a.gid == 0 {
		log.WithFields(log.Fields{"division": m.division, "phase": a.phase}).Warn("battle royale: airdrop guardian did not stand")
		rt.expireBattleRoyaleAirdrop(m, nowMs)
		return
	}
	a.state = wire.BattleRoyaleAirdropLanded
	br.airdrops.Store(a.gid, a)
	rt.pushBattleRoyaleAirdrop(m, a, nowMs)
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "airdrop_landed", Division: m.division, Match: m.id, Fields: map[string]string{
		"phase": strconv.Itoa(a.phase), "x": strconv.FormatFloat(a.at.X, 'f', 0, 64), "z": strconv.FormatFloat(a.at.Z, 'f', 0, 64),
		"ultimate": strconv.FormatBool(a.ultimate)}})
}

/*
================
expireBattleRoyaleAirdrop

The phase ended: the last airdrop's line or marker goes (0x3FB6 state
0; a landed guardian, if alive, keeps its loot).
================
*/
func (rt *Runtime) expireBattleRoyaleAirdrop(m *battleRoyaleMatch, nowMs int64) {
	if a := m.airdrop; a != nil && a.state != wire.BattleRoyaleAirdropNone {
		a.state = wire.BattleRoyaleAirdropNone
		rt.pushBattleRoyaleAirdrop(m, a, nowMs)
	}
	m.airdrop = nil
}

/*
================
battleRoyaleAirdropFrame

The airdrop's 0x3FB6 v2 at nowMs: the line always, the landing point
once landed, the time to landing while incoming.
================
*/
func battleRoyaleAirdropFrame(a *battleRoyaleAirdrop, nowMs int64) wire.Frame {
	frame := wire.BattleRoyaleAirdrop{State: a.state, Phase: uint8(a.phase),
		AX: float32(a.line.A.X), AZ: float32(a.line.A.Z), BX: float32(a.line.B.X), BZ: float32(a.line.B.Z)}
	switch a.state {
	case wire.BattleRoyaleAirdropIncoming:
		frame.EtaMs = uint32(max(a.landsMs-nowMs, 0))
	case wire.BattleRoyaleAirdropLanded:
		frame.X, frame.Z = float32(a.at.X), float32(a.at.Z)
	}
	return wire.BattleRoyaleAirdropFrame(frame)
}

/*
================
pushBattleRoyaleAirdrop

The airdrop's 0x3FB6 to every participant.
================
*/
func (rt *Runtime) pushBattleRoyaleAirdrop(m *battleRoyaleMatch, a *battleRoyaleAirdrop, nowMs int64) {
	if rt.PushCharacterFrames == nil {
		return
	}
	frames := []wire.Frame{battleRoyaleAirdropFrame(a, nowMs)}
	for _, name := range sortedNames(m.participants) {
		rt.PushCharacterFrames(m.division, name, frames)
	}
}

/*
================
takeBattleRoyaleAirdrop

The airdrop a dying guardian held, once: false for any other monster or
an airdrop of a match that is gone. Its marker goes.
================
*/
func (rt *Runtime) takeBattleRoyaleAirdrop(gid uint32) (*battleRoyaleAirdrop, bool) {
	value, ok := rt.battleRoyale.airdrops.LoadAndDelete(gid)
	if !ok {
		return nil, false
	}
	a := value.(*battleRoyaleAirdrop)
	m := rt.battleRoyaleMatchOf(a.division)
	if m == nil || m.id != a.match {
		return nil, false
	}
	if a.state != wire.BattleRoyaleAirdropNone {
		a.state = wire.BattleRoyaleAirdropNone
		rt.pushBattleRoyaleAirdrop(m, a, rt.Now().UnixMilli())
	}
	if m.airdrop == a {
		m.airdrop = nil
	}
	return a, true
}

/*
================
prepareBattleRoyaleUltimate

The ultimate weapon as a ground item: the codename's row at
UltimatePlus, every white field at 31, the maximum blue options.
================
*/
func (rt *Runtime) prepareBattleRoyaleUltimate(codename string, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	items := rt.deps.ItemReferences()
	if items == nil {
		return grounditem.Item{}, false
	}
	ref, ok := items.ItemRefByCodename(codename)
	if !ok || ref == nil || ref.TypeIDs[1] != 1 {
		log.WithField("codename", codename).Warn("battle royale: ultimate weapon missing from itemdata")
		return grounditem.Item{}, false
	}
	magic, err := loot.EquipmentMagicMaximum(codename)
	if err != nil {
		log.WithError(err).WithField("codename", codename).Warn("battle royale: ultimate weapon magic refused")
		return grounditem.Item{}, false
	}
	var bits uint64
	for field := 0; field < equipmentVarianceFieldCount(ref); field++ {
		bits |= uint64(battleRoyaleWhiteMax) << (field * varianceFieldBits)
	}
	row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1,
		Plus: battleroyale.UltimatePlus, VarianceBits: bits, Durability: combat.DurabilityFromVariance(ref, battleRoyaleWhiteMax),
		MagicOptions: magic}
	return PlanItemDrop(row, 1, at, owner, now), true
}
