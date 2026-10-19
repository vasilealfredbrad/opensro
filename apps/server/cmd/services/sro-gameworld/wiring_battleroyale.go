/*
===========================================================================

wiring_battleroyale.go - the battle royale mode at boot (port-only)

Port-only, not native: SRO_BATTLE_ROYALE turns the mode on
(docs/battle-royale.md); "off" installs nothing. Two pieces run whatever
the flag says, because they are recovery, not gameplay: a row found
holding a real character at boot is restored before the world opens, and
one found at world entry is restored before the character spawns.

===========================================================================
*/

package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/history"
)

// The match bodies: the plain adventurer model of each race and gender.
const (
	battleRoyaleModelCHMan   = "CHAR_CH_MAN_ADVENTURER"
	battleRoyaleModelCHWoman = "CHAR_CH_WOMAN_ADVENTURER"
	battleRoyaleModelEUMan   = "CHAR_EU_MAN_ADVENTURER"
	battleRoyaleModelEUWoman = "CHAR_EU_WOMAN_ADVENTURER"
)

/*
================
wireBattleRoyale

The recovery hooks always; the mode itself behind the flag.
================
*/
func (game *gameplayPlane) wireBattleRoyale(journal *history.Journal) {
	game.deps.RestoreHeldCharacter = game.items.BattleRoyaleEntryRestore
	game.deps.InteractionLocked = game.items.BattleRoyaleLocked
	// Lobby members and match characters stay off the beta player map (a
	// no-op while the mode is off).
	game.movement.MapHidden = game.items.BattleRoyaleMapHidden
	if !action.BattleRoyaleFromEnv() {
		return
	}
	skills, _ := game.deps.Skills.(enterworld.SkillRowSource)
	if skills == nil {
		log.Warnf("battle royale: the skill catalogue cannot be walked; match characters learn no build skills (%s)", action.EnvBattleRoyale)
	}
	game.deps.BattleRoyaleLobby = &enterworld.BattleRoyaleLobby{
		RegionID: battleroyale.LobbyRegion,
		X:        battleroyale.LobbyLocalX,
		Z:        battleroyale.LobbyLocalZ,
		RadiusM:  battleroyale.LobbyRadiusM,
	}
	game.items.ConfigureBattleRoyale(action.BattleRoyaleSetup{
		Seeders: battleRoyaleSeeders(game.deps),
		Skills:  skills,
		// Stage 8: a bot walks with the ground click a client sends.
		Move: func(division string, c *enterworld.Character, payload []byte) {
			game.movement.HandleMove(division, c, payload)
		},
	})
	if journal != nil {
		game.items.RecordBattleRoyale = func(event action.BattleRoyaleEvent) { journal.Record(battleRoyaleEvent(event)) }
	}
	// A match that concerns the entering character rides the entry's seed
	// frames, after the bootstrap sequence.
	seeds := game.deps.CommunitySeedFramesFor
	game.deps.CommunitySeedFramesFor = func(divisionID string, character *enterworld.Character) []enterworld.Packet {
		var frames []enterworld.Packet
		if seeds != nil {
			frames = seeds(divisionID, character)
		}
		if state, concerned := game.items.BattleRoyaleEntryState(divisionID, character); concerned {
			frames = append(frames, enterworld.NewPacket(wireOpBattleRoyaleState, state.Encode()))
		}
		// Stage 5: the bot roster (switch and nameplates) follows the state.
		if roster, concerned := game.items.BattleRoyaleEntryRoster(divisionID, character); concerned {
			frames = append(frames, enterworld.NewPacket(roster.Opcode, roster.Payload))
		}
		return frames
	}
	log.Infof("battle royale: ON (%s): lobby in Hotan by the storage keeper, arena in Roc Mountain (world 29), docs/battle-royale.md", action.EnvBattleRoyale)
}

// wireOpBattleRoyaleState is wire.OpBattleRoyaleState, named here so the
// entry seed and the live push agree.
const wireOpBattleRoyaleState = 0x3FB2

/*
================
battleRoyaleSeeders

The creation seeds the store uses, over the bootstrap dependencies.
================
*/
func battleRoyaleSeeders(deps *enterworld.Deps) battleroyale.Seeders {
	return battleroyale.Seeders{
		Model: func(race battleroyale.Race, gender int64) (string, uint32, bool) {
			codename := battleRoyaleModelEUMan
			switch {
			case race == battleroyale.RaceChinese && gender == domain.GenderFemale:
				codename = battleRoyaleModelCHWoman
			case race == battleroyale.RaceChinese:
				codename = battleRoyaleModelCHMan
			case gender == domain.GenderFemale:
				codename = battleRoyaleModelEUWoman
			}
			model := deps.Roster.ModelByCodename(codename)
			if model == nil {
				return "", 0, false
			}
			return codename, model.RefObjID, true
		},
		Skills:    enterworld.DefaultSkillSeeder(deps.Skills),
		Inventory: enterworld.StarterInventorySeeder(deps.Roster, deps.Items, deps.EquipItemsEnabled),
	}
}

/*
================
battleRoyaleEvent

A mode event as an operator history event.
================
*/
func battleRoyaleEvent(event action.BattleRoyaleEvent) history.Event {
	fields := map[string]string{"match": event.Match}
	for key, value := range event.Fields {
		fields[key] = value
	}
	return history.Event{Kind: "battle_royale_" + event.Kind, Category: "battleroyale", Code: event.Kind, Shard: event.Division,
		Character: event.Character, Message: "battle royale " + event.Kind + " (" + event.Match + ")", Fields: fields}
}

/*
================
restoreHeldBattleRoyaleCharacters

Boot recovery: every row still holding a real character (the process
died mid-match) gets it back, standing at the lobby in the default world,
before any session can enter. Runs whatever the flag says.
================
*/
func restoreHeldBattleRoyaleCharacters(authority *store.Store) int {
	restored := action.RestoreHeldCharacters(authority.DivisionIDs(), authority.Characters(), authority.MutateCharacter)
	for _, name := range restored {
		log.WithField("character", name).Info("battle royale: held character restored at boot")
	}
	return len(restored)
}

/*
================
battleRoyaleHeadless

The match bots as the tick sees them (simulation.Ticker.Headless, stage
8): each standing bot's snapshot exactly as a bound session's provider
builds it (the movement lane's HeadlessSnapshot: live world, appearance
with the worn set, body radius), in its match layer, off the beta map.
Native (the mode off) lists none.
================
*/
func (game *gameplayPlane) battleRoyaleHeadless() []simulation.SessionSnapshot {
	bots := game.items.BattleRoyaleHeadless()
	if len(bots) == 0 {
		return nil
	}
	out := make([]simulation.SessionSnapshot, 0, len(bots))
	for _, bot := range bots {
		snapshot := game.movement.HeadlessSnapshot(bot.Division, bot.Character)
		snapshot.SessionID, snapshot.Population, snapshot.MapHidden = bot.SessionID, bot.Lease, true
		out = append(out, snapshot)
	}
	return out
}

/*
================
pushBattleRoyaleBotPeerFrames

The peer frames of a character without a session: a standing battle
royale bot's (port-only) go to the sessions observing its gid, so its
pickups, level-ups and equipment visuals are seen; anyone else's have no
audience.
================
*/
func (game *gameplayPlane) pushBattleRoyaleBotPeerFrames(division, name string, frames []wire.Frame) {
	if gid, bot := game.items.BattleRoyaleHeadlessGID(division, name); bot {
		action.BroadcastObservedFrames(game.hub, division, 0, gid, frames)
	}
}
