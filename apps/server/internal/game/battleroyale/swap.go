/*
===========================================================================

swap.go - holding the real character and dressing the match character

Port-only, not native (see zone.go). Hold turns a character row into a
level-1 match character of a build and keeps the complete real record in
the row's BattleRoyaleHeld field (domain/battleroyale_hold.go); Restore
puts the real record back. Both are plain record mutations: the caller
owns the store door, the relocation and every frame. The seeders are the
store's own creation seeds, so the match body is exactly what character
creation would have made for the build's race.

Everything the match character must not inherit is reset here: the real
equipment on a body of another race crashes the client, and a pet, quest
or cooldown of the real character has no meaning in the arena.

===========================================================================
*/
package battleroyale

import (
	"errors"

	"opensro.online/server/internal/domain"
)

const (
	// MatchLevelCap is the hard level cap of a match character.
	MatchLevelCap int64 = 30
	// The match pace: every credited kill is worth 2/7 of the EXP the
	// character's level needs (MatchKillExp), 3.5 kills a level whatever
	// the monster's level, so level 1 to 30 takes about 102 kills.
	matchKillsPerLevelNum = 7
	matchKillsPerLevelDen = 2
	// MatchMaxHPPercent is the max-HP bonus of a match character (x2.5).
	MatchMaxHPPercent = 150
	// ChinesePhysicalPercent raises a Chinese match character's physical
	// attack (x1.2); WizardMagicalPercent halves a Wizard's magical attack.
	ChinesePhysicalPercent = 20
	WizardMagicalPercent   = -50
	// WizardBuildID is the build WizardMagicalPercent applies to.
	WizardBuildID uint8 = 15
	// matchStartLevel is where every match character starts.
	matchStartLevel int64 = 1
)

var (
	// ErrAlreadyHeld: the row already carries a match character.
	ErrAlreadyHeld = errors.New("battleroyale: character already holds a match character")
	// ErrNoModel: the seeders know no model for the build's race and gender.
	ErrNoModel = errors.New("battleroyale: no model for the build's race")
)

/*
================
MatchKillExp

The EXP one credited kill gives a match character whose level needs
expRequired to advance: expRequired / 3.5, rounded up. It replaces the
native reward formula, whose monster/player level-gap scale would starve
a level-30 character on the level-5 match wolf. A kill never crosses two
levels: the share is below the next level's requirement.
================
*/
func MatchKillExp(expRequired int64) int64 {
	if expRequired <= 0 {
		return 0
	}
	return (expRequired*matchKillsPerLevelDen + matchKillsPerLevelNum - 1) / matchKillsPerLevelNum
}

/*
================
Seeders

The creation seeds the match body is dressed with: the race model, the
racial base skills (store.SkillSeedFunc) and the starter inventory
(store.Options.DefaultInventory, which also sets the starting gold the
hold then clears).
================
*/
type Seeders struct {
	Model     func(race Race, gender int64) (codename string, modelRef uint32, ok bool)
	Skills    func(raceKey string, learned []uint32) ([]uint32, error)
	Inventory func(c *domain.Character)
}

/*
================
Hold

Replaces c's record with a level-1 match character of build in world,
holding the real record. The caller holds the store door. On an error
nothing changed.
================
*/
func Hold(c *domain.Character, build Build, world uint32, nowMs int64, seed Seeders) error {
	if c.BattleRoyaleHeld != nil {
		return ErrAlreadyHeld
	}
	gender := domain.ResolveCharacterGenderIndex(c)
	model, modelRef, ok := seed.Model(build.Race, gender)
	if !ok {
		return ErrNoModel
	}
	real := domain.CloneCharacter(c)
	race := int64(build.Race)
	ref := int64(modelRef)
	weaponIndex, protectorIndex := build.WeaponIndex, build.ProtectorIndex

	c.RaceIndex, c.Gender = &race, &gender
	c.ModelCodename, c.ModelRef = model, &ref
	c.WeaponSelected, c.WeaponIndex = true, &weaponIndex
	c.ArmorSelected, c.ProtectorIndex = true, &protectorIndex
	c.AnimationSetName = ""

	c.MissionInventory = nil
	c.AvatarInventory = nil
	c.ActiveCOS, c.Mercenaries, c.MercenarySummonUntilMs, c.CapturedCOS = nil, nil, 0, nil
	c.PetSkillWindows, c.ParamJobs, c.CompositeJobs, c.PremiumClock = nil, nil, nil, nil
	c.ItemGroupCooldowns, c.TimedSkillJobs, c.FortressReturnUntilMs = nil, nil, 0
	c.OffensiveSkillCooldowns, c.SharedSkillCooldowns, c.SkillActionRecoveryUntilMs = nil, nil, 0
	c.ItemUseCooldowns, c.ItemCureCooldowns, c.PetPotionCooldowns = [3]int64{}, [2]int64{}, [3]int64{}
	c.Job = domain.CharacterJob{}
	c.QuickSlots, c.Mission = nil, nil
	c.ActiveQuests, c.TrackedQuests, c.QuestSupplies = nil, nil, nil
	c.PK, c.BerserkPoints, c.MostWanted = nil, 0, nil
	c.ExperiencePercent = nil

	level := matchStartLevel
	zero := int64(0)
	c.Level, c.MaxLevel = &level, &level
	c.CurrentHP, c.CurrentMP = nil, nil
	c.SkillPoints, c.StatPoints = &zero, &zero
	c.Experience, c.SkillExp = &zero, &zero
	c.Masteries = domain.DefaultMasteries(build.RaceKey())
	SetMatchLevel(c, build, level)
	c.Skills = nil
	if seed.Skills != nil {
		if ids, err := seed.Skills(build.RaceKey(), nil); err == nil {
			c.Skills = ids
		}
	}
	if seed.Inventory != nil {
		seed.Inventory(c)
	}
	if c.MissionInventory == nil {
		c.MissionInventory = []domain.InventoryRow{}
	}
	gold := int64(0)
	c.Gold = &gold

	world32 := world
	next := domain.CharacterWorld{PackedInstance: &world32, SpawnSet: true}
	if c.World != nil {
		next.Spawn = c.World.Spawn
		next.MovementMode = c.World.MovementMode
	}
	c.World = &next
	resetRuntimeFields(c)
	c.BattleRoyaleHeld = &domain.BattleRoyaleHold{Real: real, Build: build.ID, World: world, HeldAtMs: nowMs}
	return nil
}

/*
================
Restore

Puts the held real record back into c, standing at home in the default
world, and clears the hold. The social fields a match cannot pause
(friends, whisper blocks, guild membership, the last login and the last
time seen) carry over from the match record. A hold without a record is
cleared and reported false: the row keeps the match character, which is
still a playable character, rather than losing both.
================
*/
func Restore(c *domain.Character, home domain.WorldSpawn) (*domain.BattleRoyaleHold, bool) {
	hold := c.BattleRoyaleHeld
	if hold == nil {
		return nil, false
	}
	if hold.Real == nil {
		c.BattleRoyaleHeld = nil
		return hold, false
	}
	friends, blocked, guild := c.Friends, c.BlockedWhisperers, c.GuildID
	lastLogin, lastSeen := c.LastLoginAtMs, c.LastSeenUnixMs
	*c = *hold.Real
	c.BattleRoyaleHeld = nil
	c.Friends, c.BlockedWhisperers, c.GuildID = friends, blocked, guild
	c.LastLoginAtMs, c.LastSeenUnixMs = lastLogin, lastSeen
	if c.World == nil {
		c.World = &domain.CharacterWorld{}
	}
	c.World.PackedInstance = nil
	spawn := home
	c.World.Spawn = &spawn
	c.World.SpawnSet = true
	c.World.MoveSegment = nil
	resetRuntimeFields(c)
	return hold, true
}

/*
================
Held

Whether the row carries a match character.
================
*/
func Held(c *domain.Character) bool {
	return c != nil && c.BattleRoyaleHeld != nil
}

/*
================
HeldBuild

The build of the row's match character, or false outside a match.
================
*/
func HeldBuild(c *domain.Character) (Build, bool) {
	if !Held(c) {
		return Build{}, false
	}
	return BuildByID(c.BattleRoyaleHeld.Build)
}

/*
================
SetMatchLevel

Writes the build's stats for a level and trains both masteries to it
(the mastery levels follow the level, no SP is spent).
================
*/
func SetMatchLevel(c *domain.Character, build Build, level int64) {
	level = min(max(level, matchStartLevel), MatchLevelCap)
	strength, intellect := build.Stats(level)
	c.Strength, c.Intellect = &strength, &intellect
	for i := range c.Masteries {
		if build.HasMastery(c.Masteries[i].ID) {
			c.Masteries[i].Level = level
		}
	}
}

/*
================
resetRuntimeFields

The json:"-" fields belong to the live actor, not to either record: a
swap in either direction starts them over.
================
*/
func resetRuntimeFields(c *domain.Character) {
	c.NativeTeleportMode = 0
	c.Aggressions = nil
	c.EventMembership = nil
	c.BerserkUntilMs, c.BattleUntilMs, c.LastExpLoss = 0, 0, 0
	c.DeathKind, c.NativeBodyStatus, c.BodyStatusOwner = 0, 0, 0
	c.TransformRefObjID, c.TransformMode, c.TransformShape = 0, 0, 0
	c.TransformEquipment = [9]uint32{}
	c.Buyback, c.BuybackSession = nil, 0
	c.CapturedCOS = nil
}
