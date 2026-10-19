/*
===========================================================================

presets_test.go - the build table, the record swap and the loot plans

===========================================================================
*/
package battleroyale

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestBuildTable

Every build has a distinct id, weapon kinds of its own race, two
masteries, and round-trips through its name; the default is build 11.
================
*/
func TestBuildTable(t *testing.T) {
	seen := map[uint8]bool{}
	for _, build := range Builds() {
		if seen[build.ID] {
			t.Fatalf("build %d listed twice", build.ID)
		}
		seen[build.ID] = true
		if build.Masteries[0] == 0 || build.Masteries[1] == 0 || build.Masteries[0] == build.Masteries[1] {
			t.Fatalf("build %d masteries %v", build.ID, build.Masteries)
		}
		for _, kind := range build.WeaponKinds {
			if _, ok := SunWeaponCodename(build.Race, kind, 1); !ok {
				t.Fatalf("build %d weapon kind %d is not of its race", build.ID, kind)
			}
		}
		if got := BuildByName(BuildName(build.ID)); got.ID != build.ID {
			t.Fatalf("build %d round-trips as %d", build.ID, got.ID)
		}
		if got, ok := BuildByID(build.ID); !ok || got.Name != build.Name {
			t.Fatalf("build %d lookup %+v %v", build.ID, got, ok)
		}
	}
	if DefaultPreset().ID != 11 || BuildByName(DefaultBuild).ID != 11 || BuildByName("nope").ID != 11 {
		t.Fatal("the default build is 11")
	}
	if _, ok := BuildByID(5); ok {
		t.Fatal("build 5 exists")
	}
	melee, _ := BuildByID(1)
	if s, i := melee.Stats(10); s != 20+18 || i != 20+9 {
		t.Fatalf("melee stats at 10: %d/%d", s, i)
	}
	caster, _ := BuildByID(15)
	if s, i := caster.Stats(10); s != 20+9 || i != 20+18 {
		t.Fatalf("caster stats at 10: %d/%d", s, i)
	}
}

/*
================
testSeeders
================
*/
func testSeeders() Seeders {
	return Seeders{
		Model: func(race Race, gender int64) (string, uint32, bool) {
			if race == RaceChinese {
				return "CHAR_CH_MAN_ADVENTURER", 1907, true
			}
			return "CHAR_EU_WOMAN_ADVENTURER", 14738, true
		},
		Skills: func(raceKey string, _ []uint32) ([]uint32, error) { return []uint32{1, 2}, nil },
		Inventory: func(c *domain.Character) {
			c.MissionInventory = []domain.InventoryRow{{Slot: 6, RefObjID: 9, Codename: "ITEM_EU_TSWORD_01_A_DEF"}}
		},
	}
}

/*
================
testRealCharacter

A record with something in every family a match must reset.
================
*/
func testRealCharacter() *domain.Character {
	level, gold, hp, str := int64(42), int64(123456), int64(1500), int64(120)
	packed := uint32(0x00020001)
	region, x, z := int64(0x62A8), 960.0, 458.0
	return &domain.Character{
		ID: 7, Name: "Gina", AccountID: "acc", ModelCodename: "CHAR_CH_WOMAN_FOX", RaceIndex: &[]int64{domain.RaceChina}[0],
		Level: &level, MaxLevel: &level, Gold: &gold, CurrentHP: &hp, Strength: &str, Intellect: &str,
		Masteries: []domain.CharacterMastery{{ID: 257, Level: 40}, {ID: 275, Level: 30}},
		Skills:    []uint32{2, 40, 70, 300},
		MissionInventory: []domain.InventoryRow{{Slot: 6, RefObjID: 4025, Codename: "ITEM_CH_SWORD_04_C_RARE", Plus: 7, VarianceBits: "123", Durability: 50, StackCount: 1},
			{Slot: 13, RefObjID: 4, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 50}},
		ActiveCOS:          &domain.CharacterCOS{GID: 99, Codename: "COS_P_WOLF_001", Summoned: true},
		PK:                 &domain.PKRecord{Penalty: 1200, TotalCount: 1},
		QuickSlots:         []domain.QuickSlotBinding{{Slot: 1}},
		TimedSkillJobs:     []domain.TimedSkillJob{{SkillID: 300, RemainingMs: 5000}},
		Friends:            []domain.FriendRecord{{Name: "test"}},
		World:              &domain.CharacterWorld{PackedInstance: &packed, Spawn: &domain.WorldSpawn{RegionID: &region, X: &x, Z: &z}, SpawnSet: true},
		Job:                domain.CharacterJob{Type: domain.JobThief, Grade: 2},
		NativeTeleportMode: 1, BattleUntilMs: 5, DeathKind: 3,
	}
}

/*
================
TestHoldAndRestoreRoundTrip

Hold makes a level-1 body of the build with nothing of the real record;
Restore returns the real record byte for byte, standing at home in the
default world, with the runtime-only fields cleared.
================
*/
func TestHoldAndRestoreRoundTrip(t *testing.T) {
	c := testRealCharacter()
	c.NativeTeleportMode, c.BattleUntilMs, c.DeathKind = 0, 0, 0
	original, _ := json.Marshal(c)
	build, _ := BuildByID(11)
	if err := Hold(c, build, 0x001d0003, 777, testSeeders()); err != nil {
		t.Fatal(err)
	}
	if !Held(c) || c.BattleRoyaleHeld.Build != 11 || c.BattleRoyaleHeld.World != 0x001d0003 || c.BattleRoyaleHeld.HeldAtMs != 777 {
		t.Fatalf("hold %+v", c.BattleRoyaleHeld)
	}
	if got, _ := json.Marshal(c.BattleRoyaleHeld.Real); string(got) != string(original) {
		t.Fatalf("held record differs:\n%s\n%s", got, original)
	}
	if c.ID != 7 || c.Name != "Gina" || c.AccountID != "acc" {
		t.Fatal("identity changed")
	}
	if *c.Level != 1 || *c.Gold != 0 || c.CurrentHP != nil || c.PK != nil || c.ActiveCOS != nil || c.QuickSlots != nil ||
		c.TimedSkillJobs != nil || c.Job != (domain.CharacterJob{}) || c.ModelCodename != "CHAR_EU_WOMAN_ADVENTURER" ||
		*c.RaceIndex != domain.RaceEurope || *c.Gender != domain.GenderFemale || *c.ModelRef != 14738 {
		t.Fatalf("match character inherited the real record: %+v", c)
	}
	if *c.Strength != domain.BaseStat || *c.Intellect != domain.BaseStat || len(c.Skills) != 2 || len(c.MissionInventory) != 1 {
		t.Fatalf("match character seeds: str %d int %d skills %v inv %d", *c.Strength, *c.Intellect, c.Skills, len(c.MissionInventory))
	}
	if level, ok := domain.MasteryLevel(c, 513); !ok || level != 1 {
		t.Fatalf("build mastery 513 level %d %v", level, ok)
	}
	if level, _ := domain.MasteryLevel(c, 514); level != 0 {
		t.Fatalf("other mastery 514 level %d", level)
	}
	if domain.CharacterWorldInstance(c) != 0x001d0003 || c.World.Spawn == nil || *c.World.Spawn.RegionID != 0x62A8 {
		t.Fatalf("match world %+v", c.World)
	}
	if err := Hold(c, build, 1, 1, testSeeders()); !errors.Is(err, ErrAlreadyHeld) {
		t.Fatalf("second hold: %v", err)
	}
	if build, ok := HeldBuild(c); !ok || build.ID != 11 {
		t.Fatal("held build")
	}
	// Something social happens during the match: it survives the restore.
	c.Friends = append(c.Friends, domain.FriendRecord{Name: "newfriend"})
	c.NativeTeleportMode = 2

	home := domain.WorldSpawn{RegionID: &[]int64{0x5A74}[0], X: &[]float64{1710}[0], Y: &[]float64{0}[0], Z: &[]float64{330}[0], Angle: &[]int64{0}[0]}
	hold, ok := Restore(c, home)
	if !ok || hold == nil || Held(c) {
		t.Fatal("restore refused")
	}
	if c.NativeTeleportMode != 0 || c.World.PackedInstance != nil || *c.World.Spawn.RegionID != 0x5A74 || !c.World.SpawnSet {
		t.Fatalf("restored stand %+v mode %d", c.World, c.NativeTeleportMode)
	}
	if len(c.Friends) != 2 {
		t.Fatalf("friends made during the match lost: %+v", c.Friends)
	}
	c.Friends = c.Friends[:1]
	restored := domain.CloneCharacter(c)
	restored.World = nil
	want := testRealCharacter()
	want.World, want.NativeTeleportMode, want.BattleUntilMs, want.DeathKind = nil, 0, 0, 0
	gotJSON, _ := json.Marshal(restored)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("restored record differs:\n%s\n%s", gotJSON, wantJSON)
	}
	if _, ok := Restore(c, home); ok {
		t.Fatal("a second restore succeeded")
	}
	// A hold without a record is cleared and reported.
	c.BattleRoyaleHeld = &domain.BattleRoyaleHold{Build: 1}
	if hold, ok := Restore(c, home); ok || hold == nil || Held(c) {
		t.Fatal("empty hold not cleared")
	}
}

/*
================
TestHoldRefusesWithoutAModel
================
*/
func TestHoldRefusesWithoutAModel(t *testing.T) {
	c := testRealCharacter()
	before, _ := json.Marshal(c)
	seed := testSeeders()
	seed.Model = func(Race, int64) (string, uint32, bool) { return "", 0, false }
	if err := Hold(c, DefaultPreset(), 1, 1, seed); !errors.Is(err, ErrNoModel) {
		t.Fatalf("hold without a model: %v", err)
	}
	if after, _ := json.Marshal(c); string(after) != string(before) {
		t.Fatal("a refused hold changed the record")
	}
}

/*
================
TestSetMatchLevel
================
*/
func TestSetMatchLevel(t *testing.T) {
	build, _ := BuildByID(3)
	c := &domain.Character{Masteries: domain.DefaultMasteries(domain.RaceKeyChina)}
	SetMatchLevel(c, build, 50)
	if level, _ := domain.MasteryLevel(c, 259); level != MatchLevelCap {
		t.Fatalf("bow mastery %d, want the cap", level)
	}
	if level, _ := domain.MasteryLevel(c, 273); level != MatchLevelCap {
		t.Fatalf("cold mastery %d", level)
	}
	if level, _ := domain.MasteryLevel(c, 257); level != 0 {
		t.Fatalf("sword mastery %d trained", level)
	}
	if *c.Strength != 20+2*29 || *c.Intellect != 20+29 {
		t.Fatalf("stats at the cap %d/%d", *c.Strength, *c.Intellect)
	}
}

/*
================
TestMonsterPlan

ArenaMonsters level-5 wolves whatever the player count, every point on
the ground cells (inside the arena and in a kept mask cell),
deterministic; an empty ground plans nothing.
================
*/
func TestMonsterPlan(t *testing.T) {
	mask := BuildGroundMask(nil)
	arena := ArenaCircle()
	ground := mask.Within(arena)
	a := MonsterPlan(ground, rand.New(rand.NewPCG(1, 2)))
	b := MonsterPlan(ground, rand.New(rand.NewPCG(1, 2)))
	if len(a) != ArenaMonsters || ArenaMonsters != 1500 || ArenaMonsters > MaxLiveMonsters || MaxLiveMonsters > 3000 {
		t.Fatalf("%d monsters", len(a))
	}
	for i, spawn := range a {
		if !mask.Contains(spawn.At) || !InRoc(spawn.At) || spawn.Codename != "MOB_EU_BARUSWOLF_CLON" || spawn.Rarity != 0 {
			t.Fatalf("spawn %d: %+v", i, spawn)
		}
		if spawn != b[i] {
			t.Fatalf("plan is not deterministic at %d", i)
		}
	}
	if len(MonsterPlan(GroundCells{}, rand.New(rand.NewPCG(1, 2)))) != 0 {
		t.Fatal("monsters without ground")
	}
}

/*
================
lootBase

A drop codename without its seal suffix, and its seal (-1 for a
non-seal row).
================
*/
func lootBase(codename string) (string, Seal) {
	switch {
	case strings.HasSuffix(codename, "_B_RARE"):
		return strings.TrimSuffix(codename, "_B_RARE"), SealMoon
	case strings.HasSuffix(codename, "_C_RARE"):
		return strings.TrimSuffix(codename, "_C_RARE"), SealSun
	}
	return codename, -1
}

/*
================
TestBuildArmorClasses

Every build rolls an armor class its masteries can wear.
================
*/
func TestBuildArmorClasses(t *testing.T) {
	want := map[uint8]ArmorClass{1: ArmorHeavy, 2: ArmorHeavy, 3: ArmorLight, 4: ArmorHeavy, 11: ArmorHeavy, 12: ArmorHeavy,
		13: ArmorLight, 14: ArmorLight, 15: ArmorClothes, 16: ArmorClothes}
	for _, build := range Builds() {
		if build.Armor != want[build.ID] {
			t.Fatalf("build %d armor %v", build.ID, build.Armor)
		}
	}
}
