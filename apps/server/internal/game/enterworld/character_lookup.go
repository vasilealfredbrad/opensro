package enterworld

import (
	"opensro.online/server/internal/domain"
	"strings"
)

/*
================
CharacterByName

The character source's record of name; a detached composition's
ephemeral roster (ephemeral.go) answers what the source does not know
(the store's own lookup already lists its ephemeral records).
================
*/
func (d *Deps) CharacterByName(division, name string) *Character {
	if d == nil || d.Characters == nil {
		return nil
	}
	if source, ok := d.Characters.(domain.CharacterLookup); ok {
		if c := source.CharacterByName(division, name); c != nil {
			return c
		}
		return d.ephemeralLookup().CharacterByName(division, name)
	}
	for _, c := range d.Characters.CharactersForDivision(division) {
		if c != nil && strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return d.ephemeralLookup().CharacterByName(division, name)
}

/*
================
CharacterByID

CharacterByName by id.
================
*/
func (d *Deps) CharacterByID(division string, id int64) *Character {
	if d == nil || d.Characters == nil {
		return nil
	}
	if source, ok := d.Characters.(domain.CharacterLookup); ok {
		if c := source.CharacterByID(division, id); c != nil {
			return c
		}
		return d.ephemeralLookup().CharacterByID(division, id)
	}
	for _, c := range d.Characters.CharactersForDivision(division) {
		if c != nil && c.ID == id {
			return c
		}
	}
	return d.ephemeralLookup().CharacterByID(division, id)
}

/*
================
ephemeralLookup

The detached ephemeral roster as a lookup, or one that knows nobody.
================
*/
func (d *Deps) ephemeralLookup() domain.CharacterLookup {
	if roster, ok := d.Ephemeral.(domain.CharacterLookup); ok {
		return roster
	}
	return noCharacters{}
}

/*
================
noCharacters
================
*/
type noCharacters struct{}

func (noCharacters) CharacterByName(string, string) *Character { return nil }
func (noCharacters) CharacterByID(string, int64) *Character    { return nil }
