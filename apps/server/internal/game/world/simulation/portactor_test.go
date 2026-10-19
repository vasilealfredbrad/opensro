/*
===========================================================================

portactor_test.go - the port-only levers over a scripted actor

PORT-ONLY, NOT NATIVE (portactor.go). A scripted actor (one with a
PortCombat projection, the battle royale bots) is released from a native
chase or walk home at its live pose and steered on at once; it heals;
its row's sight never picks a target for it. An ordinary monster is
refused every lever.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
portActorState

A division with one scripted actor (and the row an ordinary actor of
the same population uses), returning the state, the actor and the
population.
================
*/
func portActorState(t *testing.T, scripted bool) (*MonsterState, monster.Instance, instance.Lease) {
	t.Helper()
	const code = "MOB_THIEF_NPC_0001"
	ref := monster.MonsterRef{RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 20, RunSpeed: 50}
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: ref}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("bots")
	lease, exists := state.PopulationLease("bots", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	request := QuestMonsterSpawn{Division: "bots", Population: lease, Codename: code,
		Position: Spawn{RegionID: 0x5B6F, X: 900, Y: 20, Z: 900}, FixedPosition: true, NowMs: 1000}
	if scripted {
		request.PortCombat = &monster.PortCombat{Ref: ref, DamageDealt: 1}
	}
	actor, ok := state.SpawnQuestMonsterActor(request)
	if !ok {
		t.Fatal("no actor")
	}
	return state, actor, lease
}

/*
================
TestPortActorReleaseSteerAndHeal

A scripted actor chasing a player is released (target dropped, idle at
its pose) and SteerPortActor walks it on at once; it heals up to its
max. Its row's sight picks no target. An ordinary actor is refused all
three.
================
*/
func TestPortActorReleaseSteerAndHeal(t *testing.T) {
	state, actor, _ := portActorState(t, true)
	if !state.ArmRetaliation("bots", actor.Gid, 7) {
		t.Fatal("no retaliation")
	}
	ops := &MonsterMoverOps{Monsters: state}
	frames, err := ops.SteerPortActor("bots", actor.Gid, monster.Pose{RegionID: 0x5B6F, X: 1000, Y: 20, Z: 900}, 2000)
	mover, _ := state.Mover("bots", actor.Gid)
	if err != nil || len(frames) == 0 || mover.TargetGID() != 0 || mover.Mode() != monster.MoverWandering || mover.To.X != 1000 {
		t.Fatalf("steer out of a chase: %v, %d frames, mode %v target %d to %+v", err, len(frames), mover.Mode(), mover.TargetGID(), mover.To)
	}
	if !state.ArmRetaliation("bots", actor.Gid, 7) || !state.ReleasePortActor("bots", actor.Gid, 2500) {
		t.Fatal("release refused")
	}
	if mover, _ = state.Mover("bots", actor.Gid); mover.Mode() != monster.MoverIdle || mover.TargetGID() != 0 {
		t.Fatalf("released into %v target %d", mover.Mode(), mover.TargetGID())
	}
	if _, ok := state.ApplyDamage("bots", actor.Gid, 60); !ok {
		t.Fatal("no damage")
	}
	if healed, ok := state.HealPortActor("bots", actor.Gid, 500); !ok || healed.CurrentHP != healed.EffectiveMaxHP() {
		t.Fatalf("heal %v to %d", ok, healed.CurrentHP)
	}
	live, _ := state.Get("bots", actor.Gid)
	if _, found := (&MonsterMoverOps{Monsters: state}).acquireSightTarget("bots", live, mover.Pose,
		[]playerPose{{Gid: 7, Pose: Spawn{RegionID: 0x5B6F, X: 900, Y: 20, Z: 900}}}, monster.Tactics{SightRange: 1000}); found {
		t.Fatal("a scripted actor acquired a target by sight")
	}

	native, ordinary, _ := portActorState(t, false)
	ops = &MonsterMoverOps{Monsters: native}
	if _, err := ops.SteerPortActor("bots", ordinary.Gid, monster.Pose{RegionID: 0x5B6F, X: 1000, Y: 20, Z: 900}, 2000); err == nil {
		t.Fatal("an ordinary actor was steered as a scripted one")
	}
	if native.ReleasePortActor("bots", ordinary.Gid, 2000) {
		t.Fatal("an ordinary actor was released")
	}
	if _, ok := native.HealPortActor("bots", ordinary.Gid, 10); ok {
		t.Fatal("an ordinary actor was healed")
	}
}
