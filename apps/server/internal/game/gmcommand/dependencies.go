package gmcommand

import "opensro.online/server/internal/domain"

// Dependencies is the character lookup surface consumed by GM commands.
type Dependencies interface {
	domain.CharacterSource
	Read(divisionID string, read func())
}

// interactionLocker is the optional facet of Dependencies that names a
// sender whose commands are refused for the moment (a battle royale
// participant, port-only; enterworld.Deps.InteractionLockedFor).
type interactionLocker interface {
	InteractionLockedFor(divisionID, characterName string) bool
}
