/*
===========================================================================

battleroyale_devilspirit_test.go - a Devil's Spirit in the airdrop

Port-only, not native (game/item/devilspirit, SRO_DEVIL_SPIRIT). With the
Devil's Spirit rows loaded, the airdrop guardian's killer rolls
devilspirit.AirdropReward on the match's loot stream: a hit lies with
the guardian's loot, of the killer's sex, carrying the row's fixed
HP/MP options (ItemRef.PortOnlyMagicOptions, never a rolled avatar
pool), and names the finder shard-wide on channel 7. Without the rows
(the flag off) the same stream drops none and announces nothing.

===========================================================================
*/
package action

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/battleroyale"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/devilspirit"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// devilLootStream is the second PCG word of the forced loot stream.
const devilLootStream = 0x44657631 // "Dev1"

/*
================
devilSpiritHitSeed

The first loot-stream seed whose opening rolls give a wearer of the sex a
Devil's Spirit (a forced roll: the reward reads the stream first).
================
*/
func devilSpiritHitSeed(t *testing.T, male bool) (uint64, string) {
	t.Helper()
	for seed := uint64(1); seed < 100_000; seed++ {
		stream := rand.New(rand.NewPCG(seed, devilLootStream))
		codename, ok, err := devilspirit.AirdropReward(male, func() (uint32, error) { return stream.Uint32(), nil })
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return seed, codename
		}
	}
	t.Fatal("no seed gives a Devil's Spirit")
	return 0, ""
}

/*
================
TestBattleRoyaleAirdropDevilSpirit
================
*/
func TestBattleRoyaleAirdropDevilSpirit(t *testing.T) {
	if got := battleroyale.DevilSpiritBanner("Ann"); got != "Ann found a Devil's Spirit!" {
		t.Fatalf("banner %q", got)
	}
	fx := newBattleRoyaleFixture(t, 2)
	items := fx.addKitItems()
	fx.rt.DropRoll = func() (uint32, error) { return 16000, nil }
	for _, c := range fx.chars {
		fx.pick(t, c, 1)
	}
	fx.startMatch(t)
	m := fx.match()
	a := m.airdrop
	fx.tick(time.Duration(a.landsMs-fx.clock.NowMs()) * time.Millisecond)
	if a.state == 0 || a.gid == 0 {
		t.Fatalf("airdrop did not land: %+v", a)
	}
	var guardian monster.Instance
	for _, instance := range fx.arenaMonsters() {
		if instance.Gid == a.gid {
			guardian = instance
		}
	}
	killer := fx.chars[0]
	male := enterworld.ResolveCharacterGenderIndex(killer) != domain.GenderFemale
	seed, want := devilSpiritHitSeed(t, male)
	spot := simulation.Spawn{RegionID: guardian.Spawn.RegionID, X: 1, Z: 1}
	isDevil := func(codename string) bool { return strings.Contains(codename, "_NASRUN") }

	// The flag off (no rows): the forced stream drops no Devil's Spirit.
	a.ultimate = false
	fx.rt.battleRoyale.loot = rand.New(rand.NewPCG(seed, devilLootStream))
	drops, _ := fx.rt.battleRoyaleLoot(killer, guardian, spot, fx.clock.Now())
	for _, drop := range drops {
		if isDevil(drop.Codename) {
			t.Fatalf("a Devil's Spirit without its rows: %s", drop.Codename)
		}
	}
	if len(noticeTexts(fx.frames["*"])) != 0 {
		t.Fatalf("shard notices with the flag off: %v", noticeTexts(fx.frames["*"]))
	}

	// The rows loaded (devilspirit.Apply's shape): the same forced roll
	// drops the killer's-sex row as a new instance awakened for good, its
	// level-0 options with the UMDU the drop roll picked (16000 % 5: the
	// first value, 2 %), and the banner.
	sex := map[string]int64{"M": 1, "W": 0}
	for _, item := range devilspirit.Items() {
		items[item.Codename] = &enterworld.ItemRef{RefObjID: item.ID, Codename: item.Codename, TypeIDs: [4]int64{3, 1, 13, 3},
			Country: 3, RequiredSex: sex[item.Sex], ReqQuadTypes: [4]int64{-1, -1, -1, -1},
			PortOnlyMagicOptions: devilspirit.Options(item.ID, 0, 0)}
	}
	fx.rt.battleRoyale.airdrops.Store(guardian.Gid, &battleRoyaleAirdrop{division: m.division, match: m.id, phase: a.phase, gid: guardian.Gid})
	fx.rt.battleRoyale.loot = rand.New(rand.NewPCG(seed, devilLootStream))
	drops, _ = fx.rt.battleRoyaleLoot(killer, guardian, spot, fx.clock.Now())
	found := 0
	for _, drop := range drops {
		if !isDevil(drop.Codename) {
			continue
		}
		found++
		in := devilspirit.Instance{RefObjID: drop.RefObjID, Variance: drop.VarianceBits, Durability: drop.Durability}
		options := devilspirit.Options(drop.RefObjID, 0, 2)
		if drop.Codename != want || !reflect.DeepEqual(drop.MagicOptions, options) || !devilspirit.Decode(in).Permanent() ||
			devilspirit.Decode(in).Umdu != 2 {
			t.Fatalf("Devil's Spirit drop %+v, want %s with %x, awakened for good", drop, want, options)
		}
	}
	if found != 1 {
		t.Fatalf("%d Devil's Spirits in %+v", found, drops)
	}
	if !hasNotice(fx.frames["*"], killer.Name+" found a Devil's Spirit!") {
		t.Fatalf("no shard banner: %v", noticeTexts(fx.frames["*"]))
	}

	// Any other kill never rolls it, even with the rows loaded.
	fx.rt.battleRoyale.loot = rand.New(rand.NewPCG(seed, devilLootStream))
	wolf := guardian
	wolf.Gid, wolf.Nest.HasRarityOverride = guardian.Gid+1, false
	drops, _ = fx.rt.battleRoyaleLoot(killer, wolf, spot, fx.clock.Now())
	for _, drop := range drops {
		if isDevil(drop.Codename) {
			t.Fatal("a wolf dropped a Devil's Spirit")
		}
	}
}
