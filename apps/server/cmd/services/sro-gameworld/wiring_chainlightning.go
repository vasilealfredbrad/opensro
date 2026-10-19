/*
===========================================================================

wiring_chainlightning.go - Chain Lightning's port-only override at boot

Port-only, not native: reads the SRO_CHAIN_LIGHTNING_* settings into the
action runtime (target count, radius, damage falloff) and publishes the
hop period with the family's skill ids at world entry, so the client
times exactly the casts whose corpses the server holds.

===========================================================================
*/

package main

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
wireChainLightning
================
*/
func (game *gameplayPlane) wireChainLightning() error {
	chainLightning, err := action.ChainLightningFromEnv()
	if err != nil {
		return err
	}
	game.items.ChainLightning = chainLightning
	if chainLightning.Enabled() {
		log.Infof("skill: Chain Lightning chains to %d targets within %d m (0 = the skill's own; %s, %s)",
			chainLightning.Targets, chainLightning.RangeMeters, action.EnvChainLightningTargets, action.EnvChainLightningRange)
	}
	if chainLightning.FullDamage {
		log.Infof("skill: every Chain Lightning victim takes full damage (%s)", action.EnvChainLightningFullDamage)
	}
	// The hop is published at world entry with the family's skill ids, so
	// the client times exactly the casts whose corpses the server holds.
	if chainLightning.HopMs != 0 {
		skills, ok := game.deps.Skills.(enterworld.SkillCodenameSource)
		if !ok {
			return fmt.Errorf("%s: the skill catalogue cannot resolve codenames", action.EnvChainLightningHop)
		}
		ids := action.ChainLightningSkillIDs(skills)
		if len(ids) == 0 {
			return fmt.Errorf("%s: no Chain Lightning rank in the skill catalogue", action.EnvChainLightningHop)
		}
		game.deps.ChainLightningHop = &enterworld.ChainLightningHop{HopMs: chainLightning.HopMs, Skills: ids}
		log.Infof("skill: Chain Lightning hops every %d ms across %d ranks (%s)", chainLightning.HopMs, len(ids),
			action.EnvChainLightningHop)
	}
	return nil
}
