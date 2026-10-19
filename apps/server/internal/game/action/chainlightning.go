/*
===========================================================================

chainlightning.go - a wider Chain Lightning chain (port-only, not native)

The European wizard's Chain Lightning (SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_01
through _08) chains through its row's own efr block: shape 6, radius 100,
3 targets, 25% less damage per victim. Every rank authors the same block,
which the client's tooltip shows as a 10.0 m transfer range with a maximum
transfer of 3. areaVictims selects the chain: the primary target, then the
nearest others within the radius of the primary (no body radii), up to the
target count.

The owner asked for up to 50 targets within 10000 m (2026-10-18). Two server
settings carry it:

	SRO_CHAIN_LIGHTNING_TARGETS       the target count, 1..255
	SRO_CHAIN_LIGHTNING_RANGE_METERS  the chain radius in metres

Unset, "off" or "0" leaves each native. The radius is converted to world
units (a 1920-unit region is 192 m, so ten units a metre; the tooltip
divides the efr word by ten). Only the row's own shape-6 block of this one
skill family changes: other chains, an imbue's spread and the cast range
stay native.

The owner then asked (2026-10-18) for every victim to take full damage and
for the bolt to be seen hopping from victim to victim every half second:

	SRO_CHAIN_LIGHTNING_FULL_DAMAGE   "on": no falloff along the chain
	SRO_CHAIN_LIGHTNING_HOP_MS        the hop period in ms, 1..2000

Natively the falloff compounds per victim (planAreaVictims): 100%, 75%,
56%... and a running percent that truncates to 0 reads as full again in
FinishImpact, so a long native chain's far victims hit at full. Full damage
zeroes the row's reduction byte, so every victim takes the primary's 100%.

The hop is presentation with one authoritative consequence. Natively the
server strikes every victim in one commit and the client's AT_MOV_SPLASH
mover (wizard_light_lighting_c.efp, MOV_STRAIGHT at 400 units a second)
flies caster -> primary -> each next result target, showing each victim's
hit, HP drop and death when it arrives (8D8AD0; the client holds the HP
and death of a pending result, effective-hp.ts, damage-feedback.ts). Kills
and rewards stay in the cast's commit, as native: staggering the commit
itself would split one door's damage, settlements, drops and wire across
timers for a visual. The client learns the period at world entry
(enterworld ChainLightningHop, with the family's skill ids) and starts hop
k at the k-th period after the bolt left the caster (chain-cadence.ts). The
server's part is the corpse: a fatal victim natively stays 2 s
(monsterDeathPresentationRetention), which a native chain of 10 m outlasts
easily; at a period of 500 ms the fifth victim's corpse would be gone
before the bolt reached it. Victim k's corpse therefore stays k periods
longer, so the bolt still finds it and its death plays as on a native
chain.

===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

const (
	// EnvChainLightningTargets names the target count: a number, or "off"
	// for the row's own.
	EnvChainLightningTargets = "SRO_CHAIN_LIGHTNING_TARGETS"
	// EnvChainLightningRange names the chain radius in metres: a number,
	// or "off" for the row's own.
	EnvChainLightningRange = "SRO_CHAIN_LIGHTNING_RANGE_METERS"
	// EnvChainLightningFullDamage turns the chain's damage falloff off:
	// "on", or "off" for the row's own.
	EnvChainLightningFullDamage = "SRO_CHAIN_LIGHTNING_FULL_DAMAGE"
	// EnvChainLightningHop names the hop period in milliseconds: a number,
	// or "off" for the native flight.
	EnvChainLightningHop = "SRO_CHAIN_LIGHTNING_HOP_MS"

	// chainLightningCodenamePrefix is every rank's codename up to its
	// two-digit level (version-stable, where the numeric ids drift).
	chainLightningCodenamePrefix = "SKILL_EU_WIZARD_PSYCHICA_LIGHT_B_"
	// maxChainLightningRank bounds the two-digit level the published skill
	// ids are searched through.
	maxChainLightningRank = 99
	// chainAreaShape is efr shape 6, the nearest-first chain.
	chainAreaShape = 6
	// maxChainLightningTargets is the most an efr target byte and the
	// cast result's target count byte (wire.SkillCastAreaFrame) carry.
	maxChainLightningTargets = math.MaxUint8
	// maxChainLightningRangeMeters bounds the radius well past the width
	// of the world map.
	maxChainLightningRangeMeters = 100000
	// maxChainLightningHopMs bounds the hop period: a full 255-victim chain
	// then holds its last corpse under nine minutes.
	maxChainLightningHopMs = 2000
	// worldUnitsPerMeter converts metres to world units: a 1920-unit
	// region (worldgeom.OutdoorRegionSize) is 192 m.
	worldUnitsPerMeter = 10
)

/*
================
ChainLightning

The shard's Chain Lightning chain. A zero field keeps the row's own value
or the native presentation, so the zero value is native.
================
*/
type ChainLightning struct {
	Targets     uint8
	RangeMeters uint32
	// FullDamage zeroes the chain's damage falloff.
	FullDamage bool
	// HopMs is the client's hop period, and how much longer each next
	// victim's corpse stays.
	HopMs uint32
}

/*
================
ChainLightning.Enabled

Whether the chain itself is widened (its count or radius); FullDamage and
HopMs are their own rules.
================
*/
func (c ChainLightning) Enabled() bool {
	return c.Targets != 0 || c.RangeMeters != 0
}

/*
================
ChainLightning.family

Whether area is a Chain Lightning row's own chain: the one block every
setting here touches.
================
*/
func (c ChainLightning) family(skill enterworld.SkillRow, area enterworld.SkillOffensiveArea) bool {
	return area.Shape == chainAreaShape && strings.HasPrefix(skill.Codename, chainLightningCodenamePrefix)
}

/*
================
ChainLightning.area

The area areaVictims selects with and acceptSkillAreaAt plans with: a Chain
Lightning row's own chain with the shard's count, radius and falloff in
place of the authored ones, any other area as given. Applying it twice is
the same as once.
================
*/
func (c ChainLightning) area(skill enterworld.SkillRow, area enterworld.SkillOffensiveArea) enterworld.SkillOffensiveArea {
	if !c.family(skill, area) {
		return area
	}
	if c.Targets != 0 {
		area.MaxTargets = c.Targets
	}
	if c.RangeMeters != 0 {
		area.Radius = c.RangeMeters * worldUnitsPerMeter
	}
	if c.FullDamage {
		area.ReductionPercent = 0
	}
	return area
}

/*
================
ChainLightning.hopMs

The hop period for a Chain Lightning row's own chain, 0 natively and for
any other area: victim k's corpse stays k of them longer (publishArea).
================
*/
func (c ChainLightning) hopMs(skill enterworld.SkillRow, area enterworld.SkillOffensiveArea) int64 {
	if c.HopMs == 0 || !c.family(skill, area) {
		return 0
	}
	return int64(c.HopMs)
}

/*
================
ChainLightningSkillIDs

Every shipped rank's skill id whose own area is the chain, in rank order:
the casts the client times with the hop period.
================
*/
func ChainLightningSkillIDs(skills enterworld.SkillCodenameSource) []uint32 {
	var out []uint32
	for rank := 1; rank <= maxChainLightningRank; rank++ {
		row, ok := skills.SkillByCodename(fmt.Sprintf("%s%02d", chainLightningCodenamePrefix, rank))
		if ok && row.OffensiveArea.Shape == chainAreaShape {
			out = append(out, row.ID)
		}
	}
	return out
}

/*
================
parseChainLightningSetting

"", "off" and "0" are native (0); anything else must be a whole number in
1..limit. A malformed value is an error, so a typo never ships silently.
================
*/
func parseChainLightningSetting(env, text string, limit int) (int, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0":
		return 0, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > limit {
		return 0, fmt.Errorf("%s: %q is not a number between 1 and %d", env, text, limit)
	}
	return n, nil
}

/*
================
parseChainLightningSwitch

"", "off", "0" and "false" are native; "on", "1" and "true" turn the rule
on. Anything else is an error, as for the numbers.
================
*/
func parseChainLightningSwitch(env, text string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "off", "0", "false":
		return false, nil
	case "on", "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("%s: %q is neither on nor off", env, text)
}

/*
================
ParseChainLightning

The count and radius settings' text.
================
*/
func ParseChainLightning(targets, rangeMeters string) (ChainLightning, error) {
	count, err := parseChainLightningSetting(EnvChainLightningTargets, targets, maxChainLightningTargets)
	if err != nil {
		return ChainLightning{}, err
	}
	meters, err := parseChainLightningSetting(EnvChainLightningRange, rangeMeters, maxChainLightningRangeMeters)
	if err != nil {
		return ChainLightning{}, err
	}
	return ChainLightning{Targets: uint8(count), RangeMeters: uint32(meters)}, nil
}

/*
================
ParseChainLightningFullDamage

The full-damage switch's text.
================
*/
func ParseChainLightningFullDamage(text string) (bool, error) {
	return parseChainLightningSwitch(EnvChainLightningFullDamage, text)
}

/*
================
ParseChainLightningHop

The hop period's text: 0 is the native flight.
================
*/
func ParseChainLightningHop(text string) (uint32, error) {
	hop, err := parseChainLightningSetting(EnvChainLightningHop, text, maxChainLightningHopMs)
	return uint32(hop), err
}

/*
================
ChainLightningFromEnv

All four settings, as the GameWorld reads them at boot.
================
*/
func ChainLightningFromEnv() (ChainLightning, error) {
	chain, err := ParseChainLightning(os.Getenv(EnvChainLightningTargets), os.Getenv(EnvChainLightningRange))
	if err != nil {
		return ChainLightning{}, err
	}
	if chain.FullDamage, err = ParseChainLightningFullDamage(os.Getenv(EnvChainLightningFullDamage)); err != nil {
		return ChainLightning{}, err
	}
	if chain.HopMs, err = ParseChainLightningHop(os.Getenv(EnvChainLightningHop)); err != nil {
		return ChainLightning{}, err
	}
	return chain, nil
}
