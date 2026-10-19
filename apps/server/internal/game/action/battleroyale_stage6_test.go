/*
===========================================================================

battleroyale_stage6_test.go - the stage-6 economy through the wiring

Port-only, not native (docs/battle-royale.md "Server notes (stage 6:
economy)"). The zone supply retires the untouched wolves left outside the
circle and restocks the next one; the local supply keeps wolves beside a
participant still levelling and nobody else; the layer's drops vanish
after their lifetime with the despawn frame; a bot at the level cap
leaves the wolves to the players. The whole-match proof is the core's
economy_sim_test.go.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestBattleRoyaleZoneSupplyRetiresAndRestocks

With the zone closed to a 300 m circle around the arena centre, one
pass retires every untouched wolf past its margin (a wounded one stays,
someone is fighting it) and stocks the 200 m next circle with at least
its SupplyTarget.
================
*/
func TestBattleRoyaleZoneSupplyRetiresAndRestocks(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.startMatch(t)
	m := fx.match()
	nowMs := fx.clock.NowMs()
	centre := battleroyale.ArenaCircle().Center
	zone := battleroyale.ZoneState{Current: battleroyale.Circle{Center: centre, Radius: 3000},
		Target: battleroyale.Circle{Center: centre, Radius: 2000}}
	wolves, _ := fx.rt.battleRoyaleCensus(m, nowMs)
	wounded := uint32(0)
	for _, wolf := range wolves {
		if battleroyale.RetireOutside(zone.Current, wolf.at) {
			wounded = wolf.gid
			break
		}
	}
	if _, ok := fx.rt.Monsters.ApplyDamage(testDivision, wounded, 1); !ok || wounded == 0 {
		t.Fatal("no wolf to wound outside the circle")
	}
	_, retired := fx.rt.supplyBattleRoyaleZone(m, zone, nowMs)
	if retired == 0 {
		t.Fatal("nothing retired")
	}
	wolves, live := fx.rt.battleRoyaleCensus(m, nowMs)
	inside, keptWounded := 0, false
	for _, wolf := range wolves {
		if wolf.gid == wounded {
			keptWounded = true
		} else if battleroyale.RetireOutside(zone.Current, wolf.at) {
			t.Fatalf("wolf %d still stands outside the circle", wolf.gid)
		}
		if !battleroyale.Outside(zone.Target, wolf.at) {
			inside++
		}
	}
	area := fx.rt.battleRoyaleGroundMask().Within(zone.Target).Area()
	want := battleroyale.SupplyTarget(battleroyale.Supply{Participants: m.match.AliveCount(), GroundM2: area})
	if !keptWounded || inside < want || live > battleroyale.MaxLiveMonsters {
		t.Fatalf("wounded kept %v, %d wolves inside the next circle (want %d), %d live", keptWounded, inside, want, live)
	}
}

/*
================
TestBattleRoyaleLocalSupplyFollowsLevellers

A pass stands LocalWolves wolves within LocalSupplyRadiusM of each
player still levelling; players at the level cap get none.
================
*/
func TestBattleRoyaleLocalSupplyFollowsLevellers(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.startMatch(t)
	m := fx.match()
	nowMs := fx.clock.NowMs()
	zone, _ := m.match.ZoneAt(nowMs)
	fx.rt.localBattleRoyaleSupply(m, zone, nowMs)
	wolves, _ := fx.rt.battleRoyaleCensus(m, nowMs)
	for _, c := range fx.chars {
		live := fx.rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, nowMs)
		at := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: live.RegionID, X: live.X, Z: live.Z})
		reach := battleroyale.Circle{Center: at, Radius: battleroyale.LocalSupplyRadiusM * battleroyale.UnitsPerMeter}
		near := 0
		for _, wolf := range wolves {
			if !battleroyale.Outside(reach, wolf.at) {
				near++
			}
		}
		if near < battleroyale.LocalWolves {
			t.Fatalf("%s has %d wolves within %d m", c.Name, near, battleroyale.LocalSupplyRadiusM)
		}
	}
	capped := battleroyale.MatchLevelCap
	for _, c := range fx.chars {
		c.Level = &capped
	}
	if got := fx.rt.battleRoyaleLevellers(m, nowMs); len(got) != 0 {
		t.Fatalf("%d levellers at the cap", len(got))
	}
}

/*
================
TestBattleRoyaleGroundItemsExpire

A drop of the match layer lies GroundItemLifetimeMs, then the match tick
retires it with the 0x36AB despawn; a fresher drop stays.
================
*/
func TestBattleRoyaleGroundItemsExpire(t *testing.T) {
	fx := newBattleRoyaleFixture(t, 2)
	fx.startMatch(t)
	m := fx.match()
	population := grounditem.Population{World: uint32(m.lease.ID), Generation: m.lease.Generation}
	now := fx.clock.Now()
	old := fx.rt.Ground.Add(testDivision, grounditem.Item{Population: population, Codename: "ITEM_ETC_HP_POTION_01",
		DroppedAt: now.Add(-battleroyale.GroundItemLifetimeMs * time.Millisecond)})
	fresh := fx.rt.Ground.Add(testDivision, grounditem.Item{Population: population, Codename: "ITEM_ETC_HP_POTION_01", DroppedAt: now})
	if old.Gid == 0 || fresh.Gid == 0 {
		t.Fatal("drops refused")
	}
	m.nextGroundSweepMs = 0
	frames := fx.rt.advanceBattleRoyaleEconomy(m, fx.clock.NowMs())
	if len(frames) != 1 || frames[0].SourceGID != old.Gid || frames[0].Frames[0].Opcode != wire.OpObjectDespawn {
		t.Fatalf("despawns %+v", frames)
	}
	if _, ok := fx.rt.Ground.Get(testDivision, old.Gid); ok {
		t.Fatal("the old drop still lies")
	}
	if _, ok := fx.rt.Ground.Get(testDivision, fresh.Gid); !ok {
		t.Fatal("the fresh drop vanished")
	}
}

/*
================
TestBattleRoyaleCappedBotLeavesWolves

A bot below the cap senses the nearest wolf to hunt; at the cap it
leaves the wolves alone (players and bots are all it may still pick).
================
*/
func TestBattleRoyaleCappedBotLeavesWolves(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatch(t)
	nowMs := fx.clock.NowMs()
	profile := battleroyale.ProfileFor(m.botDifficulty)
	for _, bot := range m.bots {
		if bot.c == nil {
			t.Fatalf("bot %s never stood", bot.name)
		}
		one, capped := int64(1), battleroyale.MatchLevelCap
		bot.c.Level = &one
		if s := fx.rt.battleRoyaleBotSenses(m, bot, profile, nil, nil, nil, nowMs); s.Wolf.GID == 0 {
			t.Fatalf("bot %s finds no wolf to hunt", bot.name)
		}
		bot.c.Level = &capped
		if s := fx.rt.battleRoyaleBotSenses(m, bot, profile, nil, nil, nil, nowMs); s.Wolf.GID != 0 {
			t.Fatalf("capped bot %s hunts wolf %d", bot.name, s.Wolf.GID)
		}
		bot.c.Level = &one
	}
}

/*
================
TestBattleRoyaleLocalSupplyCountsItsOwnWolves

Stage 8c: three bots standing on one spot, every wolf of the layer gone.
One local pass stands each bot's share (BotLocalWolves) within
LocalSupplyRadiusM of every levelling bot, and the three on one spot get
one share between them, not three (the overshoot that filled the live
cap in the live match).
================
*/
func TestBattleRoyaleLocalSupplyCountsItsOwnWolves(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 20)
	nowMs := fx.clock.NowMs()
	wolves, _ := fx.rt.battleRoyaleCensus(m, nowMs)
	for _, wolf := range wolves {
		fx.rt.Monsters.Defeat(testDivision, wolf.gid, fx.clock.Now())
	}
	if left, _ := fx.rt.battleRoyaleCensus(m, nowMs); len(left) != 0 {
		t.Fatalf("%d wolves left", len(left))
	}
	spot := fx.playerAt(m.bots[0].c)
	for _, bot := range m.bots[1:3] {
		fx.standBot(t, bot, spot)
	}
	zone, _ := m.match.ZoneAt(nowMs)
	spawned := fx.rt.localBattleRoyaleSupply(m, zone, nowMs)
	_, bots := battleRoyaleAliveCounts(m)
	want := battleroyale.BotLocalWolves(bots)
	wolves, _ = fx.rt.battleRoyaleCensus(m, nowMs)
	near := func(at battleroyale.Point) int {
		n := 0
		for _, wolf := range wolves {
			if battleRoyaleDistance(at, wolf.at) <= battleroyale.LocalSupplyRadiusM*battleroyale.UnitsPerMeter {
				n++
			}
		}
		return n
	}
	if got := near(spot); got != want {
		t.Fatalf("%d wolves around the three bots on one spot (want %d)", got, want)
	}
	for _, bot := range m.bots {
		if got := near(fx.playerAt(bot.c)); got < want {
			t.Fatalf("bot %s has %d wolves within %d m (want %d)", bot.name, got, battleroyale.LocalSupplyRadiusM, want)
		}
	}
	if spawned > want*(len(m.bots)-2)+battleroyale.LocalWolves {
		t.Fatalf("%d wolves stood for %d bots and a player", spawned, len(m.bots))
	}
}
