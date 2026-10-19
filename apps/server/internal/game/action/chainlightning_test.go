/*
===========================================================================

chainlightning_test.go - the shard's Chain Lightning chain (port-only)

Every setting native and on: the count, radius, full-damage switch and hop
period parse; only the family's own shape-6 chain takes them; the
authoritative victim selection, damage and corpse retention follow them.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// chainLightningOwner is the owner's widened chain (2026-10-18); the
// falloff and hop settings are added where a test reads them.
var chainLightningOwner = ChainLightning{Targets: 50, RangeMeters: 10000}

const (
	// chainTestHP outlasts every victim's strike; chainTestFragileHP does not.
	chainTestHP        = 100000
	chainTestFragileHP = 1
	// chainTestRoll is the fixed combat roll: every victim's formula alike.
	chainTestRoll = 16000
	// chainTestHopMs is the owner's hop period.
	chainTestHopMs = 500
)

/*
================
chainLightningRow

A Chain Lightning rank as the shipped data authors every rank's chain:
shape 6, radius 100 (10 m), 3 targets, 25% falloff, hostile and objects.
================
*/
func chainLightningRow(codename string) enterworld.SkillRow {
	return enterworld.SkillRow{Codename: codename, OffensiveArea: enterworld.SkillOffensiveArea{
		Shape: 6, Radius: 100, MaxTargets: 3, ReductionPercent: 25, Select: 24}}
}

/*
================
TestChainLightningSettingsParse

Unset, "off" and "0" are native; the owner's values parse; a malformed or
out-of-range value is an error rather than a silent default.
================
*/
func TestChainLightningSettingsParse(t *testing.T) {
	for _, tc := range []struct{ targets, meters string }{{"", ""}, {"off", "OFF"}, {"0", " 0 "}} {
		got, err := ParseChainLightning(tc.targets, tc.meters)
		if err != nil || got != (ChainLightning{}) || got.Enabled() {
			t.Fatalf("%q/%q: %+v %v, want native", tc.targets, tc.meters, got, err)
		}
	}
	got, err := ParseChainLightning("50", "10000")
	if err != nil || got != chainLightningOwner || !got.Enabled() {
		t.Fatalf("owner values: %+v %v", got, err)
	}
	if got, err = ParseChainLightning("", "10000"); err != nil || got != (ChainLightning{RangeMeters: 10000}) || !got.Enabled() {
		t.Fatalf("range alone: %+v %v", got, err)
	}
	for _, tc := range []struct{ targets, meters string }{
		{"256", ""}, {"-1", ""}, {"many", ""}, {"", "100001"}, {"", "1.5"}, {"", "-10"},
	} {
		if _, err := ParseChainLightning(tc.targets, tc.meters); err == nil {
			t.Fatalf("%q/%q parsed", tc.targets, tc.meters)
		}
	}
}

/*
================
TestChainLightningAreaTouchesOnlyTheFamilysChain

The count replaces the target byte and the radius the efr word in world
units (ten a metre); each setting alone leaves the other native, and other
chains or a non-chain area of the family stay as authored.
================
*/
func TestChainLightningAreaTouchesOnlyTheFamilysChain(t *testing.T) {
	row := chainLightningRow("SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_08")
	native := row.OffensiveArea
	if got := (ChainLightning{}).area(row, native); got != native {
		t.Fatalf("native setting changed the area: %+v", got)
	}
	want := native
	want.MaxTargets, want.Radius = 50, 100000
	if got := chainLightningOwner.area(row, native); got != want {
		t.Fatalf("owner setting: %+v, want %+v", got, want)
	}
	if got := (ChainLightning{Targets: 50}).area(row, native); got.MaxTargets != 50 || got.Radius != 100 {
		t.Fatalf("targets alone: %+v", got)
	}
	if got := (ChainLightning{RangeMeters: 10000}).area(row, native); got.MaxTargets != 3 || got.Radius != 100000 {
		t.Fatalf("range alone: %+v", got)
	}
	other := chainLightningRow("SKILL_CH_LIGHTNING_STORM_A_01")
	if got := chainLightningOwner.area(other, native); got != native {
		t.Fatalf("another chain took the override: %+v", got)
	}
	sphere := native
	sphere.Shape = 1
	if got := chainLightningOwner.area(row, sphere); got != sphere {
		t.Fatalf("a non-chain area took the override: %+v", got)
	}
}

/*
================
chainTestPopulation

One monster of hp per offset from the fixture's primary, in nest order: dx
in world units along X, regions in whole regions along X.
================
*/
func chainTestPopulation(t *testing.T, hp uint32, offsets []chainTestOffset) (*Runtime, *enterworld.Character, []monster.Instance, int64) {
	t.Helper()
	rt, clock, c, primary := newCombatTestRuntime(t, hp)
	var nests []monster.NestRow
	var regions []uint16
	for _, offset := range offsets {
		spawn := primary.Spawn
		spawn.RegionID += offset.regions
		spawn.X += offset.dx
		nests = append(nests, monster.NestRow{SpawnPoint: spawn, RetailEvidence: true, MaxCount: 1})
		if !slices.Contains(regions, spawn.RegionID) {
			regions = append(regions, spawn.RegionID)
		}
	}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{primary.Ref.RefObjID: primary.Ref}, nests))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	targets := rt.Monsters.InstancesInRegions(testDivision, regions)
	if len(targets) != len(offsets) {
		t.Fatalf("population: %d monsters, want %d", len(targets), len(offsets))
	}
	return rt, c, targets, clock.NowMs()
}

/*
================
chainTestOffset
================
*/
type chainTestOffset struct {
	dx      float64
	regions uint16
}

/*
================
chainTestVictims

The gids areaVictims selects for skill's own area around primary.
================
*/
func chainTestVictims(rt *Runtime, c *enterworld.Character, skill enterworld.SkillRow, primary monster.Instance, now int64) []uint32 {
	target := combatTarget{gid: primary.Gid, monster: &primary, at: rt.monsterSpawn(testDivision, primary.Gid, now)}
	var out []uint32
	for _, victim := range rt.areaVictims(testDivision, c, skill, target, skill.OffensiveArea, 0, now) {
		out = append(out, victim.gid)
	}
	return out
}

/*
================
expectChainVictims

The victims are the first count monsters of targets, nearest first.
================
*/
func expectChainVictims(t *testing.T, label string, got []uint32, targets []monster.Instance, count int) {
	t.Helper()
	if len(got) != count {
		t.Fatalf("%s: %d victims, want %d", label, len(got), count)
	}
	for i := range count {
		if got[i] != targets[i].Gid {
			t.Fatalf("%s: victim %d is %d, want %d", label, i, got[i], targets[i].Gid)
		}
	}
}

/*
================
TestChainLightningChainsToFiftyTargets

Sixty monsters 12 units (1.2 m) apart in a row from the primary. Natively
the chain takes the primary and the two nearest; the count alone reaches
the nine within 10 m; the owner's settings take fifty, nearest first, and
another skill's chain stays native under them.
================
*/
func TestChainLightningChainsToFiftyTargets(t *testing.T) {
	var offsets []chainTestOffset
	for i := range 60 {
		offsets = append(offsets, chainTestOffset{dx: float64(12 * i)})
	}
	rt, c, targets, now := chainTestPopulation(t, chainTestHP, offsets)
	row := chainLightningRow("SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_01")
	for _, tc := range []struct {
		label   string
		setting ChainLightning
		skill   enterworld.SkillRow
		count   int
	}{
		{"native", ChainLightning{}, row, 3},
		{"range alone", ChainLightning{RangeMeters: 10000}, row, 3},
		{"targets alone", ChainLightning{Targets: 50}, row, 9},
		{"owner", chainLightningOwner, row, 50},
		{"another chain", chainLightningOwner, chainLightningRow("SKILL_CH_LIGHTNING_STORM_A_01"), 3},
	} {
		rt.ChainLightning = tc.setting
		expectChainVictims(t, tc.label, chainTestVictims(rt, c, tc.skill, targets[0], now), targets, tc.count)
	}
}

/*
================
TestChainLightningReachesAcrossRegions

A monster five regions (960 m) from the primary: outside the native 10 m,
inside the owner's 10000 m, and still selected after the nearer ones.
================
*/
func TestChainLightningReachesAcrossRegions(t *testing.T) {
	rt, c, targets, now := chainTestPopulation(t, chainTestHP, []chainTestOffset{{dx: 0}, {dx: 50}, {dx: 0, regions: 5}})
	row := chainLightningRow("SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_03")
	expectChainVictims(t, "native", chainTestVictims(rt, c, row, targets[0], now), targets, 2)
	rt.ChainLightning = chainLightningOwner
	expectChainVictims(t, "owner", chainTestVictims(rt, c, row, targets[0], now), targets, 3)
}

/*
================
TestShippedChainLightningRanksTakeTheOverride

Every shipped rank authors the 3-target, 10 m chain this override keys on,
and each takes the owner's 50 targets within 10000 m.
================
*/
func TestShippedChainLightningRanksTakeTheOverride(t *testing.T) {
	skills := shippedSkills(t)
	native := enterworld.SkillOffensiveArea{Shape: 6, Radius: 100, MaxTargets: 3, ReductionPercent: 25, Select: 24}
	for level := 1; level <= 8; level++ {
		codename := fmt.Sprintf("%s%02d", chainLightningCodenamePrefix, level)
		row, ok := skills.SkillByCodename(codename)
		if !ok {
			t.Fatalf("missing %s", codename)
		}
		if !row.DirectOffensePinned || row.OffensiveArea != native {
			t.Fatalf("%s: pinned %v, area %+v", codename, row.DirectOffensePinned, row.OffensiveArea)
		}
		if got := (ChainLightning{}).area(row, row.OffensiveArea); got != native {
			t.Fatalf("%s native: %+v", codename, got)
		}
		got := chainLightningOwner.area(row, row.OffensiveArea)
		if got.MaxTargets != 50 || got.Radius != 100000 {
			t.Fatalf("%s owner: %+v", codename, got)
		}
	}
	if _, ok := skills.SkillByCodename(chainLightningCodenamePrefix + "09"); ok {
		t.Fatal("a ninth rank exists; extend this test")
	}
}

/*
================
chainCast

One cast of the shipped rank-one row at the first of sixty monsters 1.2 m
apart in a row: the monsters before the strike, the cast result's payload
with its target-count offset and record size, and the release time.
================
*/
type chainCast struct {
	rt         *Runtime
	targets    []monster.Instance
	payload    []byte
	matrix     int
	record     int
	releasedMs int64
}

/*
================
castShippedChainLightning

The cast under setting at monsters of hp, with a fixed combat roll so every
victim's formula is the same before the chain's falloff.
================
*/
func castShippedChainLightning(t *testing.T, setting ChainLightning, hp uint32) chainCast {
	t.Helper()
	skill := shippedOffense(t, chainLightningCodenamePrefix+"01")
	var offsets []chainTestOffset
	for i := range 60 {
		offsets = append(offsets, chainTestOffset{dx: float64(12 * i)})
	}
	rt, c, targets, _ := chainTestPopulation(t, hp, offsets)
	rt.ChainLightning = setting
	rt.CombatRoll = func() (uint32, error) { return chainTestRoll, nil }
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	items := rt.deps.ItemReferences().(staticItemSource)
	staff := *items[c.MissionInventory[0].Codename]
	staff.Codename, staff.RefObjID, staff.TypeIDs[3] = "ITEM_EU_STAFF_01_A", 74, int64(skill.RequiredWeaponKinds[0])
	items[staff.Codename] = &staff
	c.MissionInventory[0].Codename, c.MissionInventory[0].RefObjID, c.MissionInventory[0].TypeFlags = staff.Codename, staff.RefObjID, staff.TypeFlags()
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpSkillCastResult || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("%+v: cast refused: %+v", setting, result)
	}
	out := chainCast{rt: rt, targets: targets, payload: result.Frames[0].Payload, matrix: 20,
		record: 4 + 9*int(skill.Attack.ImpactCount), releasedMs: rt.Now().UnixMilli()}
	if skill.ActionCastingTimeMs != 0 {
		out.releasedMs += int64(skill.ActionCastingTimeMs) + 1
		out.payload, out.matrix = releasePreparedSkillForTest(t, rt, out.releasedMs).Frames[0].Payload, 11
	}
	return out
}

/*
================
chainCast.losses

The HP each of the first count monsters lost to the cast.
================
*/
func (cast chainCast) losses(count int) []uint32 {
	out := make([]uint32, count)
	for i := range count {
		after, _ := cast.rt.Monsters.Get(testDivision, cast.targets[i].Gid)
		out[i] = cast.targets[i].CurrentHP - after.CurrentHP
	}
	return out
}

/*
================
TestShippedChainLightningCastStrikesFiftyVictims

The shipped rank-one row cast at the first of sixty monsters in a row:
natively the cast result carries 3 victims, under the owner's settings 50,
nearest first, every one of them damaged. The result's target count byte
and the frame grammar carry the larger chain unchanged.
================
*/
func TestShippedChainLightningCastStrikesFiftyVictims(t *testing.T) {
	for _, tc := range []struct {
		label   string
		setting ChainLightning
		count   int
	}{{"native", ChainLightning{}, 3}, {"owner", chainLightningOwner, 50}} {
		cast := castShippedChainLightning(t, tc.setting, chainTestHP)
		p, matrix, record := cast.payload, cast.matrix, cast.record
		if int(p[matrix]) != tc.count || len(p) != matrix+1+tc.count*record {
			t.Fatalf("%s: %d victims in a %d-byte result, want %d", tc.label, p[matrix], len(p), tc.count)
		}
		for i, loss := range cast.losses(tc.count) {
			if got := binary.LittleEndian.Uint32(p[matrix+1+i*record:]); got != cast.targets[i].Gid {
				t.Fatalf("%s: victim %d is %d, want %d", tc.label, i, got, cast.targets[i].Gid)
			}
			if loss == 0 {
				t.Fatalf("%s: victim %d took no damage", tc.label, i)
			}
		}
		if loss := cast.losses(tc.count + 1)[tc.count]; loss != 0 {
			t.Fatalf("%s: monster %d beyond the chain was struck", tc.label, tc.count)
		}
	}
}

/*
================
TestChainLightningFullDamageAndHopParse

Unset, "off" and "0" are native for both; "on" and the owner's 500 parse;
anything else is an error. ChainLightningFromEnv reads all four.
================
*/
func TestChainLightningFullDamageAndHopParse(t *testing.T) {
	for _, text := range []string{"", "off", "0", " OFF "} {
		if full, err := ParseChainLightningFullDamage(text); full || err != nil {
			t.Fatalf("full damage %q: %v %v, want native", text, full, err)
		}
		if hop, err := ParseChainLightningHop(text); hop != 0 || err != nil {
			t.Fatalf("hop %q: %d %v, want native", text, hop, err)
		}
	}
	if full, err := ParseChainLightningFullDamage("false"); full || err != nil {
		t.Fatalf("full damage false: %v %v, want native", full, err)
	}
	for _, text := range []string{"on", "1", "TRUE"} {
		if full, err := ParseChainLightningFullDamage(text); !full || err != nil {
			t.Fatalf("full damage %q: %v %v", text, full, err)
		}
	}
	if hop, err := ParseChainLightningHop("500"); hop != chainTestHopMs || err != nil {
		t.Fatalf("hop 500: %d %v", hop, err)
	}
	if _, err := ParseChainLightningFullDamage("yes please"); err == nil {
		t.Fatal("a malformed full-damage switch parsed")
	}
	for _, text := range []string{"2001", "-1", "fast", "0.5"} {
		if _, err := ParseChainLightningHop(text); err == nil {
			t.Fatalf("hop %q parsed", text)
		}
	}
	t.Setenv(EnvChainLightningTargets, "50")
	t.Setenv(EnvChainLightningRange, "10000")
	t.Setenv(EnvChainLightningFullDamage, "on")
	t.Setenv(EnvChainLightningHop, "500")
	want := ChainLightning{Targets: 50, RangeMeters: 10000, FullDamage: true, HopMs: chainTestHopMs}
	if got, err := ChainLightningFromEnv(); got != want || err != nil {
		t.Fatalf("owner env: %+v %v, want %+v", got, err, want)
	}
	t.Setenv(EnvChainLightningHop, "soon")
	if _, err := ChainLightningFromEnv(); err == nil {
		t.Fatal("a malformed hop in the environment parsed")
	}
}

/*
================
TestChainLightningFalloffAndHopTouchOnlyTheFamilysChain

Full damage zeroes the reduction byte and the hop names its period only
for the family's own shape-6 chain; natively both leave it as authored.
================
*/
func TestChainLightningFalloffAndHopTouchOnlyTheFamilysChain(t *testing.T) {
	row := chainLightningRow("SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_05")
	native := row.OffensiveArea
	setting := ChainLightning{FullDamage: true, HopMs: chainTestHopMs}
	if got := (ChainLightning{}).area(row, native); got.ReductionPercent != 25 {
		t.Fatalf("native falloff changed: %+v", got)
	}
	want := native
	want.ReductionPercent = 0
	if got := setting.area(row, native); got != want {
		t.Fatalf("full damage: %+v, want %+v", got, want)
	}
	if got := setting.area(row, setting.area(row, native)); got != want {
		t.Fatalf("applied twice: %+v, want %+v", got, want)
	}
	if got := (ChainLightning{}).hopMs(row, native); got != 0 {
		t.Fatalf("native hop %d", got)
	}
	if got := setting.hopMs(row, native); got != chainTestHopMs {
		t.Fatalf("owner hop %d", got)
	}
	other := chainLightningRow("SKILL_CH_LIGHTNING_STORM_A_01")
	sphere := native
	sphere.Shape = 1
	for _, tc := range []struct {
		label string
		skill enterworld.SkillRow
		area  enterworld.SkillOffensiveArea
	}{{"another chain", other, native}, {"a non-chain area", row, sphere}} {
		if got := setting.area(tc.skill, tc.area); got != tc.area {
			t.Fatalf("%s took the falloff: %+v", tc.label, got)
		}
		if got := setting.hopMs(tc.skill, tc.area); got != 0 {
			t.Fatalf("%s took the hop: %d", tc.label, got)
		}
	}
}

/*
================
TestShippedChainLightningSkillIDs

The ids the bootstrap publishes are the eight shipped ranks, in rank
order, each one the row the override keys on.
================
*/
func TestShippedChainLightningSkillIDs(t *testing.T) {
	skills := shippedSkills(t)
	ids := ChainLightningSkillIDs(skills)
	if len(ids) != 8 {
		t.Fatalf("%d ids, want 8: %v", len(ids), ids)
	}
	for i, id := range ids {
		row, ok := skills.SkillByCodename(fmt.Sprintf("%s%02d", chainLightningCodenamePrefix, i+1))
		if !ok || row.ID != id || row.OffensiveArea.Shape != chainAreaShape {
			t.Fatalf("rank %d: id %d, row %d %v", i+1, id, row.ID, ok)
		}
	}
}

/*
================
TestShippedChainLightningFullDamage

With one fixed roll every victim's formula is the same, so the chain's
falloff alone tells them apart. Natively it compounds by 25% a victim
until the running percent truncates to 0 at the fifteenth, which
FinishImpact reads as full again; with full damage on, all fifty victims
lose exactly what the first does.
================
*/
func TestShippedChainLightningFullDamage(t *testing.T) {
	native := castShippedChainLightning(t, chainLightningOwner, chainTestHP).losses(50)
	for i := 1; i < 14; i++ {
		if native[i] >= native[i-1] {
			t.Fatalf("native: victim %d lost %d, not less than victim %d's %d", i, native[i], i-1, native[i-1])
		}
	}
	if want := native[0] * 75 / 100; native[1] != want {
		t.Fatalf("native: victim 1 lost %d, want 75%% of %d = %d", native[1], native[0], want)
	}
	for i := 14; i < 50; i++ {
		if native[i] != native[0] {
			t.Fatalf("native: victim %d lost %d, want the full %d past the truncation", i, native[i], native[0])
		}
	}
	full := chainLightningOwner
	full.FullDamage = true
	for i, loss := range castShippedChainLightning(t, full, chainTestHP).losses(50) {
		if loss != native[0] {
			t.Fatalf("full damage: victim %d lost %d, want %d", i, loss, native[0])
		}
	}
}

/*
================
TestShippedChainLightningHopHoldsEachCorpse

Fifty one-HP victims all die in the cast's commit. Natively each corpse
leaves 2 s after the release; under the hop the k-th leaves k periods
later still, so the client's bolt reaches every corpse before it goes.
================
*/
func TestShippedChainLightningHopHoldsEachCorpse(t *testing.T) {
	for _, tc := range []struct {
		label string
		hopMs int64
	}{{"native", 0}, {"hop", chainTestHopMs}} {
		setting := chainLightningOwner
		setting.HopMs = uint32(tc.hopMs)
		cast := castShippedChainLightning(t, setting, chainTestFragileHP)
		due := map[uint32]int64{}
		cast.rt.pendingMonsterDefeatsMu.Lock()
		for _, pending := range cast.rt.pendingMonsterDefeats {
			due[pending.gid] = pending.dueAtMs
		}
		cast.rt.pendingMonsterDefeatsMu.Unlock()
		for k := range 50 {
			want := cast.releasedMs + monsterDeathPresentationRetention.Milliseconds() + int64(k)*tc.hopMs
			if got, ok := due[cast.targets[k].Gid]; !ok || got != want {
				t.Fatalf("%s: victim %d leaves at %d (%v), want %d", tc.label, k, got, ok, want)
			}
		}
	}
}
