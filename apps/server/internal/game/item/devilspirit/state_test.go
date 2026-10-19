/*
===========================================================================

state_test.go - tests for state.go

===========================================================================
*/
package devilspirit

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

const (
	// testNow is a fixed clock (unix seconds).
	testNow = 1_800_000_000
	// crimsonM / uniqueM are the male crimson A grade and S grade rows.
	crimsonM = 24385
	uniqueM  = 24391
)

/*
================
optionValues

The decoded {param id: value} of encoded options.
================
*/
func optionValues(options []uint64) map[uint32]uint32 {
	out := map[uint32]uint32{}
	for _, v := range options {
		out[uint32(v&0xffff)] = uint32(v >> 32)
	}
	return out
}

/*
================
TestOptionsFollowTheLevelTables

The later client's per-row values: the A grade at OptLevel 0 gives 15 %
HP and MP and no block rate, at 10 25 % and 2; the S grade 20 % and 1 at
0, 30 % and 3 at 10; UMDU rides along when the instance has one.
================
*/
func TestOptionsFollowTheLevelTables(t *testing.T) {
	hp, mp, block, umdu := data.option(tagHPNA).ID, data.option(tagMPNA).ID, data.option(tagBRNA).ID, data.option(tagUMDU).ID
	for _, c := range []struct {
		id                 uint32
		level              int
		hpmp, brna, umduIn uint32
	}{
		{crimsonM, 0, 15, 0, 0},
		{crimsonM, 3, 18, 1, 6},
		{crimsonM, 10, 25, 2, 0},
		{crimsonM, 14, 25, 2, 0},
		{uniqueM, 0, 20, 1, 10},
		{uniqueM, 10, 30, 3, 0},
	} {
		got := optionValues(Options(c.id, c.level, uint8(c.umduIn)))
		want := map[uint32]uint32{hp: c.hpmp, mp: c.hpmp}
		if c.brna != 0 {
			want[block] = c.brna
		}
		if c.umduIn != 0 {
			want[umdu] = c.umduIn
		}
		if len(got) != len(want) {
			t.Fatalf("%d level %d: %v, want %v", c.id, c.level, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%d level %d: %v, want %v", c.id, c.level, got, want)
			}
		}
	}
	if Options(1, 0, 0) != nil {
		t.Fatal("a retail row has Devil's Spirit options")
	}
}

/*
================
TestStateRoundTrip

The state survives the item fields it is packed into.
================
*/
func TestStateRoundTrip(t *testing.T) {
	s := State{AwakeUntil: testNow + 5, BindLevel: 7, BindUntil: testNow + 60, Umdu: 8}
	if got := Decode(encode(Instance{RefObjID: crimsonM}, s)); got != s {
		t.Fatalf("%+v, want %+v", got, s)
	}
}

/*
================
TestMakeAwakenAndExpire

A new instance is not awakened and gives its level-0 options; the first
wear awakens it for the row's 28 days; past them it gives nothing and no
skill. A permanent instance never expires.
================
*/
func TestMakeAwakenAndExpire(t *testing.T) {
	in := Make(crimsonM, 4, false, testNow)
	if Decode(in).Awakened() || len(in.Options) != 3 || len(Skills(in, testNow)) != 1 {
		t.Fatalf("new instance %+v", in)
	}
	in = Awaken(in, testNow)
	if Decode(in).AwakeUntil != testNow+2419200 || Decode(in).Remaining(testNow+100) != 2419100 {
		t.Fatalf("awakened %+v", Decode(in))
	}
	if again := Awaken(in, testNow+500); again.Durability != in.Durability {
		t.Fatal("a second wear restarted the period")
	}
	expired, changed := Sync(in, testNow+2419200)
	if !changed || len(expired.Options) != 0 || Skills(expired, testNow+2419200) != nil || !Decode(expired).Expired(testNow+2419200) {
		t.Fatalf("expired %+v", expired)
	}
	if _, changed := Sync(expired, testNow+2419300); changed {
		t.Fatal("an expired instance changed again")
	}
	permanent := Make(uniqueM, 0, true, testNow)
	if !Decode(permanent).Permanent() || Decode(permanent).Expired(1<<31) || len(permanent.Options) != 3 {
		t.Fatalf("permanent %+v", permanent)
	}
}

/*
================
TestUpgradeBindingAndItsExpiry

A scroll's binding raises the effective level (options and skills) for
its seconds; when it runs out the level and options fall back.
================
*/
func TestUpgradeBindingAndItsExpiry(t *testing.T) {
	in := Awaken(Make(crimsonM, 0, false, testNow), testNow)
	if _, err := Upgrade(Make(crimsonM, 0, false, testNow), 3, 10800, testNow); !errors.Is(err, ErrNotAwakened) {
		t.Fatalf("not awakened: %v", err)
	}
	up, err := Upgrade(in, 6, 10800, testNow)
	if err != nil || EffectiveLevel(up, testNow) != 6 {
		t.Fatalf("upgrade: level %d, %v", EffectiveLevel(up, testNow), err)
	}
	if skills := Skills(up, testNow); len(skills) != 2 || skills[0] != 31148 || skills[1] != 31150 {
		t.Fatalf("level 6 skills %v", skills)
	}
	if optionValues(up.Options)[data.option(tagHPNA).ID] != 21 {
		t.Fatalf("level 6 options %v", optionValues(up.Options))
	}
	down, changed := Sync(up, testNow+10800)
	if !changed || EffectiveLevel(down, testNow+10800) != 0 || Decode(down).BindLevel != 0 || optionValues(down.Options)[data.option(tagHPNA).ID] != 15 {
		t.Fatalf("after the binding %+v", down)
	}
	based := up
	based.Plus = 8
	if EffectiveLevel(based, testNow) != MaxLevel {
		t.Fatal("base plus binding is not clamped to the tables")
	}
	if _, err := Upgrade(in, 3, 10800, testNow+2419200); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}

/*
================
TestExtend

An extension adds its period after the later of the current expiry and
now; it refuses a not awakened or a permanent instance.
================
*/
func TestExtend(t *testing.T) {
	in := Awaken(Make(crimsonM, 0, false, testNow), testNow)
	live, err := Extend(in, 2419200, testNow+100)
	if err != nil || Decode(live).AwakeUntil != testNow+2*2419200 {
		t.Fatalf("live: %+v %v", Decode(live), err)
	}
	expired, _ := Sync(in, testNow+3*2419200)
	renewed, err := Extend(expired, 2419200, testNow+3*2419200)
	if err != nil || Decode(renewed).AwakeUntil != testNow+4*2419200 || len(renewed.Options) == 0 {
		t.Fatalf("renewed: %+v %v", renewed, err)
	}
	if _, err := Extend(Make(crimsonM, 0, false, testNow), 1, testNow); !errors.Is(err, ErrNotAwakened) {
		t.Fatalf("not awakened: %v", err)
	}
	if _, err := Extend(Make(crimsonM, 0, true, testNow), 1, testNow); !errors.Is(err, ErrPermanent) {
		t.Fatalf("permanent: %v", err)
	}
}

/*
================
TestUpgradeLevelFollowsTheRatioTable

Every draw 0..100 (rand()%101): the levels come out in the
AVATAR_SPECIAL_OPTLV_RATIO shares, and the draw 100 past the table fails
with level 0's own share.
================
*/
func TestUpgradeLevelFollowsTheRatioTable(t *testing.T) {
	counts := make([]uint32, levelCount)
	for n := uint32(0); n <= 100; n++ {
		counts[UpgradeLevel(n)]++
	}
	want := []uint32{11, 15, 15, 15, 15, 15, 5, 4, 3, 2, 1}
	for level := range want {
		if counts[level] != want[level] {
			t.Fatalf("level shares %v, want %v", counts, want)
		}
	}
	if UpgradeLevel(101) != UpgradeLevel(0) {
		t.Fatal("the draw is not taken modulo 101")
	}
}

/*
================
TestUmduValuesAndOptionRows

UMDU's generator offers 2..10 %; the option rows carry their packed tags.
================
*/
func TestUmduValuesAndOptionRows(t *testing.T) {
	values := UmduValues()
	if len(values) != 5 || values[0] != 2 || values[4] != 10 {
		t.Fatalf("UMDU values %v", values)
	}
	rows := MagicOptionRows()
	if len(rows) != 4 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, row := range rows {
		if row.Tag>>24 == 0 || row.ParamName != "+" || row.Degree != 1 || row.ParamID <= 256 {
			t.Fatalf("row %+v", row)
		}
	}
	options := enterworld.NewTextdataMagicOptions(t.TempDir())
	if added, err := options.AddPortOnlyMagicOptions(rows); err != nil || added != 4 {
		t.Fatalf("added %d, %v", added, err)
	}
	if _, err := options.AddPortOnlyMagicOptions(rows); err == nil {
		t.Fatal("a second add was admitted")
	}
}
