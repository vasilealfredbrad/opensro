/*
===========================================================================

ephemeral_test.go - live characters the store never persists

Port-only (the battle royale bots, ephemeral.go): an ephemeral record
resolves by name and id and goes through every commit door, yet it never
becomes a row, never joins the roster or the character list, holds its
name and id against creation while it lives, and is gone after Forget.

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
countCharacterRows
================
*/
func countCharacterRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM characters").Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

/*
================
TestEphemeralCharacterNeverPersists
================
*/
func TestEphemeralCharacterNeverPersists(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "Real"}); err != nil {
		t.Fatal(err)
	}
	rows := countCharacterRows(t, s)
	level := int64(1)
	bot := &domain.Character{ID: 99001, Name: "Ares", Level: &level}
	if err := s.RegisterEphemeralCharacter(testDivision, bot); err != nil {
		t.Fatal(err)
	}
	source := s.Characters().(domain.CharacterLookup)
	if source.CharacterByName(testDivision, "ares") != bot || source.CharacterByID(testDivision, 99001) != bot {
		t.Fatal("the ephemeral record does not resolve")
	}
	for _, c := range s.Characters().CharactersForDivision(testDivision) {
		if c == bot {
			t.Fatal("the ephemeral record joined the roster")
		}
	}
	// Every door runs its mutation and writes no row.
	if !s.UpdateCharacter(bot, "bot-update", func() bool { level = 7; return true }) || *bot.Level != 7 {
		t.Fatal("the update door refused the ephemeral record")
	}
	s.MutateCharacter(bot, "bot-mutate", func() { level = 8 })
	s.MutateCharacterDeferred(bot, "bot-mutate-deferred", func() { level = 9 })
	real := source.CharacterByName(testDivision, "Real")
	if !s.UpdateCharacters([]*domain.Character{real, bot}, "pvp", func() bool { return true }) {
		t.Fatal("a mixed door refused the ephemeral record")
	}
	if got := countCharacterRows(t, s); got != rows {
		t.Fatalf("%d character rows, want %d", got, rows)
	}
	// The name and the id are held while it lives.
	if err := s.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "ARES"}); !errors.Is(err, ErrCharacterNameConflict) {
		t.Fatalf("creation took a live bot's name: %v", err)
	}
	if err := s.RegisterEphemeralCharacter(testDivision, &domain.Character{ID: 99002, Name: "real"}); err == nil {
		t.Fatal("an ephemeral record took a stored name")
	}
	s.meta.NextCharID[testDivision] = 99001
	fresh := &enterworld.Character{Name: "Later"}
	if err := s.CreateCharacter(testDivision, "test-account", fresh); err != nil || fresh.ID == 99001 {
		t.Fatalf("creation took the live bot's id: %v id %d", err, fresh.ID)
	}
	s.ForgetEphemeralCharacter(bot)
	if source.CharacterByName(testDivision, "Ares") != nil || s.EphemeralCharacterCount() != 0 {
		t.Fatal("the forgotten record still resolves")
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name == "Ares" || c.ID == 99001 {
			t.Fatalf("the bot reached the database: %+v", c)
		}
	}
}
