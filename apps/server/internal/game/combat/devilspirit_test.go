/*
===========================================================================

devilspirit_test.go - the worn Devil's Spirit's options in the keeper

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

// devilSpiritSocket is the avatar socket the fixture wears it in.
const devilSpiritSocket = 2

/*
================
TestWornDevilSpiritRaisesMaxHPAndMP

Port-only, not native: a worn Devil's Spirit (game/item/devilspirit) made
with its level-0 options (MATTR_NASRUN_HPNA / _MPNA, 15 %) raises the
keeper's HP and MP maxima by 15 % through the same avatar option path as
a blessed v1.150 avatar; taken off, both return.
================
*/
func TestWornDevilSpiritRaisesMaxHPAndMP(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items, options := enterworld.NewTextdataItems(dir), enterworld.NewTextdataMagicOptions(dir)
	if _, err := devilspirit.Apply(items, options, enterworld.NewTextdataSkills(dir)); err != nil {
		t.Fatal(err)
	}
	ref, ok := items.ItemRefByCodename("ITEM_MALL_AVATAR_M_NASRUN_BLUE")
	if !ok {
		t.Fatal("no Devil's Spirit row")
	}
	c := &domain.Character{Level: pointer(10), Strength: pointer(40), Intellect: pointer(30),
		AvatarInventory: &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{{
			Slot: devilSpiritSocket, RefObjID: ref.RefObjID, Codename: ref.Codename, MagicOptions: ref.PortOnlyMagicOptions,
		}}}}
	catalogs := Catalogs{Items: items, MagicOptions: options}
	worn, err := PlayerBaseStatsWithModifiers(c, catalogs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.AvatarInventory = nil
	bare, err := PlayerBaseStatsWithModifiers(c, catalogs, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("max HP/MP worn %d/%d, bare %d/%d", worn.MaxHP, worn.MaxMP, bare.MaxHP, bare.MaxMP)
	within := func(got, base uint32) bool {
		want := float64(base) * 1.15
		return float64(got) >= want-1 && float64(got) <= want+1
	}
	if !within(worn.MaxHP, bare.MaxHP) || !within(worn.MaxMP, bare.MaxMP) {
		t.Fatalf("worn HP/MP %d/%d, bare %d/%d: not 15 %% more", worn.MaxHP, worn.MaxMP, bare.MaxHP, bare.MaxMP)
	}
}
