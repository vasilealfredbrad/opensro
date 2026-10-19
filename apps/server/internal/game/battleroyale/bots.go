/*
===========================================================================

bots.go - the server-driven fighters a GM adds to a match (stage 5)

Port-only, not native (see zone.go; docs/battle-royale.md "Stage 5:
bots"; stage 7 adds the difficulty and the count). A GM in the lobby
switches bots on (Match.SetBots) and picks how many (1-MaxBotCount) and
how hard (Match.SetBotOptions, botprofile.go); with the switch on, one
real lobby member is enough to start the countdown and the drop adds
that many bot participants beside the players. A bot is an
ordinary Participant with Bot set: it has a drop point, counts in the
alive count, is eliminated and credited kills through Eliminate and can
win. It never disconnects and has no build. The wiring owns its body (a
real match character with no session, from stage 8) and its brain; this
file owns the roster rules. The numbers below were the stage-5/7 monster
bodies'; only the economy simulation's bot model still reads them:

	BotLevel     1 -> 30 by time alive and kills (BotLevelPaced scales
	             it by the difficulty's pace)
	BotMaxHP     a match character's max HP at that level (x2.5)
	BotCodename  the level's thief row (stage 5/7)
	BotModel     the visible thief row of the i-th bot (stage 5/7)

===========================================================================
*/
package battleroyale

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
)

const (
	// botStream selects the PCG stream that shuffles the bot names.
	botStream = 0x426f7473 // "Bots"
	// botLevelMs is the time alive one bot level takes (29 levels in
	// 14.5 minutes, about the time a player farms to 30).
	botLevelMs = 30_000
	// botMonsterKillsPerLevel and botPlayerKillLevels are what kills add:
	// a level per four monsters (a player needs 3.5), two per participant.
	botMonsterKillsPerLevel = 4
	botPlayerKillLevels     = 2
	// botMajorStatPerLevel is the strength a level adds to the bot's HP
	// curve, the melee builds' 2 STR (presets.go).
	botMajorStatPerLevel = 2
	// BotModelRows is how many thief rows dress the bots: the ten rows of
	// level 1 (MOB_THIEF_NPC_0001..0010), one look each.
	BotModelRows = 10
	// botRowsPerLevel: characterdata holds ten thief rows per level, row
	// (level-1)*10+1 first (docs/battle-royale.md stage 2 "Thieves").
	botRowsPerLevel = 10
	// botCodenamePrefix is the caravan thief job row family.
	botCodenamePrefix = "MOB_THIEF_NPC_"
	// botNameTiers bounds the numbered name tiers ("Ares2" ... ), so a
	// NameTaken that refuses everything cannot spin: 40 names a tier, at
	// least MaxBotCount names however many are taken.
	botNameTiers = 99
)

// botNames are the player-like names a bot draws (shuffled per match;
// one never repeats a lobby member's name). Past the forty plain names a
// match of up to MaxBotCount bots takes them again with a number
// ("Ares2", ... "Ares6"), each tier shuffled on its own.
var botNames = [...]string{
	"Ares", "Mira", "Kael", "Luna", "Doran", "Vex", "Selene", "Thorn", "Nyx", "Rook",
	"Ivy", "Brann", "Zara", "Orin", "Lyra", "Fenris", "Kira", "Talon", "Wren", "Jax",
	"Sora", "Rhea", "Dax", "Nova", "Gale", "Echo", "Bram", "Juno", "Pike", "Vesna",
	"Cato", "Iris", "Hale", "Mako", "Rune", "Sable", "Tarek", "Yuna", "Zed", "Odin",
}

/*
================
Match.SetBots

The GM switch: during the lobby or the countdown, whether the next drop
adds bots. Switching off during a countdown with one real member cancels
it on the next Tick.
================
*/
func (m *Match) SetBots(enabled bool) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	m.bots = enabled
	return nil
}

/*
================
Match.SetBotOptions

The GM's difficulty and bot count (1-MaxBotCount) for the next drop,
during the lobby or the countdown.
================
*/
func (m *Match) SetBotOptions(difficulty BotDifficulty, count int) error {
	if m.state != StateLobby && m.state != StateCountdown {
		return ErrWrongState
	}
	if !ValidBotDifficulty(difficulty) || count < 1 || count > MaxBotCount {
		return ErrInvalidConfig
	}
	m.botDifficulty, m.botCount = difficulty, count
	return nil
}

/*
================
Match.BotOptions

The difficulty and count the next (or the running) match's bots use.
================
*/
func (m *Match) BotOptions() (BotDifficulty, int) {
	return m.botDifficulty, m.botCount
}

/*
================
Match.BotsEnabled
================
*/
func (m *Match) BotsEnabled() bool {
	return m.bots
}

/*
================
Match.quorum

The lobby size that starts (and keeps) a countdown: MinPlayers, or one
real member while bots are on (the bots fill the rest).
================
*/
func (m *Match) quorum() int {
	if m.bots {
		return 1
	}
	return m.cfg.MinPlayers
}

/*
================
Match.botIDs

The names of a new match's bots: botCount names, the plain tier first,
each tier shuffled by the seed, skipping any a lobby member holds
(case-insensitively, as character names compare) or Config.NameTaken
reports taken.
================
*/
func (m *Match) botIDs(members []PlayerID) []PlayerID {
	if !m.bots {
		return nil
	}
	taken := make(map[string]bool, len(members))
	for _, id := range members {
		taken[strings.ToLower(string(id))] = true
	}
	rng := rand.New(rand.NewPCG(m.cfg.Seed, botStream))
	out := make([]PlayerID, 0, m.botCount)
	for tier := 1; len(out) < m.botCount && tier <= botNameTiers; tier++ {
		for _, i := range rng.Perm(len(botNames)) {
			if len(out) == m.botCount {
				break
			}
			name := botNames[i]
			if tier > 1 {
				name += strconv.Itoa(tier)
			}
			if !taken[strings.ToLower(name)] && (m.cfg.NameTaken == nil || !m.cfg.NameTaken(name)) {
				out = append(out, PlayerID(name))
			}
		}
	}
	return out
}

/*
================
Match.IsBot

Whether a participant of the running or finished match is a bot.
================
*/
func (m *Match) IsBot(id PlayerID) bool {
	p, ok := m.participants[id]
	return ok && p.Bot
}

/*
================
Match.playersAlive

Whether any participant that is not a bot is still in the match.
================
*/
func (m *Match) playersAlive() bool {
	for _, p := range m.participants {
		if p.Alive && !p.Bot {
			return true
		}
	}
	return false
}

/*
================
Match.finishAmongBots

The last player is out while bots stand: nobody is left to watch, so the
match ends at once instead of running the zone down for the bots alone.
The standing bots are ranked by kills (ties by name); the first wins
(placement 1), the others take the places after it, all above the
eliminated.
================
*/
func (m *Match) finishAmongBots(nowMs int64, events []Event) []Event {
	m.state = StateFinished
	var standing []*Participant
	for _, id := range m.order {
		if p := m.participants[id]; p.Alive {
			standing = append(standing, p)
		}
	}
	slices.SortStableFunc(standing, func(a, b *Participant) int {
		return cmp.Compare(b.Kills, a.Kills)
	})
	ended := MatchEnded{AtMs: nowMs, Winner: standing[0].ID}
	for i, p := range standing {
		p.Placement = i + 1
		ended.Standings = append(ended.Standings, p.ID)
	}
	for i := len(m.eliminated) - 1; i >= 0; i-- {
		ended.Standings = append(ended.Standings, m.eliminated[i])
	}
	return append(events, ended)
}

//============================================================================

/*
================
BotLevel

A bot's level after elapsedMs alive in the match with the given kills:
one level per botLevelMs, one per botMonsterKillsPerLevel monsters and
botPlayerKillLevels per participant it eliminated, 1 to MatchLevelCap.
================
*/
func BotLevel(elapsedMs int64, monsterKills, participantKills int) int {
	level := 1 + max(elapsedMs, 0)/botLevelMs + int64(max(monsterKills, 0)/botMonsterKillsPerLevel) +
		int64(max(participantKills, 0)*botPlayerKillLevels)
	return int(min(level, MatchLevelCap))
}

/*
================
BotMaxHP

The HP a bot of level has: a match character's at that level (the native
curve over the melee builds' strength, x2.5 per the balance rule).
================
*/
func BotMaxHP(level int) int64 {
	level = int(min(max(int64(level), matchStartLevel), MatchLevelCap))
	strength := domain.BaseStat + botMajorStatPerLevel*int64(level-1)
	return charactervitals.DerivedVitalMax(int64(level), strength) * (100 + MatchMaxHPPercent) / 100
}

/*
================
BotCodename

The thief row of level whose combat numbers a bot of that level fights
with (MOB_THIEF_NPC_0001 for 1 ... _0291 for 30).
================
*/
func BotCodename(level int) string {
	level = int(min(max(int64(level), matchStartLevel), MatchLevelCap))
	return fmt.Sprintf("%s%04d", botCodenamePrefix, (level-1)*botRowsPerLevel+1)
}

/*
================
BotModel

The visible row of the i-th bot: one of the BotModelRows level-1 thief
rows, so the client shows the bot as a level-1 bandit with the row's
look and HP bar whatever its level.
================
*/
func BotModel(i int) string {
	return fmt.Sprintf("%s%04d", botCodenamePrefix, i%BotModelRows+1)
}
