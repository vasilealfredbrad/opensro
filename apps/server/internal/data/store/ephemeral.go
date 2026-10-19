/*
===========================================================================

ephemeral.go - live characters the store answers for but never persists

Port-only, not native: the battle royale bots (docs/battle-royale.md
"Stage 8") are real characters of a match that no account owns. They
must resolve like every character (by name, by id, through the commit
doors, under the store lock that orders every field read), yet nothing
of them may reach the database: no row, no id watermark, no ledger
entry, no character list.

An ephemeral record is therefore kept beside the roster, never in it:

  - s.characters, s.charDivision and the persisted shape never see it,
    so a whole-world commit, the character list, the boot loaders and
    every roster walk skip it by construction;
  - the published lookup index (character_lookup.go) lists it after the
    division's real rows, so CharacterByName and CharacterByID find it
    and a real row of the same name would win;
  - the commit doors run its mutations under the store lock like any
    record's, but mark nothing dirty: a door that touched only ephemeral
    records commits only when the ground moved with it (a pickup), never
    a character row (finishEphemeralDoorLocked);
  - creation refuses its name and skips its id while it lives.

ForgetEphemeralCharacter drops it; nothing else ever has to.

===========================================================================
*/
package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

/*
================
RegisterEphemeralCharacter

Admits c as a live, never-persisted record of division. Refused when the
name or id is taken by a real record or another ephemeral one of the
division, or the id is outside the character range.
================
*/
func (s *Store) RegisterEphemeralCharacter(division string, c *domain.Character) error {
	if c == nil || division == "" || strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("ephemeral character: missing division or name")
	}
	if c.ID < 1 || c.ID > domain.MaxCharacterID {
		return fmt.Errorf("ephemeral character %q: id %d outside 1..%d", c.Name, c.ID, domain.MaxCharacterID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, real := s.charDivision[c]; real {
		return fmt.Errorf("ephemeral character %q is a stored record", c.Name)
	}
	if _, known := s.ephemeral[c]; known {
		return fmt.Errorf("ephemeral character %q already registered", c.Name)
	}
	for _, existing := range s.characters[division] {
		if strings.EqualFold(existing.Name, c.Name) || existing.ID == c.ID {
			return fmt.Errorf("%w: ephemeral %q (id %d) in division %s", ErrCharacterNameConflict, c.Name, c.ID, division)
		}
	}
	for existing, d := range s.ephemeral {
		if d == division && (strings.EqualFold(existing.Name, c.Name) || existing.ID == c.ID) {
			return fmt.Errorf("%w: ephemeral %q (id %d) in division %s", ErrCharacterNameConflict, c.Name, c.ID, division)
		}
	}
	if s.ephemeral == nil {
		s.ephemeral = map[*domain.Character]string{}
	}
	s.ephemeral[c] = division
	s.rebuildCharacterLookupLocked(division)
	return nil
}

/*
================
ForgetEphemeralCharacter

Drops an ephemeral record; a record the store does not hold as
ephemeral is ignored.
================
*/
func (s *Store) ForgetEphemeralCharacter(c *domain.Character) {
	s.mu.Lock()
	defer s.mu.Unlock()
	division, known := s.ephemeral[c]
	if !known {
		return
	}
	delete(s.ephemeral, c)
	s.rebuildCharacterLookupLocked(division)
}

/*
================
EphemeralCharacterCount

How many ephemeral records the store holds (tests and metrics).
================
*/
func (s *Store) EphemeralCharacterCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.ephemeral)
}

/*
================
ephemeralRowsLocked

The division's ephemeral records in id order (the lookup index lists
them after the real rows).
================
*/
func (s *Store) ephemeralRowsLocked(division string) []*domain.Character {
	var out []*domain.Character
	for c, d := range s.ephemeral {
		if d == division {
			out = append(out, c)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

/*
================
ephemeralLocked

Whether c is a live ephemeral record.
================
*/
func (s *Store) ephemeralLocked(c *domain.Character) bool {
	_, known := s.ephemeral[c]
	return known
}

/*
================
ephemeralDoorLocked

Whether a door's declared records are all ephemeral (and at least one
is): its mutation stands in memory and nothing commits.
================
*/
func (s *Store) ephemeralDoorLocked(cs []*domain.Character) bool {
	seen := false
	for _, c := range cs {
		if c == nil {
			continue
		}
		if !s.ephemeralLocked(c) {
			return false
		}
		seen = true
	}
	return seen
}

/*
================
ephemeralNameLocked

Whether a live ephemeral record of division holds name (creation
refuses it).
================
*/
func (s *Store) ephemeralNameLocked(division, name string) bool {
	for c, d := range s.ephemeral {
		if d == division && strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

/*
================
ephemeralIDLocked

Whether a live ephemeral record of division holds id (creation skips it).
================
*/
func (s *Store) ephemeralIDLocked(division string, id int64) bool {
	for c, d := range s.ephemeral {
		if d == division && c.ID == id {
			return true
		}
	}
	return false
}

/*
================
finishEphemeralDoorLocked

The end of a door that touched only ephemeral records: no character row
is dirty, so the door commits only when the ground registry moved with
it (a bot's pickup or drop), exactly as that ground change would commit
under any other door; otherwise nothing is written at all.
================
*/
func (s *Store) finishEphemeralDoorLocked(label string, deferred bool) {
	if s.ground.source == nil || s.ground.revisionRecorded && s.ground.source.Revision() == s.ground.committedRev {
		return
	}
	s.finishDoorLocked(label, deferred, nil)
}
