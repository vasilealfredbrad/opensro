/*
===========================================================================

skills.go - the devil skills a worn Devil's Spirit grants

Port-only, not native. The later client's _RefSkillByItemOptLevel gives
every (Devil's Spirit row, effective OptLevel) its skills, which the
original server attaches while the item is worn (an item skill, source
type 3) and detaches when it comes off or expires:

  - the Malicious / Wicked / Vallainous Devil Skill (ECCENTRICDEMON,
    THEDEVILCONFUSION, DESTROYTHEDEVIL; a red, golden and blue row of
    each): a 10-minute buff, 20 / 25 / 30 % more damage (dru) and 10 /
    15 / 20 % movement speed (hst3), a 30-minute cooldown, castable only
    while the Devil's Spirit is worn (reqi). The later client draws the
    awakened devil on its caster while it lasts (charactervisualchange);
  - Ignore Monster's defense 1 and 2 (MOBDEFENCEIGNORE): its msid block
    has no v1.150 meaning, so the port grants it (it shows as learned)
    and gives it no effect (INFERENCE: the later server's monster-defence
    path is not ported).

The rows are the later client's skilldata (devilspirit.json "skills"),
in v1.150's own 118-column layout, parsed by the server's skill parser
(enterworld.AddPortOnlySkillRows) unchanged. Their reqi (kind 14) names
the later client's fifth avatar socket, which v1.150 does not have;
combat's reqi walk reads it as a worn Devil's Spirit (equipmentreqi.go).

===========================================================================
*/
package devilspirit

import (
	"fmt"
	"strconv"
)

const (
	// skillColumns is v1.150's (and the later client's) skilldata width.
	skillColumns = 118
	// skillNameColumn is the (Korean) name column the data leaves out.
	skillNameColumn = 4
)

/*
================
SkillTarget

The skill catalogue Apply extends.
================
*/
type SkillTarget interface {
	AddPortOnlySkillRows(rows [][]string) (int, error)
}

/*
================
SkillRows

Every devil skill as a skilldata row: its columns, "0" where the data
names none, the codename in the name column.
================
*/
func SkillRows() ([][]string, error) {
	rows := make([][]string, 0, len(data.Skills))
	for _, skill := range data.Skills {
		row := make([]string, skillColumns)
		for i := range row {
			row[i] = "0"
		}
		for key, value := range skill.Columns {
			column, err := strconv.Atoi(key)
			if err != nil || column < 0 || column >= skillColumns {
				return nil, fmt.Errorf("devilspirit: %s column %q", skill.Codename, key)
			}
			row[column] = value
		}
		row[skillNameColumn] = skill.Codename
		if row[1] != strconv.FormatUint(uint64(skill.ID), 10) || row[3] != skill.Codename {
			return nil, fmt.Errorf("devilspirit: %s columns name another skill", skill.Codename)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

/*
================
IsSkill

Whether id is a devil skill (granted only by a worn Devil's Spirit).
================
*/
func IsSkill(id uint32) bool {
	for _, skill := range data.Skills {
		if skill.ID == id {
			return true
		}
	}
	return false
}

/*
================
AllSkills

A copy of the devil skill rows.
================
*/
func AllSkills() []Skill {
	return append([]Skill(nil), data.Skills...)
}
