/*
===========================================================================

battleroyale_combat.go - the match rules the combat, loot and growth lanes ask

Port-only, not native (battleroyale.go). A match character is told apart
by its persisted hold (battleroyale.Held), so every hook here reads the
record it already holds and asks no match for it: the stat writes (HP
x2.5, the race damage factors), free-for-all PvP without murder or
penalty, the death that eliminates, the exact EXP pace and cap with the
automatic mastery and skill grant, and the 0x7FB2 build pick (the match
loot is battleroyale_loot.go's).

===========================================================================
*/

package action

import (
	"errors"
	"math"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	// battleRoyaleModifierSource keys the match writes apart from every
	// effect (which count up from 0x80000000) and the other port rules.
	battleRoyaleModifierSource uint32 = 0x7fffff02
	// battleRoyaleBotModifierSource keys a bot's difficulty writes.
	battleRoyaleBotModifierSource uint32 = 0x7fffff03
	// The keeper parameters the match writes: max HP (3) and the attack
	// ranges (13-16, combat.Stats).
	keeperParamMaxHP             uint16 = 3
	keeperParamPhysicalAttackMin uint16 = 13
	keeperParamPhysicalAttackMax uint16 = 14
	keeperParamMagicalAttackMin  uint16 = 15
	keeperParamMagicalAttackMax  uint16 = 16
)

/*
================
battleRoyaleStatWrites

A match character's keeper writes: max HP +150 %, a Chinese body's
physical attack +20 %, a Wizard's magical attack -50 %; a bot's
difficulty on top (battleRoyaleBotWrites).
================
*/
func (rt *Runtime) battleRoyaleStatWrites(division string, c *enterworld.Character) []paramkeeper.Write {
	writes := battleRoyaleStatWrites(c)
	if profile, bot := rt.battleRoyaleBotProfile(division, c); bot && len(writes) > 0 {
		writes = append(writes, battleRoyaleBotWrites(profile, writes)...)
	}
	return writes
}

/*
================
battleRoyaleBotWrites

The difficulty of a bot as keeper writes on its character (stage 8, the
one rule a bot fights by that a player does not): max HP by HPScale and
every attack range (physical and magical, minimum and maximum) by
DamageScale. The keeper sums a parameter's percents, so each write is
scaled by the match's own percent on that parameter (match, the record's
writes) to multiply the whole: Hard HP is x2.5 x 1.3, not x2.5 + 30 %.
Normal writes nothing. Its hits on players and theirs on it are
otherwise the ordinary PvP pipeline's.
================
*/
func battleRoyaleBotWrites(profile battleroyale.BotProfile, match []paramkeeper.Write) []paramkeeper.Write {
	percent := func(param uint16) float64 {
		total := 100.0
		for _, w := range match {
			if w.Parameter == param && w.Channel == paramkeeper.PercentSum {
				total += float64(w.Value)
			}
		}
		return total
	}
	var writes []paramkeeper.Write
	add := func(param uint16, scale float64) {
		if value := float32(math.Round((scale - 1) * percent(param))); value != 0 {
			writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.PercentSum, Source: battleRoyaleBotModifierSource, Value: value})
		}
	}
	add(keeperParamMaxHP, profile.HPScale)
	for _, param := range []uint16{keeperParamPhysicalAttackMin, keeperParamPhysicalAttackMax, keeperParamMagicalAttackMin, keeperParamMagicalAttackMax} {
		add(param, profile.DamageScale)
	}
	return writes
}

/*
================
battleRoyaleStatWrites

The match writes of a record (no bot difficulty, see the method).
================
*/
func battleRoyaleStatWrites(c *enterworld.Character) []paramkeeper.Write {
	build, ok := battleroyale.HeldBuild(c)
	if !ok {
		return nil
	}
	writes := []paramkeeper.Write{{Parameter: keeperParamMaxHP, Channel: paramkeeper.PercentSum, Source: battleRoyaleModifierSource, Value: battleroyale.MatchMaxHPPercent}}
	if build.Race == battleroyale.RaceChinese {
		for _, param := range []uint16{keeperParamPhysicalAttackMin, keeperParamPhysicalAttackMax} {
			writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.PercentSum, Source: battleRoyaleModifierSource, Value: battleroyale.ChinesePhysicalPercent})
		}
	}
	if build.ID == battleroyale.WizardBuildID {
		for _, param := range []uint16{keeperParamMagicalAttackMin, keeperParamMagicalAttackMax} {
			writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.PercentSum, Source: battleRoyaleModifierSource, Value: battleroyale.WizardMagicalPercent})
		}
	}
	return writes
}

/*
================
battleRoyaleOpponents

Two match characters in the same arena world: free to strike each other,
and a kill between them is a team death (no murder, no penalty).
================
*/
func battleRoyaleOpponents(a, b *enterworld.Character) bool {
	return battleroyale.Held(a) && battleroyale.Held(b) && a.ID != b.ID &&
		domain.CharacterWorldInstance(a) == domain.CharacterWorldInstance(b)
}

/*
================
battleRoyaleDeath

A match character died (settleDeathCostInDoor, inside the victim's
door): the match records the elimination, credited to a participant
killer (a player or a bot: from stage 8 both are match characters). The
next tick restores a player and lays a bot's corpse out; nothing else
happens here.
================
*/
func (rt *Runtime) battleRoyaleDeath(division string, c *enterworld.Character, killer deathKiller, nowMs int64) {
	m := rt.battleRoyaleMatchOf(division)
	if m == nil || m.match.State() != battleroyale.StateRunning {
		return
	}
	var by battleroyale.PlayerID
	if killer.player != nil && battleRoyaleOpponents(killer.player, c) {
		if _, participant := m.match.Participant(battleroyale.PlayerID(killer.player.Name)); participant {
			by = battleroyale.PlayerID(killer.player.Name)
		}
	}
	_ = m.match.Eliminate(battleroyale.PlayerID(c.Name), by, battleroyale.EliminatedKilled, nowMs)
}

//============================================================================

/*
================
grantMonsterExperience

A monster kill's EXP through the stat authority (reward_group.go); a
match character takes the match pace and cap instead.
================
*/
func (rt *Runtime) grantMonsterExperience(division string, c *enterworld.Character, exp, skillExp int64, sourceGid uint32) []wire.Frame {
	if exp == 0 && skillExp == 0 {
		return nil
	}
	if battleroyale.Held(c) {
		return rt.battleRoyaleExperience(division, c, exp, skillExp, sourceGid)
	}
	if rt.UpdateExperience == nil {
		return nil
	}
	frames, _ := rt.UpdateExperience(c, exp, skillExp, sourceGid)
	return frames
}

/*
================
battleRoyaleExperience

The match pace: a credited kill (the native formula paid this character
any EXP) is worth MatchKillExp of its level's requirement instead of the
formula's amount, so the monster/player level gap never starves a match
character on the level-5 wolf; the exact commit keeps the beta growth
from scaling it again. Nothing at the cap (a single kill cannot cross
two levels, so the cap holds). A level gained trains both masteries to
it and learns every build skill it unlocks; the client hears each as its
own ack.
================
*/
func (rt *Runtime) battleRoyaleExperience(division string, c *enterworld.Character, exp, skillExp int64, sourceGid uint32) []wire.Frame {
	levels := rt.deps.LevelData()
	if rt.CommitExperience == nil || levels == nil {
		return nil
	}
	build, _ := battleroyale.HeldBuild(c)
	before := rewardLevel(c)
	if before >= battleroyale.MatchLevelCap {
		exp = 0
	} else if exp > 0 {
		required, _ := levels.ExpRequired(before)
		exp = battleroyale.MatchKillExp(required)
		// A bot levels at its difficulty's pace (x0.8 / x1 / x1.3 a kill,
		// still below one level a kill) and counts the kill.
		if profile, bot := rt.battleRoyaleBotProfile(division, c); bot {
			exp = int64(math.Ceil(float64(exp) * profile.LevelPace))
			if m := rt.battleRoyaleMatchOf(division); m != nil {
				if b := m.battleRoyaleBotNamed(c.Name); b != nil {
					b.monsterKills++
				}
			}
		}
	}
	if exp == 0 && skillExp == 0 {
		return nil
	}
	frames, _ := rt.CommitExperience(c, exp, skillExp, sourceGid)
	after := rewardLevel(c)
	if after <= before {
		return frames
	}
	battleroyale.SetMatchLevel(c, build, after)
	for _, id := range build.Masteries {
		if level, ok := enterworld.MasteryLevel(c, id); ok {
			frames = append(frames, wire.Frame{Opcode: wire.OpMasteryLevelUpResponse, Payload: wire.EncodeMasteryLevelUpAck(id, uint8(level))})
		}
	}
	for _, id := range rt.learnBattleRoyaleSkills(c, build) {
		frames = append(frames, wire.Frame{Opcode: wire.OpSkillLearnResponse, Payload: wire.EncodeSkillLearnAck(id)})
	}
	if stats, err := rt.PlayerBaseStats(division, c); err == nil {
		frames = append(frames, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
	}
	// A level may unlock a looted weapon's gate; the reward door cannot
	// open the inventory door, so the next match tick re-checks the bag.
	if m := rt.battleRoyaleMatchOf(division); m != nil && m.equipPending != nil {
		m.equipPending[c.Name] = true
	}
	return frames
}

/*
================
learnBattleRoyaleSkills

Learns, for free, every build skill the character's masteries now admit,
rank by rank (a learned rank replaces its group's previous id, as the
0x72CB learn does). Returns the learned ids in learn order.
================
*/
func (rt *Runtime) learnBattleRoyaleSkills(c *enterworld.Character, build battleroyale.Build) []uint32 {
	rows := rt.battleRoyaleBuildSkills(build)
	if len(rows) == 0 {
		return nil
	}
	var learned []uint32
	for progress := true; progress; {
		progress = false
		for _, row := range rows {
			if !rt.battleRoyaleSkillAdmitted(c, row) {
				continue
			}
			replaced := false
			for i, id := range c.Skills {
				if group, _, ok := rt.skillGroupRank(id); ok && group == row.Group {
					c.Skills[i] = row.ID
					replaced = true
					break
				}
			}
			if !replaced {
				c.Skills = append(c.Skills, row.ID)
			}
			learned = append(learned, row.ID)
			progress = true
		}
	}
	return learned
}

/*
================
battleRoyaleSkillAdmitted

The 0x72CB gates a free learn still honours: the exact next rank of its
group, the mastery levels and the prerequisite ranks.
================
*/
func (rt *Runtime) battleRoyaleSkillAdmitted(c *enterworld.Character, row enterworld.SkillRow) bool {
	if rt.learnedGroupRank(c, row.Group) != row.Level-1 {
		return false
	}
	for _, requirement := range row.Masteries {
		if requirement.ID == 0 {
			continue
		}
		level, ok := enterworld.MasteryLevel(c, requirement.ID)
		if !ok || level < requirement.Level {
			return false
		}
	}
	for _, requirement := range row.Prerequisites {
		if requirement.ID == 0 {
			continue
		}
		if rt.learnedGroupRank(c, requirement.ID) < max(requirement.Level, 1) {
			return false
		}
	}
	return true
}

/*
================
learnedGroupRank

The learned rank of a skill group, 0 for none.
================
*/
func (rt *Runtime) learnedGroupRank(c *enterworld.Character, group uint32) int64 {
	for _, id := range c.Skills {
		if g, level, ok := rt.skillGroupRank(id); ok && g == group {
			return level
		}
	}
	return 0
}

/*
================
skillGroupRank
================
*/
func (rt *Runtime) skillGroupRank(id uint32) (uint32, int64, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return 0, 0, false
	}
	row, ok := skills.SkillByID(id)
	return row.Group, row.Level, ok
}

/*
================
battleRoyaleBuildSkills

The rows a build can learn, sorted by group, rank and id: every
learnable root (not a chain sub-row) of the build's race whose mastery
requirements all name a build mastery. Cached per build.
================
*/
func (rt *Runtime) battleRoyaleBuildSkills(build battleroyale.Build) []enterworld.SkillRow {
	br := rt.battleRoyale
	if br == nil || br.setup.Skills == nil {
		return nil
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	if rows, cached := br.skills[build.ID]; cached {
		return rows
	}
	country := uint8(enterworld.NativeCountryByte9C(&domain.Character{RaceIndex: raceIndexOf(build.Race)}))
	var rows []enterworld.SkillRow
	for _, row := range br.setup.Skills.Rows() {
		if row.ChainSub || row.Masteries[0].ID == 0 {
			continue
		}
		if row.RequiredRace != enterworld.SkillRaceAny && row.RequiredRace != country {
			continue
		}
		admitted := true
		for _, requirement := range row.Masteries {
			if requirement.ID != 0 && !build.HasMastery(requirement.ID) {
				admitted = false
			}
		}
		if admitted {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].Level != rows[j].Level {
			return rows[i].Level < rows[j].Level
		}
		return rows[i].ID < rows[j].ID
	})
	br.skills[build.ID] = rows
	return rows
}

/*
================
raceIndexOf
================
*/
func raceIndexOf(race battleroyale.Race) *int64 {
	index := int64(race)
	return &index
}

//============================================================================

/*
================
HandleBattleRoyalePick

0x7FB2 [u8 build]: a lobby member's build, during the lobby or the
countdown. A sender the last scan did not admit (it stepped into the
circle after the scan, or the scan is yet to run) is scanned for at once,
so a pick that follows the frame which opened the picker is never lost to
the tick cadence. The answer is always the sender's own frame with the
stored build, a refused pick included, so the client shows what the
server holds and never a pick the server does not have.
================
*/
func (rt *Runtime) HandleBattleRoyalePick(division string, c *enterworld.Character, payload []byte) OpResult {
	if rt.battleRoyale == nil {
		return OpResult{DiagnosticRefusal: "battle-royale-pick: the mode is off"}
	}
	id, err := wire.DecodeBattleRoyalePick(payload)
	if err != nil || c == nil {
		return OpResult{DiagnosticRefusal: "battle-royale-pick: malformed"}
	}
	build, ok := battleroyale.BuildByID(id)
	if !ok {
		return OpResult{DiagnosticRefusal: "battle-royale-pick: unknown build"}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	nowMs := rt.Now().UnixMilli()
	m := rt.battleRoyaleMatchFor(division, nowMs)
	player := battleroyale.PlayerID(c.Name)
	err = m.match.Choose(player, build.Race, battleroyale.BuildName(build.ID))
	if errors.Is(err, battleroyale.ErrUnknownPlayer) {
		rt.scanBattleRoyaleLobby(m, nowMs)
		err = m.match.Choose(player, build.Race, battleroyale.BuildName(build.ID))
	}
	answer := OpResult{Frames: []wire.Frame{wire.BattleRoyaleStateFrame(rt.battleRoyaleStateFor(m, c.Name, nowMs))}}
	if err != nil {
		answer.DiagnosticRefusal = "battle-royale-pick: " + err.Error()
	}
	return answer
}
