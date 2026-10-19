/*
===========================================================================

autoequip.go - which held item a match character should wear per slot

Port-only, not native (stage 8, docs/battle-royale.md "Server notes
(stage 8: full sets)"). A match character wears, in every slot of its
set (lootset.go: head, shoulders, chest, legs, hands, feet, weapon, the
shield of a shield build, earring, necklace, two rings), the best item
it holds by one score, and drops on the ground the usable duplicates
that can never beat the worn item. Since stage 9 (random loot) an item
the character can never use stays in the bag: the player may keep it,
drop it or hand it on. This file is the pure decision (PlanGear over GearPiece
rows) the wiring and the bots share; the wiring
(action/battleroyale_autoequip.go) reads the bag, runs the native move
and ground-drop paths and pushes the frames.

The score (GearScore), one number so a comparison is a plain ">":

	usable      the build, race, gender and armor class wear it (SlotOf);
	            an unusable item has no score and is never planned or
	            discarded. The level requirement is
	            the native equip gate's (the wiring asks it before every
	            move): a better item the level still gates is planned,
	            waits in the bag and is never discarded.
	degree      x GearScoreDegree     (100 000 000)
	Sun         + GearScoreSun        ( 10 000 000; Moon 0)
	plus (+N)   x GearScorePlus       (  1 000 000 each)
	whites      x GearScoreWhite      (      1 000 per white-stat percent
	                                   point summed over the item's
	                                   fields, at most 7 x 100)
	blues       x GearScoreBlue       (        100 each, at most 9)
	blue values + their sum, capped at gearScoreBlueValueCap (99)

Each term is smaller than one step of the term above it, so the order is
lexicographic: degree, then seal, then plus, then whites, then the
number of blues, then their values.

Weapon and shield pairing: the shield slot exists only for the shield
builds, whose weapon kinds are all one-handed (presets.go), and a
build's two-handed or ranged weapons never meet a shield; socket 7 of
any other build (ammunition) is never planned.

===========================================================================
*/
package battleroyale

import "slices"

const (
	GearScoreDegree = 100_000_000
	GearScoreSun    = 10_000_000
	GearScorePlus   = 1_000_000
	GearScoreWhite  = 1_000
	GearScoreBlue   = 100
	// gearScoreBlueValueCap keeps the blue values below one blue.
	gearScoreBlueValueCap = 99
	// UltimatePlus is the enhancement of the airdrop's ultimate weapon
	// (stage 8b); an item at it or above is never discarded.
	UltimatePlus = 8
)

/*
================
GearPiece

One held equipment item as the planner reads it: its codename, plus,
white-stat percentages summed over its fields, blue options and the sum
of their values, and whether it is worn (in a socket).
================
*/
type GearPiece struct {
	Codename     string
	Plus         uint8
	WhitePercent int
	Blues        int
	BlueValues   int64
	Worn         bool
}

/*
================
GearScore

The slot and score of a piece for killer (its build and gender); false
when the character can never use it.
================
*/
func GearScore(killer Killer, p GearPiece) (SetSlot, int64, bool) {
	slot, degree, ok := SlotOf(killer, p.Codename)
	if !ok {
		return 0, 0, false
	}
	score := int64(degree) * GearScoreDegree
	if _, seal := lootBaseOf(p.Codename); seal == SealSun {
		score += GearScoreSun
	}
	score += int64(p.Plus)*GearScorePlus + int64(p.WhitePercent)*GearScoreWhite + int64(p.Blues)*GearScoreBlue
	score += min(max(p.BlueValues, 0), gearScoreBlueValueCap)
	return slot, score, true
}

/*
================
GearPlan

What a character should wear and drop: per slot the index of the piece
to wear (-1 to leave the slot as it is), and the indexes of the
unworn pieces to drop.
================
*/
type GearPlan struct {
	Wear    [SetSlotCount]int
	Discard []int
}

/*
================
PlanGear

Over every held piece: per slot the best usable piece (the two best
rings in the two ring slots, best first), and every unworn usable piece
that is not chosen (no better than the chosen one) to drop, except a
piece at UltimatePlus or above. An unusable piece (another build, race,
gender or class, or a shield of a build without one) is left alone
(stage 9). A worn piece the planner does not know (a starter _DEF body)
scores below every usable piece, so it is replaced but never dropped
while worn.
================
*/
func PlanGear(killer Killer, pieces []GearPiece) GearPlan {
	var plan GearPlan
	for i := range plan.Wear {
		plan.Wear[i] = -1
	}
	type scored struct {
		index int
		score int64
	}
	var bySlot [SetSlotCount][]scored
	usable := map[int]bool{}
	for i, p := range pieces {
		slot, score, ok := GearScore(killer, p)
		if !ok || (slot == SlotShield && !killer.Build.Shield) {
			continue
		}
		usable[i] = true
		bySlot[slot] = append(bySlot[slot], scored{i, score})
	}
	chosen := map[int]bool{}
	for slot := range SetSlotCount {
		list := bySlot[slot]
		// A worn piece wins a tie, so equal items never swap.
		slices.SortStableFunc(list, func(a, b scored) int {
			switch {
			case a.score != b.score:
				return cmpDesc(a.score, b.score)
			case pieces[a.index].Worn != pieces[b.index].Worn:
				if pieces[a.index].Worn {
					return -1
				}
				return 1
			}
			return 0
		})
		picks := 1
		if slot == SlotRing {
			picks = 2
		}
		for n := 0; n < picks && n < len(list); n++ {
			plan.Wear[slot+SetSlot(n)] = list[n].index
			chosen[list[n].index] = true
		}
	}
	for i, p := range pieces {
		if p.Worn || chosen[i] || !usable[i] || p.Plus >= UltimatePlus {
			continue
		}
		plan.Discard = append(plan.Discard, i)
	}
	return plan
}

/*
================
cmpDesc
================
*/
func cmpDesc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}
