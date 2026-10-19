/*
===========================================================================

ephemeral.go - live characters no account owns and nothing persists

Port-only, not native: the battle royale bots (docs/battle-royale.md
"Stage 8") are real characters of a match. The authority store holds
them beside its roster (data/store/ephemeral.go): they resolve by name
and id and go through the commit doors, yet never become a row. Deps
publishes that door as Ephemeral; a detached composition (unit tests)
installs an EphemeralRoster instead, which answers the lookups the store
would.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
)

/*
================
EphemeralCharacters

The door that admits and drops a never-persisted live character.
================
*/
type EphemeralCharacters interface {
	RegisterEphemeralCharacter(division string, c *Character) error
	ForgetEphemeralCharacter(c *Character)
}

/*
================
RegisterEphemeralCharacter

Admits c through the composition's ephemeral door; refused when none is
installed.
================
*/
func (d *Deps) RegisterEphemeralCharacter(division string, c *Character) error {
	if d == nil || d.Ephemeral == nil {
		return fmt.Errorf("ephemeral characters: no door installed")
	}
	return d.Ephemeral.RegisterEphemeralCharacter(division, c)
}

/*
================
ForgetEphemeralCharacter
================
*/
func (d *Deps) ForgetEphemeralCharacter(c *Character) {
	if d != nil && d.Ephemeral != nil {
		d.Ephemeral.ForgetEphemeralCharacter(c)
	}
}

//============================================================================

/*
================
EphemeralRoster

The in-memory ephemeral door of a detached composition: the same
refusals as the store's, and the lookups Deps falls back to when its
character source does not know a name or id.
================
*/
type EphemeralRoster struct {
	mu   sync.Mutex
	rows map[*Character]string
}

/*
================
NewEphemeralRoster
================
*/
func NewEphemeralRoster() *EphemeralRoster {
	return &EphemeralRoster{rows: map[*Character]string{}}
}

/*
================
EphemeralRoster.RegisterEphemeralCharacter
================
*/
func (r *EphemeralRoster) RegisterEphemeralCharacter(division string, c *Character) error {
	if c == nil || division == "" || strings.TrimSpace(c.Name) == "" || c.ID < 1 || c.ID > domain.MaxCharacterID {
		return fmt.Errorf("ephemeral character: missing division, name or id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for existing, d := range r.rows {
		if d == division && (existing == c || strings.EqualFold(existing.Name, c.Name) || existing.ID == c.ID) {
			return fmt.Errorf("ephemeral character %q (id %d) taken in division %s", c.Name, c.ID, division)
		}
	}
	r.rows[c] = division
	return nil
}

/*
================
EphemeralRoster.ForgetEphemeralCharacter
================
*/
func (r *EphemeralRoster) ForgetEphemeralCharacter(c *Character) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, c)
}

/*
================
EphemeralRoster.CharacterByName
================
*/
func (r *EphemeralRoster) CharacterByName(division, name string) *Character {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c, d := range r.rows {
		if d == division && strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

/*
================
EphemeralRoster.CharacterByID
================
*/
func (r *EphemeralRoster) CharacterByID(division string, id int64) *Character {
	r.mu.Lock()
	defer r.mu.Unlock()
	for c, d := range r.rows {
		if d == division && c.ID == id {
			return c
		}
	}
	return nil
}

/*
================
EphemeralRoster.Len
================
*/
func (r *EphemeralRoster) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}
