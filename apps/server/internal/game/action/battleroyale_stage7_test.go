/*
===========================================================================

battleroyale_stage7_test.go - smarter bots on the action runtime

Port-only, not native (docs/battle-royale.md "Stage 7" and "Stage 8").
The 0x7FB4 v2 switch (difficulty and count) and its roster, the ring
drop around the players, the 0x3FB5 bot map every 2 s to every
participant, a bot that hunts a player in its sense range and lets it go
past 1.5 x the range, a potion drunk through the item use, the
difficulty as stat writes with a Normal bot's PvP equal to a player's,
and the cost of 200 bots' tick.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
switchBotsV2

The GM's 0x7FB4 version 2; fails the test on a refusal. Returns the
answer's roster head.
================
*/
func (fx *battleRoyaleFixture) switchBotsV2(t *testing.T, c *enterworld.Character, on bool, difficulty uint8, count uint16) {
	t.Helper()
	body := []byte{0, difficulty, byte(count), byte(count >> 8)}
	if on {
		body[0] = 1
	}
	out := fx.rt.HandleBattleRoyaleBots(testDivision, c, body)
	if out.DiagnosticRefusal != "" || len(out.Frames) != 1 {
		t.Fatalf("switch refused: %+v", out)
	}
	if head := rosterHead(t, out.Frames[0].Payload); head.Enabled != on || head.Difficulty != difficulty || head.Configured != int(count) {
		t.Fatalf("switch answered %+v", head)
	}
}

/*
================
rosterHead

A 0x3FB4 v2 head read back.
================
*/
func rosterHead(t *testing.T, payload []byte) wire.BattleRoyaleBotRoster {
	t.Helper()
	r := wire.NewReader(payload)
	_, _ = r.U8()
	enabled, _ := r.U8()
	difficulty, _ := r.U8()
	configured, _ := r.U16()
	return wire.BattleRoyaleBotRoster{Enabled: enabled == 1, Difficulty: difficulty, Configured: int(configured)}
}

/*
================
startBotMatchWith

The GM sets the switch, difficulty and count and the lone lobby starts,
every player on Blade-Fire.
================
*/
func (fx *battleRoyaleFixture) startBotMatchWith(t *testing.T, difficulty uint8, count uint16) *battleRoyaleMatch {
	t.Helper()
	fx.tick(500 * time.Millisecond)
	fx.pickBlades(t)
	fx.switchBotsV2(t, fx.chars[0], true, difficulty, count)
	fx.startMatch(t)
	return fx.match()
}

//============================================================================

/*
================
TestBattleRoyaleBotSwitchV2

The v2 switch sets the difficulty and count every lobby member hears; a
one-byte switch is normal / 20; a malformed one is refused and changes
nothing. While off, the roster still carries a valid difficulty and
count. The drop stands the configured count at the configured
difficulty, and the options carry into the next lobby.
================
*/
func TestBattleRoyaleBotSwitchV2(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 2)
	gm, player := fx.chars[0], fx.chars[1]
	fx.tick(500 * time.Millisecond)
	out := fx.rt.HandleBattleRoyaleBots(testDivision, player, []byte{1, 2, 50, 0})
	if head := rosterHead(t, out.Frames[0].Payload); out.DiagnosticRefusal == "" || head.Enabled || head.Configured != battleroyale.DefaultBotCount ||
		head.Difficulty != wire.BattleRoyaleBotNormal {
		t.Fatalf("non-GM v2 switch: %+v %+v", out, head)
	}
	fx.switchBotsV2(t, gm, true, wire.BattleRoyaleBotHard, 50)
	if head := rosterHead(t, fx.frames[player.Name][len(fx.frames[player.Name])-1].Payload); !head.Enabled || head.Configured != 50 {
		t.Fatalf("the member heard %+v", head)
	}
	if out := fx.rt.HandleBattleRoyaleBots(testDivision, gm, []byte{1, 3, 50, 0}); out.DiagnosticRefusal == "" || fx.match().botCount != 50 ||
		fx.match().botDifficulty != battleroyale.BotHard {
		t.Fatalf("a malformed switch: %+v", out)
	}
	fx.switchBotsV2(t, gm, false, wire.BattleRoyaleBotHard, 50)
	out = fx.rt.HandleBattleRoyaleBots(testDivision, gm, []byte{1})
	if head := rosterHead(t, out.Frames[0].Payload); out.DiagnosticRefusal != "" || !head.Enabled || head.Configured != 20 ||
		head.Difficulty != wire.BattleRoyaleBotNormal {
		t.Fatalf("one-byte switch: %+v", head)
	}
	fx.switchBotsV2(t, gm, true, wire.BattleRoyaleBotHard, 7)
	fx.startMatch(t)
	m := fx.match()
	if len(m.bots) != 7 || m.match.AliveCount() != 9 {
		t.Fatalf("%d bots, %d alive", len(m.bots), m.match.AliveCount())
	}
	if writes := fx.rt.battleRoyaleStatWrites(testDivision, m.bots[0].c); len(writes) < len(battleRoyaleStatWrites(m.bots[0].c))+5 {
		t.Fatalf("hard bot writes %+v carry no difficulty", writes)
	}
	if d, n := m.match.BotOptions(); d != battleroyale.BotHard || n != 7 || m.botCount != 7 {
		t.Fatal("options lost at the drop")
	}
}

/*
================
TestBattleRoyaleBotsDropAroundThePlayers

Every bot character stands at least 150 m from the player's drop and
inside the grown ring (one real player: all of them ring it), not all
over Roc, and no two bots stand within BotRingSpacingM of each other
(stage 8c: neighbours in each other's sense fought from the first
second).
================
*/
func TestBattleRoyaleBotsDropAroundThePlayers(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 60)
	player, _ := m.match.Participant(battleroyale.PlayerID(fx.chars[0].Name))
	if len(m.bots) != 60 {
		t.Fatalf("%d bots", len(m.bots))
	}
	var stood []battleroyale.Point
	for _, bot := range m.bots {
		world := fx.rt.Worlds.Snapshot(simulation.WorldKey(testDivision, bot.name), func() simulation.WorldState { return simulation.SeedWorldState(bot.c) })
		at := battleRoyaleSpawnGrid(world.LiveSpawnAt(m.startedMs))
		if d := battleRoyaleDistance(at, player.Drop); d < battleroyale.BotRingMinM*battleroyale.UnitsPerMeter-10 || d > battleroyale.BotRingReach()+10 {
			t.Fatalf("bot %s drops %.0f m from the player", bot.name, d/battleroyale.UnitsPerMeter)
		}
		for _, other := range stood {
			if d := battleRoyaleDistance(at, other); d < battleroyale.BotRingSpacingM*battleroyale.UnitsPerMeter-10 {
				t.Fatalf("bot %s drops %.0f m from another bot", bot.name, d/battleroyale.UnitsPerMeter)
			}
		}
		stood = append(stood, at)
	}
}

/*
================
botPositions

Every 0x3FB5 a character received, decoded.
================
*/
func botPositions(t *testing.T, frames []wire.Frame) [][]wire.BattleRoyaleBotPosition {
	t.Helper()
	var out [][]wire.BattleRoyaleBotPosition
	for _, f := range frames {
		if f.Opcode != wire.OpBattleRoyaleBotPositions {
			continue
		}
		r := wire.NewReader(f.Payload)
		version, _ := r.U8()
		count, _ := r.U16()
		if version != wire.BattleRoyaleBotPositionsVersion {
			t.Fatalf("positions version %d", version)
		}
		var list []wire.BattleRoyaleBotPosition
		for range int(count) {
			var p wire.BattleRoyaleBotPosition
			p.GID, _ = r.U32()
			x, _ := r.F32()
			z, _ := r.F32()
			p.X, p.Z = x, z
			p.Level, _ = r.U8()
			list = append(list, p)
		}
		if err := r.Done(); err != nil {
			t.Fatalf("positions tail: %v", err)
		}
		out = append(out, list)
	}
	return out
}

/*
================
TestBattleRoyaleBotMapEveryTwoSeconds

Every 2 s every participant, the eliminated spectator too, gets the
living bots' positions: every living bot of the roster, where it
stands, at its level.
================
*/
func TestBattleRoyaleBotMapEveryTwoSeconds(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 2)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 12)
	spectator := fx.chars[1]
	_ = m.match.Eliminate(battleroyale.PlayerID(spectator.Name), "", battleroyale.EliminatedZone, fx.clock.NowMs())
	fx.tick(time.Second)
	for name := range fx.frames {
		fx.frames[name] = nil
	}
	fx.tick(10 * time.Second)
	for _, c := range fx.chars {
		maps := botPositions(t, fx.frames[c.Name])
		if len(maps) < 4 || len(maps) > 6 {
			t.Fatalf("%s got %d bot maps in 10 s", c.Name, len(maps))
		}
		last := maps[len(maps)-1]
		alive := 0
		for _, bot := range m.bots {
			if bot.alive {
				alive++
			}
		}
		if len(last) != alive || alive == 0 {
			t.Fatalf("%s's map lists %d of %d living bots", c.Name, len(last), alive)
		}
		for _, p := range last {
			bot := m.battleRoyaleBotOf(p.GID)
			if bot == nil || !bot.alive || int64(p.Level) != rewardLevel(bot.c) ||
				battleRoyaleDistance(battleroyale.Point{X: float64(p.X), Z: float64(p.Z)}, bot.at) > 2*battleRoyaleBotMapMs/1000*60 {
				t.Fatalf("map entry %+v for bot %+v", p, bot)
			}
		}
	}
}

/*
================
TestBattleRoyaleBotHuntsAndReleasesAPlayer

A bot with a player inside its sense range presses an attack on it (the
player becomes its attack's target); a player past 1.5 x the range is
let go (the attack ends).
================
*/
func TestBattleRoyaleBotHuntsAndReleasesAPlayer(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotNormal, 20)
	bot, player := fx.bladeBot(t, m), fx.chars[0]
	fx.idleBotsBut(m, bot)
	at := fx.playerAt(bot.c)
	fx.place(player, battleRoyaleSpawnAt(battleroyale.Point{X: at.X + 40, Z: at.Z}, 20))
	fx.standBot(t, bot, at)
	fx.botStep(m)
	if intent, ok := fx.rt.combatIntentFor(testDivision, bot.name); !ok || intent.TargetGid != enterworld.ObjectIDForCharacter(player) {
		t.Fatalf("the bot did not attack the player in reach: %+v", intent)
	}
	sense := battleroyale.ProfileFor(battleroyale.BotNormal).SenseRange()
	fx.place(player, battleRoyaleSpawnAt(battleroyale.Point{X: at.X - sense*1.6, Z: at.Z}, 20))
	fx.standBot(t, bot, at)
	bot.mind.NextPressMs = 0
	fx.botStep(m)
	if intent, ok := fx.rt.combatIntentFor(testDivision, bot.name); ok && intent.TargetGid == enterworld.ObjectIDForCharacter(player) {
		t.Fatal("the fight held past 1.5 x sense")
	}
}

/*
================
TestBattleRoyaleBotDrinksAPotion

A Hard bot under 40 % HP drinks one of its kit's HP potions on its next
think, through the ordinary item use: the stack shrinks by one.
================
*/
func TestBattleRoyaleBotDrinksAPotion(t *testing.T) {
	fx := newBattleRoyaleBotFixture(t, 1)
	m := fx.startBotMatchWith(t, wire.BattleRoyaleBotHard, 20)
	bot := fx.bladeBot(t, m)
	fx.idleBotsBut(m, bot)
	// The fixture's potion row heals as the shipped degree-1 potion does.
	potion := fx.rt.deps.(*enterworld.Deps).Items.(staticItemSource)["ITEM_ETC_HP_POTION_01"]
	potion.RecoveryHP, potion.Country = 120, 3
	potions := func() int64 {
		total := int64(0)
		for _, row := range bot.c.MissionInventory {
			if len(row.Codename) > len(battleRoyaleHPPotion) && row.Codename[:len(battleRoyaleHPPotion)] == battleRoyaleHPPotion {
				total += row.StackCount
			}
		}
		return total
	}
	before := potions()
	if before == 0 {
		t.Fatal("the bot carries no HP potion")
	}
	maxHP, _, _, _ := fx.rt.playerKeeperVitals(testDivision, bot.c)
	low := maxHP / 4
	bot.c.CurrentHP = &low
	fx.standBot(t, bot, fx.playerAt(bot.c))
	out := fx.botStep(m)
	if got := potions(); got != before-1 || bot.potions != 1 {
		t.Logf("publications %+v", out)
		t.Fatalf("potions %d -> %d (%d drunk)", before, got, bot.potions)
	}
}

/*
================
botTwin

A player character with the bot's record (same build, level and gear)
standing in the arena: the reference a bot's PvP is measured against.
================
*/
func (fx *battleRoyaleFixture) botTwin(t *testing.T, bot *battleRoyaleBot) *enterworld.Character {
	t.Helper()
	twin := domain.CloneCharacter(bot.c)
	// A GM's membership passes the arena layer's 16-player capacity the
	// bots already fill.
	twin.ID, twin.Name, twin.AccountID, twin.GMPrivilege = 777, "twin", "account-twin", true
	deps := fx.rt.deps.(*enterworld.Deps)
	fixtureCharacters(deps.Characters)[testDivision] = append(fixtureCharacters(deps.Characters)[testDivision], twin)
	if err := fx.rt.AdmitCharacterSession(testDivision, twin.Name, uint64(twin.ID)); err != nil {
		t.Fatal(err)
	}
	return twin
}

/*
================
pvpHit

The HP one basic-attack press of a takes from v standing beside it at
full HP (the fixture's combat draw is fixed, so the hit is exact).
================
*/
func (fx *battleRoyaleFixture) pvpHit(t *testing.T, a, v *enterworld.Character) int64 {
	t.Helper()
	fx.rt.ClearCombatIntent(testDivision, a.Name)
	fx.rt.clearSkillFinalizes(testDivision, a.Name)
	center := battleRoyaleSpawnAt(fx.match().match.Arena().Center, 20)
	fx.place(a, center)
	fx.place(v, simulation.Spawn{RegionID: center.RegionID, X: center.X + 3, Y: 20, Z: center.Z})
	v.CurrentHP = nil
	_, _, before, _ := fx.rt.playerKeeperVitals(testDivision, v)
	out := fx.rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(v)}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("%s's strike on %s refused: %s", a.Name, v.Name, out.DiagnosticRefusal)
	}
	_, _, after, _ := fx.rt.playerKeeperVitals(testDivision, v)
	fx.rt.ClearCombatIntent(testDivision, a.Name)
	fx.rt.clearSkillFinalizes(testDivision, a.Name)
	return before - after
}

/*
================
TestBattleRoyaleBotPvPMatchesAPlayer

Damage between bots and players is the ordinary PvP pipeline (stage 8):
a Normal bot and a player of the same build, level and gear hit each
other equally hard and hold the same HP, so their fight lasts as long as
player against player; the difficulty is the one rule on top, as stat
writes (Hard x1.5 attack, x1.3 HP; Easy x0.6, x0.8; the hit, attack less
defence, moves within 20 % of the attack's scale).
================
*/
func TestBattleRoyaleBotPvPMatchesAPlayer(t *testing.T) {
	for _, c := range []struct {
		difficulty   uint8
		damage, life float64
	}{{wire.BattleRoyaleBotNormal, 1, 1}, {wire.BattleRoyaleBotHard, 1.5, 1.3}, {wire.BattleRoyaleBotEasy, 0.6, 0.8}} {
		fx := newBattleRoyaleBotFixture(t, 1)
		items := fx.rt.deps.(*enterworld.Deps).Items.(staticItemSource)
		m := fx.startBotMatchWith(t, c.difficulty, 20)
		bot := fx.bladeBot(t, m)
		// The kit blade fights with the fixture sword's combat numbers.
		weapon, _ := rowAt(bot.c, int64(inventory.SocketWeapon))
		items[weapon.Codename].Combat = items["ITEM_CH_SWORD_01_A"].Combat
		twin := fx.botTwin(t, bot)
		fx.idleBotsBut(m, bot)
		botHit, twinHit := fx.pvpHit(t, bot.c, twin), fx.pvpHit(t, twin, bot.c)
		botHP, _, _, _ := fx.rt.playerKeeperVitals(testDivision, bot.c)
		twinHP, _, _, _ := fx.rt.playerKeeperVitals(testDivision, twin)
		t.Logf("difficulty %d: the bot hits its twin player for %d, the player hits the bot for %d; HP bot %d, player %d: %.1f vs %.1f hits to kill",
			c.difficulty, botHit, twinHit, botHP, twinHP, float64(twinHP)/float64(max(botHit, 1)), float64(botHP)/float64(max(twinHit, 1)))
		if twinHit <= 0 || botHit <= 0 {
			t.Fatalf("no PvP damage: bot %d, player %d", botHit, twinHit)
		}
		// The difficulty scales attack power, and the hit is attack less
		// defence, so a hit moves a little more than the attack: within 20 %.
		if ratio := float64(botHit) / float64(twinHit); ratio < c.damage*0.8 || ratio > c.damage*1.2 || c.damage == 1 && botHit != twinHit {
			t.Fatalf("difficulty %d: a bot hits x%.2f a player's, want x%.1f", c.difficulty, ratio, c.damage)
		}
		if life := float64(botHP) / float64(twinHP); life < c.life*0.97 || life > c.life*1.03 {
			t.Fatalf("difficulty %d: a bot holds x%.2f a player's HP, want x%.1f", c.difficulty, life, c.life)
		}
	}
}

/*
================
botTickCost

Starts a match of count Hard bots (every bot thinks every tick, the
worst case) beside one player, then times n brain steps (decisions and
the commands they issue) and n tick snapshots of every bot (the movement
lane's HeadlessSnapshot, as the server's tick takes them every 100 ms).
================
*/
func botTickCost(t *testing.T, fx *battleRoyaleFixture, count uint16, n int) (brain, snapshots time.Duration) {
	fx.tick(500 * time.Millisecond)
	out := fx.rt.HandleBattleRoyaleBots(testDivision, fx.chars[0], []byte{1, wire.BattleRoyaleBotHard, byte(count), byte(count >> 8)})
	if out.DiagnosticRefusal != "" {
		t.Fatalf("switch refused: %s", out.DiagnosticRefusal)
	}
	fx.tick(500 * time.Millisecond)
	fx.tick(time.Duration(battleroyale.DefaultCountdownMs) * time.Millisecond)
	m := fx.match()
	if len(m.bots) != int(count) || len(fx.rt.BattleRoyaleHeadless()) != int(count) {
		t.Fatalf("%d bots, %d standing", len(m.bots), len(fx.rt.BattleRoyaleHeadless()))
	}
	for range n {
		fx.clock.Advance(battleRoyaleTickMs * time.Millisecond)
		began := time.Now()
		fx.botStep(m)
		brain += time.Since(began)
		began = time.Now()
		for _, h := range fx.rt.BattleRoyaleHeadless() {
			_ = fx.movement.HeadlessSnapshot(h.Division, h.Character)
		}
		snapshots += time.Since(began)
	}
	return brain / time.Duration(n), snapshots / time.Duration(n)
}

/*
================
TestBattleRoyale200BotsTickCost

200 Hard bots (characters, every bot thinking every step) beside the
match wolves: one brain step (0.5 s of match time) and one tick's
snapshots of all of them stay cheap (logged; failed past the budgets).
================
*/
func TestBattleRoyale200BotsTickCost(t *testing.T) {
	const (
		battleRoyaleBotTickBudget     = 60 * time.Millisecond
		battleRoyaleBotSnapshotBudget = 20 * time.Millisecond
	)
	fx := newBattleRoyaleBotFixture(t, 1)
	brain, snapshots := botTickCost(t, fx, battleroyale.MaxBotCount, 20)
	t.Logf("200 Hard bots: %.2f ms per brain step (every bot thinking, commands included), %.2f ms per tick of 200 snapshots, %d nav expansions over %d plans",
		float64(brain.Microseconds())/1000, float64(snapshots.Microseconds())/1000, fx.match().botNav.Expansions, fx.match().botNav.Plans)
	if brain > battleRoyaleBotTickBudget || snapshots > battleRoyaleBotSnapshotBudget {
		t.Fatalf("a 200-bot step costs %v, its snapshots %v", brain, snapshots)
	}
}
