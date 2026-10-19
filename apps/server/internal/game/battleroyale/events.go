/*
===========================================================================

events.go - what a battle royale match reports to its owner

Port-only, not native (see zone.go). Match.Tick returns these plain structs
in the order they happened; the wiring stage turns them into chat notices,
packets and teleports. Each one carries the clock time it was produced at.
Outside damage is NOT an event: it is computed on demand per player
(Match.ZoneDamage, OutsideDamage), because the wiring already walks its
players every tick and owns their HP.

===========================================================================
*/
package battleroyale

/*
================
Event

One match event. The marker method closes the set to this file's types;
consumers switch on the concrete type.
================
*/
type Event interface {
	battleRoyaleEvent()
}

/*
================
CountdownStarted

Enough players stood in the lobby: the countdown runs until EndsAtMs.
Players lists the lobby at that moment, sorted.
================
*/
type CountdownStarted struct {
	AtMs     int64
	EndsAtMs int64
	Players  []PlayerID
}

/*
================
CountdownCancelled

The lobby dropped below the minimum during the countdown; the match is
back in the lobby. Players is how many remain.
================
*/
type CountdownCancelled struct {
	AtMs    int64
	Players int
}

/*
================
CountdownTick

The countdown crossed into a new whole second: SecondsLeft is the remaining
time rounded up. A coarse tick that skips seconds reports only the latest.
================
*/
type CountdownTick struct {
	AtMs        int64
	SecondsLeft int
}

/*
================
MatchStarted

The countdown ended. Participants are the lobby at that moment, sorted by
ID, each with its chosen race and build and its drop point inside the
arena.
================
*/
type MatchStarted struct {
	AtMs         int64
	Arena        Circle
	Participants []Participant
}

/*
================
ZonePhase

The zone entered a new phase or stage. State is the zone at AtMs and
Settings the active phase (its damage). A coarse tick that skips a stage
reports only the current one.
================
*/
type ZonePhase struct {
	AtMs     int64
	State    ZoneState
	Settings Phase
}

/*
================
PlayerEliminated

A participant is out. Killer is empty unless a participant was credited.
Placement is the alive count before the elimination (the last eliminated
of ten places 2nd); AliveLeft is the count after it.
================
*/
type PlayerEliminated struct {
	AtMs      int64
	Player    PlayerID
	Killer    PlayerID
	Reason    EliminationReason
	Placement int
	AliveLeft int
}

/*
================
MatchEnded

One or no participant remains. Winner is empty and Draw true when the last
players were eliminated in the same tick. Standings run from first place
down: the winner, then the eliminated in reverse elimination order.
================
*/
type MatchEnded struct {
	AtMs      int64
	Winner    PlayerID
	Draw      bool
	Standings []PlayerID
}

/*
================
MatchCancelled

The owner aborted the match (Match.Cancel); no winner is recorded.
================
*/
type MatchCancelled struct {
	AtMs   int64
	Reason string
}

/*
================
battleRoyaleEvent

The Event markers, one per event type above.
================
*/
func (CountdownStarted) battleRoyaleEvent()   {}
func (CountdownCancelled) battleRoyaleEvent() {}
func (CountdownTick) battleRoyaleEvent()      {}
func (MatchStarted) battleRoyaleEvent()       {}
func (ZonePhase) battleRoyaleEvent()          {}
func (PlayerEliminated) battleRoyaleEvent()   {}
func (MatchEnded) battleRoyaleEvent()         {}
func (MatchCancelled) battleRoyaleEvent()     {}
