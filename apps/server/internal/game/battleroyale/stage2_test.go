/*
===========================================================================

stage2_test.go - the stage-2 rules of the core

Port-only, not native. The remembered build pick, the walkable zone
filter, the places, the shrink-speed bound of the arena and the starter
kit (docs/battle-royale.md, "Stage 2", as stage 4 changed the places,
the pacing and the seals; the supply and loot plans are stage 6's,
stage6_test.go).

===========================================================================
*/
package battleroyale

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

/*
================
TestPickSurvivesEveryLobbyPath

A pick made in the lobby, during the countdown, re-picked, or made in the
last millisecond before the start is the build the match starts with; a
member scanned out of the circle and back keeps its pick; a fresh lobby
forgets it.
================
*/
func TestPickSurvivesEveryLobbyPath(t *testing.T) {
	cfg := testConfig()
	m := mustInit(t, cfg)
	mustEnter(t, m, "a")
	mustNoErr(t, m.Choose("a", RaceChinese, "3"))
	// Scanned out for a tick (or walked out) and back in: the pick holds.
	mustNoErr(t, m.LeaveLobby("a"))
	if _, _, member := m.LobbyBuild("a"); member {
		t.Fatal("still a member after leaving")
	}
	mustEnter(t, m, "a")
	if race, build, member := m.LobbyBuild("a"); !member || race != RaceChinese || build != "3" {
		t.Fatalf("pick lost across a leave: %v %q %v", race, build, member)
	}
	// A pick before the countdown; b picks during the countdown and
	// re-picks; a re-picks in the last millisecond.
	mustEnter(t, m, "b")
	m.Tick(0)
	if m.State() != StateCountdown {
		t.Fatalf("state %v", m.State())
	}
	mustNoErr(t, m.Choose("b", RaceEuropean, "15"))
	mustNoErr(t, m.Choose("b", RaceEuropean, "13"))
	m.Tick(testStartMs - 1)
	mustNoErr(t, m.Choose("a", RaceChinese, "2"))
	started := eventsOf[MatchStarted](m.Tick(testStartMs))
	if len(started) != 1 {
		t.Fatalf("no start: state %v", m.State())
	}
	got := map[PlayerID]string{}
	for _, p := range started[0].Participants {
		got[p.ID] = p.Build
	}
	if got["a"] != "2" || got["b"] != "13" {
		t.Fatalf("builds at the start %v", got)
	}
	if err := m.Choose("a", RaceChinese, "1"); !errors.Is(err, ErrWrongState) {
		t.Fatalf("pick after the start: %v", err)
	}
	// A fresh lobby forgets the choices.
	m = mustInit(t, cfg)
	mustEnter(t, m, "a")
	if _, build, _ := m.LobbyBuild("a"); build != DefaultBuild {
		t.Fatalf("a fresh lobby remembered %q", build)
	}
}

/*
================
TestPlanZoneFilteredRedrawsRefusedTargets

A filter that refuses the western half of the arena gets every target
centre in the eastern half; a filter that refuses everything still plans.
================
*/
func TestPlanZoneFilteredRedrawsRefusedTargets(t *testing.T) {
	arena := Circle{Center: Point{X: 10000, Z: 10000}, Radius: 5000}
	schedule := ScheduleFor(4, arena.Radius)
	east := func(p Point) bool { return p.X >= arena.Center.X }
	zone := PlanZoneFiltered(schedule, arena, 0, rand.New(rand.NewPCG(9, zoneStream)), east)
	previous := arena
	for i, target := range zone.Targets {
		if !east(target.Center) {
			t.Fatalf("target %d west of the centre: %+v", i, target)
		}
		if Outside(previous, Point{X: target.Center.X + target.Radius, Z: target.Center.Z}) {
			t.Fatalf("target %d escaped its predecessor", i)
		}
		previous = target
	}
	cfg := testConfig()
	eastOfArena := func(p Point) bool { return p.X >= cfg.Arena.Center.X }
	cfg.WalkableTarget = eastOfArena
	m, _ := startMatch(t, cfg, "a", "b")
	planned, _ := m.Zone()
	for i, target := range planned.Targets {
		if !eastOfArena(target.Center) {
			t.Fatalf("match target %d ignored the filter: %+v", i, target)
		}
	}
	never := PlanZoneFiltered(schedule, arena, 0, rand.New(rand.NewPCG(9, zoneStream)), func(Point) bool { return false })
	if len(never.Targets) != len(schedule.Phases) || never.Targets[0].Radius != schedule.Phases[0].EndRadius {
		t.Fatalf("an all-refusing filter broke the plan: %+v", never.Targets)
	}
}

/*
================
TestPlaces

The lobby stands in Hotan at 15 m; the arena (stage 4) is all of Roc:
centred on the Roc square, every corner of it inside the circle, the
same circle for every player count, and InRoc admits exactly the Roc
regions.
================
*/
func TestPlaces(t *testing.T) {
	lobby := LobbyCircle()
	if lobby.Radius != 15*UnitsPerMeter || LobbyRegion != 0x5C87 {
		t.Fatalf("lobby %+v", lobby)
	}
	arena := ArenaCircle()
	minX, minZ, maxX, maxZ := RocBounds()
	if minX != 199680 || minZ != 161280 || maxX != 228480 || maxZ != 190080 {
		t.Fatalf("Roc bounds %v %v %v %v", minX, minZ, maxX, maxZ)
	}
	if arena.Center != (Point{X: 214080, Z: 175680}) || ArenaRegion != 0x5B6F {
		t.Fatalf("arena centre %+v region %04x", arena.Center, ArenaRegion)
	}
	for _, corner := range []Point{{X: minX, Z: minZ}, {X: minX, Z: maxZ}, {X: maxX, Z: minZ}, {X: maxX, Z: maxZ}} {
		if Outside(arena, corner) {
			t.Fatalf("Roc corner %+v outside the arena %+v", corner, arena)
		}
	}
	if arena.Radius != 2040*UnitsPerMeter || arena.Radius > math.Hypot(maxX-minX, maxZ-minZ)/2+100 {
		t.Fatalf("arena radius %v", arena.Radius)
	}
	for _, tc := range []struct {
		sectorX, sectorY int
		in               bool
	}{{104, 85, true}, {118, 98, true}, {111, 91, true}, {113, 84, true}, {117, 84, true}, {112, 84, false},
		{118, 84, false}, {103, 90, false}, {119, 90, false}, {110, 99, false}} {
		p := Point{X: (float64(tc.sectorX) + 0.5) * 1920, Z: (float64(tc.sectorY) + 0.5) * 1920}
		if InRoc(p) != tc.in {
			t.Fatalf("sector %d/%d in Roc %v", tc.sectorX, tc.sectorY, !tc.in)
		}
	}
}

/*
================
TestScheduleShrinkSpeedBound

On the all-Roc arena, for every player count, every shrink lasts at
least three minutes, no phase closes its radius faster than
MaxShrinkSpeed (2 m/s) nor moves its worst-case edge faster than 80 % of
the run speed, and the schedule validates.
================
*/
func TestScheduleShrinkSpeedBound(t *testing.T) {
	radius := ArenaCircle().Radius
	for players := 1; players <= 16; players++ {
		schedule := ScheduleFor(players, radius)
		if err := ValidateSchedule(schedule, radius); err != nil {
			t.Fatalf("%d players: %v", players, err)
		}
		previous := radius
		for i, phase := range schedule.Phases {
			seconds := float64(phase.ShrinkMs) / msPerSecond
			if phase.ShrinkMs < 180_000 {
				t.Fatalf("%d players phase %d shrinks in %d ms", players, i, phase.ShrinkMs)
			}
			if speed := (previous - phase.EndRadius) / seconds; speed > MaxShrinkSpeed+1e-9 {
				t.Fatalf("%d players phase %d shrinks at %.2f units/s", players, i, speed)
			}
			if edge := 2 * (previous - phase.EndRadius) / seconds; edge > 0.8*playerRunSpeed+1e-9 {
				t.Fatalf("%d players phase %d edge at %.2f units/s", players, i, edge)
			}
			previous = phase.EndRadius
		}
	}
	if MaxShrinkSpeed != 2*UnitsPerMeter || playerRunSpeed != 5*UnitsPerMeter {
		t.Fatalf("MaxShrinkSpeed %v run %v", MaxShrinkSpeed, playerRunSpeed)
	}
}

/*
================
TestStarterKitAndWeaponDegree

Every build starts with its degree-1 Sun weapon in socket 6, the shield
builds a shield and the bow builds ammunition in socket 7, and twenty HP
and MP potions; WeaponRank orders the build's Moon and Sun weapons by
degree first, then Sun over Moon.
================
*/
func TestStarterKitAndWeaponDegree(t *testing.T) {
	want := map[uint8][]string{
		1: {"ITEM_CH_BLADE_01_C_RARE", "ITEM_CH_SHIELD_01_C_RARE"}, 2: {"ITEM_CH_TBLADE_01_C_RARE"},
		3: {"ITEM_CH_BOW_01_C_RARE", "ITEM_ETC_AMMO_ARROW_01_DEF"}, 4: {"ITEM_CH_BLADE_01_C_RARE"},
		11: {"ITEM_EU_TSWORD_01_C_RARE"}, 12: {"ITEM_EU_SWORD_01_C_RARE", "ITEM_EU_SHIELD_01_C_RARE"},
		13: {"ITEM_EU_CROSSBOW_01_C_RARE", "ITEM_ETC_AMMO_BOLT_01_DEF"}, 14: {"ITEM_EU_DAGGER_01_C_RARE"},
		15: {"ITEM_EU_TSTAFF_01_C_RARE"}, 16: {"ITEM_EU_DARKSTAFF_01_C_RARE"},
	}
	for _, build := range Builds() {
		kit := build.StarterKit()
		gear := want[build.ID]
		if len(kit) != len(gear)+2 || kit[0].Codename != gear[0] || kit[0].Slot != KitWeaponSlot || kit[0].Count != 1 {
			t.Fatalf("build %d kit %+v", build.ID, kit)
		}
		if len(gear) == 2 && (kit[1].Codename != gear[1] || kit[1].Slot != KitShieldSlot) {
			t.Fatalf("build %d off-hand %+v", build.ID, kit[1])
		}
		potions := kit[len(kit)-2:]
		if potions[0].Codename != "ITEM_ETC_HP_POTION_01" || potions[1].Codename != "ITEM_ETC_MP_POTION_01" ||
			potions[0].Count != kitPotions || potions[0].Slot != KitBagSlot {
			t.Fatalf("build %d potions %+v", build.ID, potions)
		}
		if WeaponRank(build, kit[0].Codename) != 3 {
			t.Fatalf("build %d starter degree", build.ID)
		}
	}
	blade, _ := BuildByID(1)
	if WeaponRank(blade, "ITEM_CH_SWORD_03_C_RARE") != 7 || WeaponRank(blade, "ITEM_CH_SWORD_03_B_RARE") != 6 ||
		WeaponRank(blade, "ITEM_CH_SPEAR_03_C_RARE") != 0 || WeaponRank(blade, "ITEM_CH_SHIELD_03_C_RARE") != 0 ||
		WeaponRank(blade, "ITEM_CH_SWORD_03_A_RARE") != 0 {
		t.Fatal("weapon rank reading")
	}
	// A higher degree wins over the seal: Moon 2 beats Sun 1.
	if WeaponRank(blade, "ITEM_CH_BLADE_02_B_RARE") <= WeaponRank(blade, "ITEM_CH_BLADE_01_C_RARE") {
		t.Fatal("Moon degree 2 does not beat Sun degree 1")
	}
}

/*
================
TestMatchKillExpPace

On the shipped leveldata curve (column 1, levels 1-29) MatchKillExp
takes a character from level 1 to the cap in 106 credited kills (about
8.8 minutes at one kill every five seconds), no kill crosses two levels,
and nothing is paid for an unknown requirement.
================
*/
func TestMatchKillExpPace(t *testing.T) {
	curve := []int64{118, 470, 1058, 1880, 2938, 5640, 9048, 13160, 17978, 23500, 34898, 47940, 62628, 78960, 96938,
		127840, 161798, 198810, 238878, 282000, 351231, 427755, 512196, 605232, 707587, 820046, 943453, 1078717, 1226815}
	level, exp, kills := int64(1), int64(0), 0
	for level < MatchLevelCap {
		required := curve[level-1]
		exp += MatchKillExp(required)
		kills++
		if exp >= required {
			exp -= required
			level++
			if level < MatchLevelCap && exp >= curve[level-1] {
				t.Fatalf("a kill crossed two levels at %d", level)
			}
		}
	}
	if kills != 106 {
		t.Fatalf("%d kills to the cap, want 106", kills)
	}
	if MatchKillExp(118) != 34 || MatchKillExp(0) != 0 || MatchKillExp(-5) != 0 {
		t.Fatal("kill share")
	}
}
