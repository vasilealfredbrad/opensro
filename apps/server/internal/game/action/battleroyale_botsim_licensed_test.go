/*
===========================================================================

battleroyale_botsim_licensed_test.go - bots on the shipped skill data

Port-only, not native (docs/battle-royale.md "Stage 8c"). The battle
royale fixture with the shipped skilldata, itemdata and leveldata and
the progression's own EXP commit (licensed: SRO_GAME_ROOT): a bot of
every build casts its attack skills and its buffs through the server's
own command admission, and the acceptance run of one player and twenty
Normal bots through the first five minutes of a match (the whole action
tick every 100 ms): the bots farm and level, use their skills, have
wolves near them, and do not kill each other early.

===========================================================================
*/

package action

import (
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
newBattleRoyaleShippedFixture

The bot fixture over the shipped skills, items and levels: the build
skill grant and the creation's base skills come from skilldata, the kits
from itemdata, and a credited kill levels the character through the
progression's exact EXP commit, as the server wires it.
================
*/
func newBattleRoyaleShippedFixture(t *testing.T, n int) *battleRoyaleFixture {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	fx := newBattleRoyaleBotFixture(t, n)
	deps := fx.rt.deps.(*enterworld.Deps)
	skills := enterworld.NewTextdataSkills(dir)
	deps.Skills = skills
	deps.Items = enterworld.NewTextdataItems(dir)
	deps.Levels = enterworld.NewTextdataLevels(dir)
	br := fx.rt.battleRoyale
	br.setup.Skills = skills
	br.setup.Seeders.Skills = enterworld.DefaultSkillSeeder(skills)
	br.skills = map[uint8][]enterworld.SkillRow{}
	fx.rt.CommitExperience = progression.NewRuntime(deps).ExactExperienceUpdater()
	return fx
}

/*
================
TestBattleRoyaleBotsCastTheirBuildSkills

One bot of every build at level 20 (its masteries trained and its build
skills learned as a level-up does), its kit worn, farming the wolves
around it for 40 s: every build casts at least one attack skill (not
only the basic attack), and every build with a buff its kit admits
casts one.
================
*/
func TestBattleRoyaleBotsCastTheirBuildSkills(t *testing.T) {
	const level = 20
	fx := newBattleRoyaleShippedFixture(t, 1)
	builds := battleroyale.Builds()
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, uint16(len(builds)))
	nowMs := fx.clock.NowMs()
	wantBuff := map[string]bool{}
	for i, bot := range m.bots {
		at, ok := fx.rt.battleRoyaleGround(bot.at)
		if !ok {
			t.Fatalf("bot %s stands off the ground", bot.name)
		}
		fx.rt.retireBattleRoyaleBot(m, bot)
		bot.build = builds[i]
		if !fx.rt.standBattleRoyaleBot(m, bot, at, nowMs) {
			t.Fatalf("bot %s of build %d cannot stand", bot.name, bot.build.ID)
		}
		lvl := int64(level)
		bot.c.Level, bot.c.MaxLevel = &lvl, &lvl
		battleroyale.SetMatchLevel(bot.c, bot.build, level)
		fx.rt.learnBattleRoyaleSkills(bot.c, bot.build)
		known := fx.rt.battleRoyaleBotSkillsOf(bot.c)
		if len(known.attacks) == 0 {
			t.Fatalf("build %d knows no attack skill at level %d (%d skills)", bot.build.ID, level, len(bot.c.Skills))
		}
		snapshot := fx.rt.characterSnapshot(testDivision, bot.c)
		for _, row := range known.buffs {
			// A buff the build cannot cast with its kit (a shield buff
			// without a shield) is not owed.
			if fx.rt.skillAdmission(testDivision, snapshot, row, nowMs, nil, nil, admitCommand) == 0 {
				wantBuff[bot.name] = true
			}
		}
	}
	fx.runtimeTick(40 * time.Second)
	for _, bot := range m.bots {
		if bot.stats == nil {
			t.Fatalf("bot %s has no counters", bot.name)
		}
		skills, buffs := bot.stats.skills.Load(), bot.stats.buffs.Load()
		t.Logf("build %2d %-12s skills %3d buffs %2d last %d wolves %d", bot.build.ID, battleroyale.BuildName(bot.build.ID), skills, buffs,
			bot.stats.lastSkill.Load(), bot.monsterKills)
		if skills == 0 {
			t.Errorf("build %d (%s) cast no attack skill", bot.build.ID, bot.name)
		}
		if wantBuff[bot.name] && buffs == 0 {
			t.Errorf("build %d (%s) cast no buff", bot.build.ID, bot.name)
		}
	}
}

/*
================
TestBattleRoyaleBotsFarmFirstAcceptance

The stage 8c acceptance run: one real player and twenty Normal bots,
the whole action tick every 100 ms for the first five minutes. The
player is kept alive (its HP refilled every second; the bots may and do
hunt it, but a dead last player ends the match at once). Asserts: the
median bot level reaches botSimMedianLevel, at most botSimEarlyBotKills
bots die to bots in the first three minutes, every bot cast a skill,
at every 30 s sample at most botSimStarved levelling bots have no wolf
within the brain's farm range, and when the player walks up to a
farming bot (visitDistanceM away at visitSecond) it attacks him within
visitTurnSeconds (the alert meter). The numbers are logged.
================
*/
func TestBattleRoyaleBotsFarmFirstAcceptance(t *testing.T) {
	const (
		bots                = 20
		simSeconds          = 300
		earlyMs             = 180_000
		botSimMedianLevel   = 8
		botSimEarlyBotKills = 3
		botSimStarved       = 2
		visitSecond         = 240
		visitDistanceM      = 40
		visitTurnSeconds    = 8
	)
	fx := newBattleRoyaleShippedFixture(t, 1)
	var events []BattleRoyaleEvent
	fx.rt.RecordBattleRoyale = func(event BattleRoyaleEvent) { events = append(events, event) }
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, bots)
	player := fx.chars[0]
	worstStarved, hurtSeconds := 0, 0
	attackers := map[string]bool{}
	playerGID := enterworld.ObjectIDForCharacter(player)
	var visited *battleRoyaleBot
	visitedAt := 0
	for sec := 1; sec <= simSeconds; sec++ {
		if sec == visitSecond {
			// The player walks up to a farming bot: its alert meter must
			// turn it on him within visitTurnSeconds.
			for _, bot := range m.bots {
				if bot.alive && bot.c != nil {
					visited = bot
					break
				}
			}
			at := fx.playerAt(visited.c)
			fx.place(player, battleRoyaleSpawnAt(battleroyale.Point{X: at.X + visitDistanceM*battleroyale.UnitsPerMeter, Z: at.Z}, 20))
		}
		fx.runtimeTick(time.Second)
		unlock := fx.rt.lockDivision(testDivision)
		if maxHP, _, hp, _ := fx.rt.playerKeeperVitals(testDivision, player); hp < maxHP {
			hurtSeconds++
		}
		for _, bot := range m.bots {
			if intent, ok := fx.rt.combatIntentFor(testDivision, bot.name); ok && intent.TargetGid == playerGID {
				attackers[bot.name] = true
				if bot == visited && visitedAt == 0 {
					visitedAt = sec
				}
			}
		}
		player.CurrentHP = nil
		if sec%30 == 0 {
			levelling, starved := fx.rt.battleRoyaleStarvedBots(m, fx.clock.NowMs())
			worstStarved = max(worstStarved, starved)
			t.Logf("t=%3ds: %d levelling bots, %d without a wolf within %d m", sec, levelling, starved, battleroyale.BotFarmRangeM)
		}
		unlock()
		if m.match.State() != battleroyale.StateRunning {
			t.Fatalf("the match ended at %d s", sec)
		}
	}
	levels := make([]int, 0, bots)
	earlyBotKills, deaths := 0, 0
	for _, event := range events {
		if event.Kind != "player_eliminated" || event.Fields["bot"] != "1" {
			continue
		}
		deaths++
		if event.Fields["killerBot"] == "1" && atoiOr(event.Fields["elapsedMs"]) <= earlyMs {
			earlyBotKills++
		}
	}
	var skills, buffs, picked, wolves, relocations int64
	silent := 0
	for _, bot := range m.bots {
		level := 0
		if bot.c != nil {
			level = int(rewardLevel(bot.c))
		}
		for _, event := range events {
			if event.Kind == "player_eliminated" && event.Character == bot.name {
				level = atoiOr(event.Fields["botLevel"])
			}
		}
		levels = append(levels, level)
		cast := bot.stats.skills.Load()
		if cast == 0 {
			silent++
		}
		skills += cast
		buffs += bot.stats.buffs.Load()
		picked += bot.stats.picked.Load()
		wolves += int64(bot.monsterKills)
		relocations += int64(bot.relocations)
	}
	slices.Sort(levels)
	median := levels[len(levels)/2]
	t.Logf("after %d s: bot levels %v (median %d), %d bot deaths (%d to bots in the first %d s), skills %d, buffs %d, pickups %d, wolf kills %d, relocations %d, worst starved %d; %d bots attacked the player (the visited bot %d s after he came), hurt in %d s",
		simSeconds, levels, median, deaths, earlyBotKills, earlyMs/1000, skills, buffs, picked, wolves, relocations, worstStarved, len(attackers),
		visitedAt-visitSecond, hurtSeconds)
	if median < botSimMedianLevel {
		t.Errorf("median bot level %d, want >= %d", median, botSimMedianLevel)
	}
	if earlyBotKills > botSimEarlyBotKills {
		t.Errorf("%d bots killed by bots in the first %d s, want <= %d", earlyBotKills, earlyMs/1000, botSimEarlyBotKills)
	}
	if silent > 0 {
		t.Errorf("%d bots never cast a skill", silent)
	}
	if visitedAt == 0 || visitedAt-visitSecond > visitTurnSeconds {
		t.Errorf("the bot the player walked up to attacked him at %d s (walked up at %d s)", visitedAt, visitSecond)
	}
	if worstStarved > botSimStarved {
		t.Errorf("up to %d levelling bots had no wolf near them, want <= %d", worstStarved, botSimStarved)
	}
}

/*
================
atoiOr

A telemetry field as an int, 0 when it is not one.
================
*/
func atoiOr(field string) int {
	n := 0
	for _, r := range field {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
