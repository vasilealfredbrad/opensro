/*
===========================================================================

portonlyskills.go - skill rows the v1.150 data does not have

Port-only, not native. A port-only skill (the Devil's Spirit's devil
buffs, game/item/devilspirit) is a whole skilldata row of a later client
in the same 118-column layout v1.150 ships. It is parsed by the same
parser as every shipped shard, as one more shard after them, so its
effect program, plan and rank are compiled exactly as a native row's
would be. Like AddPortOnlyItems it runs at wiring, after the readiness
load and before any owner reads a row or the references are archived.

===========================================================================
*/
package enterworld

import "fmt"

/*
================
AddPortOnlySkillRows

Parses the shipped shards again with rows after them and adopts that
parse as this loader's own (the process-wide shared parse of the plain
files stays as it was). A row whose id or codename the loaded data
already uses, a short row, or rows the parse drops refuse the whole set.
================
*/
func (t *TextdataSkills) AddPortOnlySkillRows(rows [][]string) (int, error) {
	t.once.Do(t.load)
	if t.loadErr != nil {
		return 0, t.loadErr
	}
	if t.rows.archive != nil {
		return 0, fmt.Errorf("port-only skills: references are already archived")
	}
	ids, codenames := map[uint32]bool{}, map[string]bool{}
	for _, fields := range rows {
		if len(fields) < skilldataMinColumns {
			return 0, fmt.Errorf("port-only skills: a row has %d columns", len(fields))
		}
		id, ok := textdataInt(fields[skilldataColID])
		codename := fields[skilldataColCodename]
		if !ok || id < 1 || id > 0xffffffff || codename == "" {
			return 0, fmt.Errorf("port-only skills: row %q has no identity", fields[skilldataColID])
		}
		if _, used := t.rows.lookup(uint32(id)); used || ids[uint32(id)] {
			return 0, fmt.Errorf("port-only skills: id %d of %s is already used", id, codename)
		}
		if _, used := t.byCodename[codename]; used || codenames[codename] {
			return 0, fmt.Errorf("port-only skills: codename %s is already used", codename)
		}
		ids[uint32(id)], codenames[codename] = true, true
	}
	parsed := &TextdataSkills{dir: t.dir, portOnly: rows}
	parsed.parse(skillShards(t.dir))
	if parsed.loadErr != nil {
		return 0, parsed.loadErr
	}
	for id := range ids {
		if _, ok := parsed.rows.lookup(id); !ok {
			return 0, fmt.Errorf("port-only skills: row %d did not parse", id)
		}
	}
	t.rows, t.plans, t.byCodename = parsed.rows, parsed.plans, parsed.byCodename
	t.shared, t.sharedKey = parsed, ""
	t.indexLearnedRanks()
	return len(rows), nil
}
