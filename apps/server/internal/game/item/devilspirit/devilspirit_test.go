/*
===========================================================================

devilspirit_test.go - tests for devilspirit.go

===========================================================================
*/
package devilspirit

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// attachmentTypeFlags packs TID 3.1.13.3, the v1.150 avatar attachment.
	attachmentTypeFlags = 0x1eac
	// genderToolTypeFlags packs TID 3.3.13.8, the template of the scroll
	// and the extension.
	genderToolTypeFlags = 0x46ec
)

/*
================
TestEmbeddedRowsAreValid

The embedded table names eight rows, one per sex and grade, each with the
later client's id above the v1.150 range.
================
*/
func TestEmbeddedRowsAreValid(t *testing.T) {
	items := Items()
	if len(items) != 8 {
		t.Fatalf("%d rows, want 8", len(items))
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Sex+item.Grade] = true
		if item.ID < firstPortOnlyID || !strings.Contains(item.Codename, "_NASRUN") {
			t.Fatalf("row %+v", item)
		}
	}
	if len(seen) != 8 {
		t.Fatalf("sex and grade pairs %v", seen)
	}
}

/*
================
TestParseRefusesBadTables
================
*/
func TestParseRefusesBadTables(t *testing.T) {
	for name, raw := range map[string]string{
		"format":   `{"format":"x","portType":[3,1,13,3],"items":[]}`,
		"type":     `{"format":"opensro-devil-spirit-v1","portType":[3,1,14,1],"items":[]}`,
		"empty":    `{"format":"opensro-devil-spirit-v1","portType":[3,1,13,3],"items":[]}`,
		"retailId": `{"format":"opensro-devil-spirit-v1","portType":[3,1,13,3],"templates":{"M":"T"},"items":[{"id":24154,"codename":"A","sex":"M","name":"n","icon":"i","models":["m"]}]}`,
		"template": `{"format":"opensro-devil-spirit-v1","portType":[3,1,13,3],"templates":{},"items":[{"id":24385,"codename":"A","sex":"M","name":"n","icon":"i","models":["m"]}]}`,
		"model":    `{"format":"opensro-devil-spirit-v1","portType":[3,1,13,3],"templates":{"M":"T"},"items":[{"id":24385,"codename":"A","sex":"M","name":"n","icon":"i","models":[]}]}`,
	} {
		if _, err := parse([]byte(raw)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

/*
================
TestFromEnv
================
*/
func TestFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"on": true, "1": true, "TRUE": true, "": false, "off": false, "0": false} {
		t.Setenv(EnvDevilSpirit, value)
		if FromEnv() != want {
			t.Fatalf("%q: %v", value, !want)
		}
	}
}

/*
================
TestAirdropReward

A roll that misses gives nothing; a hit gives the wearer's sex, the S
grade on its own share of the second roll.
================
*/
func TestAirdropReward(t *testing.T) {
	rolls := func(values ...uint32) func() (uint32, error) {
		return func() (uint32, error) {
			n := values[0]
			values = values[1:]
			return n, nil
		}
	}
	if _, ok, err := AirdropReward(true, rolls(1)); ok || err != nil {
		t.Fatal("a missed roll gave a Devil's Spirit")
	}
	for _, c := range []struct {
		male bool
		pick uint32
		want string
	}{
		{true, 1, "ITEM_MALL_AVATAR_M_NASRUN_YELLOW"},
		{false, 2, "ITEM_MALL_AVATAR_W_NASRUN_BLUE"},
		{false, 3, "ITEM_MALL_AVATAR_W_NASRUN"},
		{true, airdropUniqueChance, "ITEM_MALL_AVATAR_M_NASRUN_UNIQUE"},
	} {
		codename, ok, err := AirdropReward(c.male, rolls(airdropChance, c.pick))
		if !ok || err != nil || codename != c.want {
			t.Fatalf("pick %d: %q %v %v, want %s", c.pick, codename, ok, err, c.want)
		}
	}
}

/*
================
codenameItems

Item rows by codename, a stand-in ItemSource.
================
*/
type codenameItems map[string]*enterworld.ItemRef

/*
================
ItemRefByCodename
================
*/
func (m codenameItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := m[codename]
	return ref, ok
}

/*
================
TestLoaded

Every row present with its id and options is loaded; a missing row, a
row without its options (a template, not an Apply) or no source is not.
================
*/
func TestLoaded(t *testing.T) {
	options := Options(Items()[0].ID, 0, 0)
	items := codenameItems{}
	for _, item := range Items() {
		items[item.Codename] = &enterworld.ItemRef{RefObjID: item.ID, Codename: item.Codename, PortOnlyMagicOptions: options}
	}
	if !Loaded(items) {
		t.Fatal("every row present, not loaded")
	}
	last := Items()[len(Items())-1].Codename
	items[last] = &enterworld.ItemRef{RefObjID: Items()[len(Items())-1].ID, Codename: last}
	if Loaded(items) {
		t.Fatal("a row without its options counts as loaded")
	}
	delete(items, last)
	if Loaded(items) || Loaded(nil) || Loaded(codenameItems{}) {
		t.Fatal("missing rows count as loaded")
	}
}

/*
================
TestApplyAddsAttachmentRowsOnShippedData

Port-only, not native: applied over the shipped textdata, every row is a
v1.150 avatar attachment of its sex with the later client's id, name and
icon, made with the HP and MP avatar options, and listed for the GM
console. Unapplied (the flag off) no row exists. A second apply refuses.
================
*/
func TestApplyAddsAttachmentRowsOnShippedData(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	native := enterworld.NewTextdataItems(dir)
	for _, item := range Items() {
		if _, ok := native.ItemRefByID(item.ID); ok {
			t.Fatalf("v1.150 data already holds id %d", item.ID)
		}
	}
	items, options, skills := enterworld.NewTextdataItems(dir), enterworld.NewTextdataMagicOptions(dir), enterworld.NewTextdataSkills(dir)
	added, err := Apply(items, options, skills)
	if err != nil || added != len(Items())+len(EtcItems()) {
		t.Fatalf("added %d, %v", added, err)
	}
	if Loaded(native) || !Loaded(items) {
		t.Fatalf("loaded: native %v, applied %v", Loaded(native), Loaded(items))
	}
	for _, row := range MagicOptionRows() {
		got, ok := options.MagicOptionByParamID(row.ParamID)
		if !ok || got.OptionName != row.OptionName || got.Tag != row.Tag {
			t.Fatalf("option %d: %+v", row.ParamID, got)
		}
	}
	commands := map[uint32]enterworld.ItemCommandReference{}
	for _, row := range items.ItemCommandReferences() {
		commands[row.RefObjID] = row
	}
	for _, item := range Items() {
		ref, ok := items.ItemRefByID(item.ID)
		if !ok || ref.Codename != item.Codename || ref.Name != item.Name || ref.Icon != item.Icon {
			t.Fatalf("%s: %+v", item.Codename, ref)
		}
		wantSex := map[string]int64{"M": 1, "W": 0}[item.Sex]
		if ref.TypeFlags() != attachmentTypeFlags || ref.RequiredSex != wantSex {
			t.Fatalf("%s: type %#x sex %d", item.Codename, ref.TypeFlags(), ref.RequiredSex)
		}
		if !equalOptions(ref.PortOnlyMagicOptions, Options(item.ID, 0, 0)) {
			t.Fatalf("%s options %x", item.Codename, ref.PortOnlyMagicOptions)
		}
		if commands[item.ID].Codename != item.Codename {
			t.Fatalf("%s is not in the GM catalogue", item.Codename)
		}
	}
	for _, etc := range EtcItems() {
		ref, ok := items.ItemRefByID(etc.ID)
		if !ok || ref.Codename != etc.Codename || ref.TypeFlags() != genderToolTypeFlags || commands[etc.ID].Codename != etc.Codename {
			t.Fatalf("%s: %+v", etc.Codename, ref)
		}
	}
	template, _ := items.ItemRefByCodename("ITEM_MALL_AVATAR_M_HALLOWEEN_ATTACH")
	if len(template.PortOnlyMagicOptions) != 0 || template.RefObjID == Items()[0].ID {
		t.Fatal("the template row changed")
	}
	for _, skill := range AllSkills() {
		row, ok := skills.SkillByID(skill.ID)
		if !ok || row.Codename != skill.Codename {
			t.Fatalf("skill %s: %+v", skill.Codename, row)
		}
	}
	if _, err := Apply(items, options, skills); err == nil {
		t.Fatal("a second apply was admitted")
	}
}
