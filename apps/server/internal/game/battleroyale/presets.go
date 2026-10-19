/*
===========================================================================

presets.go - the match builds a participant chooses from

Port-only, not native (see zone.go). A build fixes the race, the two
masteries the match character trains, the weapon kinds its loot favours
and the creation choices that dress its level-1 body. The ids are shared
with the client picker (docs/battle-royale.md, "Builds"); the table is
data the wiring reads, never a rule the client can bend.

===========================================================================
*/
package battleroyale

import (
	"fmt"
	"strconv"

	"opensro.online/server/internal/domain"
)

const (
	// DefaultBuildID is the build a participant fights with when it never
	// picks one (docs/battle-royale.md: 11, Warrior 2H).
	DefaultBuildID uint8 = 11

	// The native mastery ids (skillmasterydata.txt).
	masteryBicheon   uint32 = 257 // sword and blade
	masteryHeuksal   uint32 = 258 // spear and glaive
	masteryPacheon   uint32 = 259 // bow
	masteryCold      uint32 = 273
	masteryLightning uint32 = 274
	masteryFire      uint32 = 275
	masteryForce     uint32 = 276
	masteryWarrior   uint32 = 513
	masteryWizard    uint32 = 514
	masteryRogue     uint32 = 515
	masteryWarlock   uint32 = 516
	masteryBard      uint32 = 517
	masteryCleric    uint32 = 518

	// The weapon TID4 kinds (itemdata column 12).
	WeaponSword        uint8 = 2
	WeaponBlade        uint8 = 3
	WeaponSpear        uint8 = 4
	WeaponGlaive       uint8 = 5
	WeaponBow          uint8 = 6
	WeaponOneHandEU    uint8 = 7
	WeaponTwoHandEU    uint8 = 8
	WeaponAxe          uint8 = 9
	WeaponDarkStaff    uint8 = 10
	WeaponTwoHandStaff uint8 = 11
	WeaponCrossbow     uint8 = 12
	WeaponDagger       uint8 = 13
	WeaponHarp         uint8 = 14
	WeaponStaff        uint8 = 15

	// The creation weapon indexes (enterworld/loadout.go chinaWeaponKinds
	// and europeWeaponRules) and protector indexes that dress the body.
	creationCHSword    = 1
	creationCHBlade    = 2
	creationCHSpear    = 3
	creationCHGlaive   = 4
	creationCHBow      = 5
	creationEUDagger   = 1
	creationEUSword    = 2
	creationEUTwoHand  = 3
	creationEUCrossbow = 5
	creationEUDark     = 6
	creationEUTwoStaff = 7
	creationCHHeavy    = 1
	creationCHLight    = 2
	creationEUFirst    = 1

	// The stat split per level above the creation base (BaseStat 20):
	// melee builds take 2 STR and 1 INT, casters 2 INT and 1 STR.
	statMajorPerLevel = 2
	statMinorPerLevel = 1
)

/*
================
Build

One match build. Masteries are trained to the match level on every
level-up; WeaponKinds are the TID4 kinds the match loot rolls weapons of;
Shield adds a shield to the loot and the starter body; Armor is the armor
class the match loot rolls (the class the build's masteries can wear);
Caster chooses the 2 INT : 1 STR split. WeaponIndex and ProtectorIndex are the creation
choices that dress the level-1 body with the race's _DEF starter set.
================
*/
type Build struct {
	ID             uint8
	Race           Race
	Name           string
	Masteries      [2]uint32
	WeaponKinds    []uint8
	Shield         bool
	Armor          ArmorClass
	Caster         bool
	WeaponIndex    int64
	ProtectorIndex int64
}

/*
================
ArmorClass

The armor class a build wears: heavy, light or clothes (garment for the
Chinese, robe for the Europeans). The European rows gate on masteries
(itemdata ReqLevelType 513 Warrior for heavy; 513/515/518 for light;
514/516/517/518 for clothes), so each build's class is one its two
masteries admit; the Chinese rows gate on level alone and follow the
creation protector.
================
*/
type ArmorClass uint8

const (
	ArmorHeavy ArmorClass = iota
	ArmorLight
	ArmorClothes
)

/*
================
ArmorClass.word

The itemdata codename word of the class.
================
*/
func (a ArmorClass) word() string {
	switch a {
	case ArmorLight:
		return "LIGHT"
	case ArmorClothes:
		return "CLOTHES"
	}
	return "HEAVY"
}

/*
================
Builds

The build table in id order. A fresh slice on every call: no package
state can be mutated.
================
*/
func Builds() []Build {
	return []Build{
		{ID: 1, Race: RaceChinese, Name: "Blade-Fire", Masteries: [2]uint32{masteryBicheon, masteryFire},
			WeaponKinds: []uint8{WeaponSword, WeaponBlade}, Shield: true, Armor: ArmorHeavy, WeaponIndex: creationCHBlade, ProtectorIndex: creationCHHeavy},
		{ID: 2, Race: RaceChinese, Name: "Glaive-Lightning", Masteries: [2]uint32{masteryHeuksal, masteryLightning},
			WeaponKinds: []uint8{WeaponSpear, WeaponGlaive}, Armor: ArmorHeavy, WeaponIndex: creationCHGlaive, ProtectorIndex: creationCHHeavy},
		{ID: 3, Race: RaceChinese, Name: "Bow-Cold", Masteries: [2]uint32{masteryPacheon, masteryCold},
			WeaponKinds: []uint8{WeaponBow}, Armor: ArmorLight, WeaponIndex: creationCHBow, ProtectorIndex: creationCHLight},
		{ID: 4, Race: RaceChinese, Name: "Blade-Heal", Masteries: [2]uint32{masteryBicheon, masteryForce},
			WeaponKinds: []uint8{WeaponSword, WeaponBlade}, Armor: ArmorHeavy, WeaponIndex: creationCHBlade, ProtectorIndex: creationCHHeavy},
		{ID: 11, Race: RaceEuropean, Name: "Warrior 2H", Masteries: [2]uint32{masteryWarrior, masteryCleric},
			WeaponKinds: []uint8{WeaponTwoHandEU}, Armor: ArmorHeavy, WeaponIndex: creationEUTwoHand, ProtectorIndex: creationEUFirst},
		{ID: 12, Race: RaceEuropean, Name: "Knight", Masteries: [2]uint32{masteryWarrior, masteryCleric},
			WeaponKinds: []uint8{WeaponOneHandEU}, Shield: true, Armor: ArmorHeavy, WeaponIndex: creationEUSword, ProtectorIndex: creationEUFirst},
		{ID: 13, Race: RaceEuropean, Name: "Ranger", Masteries: [2]uint32{masteryRogue, masteryWarrior},
			WeaponKinds: []uint8{WeaponCrossbow}, Armor: ArmorLight, WeaponIndex: creationEUCrossbow, ProtectorIndex: creationEUFirst},
		{ID: 14, Race: RaceEuropean, Name: "Assassin", Masteries: [2]uint32{masteryRogue, masteryWarrior},
			WeaponKinds: []uint8{WeaponDagger}, Armor: ArmorLight, WeaponIndex: creationEUDagger, ProtectorIndex: creationEUFirst},
		{ID: 15, Race: RaceEuropean, Name: "Wizard", Masteries: [2]uint32{masteryWizard, masteryCleric},
			WeaponKinds: []uint8{WeaponTwoHandStaff}, Armor: ArmorClothes, Caster: true, WeaponIndex: creationEUTwoStaff, ProtectorIndex: creationEUFirst},
		{ID: 16, Race: RaceEuropean, Name: "Warlock", Masteries: [2]uint32{masteryWarlock, masteryBard},
			WeaponKinds: []uint8{WeaponDarkStaff}, Armor: ArmorClothes, Caster: true, WeaponIndex: creationEUDark, ProtectorIndex: creationEUFirst},
	}
}

/*
================
BuildByID

The build with that id, or false for an id the table does not hold.
================
*/
func BuildByID(id uint8) (Build, bool) {
	for _, build := range Builds() {
		if build.ID == id {
			return build, true
		}
	}
	return Build{}, false
}

/*
================
DefaultPreset

The build of DefaultBuildID (the match core's DefaultBuild is the name a
lobby entry carries; the wiring maps BuildName to this table).
================
*/
func DefaultPreset() Build {
	build, _ := BuildByID(DefaultBuildID)
	return build
}

/*
================
BuildName

The build name the match core records for a lobby member: the id in
decimal, so Participant.Build round-trips through BuildByName.
================
*/
func BuildName(id uint8) string {
	return fmt.Sprintf("%d", id)
}

/*
================
BuildByName

The build a recorded name (BuildName) means; the default build for the
core's DefaultBuild or anything unknown.
================
*/
func BuildByName(name string) Build {
	id, err := strconv.ParseUint(name, 10, 8)
	if err != nil {
		return DefaultPreset()
	}
	build, ok := BuildByID(uint8(id))
	if !ok {
		return DefaultPreset()
	}
	return build
}

/*
================
Build.RaceKey

The domain race key of the build's race.
================
*/
func (b Build) RaceKey() string {
	if b.Race == RaceChinese {
		return domain.RaceKeyChina
	}
	return domain.RaceKeyEurope
}

/*
================
Build.Stats

The STR and INT of the match character at a level: the creation base plus
the build's split for every level gained.
================
*/
func (b Build) Stats(level int64) (strength, intellect int64) {
	gained := max(level-1, 0)
	major := domain.BaseStat + statMajorPerLevel*gained
	minor := domain.BaseStat + statMinorPerLevel*gained
	if b.Caster {
		return minor, major
	}
	return major, minor
}

const (
	// The starter kit (stage 2): a degree-1 Sun weapon of the build in the
	// weapon socket, a degree-1 Sun shield (shield builds) or the creation
	// ammunition (bow and crossbow) in the shield socket, and twenty HP and
	// MP potions in the bag.
	KitWeaponSlot     = 6
	KitShieldSlot     = 7
	KitBagSlot        = -1 // the first free bag slot
	kitPotions        = 20
	kitAmmo           = 250
	kitDegree         = 1
	kitArrowCodename  = "ITEM_ETC_AMMO_ARROW_01_DEF"
	kitBoltCodename   = "ITEM_ETC_AMMO_BOLT_01_DEF"
	kitHPCodename     = "ITEM_ETC_HP_POTION_01"
	kitMPCodename     = "ITEM_ETC_MP_POTION_01"
	kitShieldCodename = "ITEM_%s_SHIELD_%02d_C_RARE"
)

/*
================
KitItem

One starter-kit row: the itemdata codename, the slot it goes to
(KitBagSlot for the first free bag slot) and its count.
================
*/
type KitItem struct {
	Codename string
	Slot     int
	Count    int64
}

/*
================
Build.StarterWeaponKind

The kind the build's starter weapon is: its creation weapon, the second
kind where the build has two (blade for Blade-Fire, glaive for
Glaive-Lightning).
================
*/
func (b Build) StarterWeaponKind() uint8 {
	if len(b.WeaponKinds) == 0 {
		return 0
	}
	return b.WeaponKinds[len(b.WeaponKinds)-1]
}

/*
================
Build.StarterKit

The rows a match character starts with (docs/battle-royale.md, stage 2
item 7): the weapon first, then the off-hand (shield or ammunition) when
the build has one, then the potions.
================
*/
func (b Build) StarterKit() []KitItem {
	var kit []KitItem
	if codename, ok := SunWeaponCodename(b.Race, b.StarterWeaponKind(), kitDegree); ok {
		kit = append(kit, KitItem{Codename: codename, Slot: KitWeaponSlot, Count: 1})
	}
	switch {
	case b.Shield:
		kit = append(kit, KitItem{Codename: fmt.Sprintf(kitShieldCodename, racePrefix(b.Race), kitDegree), Slot: KitShieldSlot, Count: 1})
	case b.HasWeaponKind(WeaponBow):
		kit = append(kit, KitItem{Codename: kitArrowCodename, Slot: KitShieldSlot, Count: kitAmmo})
	case b.HasWeaponKind(WeaponCrossbow):
		kit = append(kit, KitItem{Codename: kitBoltCodename, Slot: KitShieldSlot, Count: kitAmmo})
	}
	return append(kit,
		KitItem{Codename: kitHPCodename, Slot: KitBagSlot, Count: kitPotions},
		KitItem{Codename: kitMPCodename, Slot: KitBagSlot, Count: kitPotions})
}

/*
================
Build.HasMastery
================
*/
func (b Build) HasMastery(id uint32) bool {
	return id != 0 && (b.Masteries[0] == id || b.Masteries[1] == id)
}

/*
================
Build.HasWeaponKind
================
*/
func (b Build) HasWeaponKind(kind uint8) bool {
	for _, k := range b.WeaponKinds {
		if k == kind {
			return true
		}
	}
	return false
}
