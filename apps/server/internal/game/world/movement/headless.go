/*
===========================================================================

headless.go - the tick snapshot of a character no session drives

Port-only, not native: a battle royale bot (docs/battle-royale.md
"Stage 8") is a real character with no transport session. The tick sees
it through exactly the snapshot a bound session's provider builds
(sessionworld.go: the shared live world, the peer appearance with its
worn equipment, the body radius, the combat eligibility), marked
Headless so the tick treats it as a peer and a mover, never as a viewer.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
HeadlessSnapshot

The tick snapshot of character in divisionID, as WorldBound's provider
would build it for a bound session, marked Headless. The caller names
its SessionID and population (simulation.Ticker.Headless).
==================
*/
func (rt *Runtime) HeadlessSnapshot(divisionID string, character *enterworld.Character) simulation.SessionSnapshot {
	if character == nil {
		return simulation.SessionSnapshot{}
	}
	provider := sessionWorld{rt: rt, divisionID: divisionID, character: character}
	snapshot := provider.WorldSnapshot()
	snapshot.Headless = true
	return snapshot
}
