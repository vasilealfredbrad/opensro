/*
===========================================================================

portonlyitems.go - item reference rows the v1.150 data does not have

Port-only, not native. A port-only item (the Devil's Spirit,
game/item/devilspirit) is a whole new reference row, not an edit of a
retail one. It is cloned from a retail template row of the same family,
so every NativeFields column, requirement and type the owners read is a
v1.150 value, and then takes its own identity: id, codename, name, icon.
Like the other port-only Apply* edits it lands on the loaded rows before
any owner reads them and before the rows are archived, so the GM
catalogue, the browser references, bootstrap snapshots, equip gates and
the stat projection all see the same row.

===========================================================================
*/
package enterworld

import "fmt"

/*
================
PortOnlyItem

One port-only reference row: the retail Template it is cloned from and
the identity it replaces. MagicOptions are the encoded options every
instance of the row is made with (ItemRef.PortOnlyMagicOptions).
================
*/
type PortOnlyItem struct {
	RefObjID          uint32
	Codename          string
	Template          string
	NameStrID         string
	Name              string
	DescriptionSymbol string
	Icon              string
	MagicOptions      []uint64
}

/*
================
AddPortOnlyItems

Clones each item's template row under the item's identity. A missing
template, an id or codename the loaded data already uses, or rows that
are already archived refuse the whole set before any row is added.
================
*/
func (t *TextdataItems) AddPortOnlyItems(items []PortOnlyItem) (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("port-only items: references are already archived")
	}
	rows := make([]*ItemRef, 0, len(items))
	ids, codenames := map[uint32]bool{}, map[string]bool{}
	for _, item := range items {
		if item.RefObjID == 0 || item.Codename == "" {
			return 0, fmt.Errorf("port-only items: row %d %q has no identity", item.RefObjID, item.Codename)
		}
		if _, used := t.byID[item.RefObjID]; used || ids[item.RefObjID] {
			return 0, fmt.Errorf("port-only items: id %d of %s is already used", item.RefObjID, item.Codename)
		}
		if _, used := t.byCodename[item.Codename]; used || codenames[item.Codename] {
			return 0, fmt.Errorf("port-only items: codename %s is already used", item.Codename)
		}
		template, ok := t.byCodename[item.Template]
		if !ok {
			return 0, fmt.Errorf("port-only items: %s has no template row %s", item.Codename, item.Template)
		}
		ids[item.RefObjID], codenames[item.Codename] = true, true
		row := *template
		if template.VarianceIntMin1c0 != nil {
			variance := *template.VarianceIntMin1c0
			row.VarianceIntMin1c0 = &variance
		}
		row.SummonLevelThresholds = append([]uint8(nil), template.SummonLevelThresholds...)
		row.RefObjID = item.RefObjID
		row.Codename = item.Codename
		row.NameStrID = item.NameStrID
		row.Name = item.Name
		row.DescriptionSymbol = item.DescriptionSymbol
		row.Icon = item.Icon
		row.PortOnlyMagicOptions = append([]uint64(nil), item.MagicOptions...)
		rows = append(rows, &row)
	}
	for _, row := range rows {
		t.byCodename[row.Codename] = row
		t.byID[row.RefObjID] = row
	}
	return len(rows), nil
}
