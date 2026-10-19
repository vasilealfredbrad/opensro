/*
===========================================================================

stage6_licensed_test.go - the stage-6 loot rows against the shipped itemdata

Port-only, not native. RequiredLevel must be the itemdata requirement of
every Moon/Sun equipment row of degrees 1-4 (the level quad, or the
mastery quad of the European armor, which the match trains to the
character level), and every row a build can drop must exist. Skips
without the licensed data.

===========================================================================
*/
package battleroyale

import (
	"regexp"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// maxItemRefID bounds the itemdata id walk (the shipped rows end near
// 40 000).
const maxItemRefID = 100_000

/*
================
TestRequiredLevelMatchesItemdata
================
*/
func TestRequiredLevelMatchesItemdata(t *testing.T) {
	items := enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	sealRow := regexp.MustCompile(`^ITEM_(CH|EU)_.*_0[1-4](_[A-Z]{2})?_[BC]_RARE$`)
	checked := 0
	for id := uint32(1); id < maxItemRefID; id++ {
		ref, ok := items.ItemRefByID(id)
		if !ok || ref == nil || ref.TypeIDs[1] != 1 || !sealRow.MatchString(ref.Codename) {
			continue
		}
		want := int64(-1)
		for slot, kind := range ref.ReqQuadTypes {
			if kind == 1 || kind > 10 {
				want = max(want, ref.ReqQuadValues[slot])
			}
		}
		got, known := RequiredLevel(ref.Codename)
		if !known || got != want {
			t.Fatalf("%s: RequiredLevel %d (%v), itemdata %d", ref.Codename, got, known, want)
		}
		checked++
	}
	if checked < 800 {
		t.Fatalf("only %d Moon/Sun rows checked", checked)
	}
	for _, build := range Builds() {
		for _, female := range []bool{false, true} {
			for _, row := range equipmentRows(Killer{Build: build, Female: female}) {
				for _, seal := range []Seal{SealMoon, SealSun} {
					if _, ok := items.ItemRefByCodename(row.base + "_" + seal.suffix()); !ok {
						t.Fatalf("build %d drops %s_%s, missing from itemdata", build.ID, row.base, seal.suffix())
					}
				}
			}
		}
	}
	for level := int64(1); level <= MatchLevelCap; level++ {
		tier := PotionTier(level)
		for _, drop := range append(potionDrops(tier, 1), vigorDrop(tier, 1), pillDrop(level, 1)) {
			if _, ok := items.ItemRefByCodename(drop.Codename); !ok {
				t.Fatalf("level %d consumable %s missing", level, drop.Codename)
			}
		}
	}
	t.Logf("%d Moon/Sun rows match RequiredLevel", checked)
}
