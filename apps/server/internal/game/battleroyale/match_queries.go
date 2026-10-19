/*
===========================================================================

match_queries.go - the owner's read-only view of a match

Port-only, not native (see zone.go). Everything the wiring asks a Match
without changing it: its state, the lobby and its choices, the
participants, the planned zone and the countdown. The one writer here,
SetArena, sizes the arena before the match starts; the state machine
itself is match.go.

===========================================================================
*/
package battleroyale

/*
================
Match.State
================
*/
func (m *Match) State() MatchState {
	return m.state
}

/*
================
Match.LobbyCount

How many players stand in the lobby (0 once the match started).
================
*/
func (m *Match) LobbyCount() int {
	return len(m.lobby)
}

/*
================
Match.AliveCount

How many participants are still in the running match.
================
*/
func (m *Match) AliveCount() int {
	return m.alive
}

/*
================
Match.Participant

A copy of one participant, once the match started.
================
*/
func (m *Match) Participant(id PlayerID) (Participant, bool) {
	p, ok := m.participants[id]
	if !ok {
		return Participant{}, false
	}
	return *p, true
}

/*
================
Match.Participants

Copies of every participant, sorted by ID.
================
*/
func (m *Match) Participants() []Participant {
	out := make([]Participant, len(m.order))
	for i, id := range m.order {
		out[i] = *m.participants[id]
	}
	return out
}

/*
================
Match.ZoneAt

The zone at nowMs, once the match started (still answered after it ended,
so the wiring can draw the last circle).
================
*/
func (m *Match) ZoneAt(nowMs int64) (ZoneState, bool) {
	if m.participants == nil {
		return ZoneState{}, false
	}
	return CircleAt(m.zone, nowMs), true
}

/*
================
Match.ZonePlan

The whole zone plan once the match started: what a bot's brain samples
ahead (botbrain.go).
================
*/
func (m *Match) ZonePlan() (Zone, bool) {
	if m.participants == nil {
		return Zone{}, false
	}
	return m.zone, true
}

/*
================
Match.SetArena

Replaces the arena before the match starts (the wiring sizes it by the
lobby count during the countdown). The same validation as Init applies;
a refused arena leaves the old one.
================
*/
func (m *Match) SetArena(arena Circle) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	cfg := m.cfg
	cfg.Arena = arena
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return err
	}
	m.cfg = cfg
	return nil
}

/*
================
Match.Arena
================
*/
func (m *Match) Arena() Circle {
	return m.cfg.Arena
}

/*
================
Match.CountdownRemaining

Milliseconds until the countdown ends, 0 outside the countdown.
================
*/
func (m *Match) CountdownRemaining(nowMs int64) int64 {
	if m.state != StateCountdown {
		return 0
	}
	return max(m.countdownEndMs-nowMs, 0)
}

/*
================
Match.Zone

The planned zone, once the match started.
================
*/
func (m *Match) Zone() (Zone, bool) {
	if m.participants == nil {
		return Zone{}, false
	}
	return m.zone, true
}

/*
================
Match.LobbyPlayers

The lobby members, sorted (empty once the match started).
================
*/
func (m *Match) LobbyPlayers() []PlayerID {
	return m.lobbyIDs()
}

/*
================
Match.LobbyBuild

A lobby member's recorded race and build, or false for a non-member.
================
*/
func (m *Match) LobbyBuild(id PlayerID) (Race, string, bool) {
	entry, ok := m.lobby[id]
	if !ok {
		return 0, "", false
	}
	return entry.race, entry.build, true
}
