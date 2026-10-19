/*
===========================================================================

portonlyskills_test.go - tests for portonlyskills.go

===========================================================================
*/
package enterworld

import (
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
portOnlyTestRow

A 118-column self buff of the later-client shape: 800 ms cast, a 10 s
dura with a 20 % damage rate (dru) and 10 % movement (hst3).
================
*/
func portOnlyTestRow(id, codename string) []string {
	row := make([]string, 118)
	for i := range row {
		row[i] = "0"
	}
	for column, value := range map[int]string{0: "1", 1: id, 2: "65000", 3: codename, 4: codename, 5: codename,
		7: "1", 8: "2", 11: "166", 12: "1", 13: "800", 14: "60000", 18: "1795162206", 34: "1", 47: "3",
		50: "255", 51: "255", 57: "255", 58: "255", 59: "255", 61: "skill\\etc\\x.ddj", 62: "SN_X", 63: "xxx",
		64: "SN_X_TT_DESC", 65: "xxx", 68: "3", 69: "1685418593", 70: "10000", 71: "6582901", 72: "20", 73: "20",
		74: "1752396851", 75: "10"} {
		row[column] = value
	}
	return row
}

/*
================
TestAddPortOnlySkillRows

Port-only: a later client's row is parsed with the shipped shards, marked
PortOnly, and its self buff of damage rate and movement is admitted as a
timed effect, a shape no native row may take. An id or codename already
used, a short row, or a second add of the same row refuses.
================
*/
func TestAddPortOnlySkillRows(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	skills := NewTextdataSkills(dir)
	if _, err := skills.AddPortOnlySkillRows([][]string{{"1", "2"}}); err == nil {
		t.Fatal("a short row was admitted")
	}
	used, ok := skills.SkillByID(1)
	if ok {
		if _, err := skills.AddPortOnlySkillRows([][]string{portOnlyTestRow("1", "SKILL_PORT_TEST")}); err == nil ||
			!strings.Contains(err.Error(), "already used") {
			t.Fatalf("id %d (%s) reused: %v", used.ID, used.Codename, err)
		}
	}
	added, err := skills.AddPortOnlySkillRows([][]string{portOnlyTestRow("4000000", "SKILL_PORT_TEST")})
	if err != nil || added != 1 {
		t.Fatalf("added %d, %v", added, err)
	}
	row, ok := skills.SkillByCodename("SKILL_PORT_TEST")
	if !ok || !row.PortOnly || !row.TimedEffect.Pinned || !row.TimedEffect.Attributes.DamageRate || !row.MovementModifier.Supported {
		t.Fatalf("port-only row %+v", row.TimedEffect)
	}
	if native, ok := skills.SkillByCodename(used.Codename); ok && native.PortOnly {
		t.Fatal("a shipped row is port-only")
	}
	if _, err := skills.AddPortOnlySkillRows([][]string{portOnlyTestRow("4000000", "SKILL_PORT_TEST")}); err == nil {
		t.Fatal("a second add was admitted")
	}
}
