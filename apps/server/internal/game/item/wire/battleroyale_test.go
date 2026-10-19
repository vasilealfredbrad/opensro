/*
===========================================================================

battleroyale_test.go - the battle royale frames byte for byte

Port-only, not native: the layouts docs/battle-royale.md fixes for 0x3FB2,
0x7FB2, 0x3FB3, 0x7FB4 and 0x3FB4.

===========================================================================
*/

package wire

import (
	"bytes"
	"testing"
)

/*
================
TestBattleRoyaleScoreboardEncoding

[u8 1][u8 final][u8 count] then per entry [u16 len][name][u16 kills][u8
placement][u8 self], little-endian.
================
*/
func TestBattleRoyaleScoreboardEncoding(t *testing.T) {
	got := EncodeBattleRoyaleScoreboard(true, []BattleRoyaleScore{
		{Name: "Gina", Kills: 3, Placement: 1, Self: true},
		{Name: "test", Kills: 300, Placement: 2},
	})
	want := []byte{1, 1, 2,
		4, 0, 'G', 'i', 'n', 'a', 3, 0, 1, 1,
		4, 0, 't', 'e', 's', 't', 0x2c, 0x01, 2, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("scoreboard\n got %x\nwant %x", got, want)
	}
	if empty := EncodeBattleRoyaleScoreboard(false, nil); !bytes.Equal(empty, []byte{1, 0, 0}) {
		t.Fatalf("empty scoreboard %x", empty)
	}
	if frame := BattleRoyaleScoreboardFrame(false, nil); frame.Opcode != OpBattleRoyaleScoreboard || OpBattleRoyaleScoreboard != 0x3FB3 {
		t.Fatalf("frame %+v", frame)
	}
	many := make([]BattleRoyaleScore, 300)
	if got := EncodeBattleRoyaleScoreboard(false, many); got[2] != 0xff || len(got) != 3+255*battleRoyaleScoreEntryFixedBytes {
		t.Fatalf("300 entries: count %d, %d bytes", got[2], len(got))
	}
	long := EncodeBattleRoyaleScoreboard(false, []BattleRoyaleScore{{Name: string(make([]byte, 100))}})
	if long[3] != 64 || long[4] != 0 || len(long) != 3+battleRoyaleScoreEntryFixedBytes+64 {
		t.Fatalf("a long name was not cut to 64 bytes: %d", len(long))
	}
}

/*
================
TestBattleRoyalePickDecoding
================
*/
func TestBattleRoyalePickDecoding(t *testing.T) {
	if build, err := DecodeBattleRoyalePick([]byte{13}); err != nil || build != 13 {
		t.Fatalf("pick %d %v", build, err)
	}
	for _, bad := range [][]byte{nil, {}, {1, 2}} {
		if _, err := DecodeBattleRoyalePick(bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

/*
================
TestBattleRoyaleStateLayout

The fixed head is 57 bytes plus the winner name; the version leads.
================
*/
func TestBattleRoyaleStateLayout(t *testing.T) {
	payload := BattleRoyaleState{State: BattleRoyaleStateFinished, Role: BattleRoyaleRoleAlive, Build: 3, Winner: "ab"}.Encode()
	if len(payload) != battleRoyaleStateFixedBytes+2 || payload[0] != BattleRoyaleStateVersion || payload[1] != 4 || payload[2] != 2 || payload[3] != 3 {
		t.Fatalf("state frame %x", payload)
	}
	if !bytes.Equal(payload[len(payload)-4:], []byte{2, 0, 'a', 'b'}) {
		t.Fatalf("winner tail %x", payload[len(payload)-4:])
	}
}

/*
================
TestBattleRoyaleBotRosterEncoding

Version 2: [u8 2][u8 botsEnabled][u8 difficulty][u16 configured][u16
count] then per bot [u32 gid][u16 len][name], little-endian.
================
*/
func TestBattleRoyaleBotRosterEncoding(t *testing.T) {
	got := EncodeBattleRoyaleBotRoster(BattleRoyaleBotRoster{Enabled: true, Difficulty: BattleRoyaleBotHard, Configured: 150},
		[]BattleRoyaleBot{{GID: 0x01020304, Name: "Ares"}, {GID: 7, Name: "Mi"}})
	want := []byte{2, 1, 2, 150, 0, 2, 0,
		4, 3, 2, 1, 4, 0, 'A', 'r', 'e', 's',
		7, 0, 0, 0, 2, 0, 'M', 'i'}
	if !bytes.Equal(got, want) {
		t.Fatalf("roster got %x want %x", got, want)
	}
	if frame := BattleRoyaleBotRosterFrame(BattleRoyaleBotRoster{}, nil); frame.Opcode != OpBattleRoyaleBotRoster || OpBattleRoyaleBotRoster != 0x3FB4 || OpBattleRoyaleBots != 0x7FB4 {
		t.Fatalf("frame %+v", frame)
	}
	long := EncodeBattleRoyaleBotRoster(BattleRoyaleBotRoster{Configured: 20}, []BattleRoyaleBot{{Name: string(make([]byte, 100))}})
	if long[battleRoyaleBotRosterHeadBytes+4] != 64 || len(long) != battleRoyaleBotRosterHeadBytes+battleRoyaleBotEntryFixedBytes+64 {
		t.Fatalf("a long name was not cut to 64 bytes: %d", len(long))
	}
	many := make([]BattleRoyaleBot, 300)
	if r := NewReader(EncodeBattleRoyaleBotRoster(BattleRoyaleBotRoster{Configured: 200}, many)[5:]); true {
		if n, _ := r.U16(); n != 300 {
			t.Fatalf("a roster of 300 bots counts %d", n)
		}
	}
}

/*
================
TestBattleRoyaleBotRosterAlwaysValid

The client ends the session on a roster whose difficulty is above 2 or
whose configured count is outside 1..200: the off switch, a zero value
and out-of-range values all go out as a valid difficulty and count (the
default normal / 20 when nothing valid was stored).
================
*/
func TestBattleRoyaleBotRosterAlwaysValid(t *testing.T) {
	for _, c := range []struct {
		roster     BattleRoyaleBotRoster
		difficulty uint8
		configured uint16
	}{
		{BattleRoyaleBotRoster{}, BattleRoyaleBotEasy, BattleRoyaleBotDefaultCount},
		{BattleRoyaleBotRoster{Enabled: false, Difficulty: BattleRoyaleBotNormal, Configured: BattleRoyaleBotDefaultCount}, BattleRoyaleBotNormal, 20},
		{BattleRoyaleBotRoster{Enabled: false, Difficulty: BattleRoyaleBotHard, Configured: 77}, BattleRoyaleBotHard, 77},
		{BattleRoyaleBotRoster{Enabled: true, Difficulty: 9, Configured: 201}, BattleRoyaleBotNormal, 20},
		{BattleRoyaleBotRoster{Enabled: true, Difficulty: 0, Configured: -5}, BattleRoyaleBotEasy, 20},
	} {
		payload := EncodeBattleRoyaleBotRoster(c.roster, nil)
		r := NewReader(payload)
		version, _ := r.U8()
		enabled, _ := r.U8()
		difficulty, _ := r.U8()
		configured, _ := r.U16()
		count, _ := r.U16()
		if version != 2 || enabled != boolByte(c.roster.Enabled) || difficulty != c.difficulty || configured != c.configured ||
			configured < 1 || configured > BattleRoyaleBotMaxCount || difficulty > BattleRoyaleBotHard || count != 0 || r.Done() != nil {
			t.Fatalf("%+v went out as %x", c.roster, payload)
		}
	}
}

/*
================
TestBattleRoyaleBotSwitchDecoding

0x7FB4 v2 [u8 enabled][u8 difficulty][u16 count]; the stage-5 one byte
is enabled with normal / 20; anything else is refused.
================
*/
func TestBattleRoyaleBotSwitchDecoding(t *testing.T) {
	for _, c := range []struct {
		payload []byte
		want    BattleRoyaleBotSwitch
		ok      bool
	}{
		{[]byte{1}, BattleRoyaleBotSwitch{true, BattleRoyaleBotNormal, 20}, true},
		{[]byte{0}, BattleRoyaleBotSwitch{false, BattleRoyaleBotNormal, 20}, true},
		{[]byte{1, 2, 200, 0}, BattleRoyaleBotSwitch{true, BattleRoyaleBotHard, 200}, true},
		{[]byte{0, 0, 1, 0}, BattleRoyaleBotSwitch{false, BattleRoyaleBotEasy, 1}, true},
		{[]byte{2}, BattleRoyaleBotSwitch{}, false},
		{nil, BattleRoyaleBotSwitch{}, false},
		{[]byte{1, 0}, BattleRoyaleBotSwitch{}, false},
		{[]byte{1, 3, 20, 0}, BattleRoyaleBotSwitch{}, false},
		{[]byte{1, 1, 0, 0}, BattleRoyaleBotSwitch{}, false},
		{[]byte{1, 1, 201, 0}, BattleRoyaleBotSwitch{}, false},
		{[]byte{2, 1, 20, 0}, BattleRoyaleBotSwitch{}, false},
		{[]byte{1, 1, 20, 0, 0}, BattleRoyaleBotSwitch{}, false},
	} {
		got, err := DecodeBattleRoyaleBots(c.payload)
		if (err == nil) != c.ok || got != c.want {
			t.Fatalf("switch %x: %+v %v", c.payload, got, err)
		}
	}
}

/*
================
TestBattleRoyaleBotPositionsEncoding

[u8 1][u16 count] then per bot [u32 gid][f32 x][f32 z][u8 level].
================
*/
func TestBattleRoyaleBotPositionsEncoding(t *testing.T) {
	got := EncodeBattleRoyaleBotPositions([]BattleRoyaleBotPosition{{GID: 0x01020304, X: 1, Z: -2, Level: 30}})
	want := []byte{1, 1, 0, 4, 3, 2, 1, 0, 0, 0x80, 0x3f, 0, 0, 0, 0xc0, 30}
	if !bytes.Equal(got, want) {
		t.Fatalf("positions got %x want %x", got, want)
	}
	if frame := BattleRoyaleBotPositionsFrame(nil); frame.Opcode != 0x3FB5 || !bytes.Equal(frame.Payload, []byte{1, 0, 0}) {
		t.Fatalf("empty positions %+v", frame)
	}
}
