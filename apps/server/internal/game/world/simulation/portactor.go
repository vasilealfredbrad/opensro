/*
===========================================================================

portactor.go - port-only controls over a scripted actor

PORT-ONLY, NOT NATIVE: the battle royale bots (SRO_BATTLE_ROYALE,
docs/battle-royale.md stage 5) are ordinary monsters a server-side
controller steers. These are the levers it pulls besides the native ones
(ArmRetaliation, RefreshJobMonster): replace an actor's combat projection
(monster.PortCombat) as it levels, walk an idle or wandering actor to a
point with the ordinary movement publication, take a scripted actor out
of a fight or a walk home its controller no longer wants (stage 7), and
heal one (a bot's potion). No native path calls any of them.

===========================================================================
*/

package simulation

import (
	"fmt"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
SetPortCombat

Installs combat (nil clears it) on a living actor. False when the actor
is gone or dead.
================
*/
func (s *MonsterState) SetPortCombat(division string, gid uint32, combat *monster.PortCombat) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 {
		return false
	}
	instance.PortCombat = combat
	state.instances.set(gid, instance)
	return true
}

/*
================
SteerMonster

Walks (runs) a living actor from its live pose toward destination as one
ordinary movement leg, clipped by the navigation owner like a wander
leg. Only an idle, wandering or still spawning actor is steered (a bot
far from every player sits in a dormant region whose tick never ends
its spawn hold): a chase, an attack, a summon or a displacement keeps
its native owner, and the caller asks again on a later tick. The frames
are the leg's publication.
================
*/
func (ops *MonsterMoverOps) SteerMonster(division string, gid uint32, destination monster.Pose, now int64) ([]Frame, error) {
	instance, ok := ops.Monsters.Get(division, gid)
	if !ok || instance.CurrentHP == 0 {
		return nil, fmt.Errorf("actor absent/dead")
	}
	if now < instance.SummonActionUntilMs {
		return nil, fmt.Errorf("summon recovery still owns the actor")
	}
	mover, ok := ops.Monsters.Mover(division, gid)
	if !ok {
		return nil, fmt.Errorf("actor mover absent")
	}
	if mover.Mode() == monster.MoverSpawning {
		mustMoverTransition(&mover, monster.MoverEventSpawnHoldElapsed, 0)
	}
	if mover.Mode() == monster.MoverWandering {
		mover.Pose = mover.LivePoseAt(now, ops.TerrainHeight)
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	}
	if mover.Mode() != monster.MoverIdle {
		return nil, fmt.Errorf("actor not idle")
	}
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	return ops.commitSegment(division, instance, mover, destination, instance.RunSpeed(), wire.MoveStateRun, now), nil
}

/*
================
SteerPortActor

SteerMonster for a scripted actor (one with a PortCombat projection, the
battle royale bots): a native chase, attack, recovery, return or follow
it is in is ended first at its live pose (releasePortActor), so the
controller, not the row's homing, decides where the bot goes. Any other
actor is refused.
================
*/
func (ops *MonsterMoverOps) SteerPortActor(division string, gid uint32, destination monster.Pose, now int64) ([]Frame, error) {
	if !ops.Monsters.releasePortActor(division, gid, now) {
		return nil, fmt.Errorf("actor not a releasable scripted actor")
	}
	return ops.SteerMonster(division, gid, destination, now)
}

/*
================
ReleasePortActor

Ends a scripted actor's native fight or walk at its live pose (see
releasePortActor), so it stands idle until its controller steers it or
arms it again. False when the actor is gone, dead, not scripted or held
by a motion (a knockdown).
================
*/
func (s *MonsterState) ReleasePortActor(division string, gid uint32, now int64) bool {
	return s.releasePortActor(division, gid, now)
}

/*
================
releasePortActor

Brings a living scripted actor to the idle state at its live pose: a
chase or an attack ends as an abandoned trace (target dropped), a return,
recovery, follow or pending hold as a displacement. Idle, wandering and
spawning actors are left as they are (SteerMonster takes them).
================
*/
func (s *MonsterState) releasePortActor(division string, gid uint32, now int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 || instance.PortCombat == nil || state.movers == nil {
		return false
	}
	mover, ok := state.movers.lookup(gid)
	if !ok {
		return true
	}
	event, target := monster.MoverEventDisplaced, mover.TargetGID()
	switch mover.Mode() {
	case monster.MoverIdle, monster.MoverWandering, monster.MoverSpawning:
		return true
	case monster.MoverChasing, monster.MoverAttacking, monster.MoverBattleReentry:
		event, target = monster.MoverEventTraceAbandoned, 0
	}
	mover.Pose = mover.LivePoseAt(now, nil)
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	if err := mover.Transition(event, target); err != nil {
		return false
	}
	return s.commitMoverLocked(state, gid, mover)
}

/*
================
HealPortActor

Adds amount HP (at most to its max) to a living scripted actor: a bot's
potion. Returns the healed actor; false when it is gone, dead or not
scripted.
================
*/
func (s *MonsterState) HealPortActor(division string, gid uint32, amount uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP == 0 || instance.PortCombat == nil {
		return monster.Instance{}, false
	}
	instance.CurrentHP += min(amount, instance.EffectiveMaxHP()-instance.CurrentHP)
	state.instances.set(gid, instance)
	return instance, true
}
