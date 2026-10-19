/*
===========================================================================

battleroyale_airdrop_test.go - the airdrop's flight line, landing,
marker and ultimate weapon

Port-only, not native (stage 8b, stage 9 item 5). The phase start shows
every participant the 0x3FB6 v2 flight line (state 1) with the time to
landing and the banner; AirdropLeadMs later the guardian stands at the
landing point, a walkable point of the line inside the next circle, and
every participant gets state 2 there; its death sends state 0 and, when
the airdrop holds it, drops the ultimate weapon (+8, Sun, the build's
weapon of the killer's degree, every white at 31, the maximum blues)
and names the finder shard-wide on channel 7; the next phase start
sends state 0 for an airdrop still showing.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
airdropFrames

The decoded 0x3FB6 v2 frames among frames.
================
*/
func airdropFrames(t *testing.T, frames []wire.Frame) []wire.BattleRoyaleAirdrop {
	t.Helper()
	var out []wire.BattleRoyaleAirdrop
	for _, f := range frames {
		if f.Opcode != wire.OpBattleRoyaleAirdrop {
			continue
		}
		p := f.Payload
		if len(p) != wire.BattleRoyaleAirdropBytes || p[0] != wire.BattleRoyaleAirdropVersion || p[1] > wire.BattleRoyaleAirdropLanded {
			t.Fatalf("0x3FB6 % x", p)
		}
		f32 := func(at int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(p[at:])) }
		out = append(out, wire.BattleRoyaleAirdrop{State: p[1], Phase: p[2], AX: f32(3), AZ: f32(7), BX: f32(11), BZ: f32(15),
			X: f32(19), Z: f32(23), EtaMs: binary.LittleEndian.Uint32(p[27:])})
	}
	return out
}

/*
================
lineDistance

The distance of p from the infinite line through a and b.
================
*/
func lineDistance(a, b, p battleroyale.Point) float64 {
	dx, dz := b.X-a.X, b.Z-a.Z
	return math.Abs(dx*(p.Z-a.Z)-dz*(p.X-a.X)) / math.Hypot(dx, dz)
}

/*
================
TestBattleRoyaleAirdrop
================
*/
func TestBattleRoyaleAirdrop(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.addKitItems()
	fx.rt.DropRoll = func() (uint32, error) { return 16000, nil }
	for _, c := range fx.chars {
		fx.pick(t, c, 1)
	}
	fx.startMatch(t)
	m := fx.match()
	zone, _ := m.match.Zone()
	landing := battleroyale.AirdropLandingCircle(zone.Targets[0])

	// Incoming: the line and the time to landing, no guardian yet.
	a := m.airdrop
	if a == nil || a.state != wire.BattleRoyaleAirdropIncoming || a.phase != 0 || a.gid != 0 || a.landsMs != m.startedMs+battleroyale.AirdropLeadMs {
		t.Fatalf("airdrop %+v, started %d", a, m.startedMs)
	}
	for _, instance := range fx.arenaMonsters() {
		if instance.Nest.HasRarityOverride && battleroyale.IsSupplyElite(instance.Ref.Codename, instance.Nest.RarityOverride) {
			t.Fatal("the guardian stands before the airdrop lands")
		}
	}
	for _, c := range fx.chars {
		frames := airdropFrames(t, fx.frames[c.Name])
		if len(frames) != 1 || frames[0].State != wire.BattleRoyaleAirdropIncoming || frames[0].Phase != 0 || frames[0].X != 0 || frames[0].Z != 0 ||
			frames[0].EtaMs == 0 || frames[0].EtaMs > battleroyale.AirdropLeadMs {
			t.Fatalf("%s incoming %+v", c.Name, frames)
		}
		lineA := battleroyale.Point{X: float64(frames[0].AX), Z: float64(frames[0].AZ)}
		lineB := battleroyale.Point{X: float64(frames[0].BX), Z: float64(frames[0].BZ)}
		if !battleroyale.Outside(landing, lineA) || !battleroyale.Outside(landing, lineB) || lineDistance(lineA, lineB, landing.Center) > landing.Radius {
			t.Fatalf("%s line %+v %+v does not cross %+v", c.Name, lineA, lineB, landing)
		}
		if !hasNotice(fx.frames[c.Name], battleroyale.AirdropBanner) || battleroyale.AirdropBanner != "Airdrop incoming - watch the flight line on your map!" {
			t.Fatalf("%s got no flight-line banner: %v", c.Name, noticeTexts(fx.frames[c.Name]))
		}
	}

	// Landing: not a tick before AirdropLeadMs, then the guardian at the
	// landing point, on the line, inside the next circle, walkable.
	fx.tick(time.Duration(a.landsMs-fx.clock.NowMs()-battleRoyaleTickMs) * time.Millisecond)
	if a.state != wire.BattleRoyaleAirdropIncoming || len(airdropFrames(t, fx.frames[fx.chars[0].Name])) != 1 {
		t.Fatalf("landed early: %+v", a)
	}
	fx.tick(battleRoyaleTickMs * time.Millisecond)
	if a.state != wire.BattleRoyaleAirdropLanded || a.gid == 0 || m.airdrop != a {
		t.Fatalf("not landed at %d: %+v", fx.clock.NowMs(), a)
	}
	if lineDistance(a.line.A, a.line.B, a.line.Landing) > 1e-6 || battleroyale.Outside(landing, a.line.Landing) {
		t.Fatalf("landing %+v off the line or outside %+v", a.line.Landing, landing)
	}
	if _, ok := fx.rt.battleRoyaleGround(a.line.Landing); !ok {
		t.Fatalf("landing %+v on refused ground", a.line.Landing)
	}
	var guardian monster.Instance
	for _, instance := range fx.arenaMonsters() {
		if instance.Gid == a.gid {
			guardian = instance
		}
	}
	at := battleRoyaleSpawnAt(a.line.Landing, 0)
	if guardian.Gid == 0 || guardian.Spawn.RegionID != at.RegionID || math.Abs(float64(guardian.Spawn.X-at.X)) > 1 || math.Abs(float64(guardian.Spawn.Z-at.Z)) > 1 {
		t.Fatalf("guardian %+v, landing %+v", guardian.Spawn, at)
	}
	for _, c := range fx.chars {
		frames := airdropFrames(t, fx.frames[c.Name])
		last := frames[len(frames)-1]
		if len(frames) != 2 || last.State != wire.BattleRoyaleAirdropLanded || last.X != float32(a.at.X) || last.Z != float32(a.at.Z) ||
			last.EtaMs != 0 || last.AX != frames[0].AX || last.BZ != frames[0].BZ {
			t.Fatalf("%s landed %+v, airdrop at %+v", c.Name, frames, a.at)
		}
	}

	// The guardian's death: state 0, the ultimate weapon, the shard banner.
	a.ultimate = true
	killer := fx.chars[0]
	spot := simulation.Spawn{RegionID: guardian.Spawn.RegionID, X: 1, Z: 1}
	drops, _ := fx.rt.battleRoyaleLoot(killer, guardian, spot, fx.clock.Now())
	var found int
	for _, drop := range drops {
		if drop.Plus != battleroyale.UltimatePlus {
			continue
		}
		found++
		if drop.Codename != "ITEM_CH_BLADE_01_C_RARE" && drop.Codename != "ITEM_CH_SWORD_01_C_RARE" {
			t.Fatalf("ultimate weapon %s", drop.Codename)
		}
		for field := 0; field < 7; field++ {
			if (drop.VarianceBits>>(field*varianceFieldBits))&battleRoyaleWhiteMax != battleRoyaleWhiteMax {
				t.Fatalf("white field %d of %x", field, drop.VarianceBits)
			}
		}
		want, _ := loot.EquipmentMagicMaximum(drop.Codename)
		if len(drop.MagicOptions) != len(want) || len(want) <= battleRoyaleMinBlues {
			t.Fatalf("%d blues, want %d", len(drop.MagicOptions), len(want))
		}
	}
	if found != 1 {
		t.Fatalf("%d ultimate weapons in %+v", found, drops)
	}
	if !hasNotice(fx.frames["*"], killer.Name+" found the ultimate weapon!") {
		t.Fatalf("no shard banner: %v", noticeTexts(fx.frames["*"]))
	}
	for _, c := range fx.chars {
		frames := airdropFrames(t, fx.frames[c.Name])
		if len(frames) != 3 || frames[2].State != wire.BattleRoyaleAirdropNone || frames[2].Phase != 0 {
			t.Fatalf("%s frames after the loot %+v", c.Name, frames)
		}
	}
	if m.airdrop != nil {
		t.Fatal("the looted airdrop still shows")
	}
	// The guardian's airdrop is taken once.
	drops, _ = fx.rt.battleRoyaleLoot(killer, guardian, spot, fx.clock.Now())
	for _, drop := range drops {
		if drop.Plus == battleroyale.UltimatePlus {
			t.Fatal("a second ultimate weapon")
		}
	}

	// An airdrop still showing at the next phase start goes (state 0).
	m.airdrop = &battleRoyaleAirdrop{division: m.division, match: m.id, phase: 1, state: wire.BattleRoyaleAirdropIncoming}
	fx.rt.expireBattleRoyaleAirdrop(m, fx.clock.NowMs())
	for _, c := range fx.chars {
		frames := airdropFrames(t, fx.frames[c.Name])
		if last := frames[len(frames)-1]; last.State != wire.BattleRoyaleAirdropNone || last.Phase != 1 || m.airdrop != nil {
			t.Fatalf("%s expiry %+v", c.Name, frames)
		}
	}
}

/*
================
TestBattleRoyaleAirdropFollowsThePhases

Every phase start (the zone's waiting event, as the core reports it)
sends state 0 for the last airdrop and state 1 for its own, with the
line across its next circle; every airdrop lands AirdropLeadMs after
its phase starts, on its line, inside its next circle (the final
phase's point target widened to SupplyMinRadius), on walkable ground.
================
*/
func TestBattleRoyaleAirdropFollowsThePhases(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.addKitItems()
	for _, c := range fx.chars {
		fx.pick(t, c, 1)
	}
	fx.startMatch(t)
	m := fx.match()
	zone, _ := m.match.Zone()
	name := fx.chars[0].Name
	for phase := range len(zone.Targets) {
		if phase > 0 {
			fx.rt.replenishBattleRoyaleArena(m, battleroyale.ZonePhase{AtMs: fx.clock.NowMs(), State: battleroyale.ZoneState{Phase: phase,
				Stage: battleroyale.ZoneWaiting, Target: zone.Targets[phase]}}, fx.clock.NowMs())
			frames := airdropFrames(t, fx.frames[name])
			n := len(frames)
			if n < 2 || frames[n-2].State != wire.BattleRoyaleAirdropNone || int(frames[n-2].Phase) != phase-1 ||
				frames[n-1].State != wire.BattleRoyaleAirdropIncoming || int(frames[n-1].Phase) != phase || frames[n-1].EtaMs != battleroyale.AirdropLeadMs {
				t.Fatalf("phase %d start: %+v", phase, frames[max(n-3, 0):])
			}
		}
		a := m.airdrop
		if a == nil || a.phase != phase || a.state != wire.BattleRoyaleAirdropIncoming {
			t.Fatalf("phase %d airdrop %+v", phase, a)
		}
		landing := battleroyale.AirdropLandingCircle(zone.Targets[phase])
		if !battleroyale.Outside(landing, a.line.A) || !battleroyale.Outside(landing, a.line.B) {
			t.Fatalf("phase %d line ends %+v %+v inside %+v", phase, a.line.A, a.line.B, landing)
		}
		// The clock alone moves (the core's own phases would start over
		// this test's): a millisecond early it waits, then it lands.
		fx.clock.Advance(time.Duration(a.landsMs-fx.clock.NowMs()-1) * time.Millisecond)
		fx.rt.landBattleRoyaleAirdrop(m, fx.clock.NowMs())
		if a.state != wire.BattleRoyaleAirdropIncoming {
			t.Fatalf("phase %d landed early", phase)
		}
		fx.clock.Advance(time.Millisecond)
		fx.rt.landBattleRoyaleAirdrop(m, fx.clock.NowMs())
		if a.state != wire.BattleRoyaleAirdropLanded || battleroyale.Outside(landing, a.line.Landing) ||
			lineDistance(a.line.A, a.line.B, a.line.Landing) > 1e-6 {
			t.Fatalf("phase %d: %+v, landing circle %+v", phase, a, landing)
		}
		if _, ok := fx.rt.battleRoyaleGround(a.line.Landing); !ok {
			t.Fatalf("phase %d landing on refused ground", phase)
		}
	}
	if zone.Targets[len(zone.Targets)-1].Radius != 0 {
		t.Fatal("the final target is not a point")
	}
}
