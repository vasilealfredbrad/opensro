/*
===========================================================================

lootset.go - full sets: the slots a build fills and what it holds there

Port-only, not native (stage 8 item 4, docs/battle-royale.md "Server
notes (stage 8: full sets)"). A match character's set is twelve slots:
the six armor sockets of its class (head, shoulders, chest, legs, hands,
feet; inventory.SocketHead..SocketFoot), its weapon, its shield (shield
builds only), an earring, a necklace and two rings. The head socket
takes two itemdata rows per degree: HA (the helm) and CA (the crown or
diadem, also TID4 1); every other socket takes one.

SlotOf tells which slot of a build a codename fills (the auto-equip's
question, autoequip.go); SetState is what a character holds per slot,
which the economy simulation keeps to report complete sets. Stage 9
removed the stage-8 slot bias and pity rule: the loot is random
(lootrandom.go) and never reads a set.

===========================================================================
*/
package battleroyale

import "strings"

/*
================
SetSlot

One slot of a match set, in the order the docs table and the
simulation report them.
================
*/
type SetSlot uint8

const (
	SlotHead SetSlot = iota
	SlotShoulders
	SlotChest
	SlotLegs
	SlotHands
	SlotFeet
	SlotWeapon
	SlotShield
	SlotEarring
	SlotNecklace
	SlotRing
	SlotRingSecond
	// SetSlotCount is the number of slots; a build without a shield
	// fills SetSlotCount - 1 of them.
	SetSlotCount
)

/*
================
SetSlot.Name

The slot's name in logs and the docs table.
================
*/
func (s SetSlot) Name() string {
	names := [SetSlotCount]string{"head", "shoulders", "chest", "legs", "hands", "feet", "weapon", "shield",
		"earring", "necklace", "ring", "ring 2"}
	if s >= SetSlotCount {
		return "?"
	}
	return names[s]
}

/*
================
armorPartSlots

The set slot of each armor codename part (TID4: HA and CA 1 head, SA 2
shoulders, BA 3 chest, LA 4 legs, AA 5 hands, FA 6 feet). Fresh map: no
package state.
================
*/
func armorPartSlots() map[string]SetSlot {
	return map[string]SetSlot{"HA": SlotHead, "CA": SlotHead, "SA": SlotShoulders, "BA": SlotChest, "LA": SlotLegs,
		"AA": SlotHands, "FA": SlotFeet}
}

/*
================
accessoryWordSlots

The set slot of each accessory word (a ring row fills either ring slot).
================
*/
func accessoryWordSlots() map[string]SetSlot {
	return map[string]SetSlot{"EARRING": SlotEarring, "NECKLACE": SlotNecklace, "RING": SlotRing}
}

/*
================
BuildSlots

The slots a build's set has: all twelve, without the shield for a
build that carries none.
================
*/
func BuildSlots(build Build) []SetSlot {
	var out []SetSlot
	for slot := range SetSlotCount {
		if slot == SlotShield && !build.Shield {
			continue
		}
		out = append(out, slot)
	}
	return out
}

/*
================
SetState

The best degree (1-4) a participant holds per slot, 0 for none. Ring
holds the better ring, RingSecond the other.
================
*/
type SetState struct {
	Degree [SetSlotCount]int
}

/*
================
SlotOf

The set slot and degree of a Moon or Sun codename for killer's build,
race, gender and class; false for anything the build cannot wear (a
ring answers SlotRing).
================
*/
func SlotOf(killer Killer, codename string) (SetSlot, int, bool) {
	base, _ := lootBaseOf(codename)
	for _, row := range equipmentRows(killer) {
		if row.base == base {
			return row.slot, row.degree, true
		}
	}
	return 0, 0, false
}

/*
================
lootBaseOf

A Moon/Sun codename without its seal suffix, and its seal.
================
*/
func lootBaseOf(codename string) (string, Seal) {
	if base, ok := strings.CutSuffix(codename, "_"+SealSun.suffix()); ok {
		return base, SealSun
	}
	base, _ := strings.CutSuffix(codename, "_"+SealMoon.suffix())
	return base, SealMoon
}

/*
================
SetState.Take

Adds one held codename: it fills its slot when it beats the degree
there (a ring pushes the worse ring into the second ring slot).
Reports whether the set improved.
================
*/
func (s *SetState) Take(killer Killer, codename string) bool {
	slot, degree, ok := SlotOf(killer, codename)
	if !ok {
		return false
	}
	if slot == SlotRing {
		switch {
		case degree > s.Degree[SlotRing]:
			s.Degree[SlotRing], s.Degree[SlotRingSecond] = degree, s.Degree[SlotRing]
			return true
		case degree > s.Degree[SlotRingSecond]:
			s.Degree[SlotRingSecond] = degree
			return true
		}
		return false
	}
	if degree > s.Degree[slot] {
		s.Degree[slot] = degree
		return true
	}
	return false
}

/*
================
SetState.Complete

Whether every slot of the build holds an item that is current at level:
degree maxDegree, or a degree whose row of the slot needs a level in
[level - LootWindow, level] or above the window's floor (what the window
could still offer is never newer).
================
*/
func (s SetState) Complete(build Build, level int64) bool {
	for _, slot := range BuildSlots(build) {
		if !s.SlotCurrent(slot, level) {
			return false
		}
	}
	return true
}

/*
================
SetState.SlotCurrent

Whether slot holds degree maxDegree, or an item at least as new as the
newest row of that slot a killer of level can still be offered (the
newest degree whose required level is at most level).
================
*/
func (s SetState) SlotCurrent(slot SetSlot, level int64) bool {
	held := s.Degree[slot]
	if held >= maxDegree {
		return true
	}
	if held == 0 {
		return false
	}
	return held >= newestDegree(slot, level)
}

/*
================
newestDegree

The highest degree whose row of slot needs at most level (its smallest
required level: the head's HA and CA share theirs).
================
*/
func newestDegree(slot SetSlot, level int64) int {
	offset := slotOffset(slot)
	best := 1
	for degree := 2; degree <= maxDegree; degree++ {
		if degreeLevel(degree, offset) <= level {
			best = degree
		}
	}
	return best
}

/*
================
slotOffset

The required-level offset of a slot's rows (loot.go armorPartOffsets,
accessoryOffsets; weapons and shields 0).
================
*/
func slotOffset(slot SetSlot) int64 {
	for part, partSlot := range armorPartSlots() {
		if partSlot == slot {
			return armorPartOffsets()[part]
		}
	}
	for word, wordSlot := range accessoryWordSlots() {
		if wordSlot == slot || (slot == SlotRingSecond && wordSlot == SlotRing) {
			return accessoryOffsets()[word]
		}
	}
	return 0
}
