/*
===========================================================================

state.go - one Devil's Spirit's awakening, enhancement and options

Port-only, not native. In the later client a Devil's Spirit instance
carries three things the v1.150 item record has no column for:

  - the awakening: the item's Data word, (hasExpiry << 31) |
    remainingSeconds. A new Devil's Spirit is not awakened; awakened, it
    runs for the row's Param1 (2419200 s, 28 days) and an expired one
    gives nothing until an extension item renews it;
  - the timed enhancement (a binding option of type 3, written by the
    Awakening Enhancement Scroll): a level and an expiry;
  - its stats, which no item row stores: _RefMagicOptByItemOptLevel and
    _RefSkillByItemOptLevel give the options (HP %, MP %, block rate)
    and skills of each (RefItemID, effective OptLevel), where the
    effective OptLevel is the base OptLevel plus the live binding's
    level.

The port keeps all of it in the fields every item owner already carries,
copies and persists, so no converter, store or wire shape changes:

  - Durability: the awakening's absolute expiry (unix seconds), 0 when
    not awakened, permanentAwake when it never expires (the battle royale
    airdrop's). The client shows the remaining time from it;
  - VarianceBits (an avatar has no white stats): bits 0..31 the
    binding's expiry (unix seconds), bits 32..39 its level, bits 40..47
    the MATTR_NASRUN_UMDU percent the item was made with;
  - Plus: the base OptLevel;
  - MagicOptions: derived by Sync from the above, so the stat projection
    (combat's magic option path) and the tooltip read them as any item's
    options. An expired Devil's Spirit carries none: the original's
    sweep detaches all of its stats.

Sync is the one owner of the derivation: every path that changes the
state (making, equipping, the scroll, the extension, the expiry sweep)
ends with it.

===========================================================================
*/
package devilspirit

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

const (
	// permanentAwake is the awakening expiry of a Devil's Spirit that never
	// expires (the original's Data word without the hasExpiry bit).
	permanentAwake = 0xffffffff

	bindExpiryMask = 0xffffffff
	bindLevelShift = 32
	umduShift      = 40
	byteMask       = 0xff

	// MaxLevel is the highest effective OptLevel the ability tables author.
	MaxLevel = 10
)

/*
================
State

One instance's port-only state, decoded from its item fields.
================
*/
type State struct {
	AwakeUntil uint32 // unix seconds; 0 not awakened; permanentAwake never
	BindLevel  uint8
	BindUntil  uint32 // unix seconds; 0 no binding
	Umdu       uint8  // MATTR_NASRUN_UMDU percent
}

/*
================
Instance

The item fields the state lives in (inventory.Item and
enterworld.InventoryRow carry the same words in different types).
================
*/
type Instance struct {
	RefObjID   uint32
	Plus       uint8
	Variance   uint64
	Durability uint32
	Options    []uint64
}

/*
================
Decode
================
*/
func Decode(in Instance) State {
	return State{
		AwakeUntil: in.Durability,
		BindUntil:  uint32(in.Variance & bindExpiryMask),
		BindLevel:  uint8(in.Variance >> bindLevelShift & byteMask),
		Umdu:       uint8(in.Variance >> umduShift & byteMask),
	}
}

/*
================
encode

Writes s back into in's Durability and Variance.
================
*/
func encode(in Instance, s State) Instance {
	in.Durability = s.AwakeUntil
	in.Variance = uint64(s.BindUntil) | uint64(s.BindLevel)<<bindLevelShift | uint64(s.Umdu)<<umduShift
	return in
}

/*
================
Awakened
================
*/
func (s State) Awakened() bool {
	return s.AwakeUntil != 0
}

/*
================
Expired

An awakened Devil's Spirit whose period has run out.
================
*/
func (s State) Expired(now int64) bool {
	return s.AwakeUntil != 0 && s.AwakeUntil != permanentAwake && int64(s.AwakeUntil) <= now
}

/*
================
Permanent
================
*/
func (s State) Permanent() bool {
	return s.AwakeUntil == permanentAwake
}

/*
================
Remaining

The awakening's remaining seconds (0 expired or not awakened), the
original's Data word without its flag.
================
*/
func (s State) Remaining(now int64) uint32 {
	if !s.Awakened() || s.Permanent() || s.Expired(now) {
		return 0
	}
	return uint32(int64(s.AwakeUntil) - now)
}

/*
================
bindingLive
================
*/
func (s State) bindingLive(now int64) bool {
	return s.BindLevel != 0 && int64(s.BindUntil) > now
}

/*
================
EffectiveLevel

The base OptLevel plus a live binding's level (an expired binding counts
0), clamped to the ability tables' MaxLevel.
================
*/
func EffectiveLevel(in Instance, now int64) int {
	level := int(in.Plus)
	if s := Decode(in); s.bindingLive(now) {
		level += int(s.BindLevel)
	}
	return min(level, MaxLevel)
}

/*
================
levelOf
================
*/
func levelOf(refObjID uint32, level int) (Level, bool) {
	for _, item := range data.Items {
		if item.ID == refObjID {
			return item.Levels[min(max(level, 0), MaxLevel)], true
		}
	}
	return Level{}, false
}

/*
================
Options

The encoded options of refObjID at level: MATTR_NASRUN_UMDU (when the
item carries it), MATTR_NASRUN_HPNA and _MPNA, and _BLOCKRATE when the
level authors one. Values are the later client's
_RefMagicOptByItemOptLevel rows.
================
*/
func Options(refObjID uint32, level int, umdu uint8) []uint64 {
	row, ok := levelOf(refObjID, level)
	if !ok {
		return nil
	}
	var out []uint64
	if umdu != 0 {
		out = append(out, enterworld.EncodeMagicOption(data.option(tagUMDU).ID, uint32(umdu)))
	}
	out = append(out,
		enterworld.EncodeMagicOption(data.option(tagHPNA).ID, row.HP),
		enterworld.EncodeMagicOption(data.option(tagMPNA).ID, row.MP))
	if row.Block != 0 {
		out = append(out, enterworld.EncodeMagicOption(data.option(tagBRNA).ID, row.Block))
	}
	return out
}

/*
================
Skills

The skill ids the worn instance grants at its effective level
(_RefSkillByItemOptLevel); none while expired.
================
*/
func Skills(in Instance, now int64) []uint32 {
	if Decode(in).Expired(now) {
		return nil
	}
	row, ok := levelOf(in.RefObjID, EffectiveLevel(in, now))
	if !ok {
		return nil
	}
	return append([]uint32(nil), row.Skills...)
}

/*
================
Sync

Brings in up to date at now: an expired binding is cleared, and the
options are derived again (none once the awakening has expired). changed
reports whether any field moved, so the caller knows to persist and to
tell the client.
================
*/
func Sync(in Instance, now int64) (Instance, bool) {
	if !Is(in.RefObjID) {
		return in, false
	}
	s := Decode(in)
	out := in
	if s.BindLevel != 0 && !s.bindingLive(now) || s.BindLevel == 0 && s.BindUntil != 0 {
		s.BindLevel, s.BindUntil = 0, 0
		out = encode(out, s)
	}
	var options []uint64
	if !s.Expired(now) {
		options = Options(in.RefObjID, EffectiveLevel(out, now), s.Umdu)
	}
	out.Options = options
	changed := out.Variance != in.Variance || out.Durability != in.Durability || !equalOptions(out.Options, in.Options)
	return out, changed
}

/*
================
equalOptions
================
*/
func equalOptions(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

/*
================
Make

A new instance of refObjID. umdu is the MATTR_NASRUN_UMDU percent it is
made with (UmduValues); permanent makes it awakened for good (the battle
royale airdrop's: a match is shorter than any period, and the item never
leaves it).
================
*/
func Make(refObjID uint32, umdu uint8, permanent bool, now int64) Instance {
	s := State{Umdu: umdu}
	if permanent {
		s.AwakeUntil = permanentAwake
	}
	in, _ := Sync(encode(Instance{RefObjID: refObjID}, s), now)
	return in
}

/*
================
UmduValues

The values MATTR_NASRUN_UMDU's generator offers (its magicoption row's
range words, two u16 values each: 2, 4, 6, 8 and 10 %).

INFERENCE: the later client grants MATTR_NASRUN_UMDU only through its
Devil's Spirit alchemy stone (ITEM_MALL_NASRUN_ARCHEMY_MAGICSTONE_UMDU,
"grant extra damage on unique monsters"), a mall stone the port does not
carry; a new Devil's Spirit is made as if that stone had been applied,
its value drawn from the stone option's own generator as every v1.150
stone value is (alchemy.RollMagicValue).
================
*/
func UmduValues() []uint8 {
	var out []uint8
	for _, word := range data.option(tagUMDU).RangeWords {
		for _, v := range []uint32{word >> 16, word & 0xffff} {
			if v != 0 {
				out = append(out, uint8(v))
			}
		}
	}
	return out
}

/*
================
Awaken

Starts the awakening of a not yet awakened instance at now. The original
asks the player ("Do you want to awaken this item?") when a Devil's
Spirit is used; v1.150 has no use action for an avatar item, so the
port awakens it when it is first worn (INFERENCE).
================
*/
func Awaken(in Instance, now int64) Instance {
	s := Decode(in)
	if s.Awakened() {
		return in
	}
	s.AwakeUntil = uint32(now + int64(data.AwakeSeconds))
	out, _ := Sync(encode(in, s), now)
	return out
}

// The upgrade and extension refusals.
var (
	ErrNotDevilSpirit = fmt.Errorf("devilspirit: not a Devil's Spirit")
	ErrNotAwakened    = fmt.Errorf("devilspirit: not awakened")
	ErrExpired        = fmt.Errorf("devilspirit: awakening expired")
	ErrPermanent      = fmt.Errorf("devilspirit: never expires")
)

/*
================
UpgradeLevel

The Awakening Enhancement Scroll's roll (the original's
AVATAR_SPECIAL_OPTLV_RATIO table, refshardcontentconfig.txt): n is a
draw, taken modulo 101 as the original's rand()%101; the first level
whose cumulative ratio exceeds it is the result. 0 (and the draw 100,
past the table's 100 total) is a failure.
================
*/
func UpgradeLevel(n uint32) uint8 {
	roll := n % 101
	var total uint32
	for level, ratio := range data.UpgradeRatios {
		total += ratio
		if roll < total {
			return uint8(level)
		}
	}
	return 0
}

/*
================
Upgrade

Applies a scroll's result: a binding of level for seconds from now. The
target must be awakened and not expired (the original's "not expired"
and Param2 == 1 checks; every port row is upgradeable). A level 0 is the
caller's failure, never written.
================
*/
func Upgrade(in Instance, level uint8, seconds uint32, now int64) (Instance, error) {
	if !Is(in.RefObjID) {
		return in, ErrNotDevilSpirit
	}
	s := Decode(in)
	switch {
	case !s.Awakened():
		return in, ErrNotAwakened
	case s.Expired(now):
		return in, ErrExpired
	case level == 0:
		return in, fmt.Errorf("devilspirit: level 0 is a failure")
	}
	s.BindLevel, s.BindUntil = level, uint32(now+int64(seconds))
	out, _ := Sync(encode(in, s), now)
	return out, nil
}

/*
================
Extend

The extension item: the awakening's new expiry is max(current, now) plus
seconds. A not awakened instance refuses ("The item can be used after
awakening."), as does a permanent one.
================
*/
func Extend(in Instance, seconds uint32, now int64) (Instance, error) {
	if !Is(in.RefObjID) {
		return in, ErrNotDevilSpirit
	}
	s := Decode(in)
	switch {
	case !s.Awakened():
		return in, ErrNotAwakened
	case s.Permanent():
		return in, ErrPermanent
	}
	from := max(int64(s.AwakeUntil), now)
	s.AwakeUntil = uint32(min(from+int64(seconds), permanentAwake-1))
	out, _ := Sync(encode(in, s), now)
	return out, nil
}
