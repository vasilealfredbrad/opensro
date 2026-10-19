/*
===========================================================================

battleroyale_persist_test.go - the real record survives a match and a crash

Port-only, not native. A store-backed composition (the server wiring's:
store.Open -> Deps over its doors -> NewRuntime) runs matches against a
real authority; the state file is copied at every phase and hydrated by
a fresh store whose boot recovery restores the hold, and a child process
is killed at random instants mid-match. In every case the real character
comes back as it was, except for its stand, which is the lobby.

===========================================================================
*/

package action

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	battleRoyaleSeedGold   = 5000
	battleRoyaleSeedSword  = "ITEM_CH_SWORD_01_A"
	battleRoyaleSeedModel  = "CHAR_CH_MAN_ADVENTURER"
	envBattleRoyaleHelper  = "SRO_BR_BURST_HELPER"
	envBattleRoyaleDir     = "SRO_BR_BURST_DIR"
	battleRoyaleKillRuns   = 6
	battleRoyaleHelperStop = 30 * time.Second
)

/*
================
battleRoyaleDoor

One store-backed composition.
================
*/
type battleRoyaleDoor struct {
	rt        *Runtime
	authority *store.Store
	clock     *fakeClock
	chars     []*enterworld.Character
	dir       string
}

/*
================
battleRoyaleDoorSeed

A level-1 Chinese character of the fixture: the sword, some gold, the
lobby stand.
================
*/
func battleRoyaleDoorSeed(i int) *enterworld.Character {
	gold, level, base := int64(battleRoyaleSeedGold), int64(1), int64(domain.BaseStat)
	c := &enterworld.Character{
		Name: fmt.Sprintf("royale%d", i), ModelCodename: battleRoyaleSeedModel, RaceIndex: testInt64(enterworld.RaceChina),
		Gold: &gold, Level: &level, MaxLevel: &level, Strength: &base, Intellect: &base,
		Masteries: []enterworld.CharacterMastery{{ID: 257, Level: 1}},
		Skills:    []uint32{2},
		MissionInventory: []enterworld.InventoryRow{{Slot: 6, RefObjID: 71, Codename: battleRoyaleSeedSword,
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", Durability: 76, StackCount: 1}},
	}
	writeBackWorld(c, simulation.WorldState{Spawn: simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ}, SpawnSet: true})
	return c
}

/*
================
openBattleRoyaleDoor

Opens (or reopens) the authority in dir, creating n characters on a first
boot, and composes the runtime over its doors. The mode is on.
================
*/
func openBattleRoyaleDoor(t *testing.T, dir string, n int) *battleRoyaleDoor {
	t.Helper()
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)
	existing := authority.Characters().CharactersForDivision(testDivision)
	for i := len(existing); i < n; i++ {
		if err := authority.CreateCharacter(testDivision, fmt.Sprintf("royale-account-%d", i), battleRoyaleDoorSeed(i)); err != nil {
			t.Fatalf("CreateCharacter: %v", err)
		}
	}
	chars := authority.Characters().CharactersForDivision(testDivision)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	sword := &enterworld.ItemRef{RefObjID: 71, Codename: battleRoyaleSeedSword, TypeIDs: [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{ActionRange: 6}}
	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Items:      staticItemSource{sword.Codename: sword},
		Levels:     testCombatRewardLevels(),
		Skills:     staticSkillSource{2: {ID: 2, Codename: "SKILL_CH_SWORD_BASE_01", Group: 1, Level: 1}},
		Roster: &enterworld.Roster{Format: "sro-server-character-authority", Version: 3, Models: []enterworld.RosterModel{
			{Codename: battleRoyaleSeedModel, RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000},
			{Codename: "CHAR_EU_MAN_NOBLE", RefObjID: 14717, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000}}},
		SpawnTerrainHeight: func(uint16, float64, float64) (float64, bool) { return 20, true },
		MutateCharacter:    authority.MutateCharacter,
		MutateCharacters:   authority.MutateCharacters,
		UpdateCharacter:    authority.UpdateCharacter,
		UpdateCharacters:   authority.UpdateCharacters,
	}
	rt := NewRuntime(deps, battleRoyaleTestMonsters(clock))
	rt.Now = clock.Now
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	rt.CombatRoll = func() (uint32, error) { return 100, nil }
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	rt.PushShardPeerFrames = func(string, string, []wire.Frame) {}
	rt.SilkWallet = authority
	fx := &battleRoyaleFixture{rt: rt, clock: clock, sword: battleRoyaleDoorSeed(0).MissionInventory[0]}
	rt.ConfigureBattleRoyale(BattleRoyaleSetup{Seeders: fx.seeders(), Seed: func() uint64 { return battleRoyaleTestSeed }})
	return &battleRoyaleDoor{rt: rt, authority: authority, clock: clock, chars: chars, dir: dir}
}

/*
================
admitAll

Every character into the world, standing in the lobby circle.
================
*/
func (d *battleRoyaleDoor) admitAll(t *testing.T) {
	t.Helper()
	for _, c := range d.chars {
		if err := d.rt.AdmitCharacterSession(testDivision, c.Name, uint64(c.ID)); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
tick
================
*/
func (d *battleRoyaleDoor) tick(span time.Duration) {
	for elapsed := time.Duration(0); elapsed < span; elapsed += battleRoyaleTickMs * time.Millisecond {
		d.clock.Advance(battleRoyaleTickMs * time.Millisecond)
		d.rt.advanceBattleRoyale(d.clock.NowMs())
	}
}

/*
================
snapshot

A consistent copy of the state file in its own directory.
================
*/
func (d *battleRoyaleDoor) snapshot(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.authority.BackupTo(filepath.Join(dir, store.DBFileName)); err != nil {
		t.Fatalf("snapshot %s: %v", name, err)
	}
	return dir
}

/*
================
bootRestoredRecords

A fresh store over dir, the boot recovery, and every record's bytes.
================
*/
func bootRestoredRecords(t *testing.T, dir string) (map[string][]byte, []string) {
	t.Helper()
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	defer authority.Close()
	restored := RestoreHeldCharacters(authority.DivisionIDs(), authority.Characters(), authority.MutateCharacter)
	out := map[string][]byte{}
	for _, c := range authority.Characters().CharactersForDivision(testDivision) {
		if battleroyale.Held(c) {
			t.Fatalf("%s still held after the boot recovery", c.Name)
		}
		if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
			t.Fatalf("%s not in the default world after the boot recovery", c.Name)
		}
		out[c.Name] = recordBytes(t, c)
	}
	return out, restored
}

/*
================
TestBattleRoyaleRealRecordSurvivesEveryPhase

One match against a real store; at every phase (lobby, countdown, the
drop, each zone stage, after a death, after the win) the state file is
copied and hydrated by a fresh store whose boot recovery must return
every real record as it was; and once the match closed, the live records
are the originals too.
================
*/
func TestBattleRoyaleRealRecordSurvivesEveryPhase(t *testing.T) {
	d := openBattleRoyaleDoor(t, filepath.Join(t.TempDir(), "authority"), 2)
	d.admitAll(t)
	originals := map[string][]byte{}
	for _, c := range d.chars {
		originals[c.Name] = recordBytes(t, c)
	}
	snapshots := map[string]string{}
	take := func(name string) {
		if _, taken := snapshots[name]; !taken {
			snapshots[name] = d.snapshot(t, name)
		}
	}
	d.tick(battleRoyaleTickMs * time.Millisecond)
	take("lobby-countdown")
	m := d.rt.battleRoyaleMatchOf(testDivision)
	d.tick(time.Duration(battleroyale.DefaultCountdownMs) * time.Millisecond)
	if m.match.State() != battleroyale.StateRunning {
		t.Fatalf("state %v after the countdown", m.match.State())
	}
	for _, c := range d.chars {
		if !battleroyale.Held(c) {
			t.Fatalf("%s not held after the drop", c.Name)
		}
	}
	take("dropped")
	center := battleRoyaleSpawnAt(m.match.Arena().Center, 20)
	for _, c := range d.chars {
		d.rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) { w.Spawn, w.SpawnSet, w.MovementSourceSeeded = center, true, true })
	}
	zone, _ := m.match.Zone()
	deaths := 0
	for step := 0; step < 4000 && m.match.State() == battleroyale.StateRunning; step++ {
		d.tick(time.Second)
		at := battleroyale.CircleAt(zone, d.clock.NowMs())
		take(fmt.Sprintf("zone-%d-%s", at.Phase, at.Stage))
		if alive := m.match.AliveCount(); alive < len(d.chars)-deaths {
			deaths = len(d.chars) - alive
			take("after-death")
		}
	}
	if m.match.State() != battleroyale.StateFinished {
		t.Fatalf("the zone never resolved the match: %v", m.match.State())
	}
	take("after-win")
	d.tick(battleRoyaleLingerMs * time.Millisecond)
	for _, c := range d.chars {
		if battleroyale.Held(c) {
			t.Fatalf("%s still held after the match closed", c.Name)
		}
		if got := recordBytes(t, c); !bytes.Equal(got, originals[c.Name]) {
			t.Fatalf("%s differs after the match:\n%s\n%s", c.Name, got, originals[c.Name])
		}
	}
	if len(snapshots) < 7 {
		t.Fatalf("only %d phases snapshotted: %v", len(snapshots), snapshots)
	}
	for name, dir := range snapshots {
		records, restored := bootRestoredRecords(t, dir)
		for _, c := range d.chars {
			if got, ok := records[c.Name]; !ok || !bytes.Equal(got, originals[c.Name]) {
				t.Fatalf("phase %s: %s hydrated differently (boot restored %v):\n%s\n%s", name, c.Name, restored, got, originals[c.Name])
			}
		}
	}
}

/*
================
TestBattleRoyaleHoldSurvivesRestartAndEntry

The same directory reopened mid-match: the boot recovery restores the
hold; and a held record whose match is gone restores at world entry.
================
*/
func TestBattleRoyaleHoldSurvivesRestartAndEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")
	d := openBattleRoyaleDoor(t, dir, 2)
	d.admitAll(t)
	originals := map[string][]byte{}
	for _, c := range d.chars {
		originals[c.Name] = recordBytes(t, c)
	}
	d.tick(time.Duration(battleroyale.DefaultCountdownMs+battleRoyaleTickMs) * time.Millisecond)
	for _, c := range d.chars {
		if !battleroyale.Held(c) {
			t.Fatalf("%s not held", c.Name)
		}
	}
	d.authority.Close()
	r := openBattleRoyaleDoor(t, dir, 0)
	// Nothing restored the rows yet: the hold came back from the file.
	for _, c := range r.chars {
		if !battleroyale.Held(c) || c.BattleRoyaleHeld.Real == nil {
			t.Fatalf("%s lost its hold across the restart", c.Name)
		}
	}
	// World entry of the first: no running match knows it, so it restores.
	if err := r.rt.BattleRoyaleEntryRestore(testDivision, r.chars[0]); err != nil {
		t.Fatal(err)
	}
	if got := recordBytes(t, r.chars[0]); !bytes.Equal(got, originals[r.chars[0].Name]) {
		t.Fatalf("entry restore differs:\n%s\n%s", got, originals[r.chars[0].Name])
	}
	// The boot recovery for the second.
	if restored := RestoreHeldCharacters(r.authority.DivisionIDs(), r.authority.Characters(), r.authority.MutateCharacter); len(restored) != 1 {
		t.Fatalf("boot restored %v", restored)
	}
	if got := recordBytes(t, r.chars[1]); !bytes.Equal(got, originals[r.chars[1].Name]) {
		t.Fatalf("boot restore differs:\n%s\n%s", got, originals[r.chars[1].Name])
	}
	r.authority.Close()
	f := openBattleRoyaleDoor(t, dir, 0)
	for _, c := range f.chars {
		if battleroyale.Held(c) || !bytes.Equal(recordBytes(t, c), originals[c.Name]) {
			t.Fatalf("%s not the original after the second restart", c.Name)
		}
	}
}

/*
================
TestBattleRoyaleKillMidMatchNeverTearsRecords

A child process runs matches back to back against a real store and is
killed at a random instant; the survivor's rows are whole: a held row's
real record restores to the seed, an unheld row is the seed, and the
state file never fell back to its backup.
================
*/
func TestBattleRoyaleKillMidMatchNeverTearsRecords(t *testing.T) {
	if os.Getenv(envBattleRoyaleHelper) == "1" {
		battleRoyaleHelperMain(t)
		return
	}
	if testing.Short() {
		t.Skip("child-process kill storm")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < battleRoyaleKillRuns; i++ {
		// The store opens in a few hundred ms; later runs land mid-match.
		killAfter := time.Duration(200+i*300) * time.Millisecond
		t.Run(fmt.Sprintf("kill-%d", i), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "authority")
			child := exec.Command(exe, "-test.run", "^TestBattleRoyaleKillMidMatchNeverTearsRecords$", "-test.v")
			child.Env = append(os.Environ(), envBattleRoyaleHelper+"=1", envBattleRoyaleDir+"="+dir)
			var childOut bytes.Buffer
			child.Stdout, child.Stderr = &childOut, &childOut
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(killAfter) //nolint:forbidigo // fault injection: kill the child at a wall-clock moment
			if err := child.Process.Kill(); err != nil {
				t.Fatalf("kill: %v (child output: %s)", err, childOut.String())
			}
			_ = child.Wait()
			_ = os.Remove(filepath.Join(dir, "authority.lock"))
			survivor, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
			if err != nil {
				t.Fatalf("survivor store does not open: %v (child output: %s)", err, childOut.String())
			}
			defer survivor.Close()
			if survivor.Health().LoadedFromBak {
				t.Fatalf("survivor recovered from .bak (child output: %s)", childOut.String())
			}
			if quarantines, _ := filepath.Glob(filepath.Join(dir, store.DBFileName+".corrupt-*")); len(quarantines) != 0 {
				t.Fatalf("kill produced quarantine artifacts: %v", quarantines)
			}
			RestoreHeldCharacters(survivor.DivisionIDs(), survivor.Characters(), survivor.MutateCharacter)
			for _, c := range survivor.Characters().CharactersForDivision(testDivision) {
				assertBattleRoyaleSeedRecord(t, c)
			}
		})
	}
}

/*
================
assertBattleRoyaleSeedRecord

The real record as the seed made it, whatever the match did.
================
*/
func assertBattleRoyaleSeedRecord(t *testing.T, c *enterworld.Character) {
	t.Helper()
	if battleroyale.Held(c) {
		t.Fatalf("%s still held", c.Name)
	}
	if c.Level == nil || *c.Level != 1 || c.Gold == nil || *c.Gold != battleRoyaleSeedGold || c.ModelCodename != battleRoyaleSeedModel {
		t.Fatalf("%s torn: level %v gold %v model %s", c.Name, c.Level, c.Gold, c.ModelCodename)
	}
	if len(c.MissionInventory) != 1 || c.MissionInventory[0].Codename != battleRoyaleSeedSword || c.MissionInventory[0].Slot != 6 {
		t.Fatalf("%s torn inventory: %+v", c.Name, c.MissionInventory)
	}
	if len(c.Masteries) != 1 || c.Masteries[0] != (enterworld.CharacterMastery{ID: 257, Level: 1}) {
		t.Fatalf("%s torn masteries: %+v", c.Name, c.Masteries)
	}
	if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
		t.Fatalf("%s left in world %08x", c.Name, domain.CharacterWorldInstance(c))
	}
}

/*
================
battleRoyaleHelperMain

The child: matches back to back until killed. Each match drops both
characters, sends the first outside the zone until it dies, lets the
winner linger and closes; every step commits through the real door.
================
*/
func battleRoyaleHelperMain(t *testing.T) {
	dir := os.Getenv(envBattleRoyaleDir)
	if dir == "" {
		t.Fatal("helper without " + envBattleRoyaleDir)
	}
	d := openBattleRoyaleDoor(t, dir, 2)
	d.admitAll(t)
	deadline := time.Now().Add(battleRoyaleHelperStop)
	outside := simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ}
	for time.Now().Before(deadline) {
		d.tick(battleRoyaleTickMs * time.Millisecond)
		m := d.rt.battleRoyaleMatchOf(testDivision)
		if m.match.State() == battleroyale.StateRunning {
			victim := d.chars[0]
			if !battleroyale.Held(victim) {
				victim = d.chars[1]
			}
			d.rt.Worlds.Update(simulation.WorldKey(testDivision, victim.Name), func() simulation.WorldState { return simulation.SeedWorldState(victim) },
				func(w *simulation.WorldState) { w.Spawn, w.SpawnSet, w.MovementSourceSeeded = outside, true, true })
			d.tick(5 * time.Second)
		}
		// A closed match reopens the lobby; both stand in it again.
		for _, c := range d.chars {
			if !battleroyale.Held(c) {
				d.rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
					func(w *simulation.WorldState) {
						w.Spawn, w.SpawnSet, w.MovementSourceSeeded = simulation.Spawn{RegionID: battleRoyaleLobbyRegion, X: battleRoyaleLobbyX, Y: 20, Z: battleRoyaleLobbyZ}, true, true
					})
			}
		}
	}
	t.Fatal("battle royale helper survived its guard - the parent never killed it")
}
