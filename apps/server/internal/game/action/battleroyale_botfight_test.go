/*
===========================================================================

battleroyale_botfight_test.go - a bot body fights an ordinary player

Port-only, not native (docs/battle-royale.md "Stage 7"). The stage-5 bots
stand as caravan thief rows (MOB_THIEF_NPC_*, TID4 2). A native thief
monster refuses every player outside job mode (JobTargetRefused, 529110),
so the first live match's bots chased a player, had every strike refused
and walked home to their drop point. This test runs the real division
transaction, simulation tick, chase and damage owner with a thief-typed
actor that carries the bots' combat projection, armed against an
ordinary (job state 4) player standing beside it: the bot must keep its
target and strike until the player is dead.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// botFightThiefTypeID4 is the thief class of the caravan thief rows.
const botFightThiefTypeID4 = 2

// botFightRegion is a Roc Mountain region (the arena's centre region).
const botFightRegion uint16 = 0x5B6F

/*
================
TestBattleRoyaleBotKeepsAttackingAnOrdinaryPlayer
================
*/
func TestBattleRoyaleBotKeepsAttackingAnOrdinaryPlayer(t *testing.T) {
	rt, clock, c, original := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 30, 30, 100
	skills[2] = skill
	ref := original.Ref
	ref.TypeID4 = botFightThiefTypeID4
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = 2, 50, 20, 100
	ref.Codename = "MOB_THIEF_NPC_0001"
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: original.Spawn.RegionID,
			X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z}, RetailEvidence: true, MaxCount: 1}}))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision(testDivision)
	state.AdvancePopulation(clock.NowMs())
	rt.Monsters = state
	native := state.InstancesInRegions(testDivision, []uint16{original.Spawn.RegionID})[0]
	lease, ok := state.ObjectPopulation(testDivision, native.Gid)
	if !ok {
		t.Fatal("missing population")
	}
	if !native.ThiefMonster() || !native.JobTargetRefused(enterworld.PartyJobClass(c)) {
		t.Fatal("the fixture row is not a thief monster that natively refuses an ordinary player")
	}
	// The fight stands on Roc (a battlefield): the fixture's own region is a
	// town, where a monster vanishes (vanishInSafeZone).
	*c.World.Spawn.RegionID = int64(botFightRegion)
	bot, ok := state.SpawnQuestMonsterActor(simulation.QuestMonsterSpawn{Division: testDivision, Population: lease,
		Codename: ref.Codename, Position: simulation.Spawn{RegionID: botFightRegion, X: 970, Y: 20, Z: 458},
		FixedPosition: true, NowMs: clock.NowMs(), PortCombat: &monster.PortCombat{Ref: ref, DamageDealt: 1}})
	if !ok {
		t.Fatal("no bot body")
	}
	viewer := simulation.SessionSnapshot{SessionID: "bot-fight", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	player := enterworld.ObjectIDForCharacter(c)
	if !state.ArmRetaliation(testDivision, bot.Gid, player) {
		t.Fatal("no retaliation")
	}
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan,
		RunAction: rt.RunMonsterAction}
	hits, previous := 0, enterworld.CurrentHP(c)
	for step := 0; step < 600 && enterworld.CurrentHP(c) > 0; step++ {
		clock.now = clock.now.Add(100 * time.Millisecond)
		viewer.World = simulation.SeedWorldState(c)
		ops.RunMonsterLeg(clock.NowMs(), []simulation.SessionSnapshot{viewer}, &summonTickPusher{})
		if hp := enterworld.CurrentHP(c); hp < previous {
			hits, previous = hits+1, hp
		}
		mover, _ := state.Mover(testDivision, bot.Gid)
		if enterworld.CurrentHP(c) > 0 && (mover.TargetGID() != player || mover.Mode() == monster.MoverReturning) {
			t.Fatalf("step %d: the bot dropped its target (mode %v, target %d, %d hits, HP %d)",
				step, mover.Mode(), mover.TargetGID(), hits, enterworld.CurrentHP(c))
		}
	}
	if enterworld.CurrentHP(c) != 0 || hits < 2 {
		t.Fatalf("the bot did not fight the player down: HP %d after %d hits", enterworld.CurrentHP(c), hits)
	}
}
