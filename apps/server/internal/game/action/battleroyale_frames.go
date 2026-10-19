/*
===========================================================================

battleroyale_frames.go - what a player sees of the match

Port-only, not native (battleroyale.go). The 0x3FB2 state frame is one
player's whole view, sent whenever that view changes and once at world
entry; the countdown and zone banners ride the native notice channel 7,
the kill feed the port-only channel 0x82 (wire/battleroyale.go), and the
winner is announced to the shard. The telemetry events go to the history
journal through RecordBattleRoyale.

===========================================================================
*/

package action

import (
	"fmt"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
battleRoyaleStateFor

The state frame of one player: a lobby member or a participant sees the
match; anyone else sees nothing (state 0).
================
*/
func (rt *Runtime) battleRoyaleStateFor(m *battleRoyaleMatch, name string, nowMs int64) wire.BattleRoyaleState {
	lobby := battleRoyaleLobby()
	state := wire.BattleRoyaleState{Lobby: battleRoyaleWireCircle(lobby)}
	id := battleroyale.PlayerID(name)
	switch m.match.State() {
	case battleroyale.StateLobby, battleroyale.StateCountdown:
		_, build, member := m.match.LobbyBuild(id)
		if !member {
			return state
		}
		state.State, state.Role = wire.BattleRoyaleStateLobby, wire.BattleRoyaleRoleMember
		if m.match.State() == battleroyale.StateCountdown {
			state.State = wire.BattleRoyaleStateCountdown
			state.CountdownMs = uint32(m.match.CountdownRemaining(nowMs))
		}
		state.Build = battleroyale.BuildByName(build).ID
		state.Members = uint8(min(m.match.LobbyCount(), 0xff))
	case battleroyale.StateRunning, battleroyale.StateFinished:
		p, participant := m.match.Participant(id)
		if !participant {
			return state
		}
		state.State, state.Role = wire.BattleRoyaleStateRunning, wire.BattleRoyaleRoleAlive
		if !p.Alive {
			state.Role = wire.BattleRoyaleRoleEliminated
		}
		if m.match.State() == battleroyale.StateFinished {
			state.State, state.Winner = wire.BattleRoyaleStateFinished, m.winner
		}
		state.Build = battleroyale.BuildByName(p.Build).ID
		// Every participant counts, the bots included (stage 5).
		state.Members = uint8(min(len(m.match.Participants()), 0xff))
		state.Alive = uint8(min(m.match.AliveCount(), 0xff))
		rt.battleRoyaleZoneState(m, &state, nowMs)
	}
	return state
}

/*
================
battleRoyaleZoneState

The zone fields of a running or finished match.
================
*/
func (rt *Runtime) battleRoyaleZoneState(m *battleRoyaleMatch, state *wire.BattleRoyaleState, nowMs int64) {
	zone, ok := m.match.Zone()
	if !ok || len(zone.Schedule.Phases) == 0 {
		return
	}
	at := battleroyale.CircleAt(zone, nowMs)
	state.Phase, state.Phases = uint8(at.Phase), uint8(len(zone.Schedule.Phases))
	switch at.Stage {
	case battleroyale.ZoneWaiting:
		state.Stage = wire.BattleRoyaleStageWaiting
	case battleroyale.ZoneShrinking:
		state.Stage = wire.BattleRoyaleStageShrinking
	default:
		state.Stage = wire.BattleRoyaleStageFinal
	}
	state.StageMs = uint32(at.RemainingMs)
	state.Current, state.Next = battleRoyaleWireCircle(at.Current), battleRoyaleWireCircle(at.Target)
	if at.Phase >= 0 && at.Phase < len(zone.Schedule.Phases) {
		state.DamagePerSec = float32(zone.Schedule.Phases[at.Phase].DamagePctPerSec)
	}
}

/*
================
battleRoyaleWireCircle
================
*/
func battleRoyaleWireCircle(c battleroyale.Circle) wire.BattleRoyaleCircle {
	return wire.BattleRoyaleCircle{X: float32(c.Center.X), Z: float32(c.Center.Z), Radius: float32(c.Radius)}
}

/*
================
pushBattleRoyaleState
================
*/
func (rt *Runtime) pushBattleRoyaleState(m *battleRoyaleMatch, name string, nowMs int64) {
	if rt.PushCharacterFrames == nil {
		return
	}
	rt.PushCharacterFrames(m.division, name, []wire.Frame{wire.BattleRoyaleStateFrame(rt.battleRoyaleStateFor(m, name, nowMs))})
}

/*
================
pushBattleRoyaleStates

Each named player's own frame, in name order.
================
*/
func (rt *Runtime) pushBattleRoyaleStates(m *battleRoyaleMatch, names map[string]bool, nowMs int64) {
	for _, name := range sortedNames(names) {
		rt.pushBattleRoyaleState(m, name, nowMs)
	}
}

/*
================
pushBattleRoyaleScoreboard

The 0x3FB3 scoreboard to every participant: one entry per participant
in ID order with its kills and placement (0 while alive), the receiver's
own entry marked; final opens the window at the end.
================
*/
func (rt *Runtime) pushBattleRoyaleScoreboard(m *battleRoyaleMatch, final bool) {
	if rt.PushCharacterFrames == nil {
		return
	}
	participants := m.match.Participants()
	for _, name := range sortedNames(m.participants) {
		entries := make([]wire.BattleRoyaleScore, 0, len(participants))
		for _, p := range participants {
			entries = append(entries, wire.BattleRoyaleScore{Name: string(p.ID), Kills: uint16(min(p.Kills, 0xffff)),
				Placement: uint8(min(p.Placement, 0xff)), Self: string(p.ID) == name})
		}
		rt.PushCharacterFrames(m.division, name, []wire.Frame{wire.BattleRoyaleScoreboardFrame(final, entries)})
	}
}

/*
================
BattleRoyaleEntryState

The frame a world entry appends when a match concerns the character
(enterworld CommunitySeedFramesFor): false when nothing is on.
================
*/
func (rt *Runtime) BattleRoyaleEntryState(division string, c *enterworld.Character) (wire.BattleRoyaleState, bool) {
	if rt.battleRoyale == nil || c == nil {
		return wire.BattleRoyaleState{}, false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	m := rt.battleRoyaleMatchOf(division)
	if m == nil {
		return wire.BattleRoyaleState{}, false
	}
	state := rt.battleRoyaleStateFor(m, c.Name, rt.Now().UnixMilli())
	return state, state.State != wire.BattleRoyaleStateNone
}

//============================================================================

/*
================
battleRoyaleBanner

A channel-7 notice to the named players.
================
*/
func (rt *Runtime) battleRoyaleBanner(m *battleRoyaleMatch, names map[string]bool, text string) {
	if rt.PushCharacterFrames == nil {
		return
	}
	frames := []wire.Frame{wire.NotificationFrame(clampNotice(text))}
	for _, name := range sortedNames(names) {
		rt.PushCharacterFrames(m.division, name, frames)
	}
}

/*
================
battleRoyaleFeed

A kill-feed line (channel 0x82) to every participant.
================
*/
func (rt *Runtime) battleRoyaleFeed(m *battleRoyaleMatch, text string) {
	if rt.PushCharacterFrames == nil {
		return
	}
	frames := []wire.Frame{wire.BattleRoyaleNotificationFrame(clampNotice(text))}
	for _, name := range sortedNames(m.participants) {
		rt.PushCharacterFrames(m.division, name, frames)
	}
}

/*
================
announceBattleRoyaleShard

A channel-7 notice to every session of the division.
================
*/
func (rt *Runtime) announceBattleRoyaleShard(division, text string) {
	if rt.PushShardPeerFrames == nil {
		return
	}
	rt.PushShardPeerFrames(division, "", []wire.Frame{wire.NotificationFrame(clampNotice(text))})
}

/*
================
announceBattleRoyaleZone

The zone banner of a phase edge, and its telemetry.
================
*/
func (rt *Runtime) announceBattleRoyaleZone(m *battleRoyaleMatch, e battleroyale.ZonePhase) {
	zone, _ := m.match.Zone()
	phases := len(zone.Schedule.Phases)
	var text string
	switch e.State.Stage {
	case battleroyale.ZoneWaiting:
		text = fmt.Sprintf("Zone %d/%d closes in %d s", e.State.Phase+1, phases, (e.State.RemainingMs+999)/1000)
	case battleroyale.ZoneShrinking:
		text = fmt.Sprintf("Zone %d/%d is closing", e.State.Phase+1, phases)
	default:
		text = "Final zone"
	}
	rt.battleRoyaleBanner(m, m.participants, text)
	rt.recordBattleRoyale(BattleRoyaleEvent{Kind: "zone_phase", Division: m.division, Match: m.id, Fields: map[string]string{
		"phase": fmt.Sprint(e.State.Phase), "stage": e.State.Stage.String(), "remainingMs": fmt.Sprint(e.State.RemainingMs),
		"radius": fmt.Sprintf("%.0f", e.State.Current.Radius), "targetRadius": fmt.Sprintf("%.0f", e.State.Target.Radius),
		"damagePerSec": fmt.Sprintf("%g", e.Settings.DamagePctPerSec), "alive": fmt.Sprint(m.match.AliveCount())}})
}

/*
================
recordBattleRoyale
================
*/
func (rt *Runtime) recordBattleRoyale(event BattleRoyaleEvent) {
	if rt.RecordBattleRoyale != nil {
		rt.RecordBattleRoyale(event)
	}
}
