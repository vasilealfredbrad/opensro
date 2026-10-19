/*
===========================================================================

battleroyale_airdrop_test.go - the 0x3FB6 v2 airdrop bytes

Port-only, not native (docs/battle-royale.md "Stage 9", 0x3FB6 version
2): exactly 31 bytes, version 2, state, phase, the flight line and the
landing point as little-endian f32 grid units, then etaMs as a
little-endian u32.

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestBattleRoyaleAirdropEncoding
================
*/
func TestBattleRoyaleAirdropEncoding(t *testing.T) {
	frame := BattleRoyaleAirdropFrame(BattleRoyaleAirdrop{State: BattleRoyaleAirdropIncoming, Phase: 3, AX: 1.5, AZ: -2, BX: 2, BZ: 0,
		EtaMs: 60000})
	want := []byte{2, 1, 3,
		0x00, 0x00, 0xC0, 0x3F, // ax 1.5
		0x00, 0x00, 0x00, 0xC0, // az -2
		0x00, 0x00, 0x00, 0x40, // bx 2
		0x00, 0x00, 0x00, 0x00, // bz 0
		0x00, 0x00, 0x00, 0x00, // x
		0x00, 0x00, 0x00, 0x00, // z
		0x60, 0xEA, 0x00, 0x00, // etaMs 60000
	}
	if frame.Opcode != 0x3FB6 || !bytes.Equal(frame.Payload, want) || len(frame.Payload) != BattleRoyaleAirdropBytes || BattleRoyaleAirdropBytes != 31 {
		t.Fatalf("frame %#04x % x", frame.Opcode, frame.Payload)
	}
	landed := EncodeBattleRoyaleAirdrop(BattleRoyaleAirdrop{State: BattleRoyaleAirdropLanded, Phase: 5, AX: 1.5, AZ: -2, BX: 2, X: -1, Z: 1})
	if len(landed) != 31 || landed[1] != 2 || landed[2] != 5 || !bytes.Equal(landed[19:27], []byte{0x00, 0x00, 0x80, 0xBF, 0x00, 0x00, 0x80, 0x3F}) ||
		!bytes.Equal(landed[27:], []byte{0, 0, 0, 0}) || !bytes.Equal(landed[3:19], want[3:19]) {
		t.Fatalf("landed % x", landed)
	}
	none := EncodeBattleRoyaleAirdrop(BattleRoyaleAirdrop{Phase: 1})
	if len(none) != 31 || none[0] != 2 || none[1] != 0 || none[2] != 1 {
		t.Fatalf("none % x", none)
	}
}
