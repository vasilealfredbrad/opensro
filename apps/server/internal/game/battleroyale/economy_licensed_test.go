/*
===========================================================================

economy_licensed_test.go - the stage-6 economy on the shipped Roc ground

Port-only, not native. The whole-match simulation of
economy_sim_test.go on the real ground mask (the navmesh test the wiring
asks, places_licensed_test.go rocWalkable), and its EXP curve against
the shipped leveldata; stage 7 adds the 20/100/200-bot table on Normal
and Hard. Skips without the licensed data. The docs table
(docs/battle-royale.md stage 6) is this test's log.

===========================================================================
*/
package battleroyale_test

import (
	"testing"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestEconomySimulationOnRoc
================
*/
func TestEconomySimulationOnRoc(t *testing.T) {
	levels := enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	for i, want := range simExpCurve() {
		if got, ok := levels.ExpRequired(int64(i + 1)); !ok || got != want {
			t.Fatalf("leveldata level %d needs %d (%v), the simulation %d", i+1, got, ok, want)
		}
	}
	v := movement.NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatal(err)
	}
	mask := battleroyale.BuildGroundMask(rocWalkable(v))
	t.Logf("Roc ground mask %.2f km2", mask.Area()/1e6)
	runEconomy(t, mask)
	runBotEconomy(t, mask)
}
