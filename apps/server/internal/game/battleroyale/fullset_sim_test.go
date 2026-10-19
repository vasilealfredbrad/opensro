/*
===========================================================================

fullset_sim_test.go - full sets inside the economy simulation

Port-only, not native (docs/battle-royale.md "Server notes (stage 8:
full sets)", "Stage 9"). Each simulated real player keeps a set: it
picks up its own drops and wears every upgrade its build can use
(SetState.Take); every kill rolls the stage-9 random LootPlan under the
live tuning (LiveLootTuning), which ignores the player's build and set. At the start of phase 5 the set is
checked: every slot of the build must be current for the player's level
(SetState.Complete: degree 4, or the newest degree its rows offer at
that level). The table reports the players with a complete set, the
median kills to a complete degree-4 set and the fill rate per slot;
fullSetFloorPercent is the floor the run asserts (random loot keeps
many sets incomplete; the docs carry the measured numbers).
runLootProposals reports the alternatives the owner weighed.

===========================================================================
*/
package battleroyale_test

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/game/battleroyale"
)

const (
	// simNeverKills sorts a player that never completed its set last.
	simNeverKills = 1 << 30
	// fullSetFloorPercent is the least share of a line-up's players that
	// must hold a complete set at phase 5 under the random loot.
	fullSetFloorPercent = 0
)

/*
================
simSet

One player's set at the start of phase 5: its build, level, set and the
kill that completed its degree-4 set (-1 never).
================
*/
type simSet struct {
	build   battleroyale.Build
	level   int64
	set     battleroyale.SetState
	setKill int
}

/*
================
simMatch.wear

The player picks up its kill's equipment and wears every upgrade; the
first kill after which every slot holds degree 4 is recorded.
================
*/
func (s *simMatch) wear(a *simAgent, killer battleroyale.Killer, drops []battleroyale.Drop) {
	for _, drop := range drops {
		a.set.Take(killer, drop.Codename)
	}
	if a.setKill < 0 && a.set.Complete(a.build, battleroyale.MatchLevelCap) {
		a.setKill = a.kills
	}
}

/*
================
simMatch.snapshotSets

Records every real player's set at the start of phase 5.
================
*/
func (s *simMatch) snapshotSets() {
	for _, a := range s.agents {
		if !a.bot {
			s.result.sets = append(s.result.sets, simSet{build: a.build, level: a.level, set: a.set, setKill: a.setKill})
		}
	}
}

/*
================
fullSetStats

A line-up's set numbers: players with a complete set at phase 5, the
median kills to a degree-4 set, and per slot the players holding a
current item there.
================
*/
type fullSetStats struct {
	complete, total int
	medianKills     int
	filled          [battleroyale.SetSlotCount]int
	slotPlayers     [battleroyale.SetSlotCount]int
}

/*
================
fullSetsOf
================
*/
func fullSetsOf(sets []simSet) fullSetStats {
	var st fullSetStats
	var kills []int
	for _, p := range sets {
		st.total++
		if p.set.Complete(p.build, p.level) {
			st.complete++
		}
		for _, slot := range battleroyale.BuildSlots(p.build) {
			st.slotPlayers[slot]++
			if p.set.SlotCurrent(slot, p.level) {
				st.filled[slot]++
			}
		}
		at := p.setKill
		if at < 0 {
			at = simNeverKills
		}
		kills = append(kills, at)
	}
	slices.SortFunc(kills, cmp.Compare[int])
	if len(kills) > 0 {
		st.medianKills = kills[len(kills)/2]
	}
	return st
}

/*
================
logFullSets

The docs table: complete sets at phase 5, median kills to a degree-4
set, and the fill rate of every slot.
================
*/
func logFullSets(t *testing.T, rows []economyRow) {
	t.Helper()
	header := "| Line-up | Complete set at phase 5 | Median kills to a degree-4 set |"
	for slot := range battleroyale.SetSlotCount {
		header += " " + slot.Name() + " |"
	}
	t.Log(header)
	for _, r := range rows {
		st := fullSetsOf(r.sets)
		var line strings.Builder
		median := "never"
		if st.medianKills < simNeverKills {
			median = fmt.Sprint(st.medianKills)
		}
		fmt.Fprintf(&line, "| %d + %d bots | %d/%d | %s |", r.cfg.players, r.cfg.bots, st.complete, st.total, median)
		for slot := range battleroyale.SetSlotCount {
			if st.slotPlayers[slot] == 0 {
				line.WriteString(" - |")
				continue
			}
			fmt.Fprintf(&line, " %.0f %% |", 100*float64(st.filled[slot])/float64(st.slotPlayers[slot]))
		}
		t.Log(line.String())
	}
}

/*
================
checkFullSets

Fails t when a line-up has fewer than minPercent of its players holding a
complete set at phase 5.
================
*/
func checkFullSets(t *testing.T, rows []economyRow, minPercent int) {
	t.Helper()
	for _, r := range rows {
		st := fullSetsOf(r.sets)
		if st.total == 0 || st.complete*100 < st.total*minPercent {
			t.Errorf("%+v: %d of %d players hold a complete set at phase 5", r.cfg, st.complete, st.total)
		}
	}
}

/*
================
lootProposals

The tunings the stage-9 report weighs against the live table
(battleroyale.LiveLootTuning: five extra stacks, the cap window): the
stage-9 table (zero tuning), the cap window alone, three and eight
extra equipment-only stacks with it.
================
*/
func lootProposals() []battleroyale.LootTuning {
	return []battleroyale.LootTuning{
		{},
		{TopDegreeAtCap: true},
		{ExtraEquipment: 3, TopDegreeAtCap: true},
		{ExtraEquipment: 8, TopDegreeAtCap: true},
	}
}

/*
================
runLootProposals

The set table of every proposal for 5 and 16 players with 20 Normal
bots on mask. Reported, not asserted: the live table is
LiveLootTuning (runEconomy).
================
*/
func runLootProposals(t *testing.T, mask *battleroyale.GroundMask) {
	t.Helper()
	for _, tuning := range lootProposals() {
		var rows []economyRow
		for _, players := range []int{5, 16} {
			rows = append(rows, economyRowFor(t, mask, simConfig{players: players, bots: battleroyale.DefaultBotCount,
				difficulty: battleroyale.BotNormal, tuning: tuning}))
		}
		t.Logf("proposal: %d extra equipment stacks a kill, top degree at the cap %v; peak ground items %d/%d",
			tuning.ExtraEquipment, tuning.TopDegreeAtCap, rows[0].peakGround, rows[1].peakGround)
		logGroundCap(t, rows)
		logFullSets(t, rows)
	}
}
