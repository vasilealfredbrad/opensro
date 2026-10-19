/*
===========================================================================

playermap_test.go - the beta world map roster and its exclusions

===========================================================================
*/
package simulation

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPlayerMapHidesMarkedSessions

A MapHidden session (port-only: a battle royale member or participant)
appears in nobody's roster and receives an empty one; the others see
each other as before.
================
*/
func TestPlayerMapHidesMarkedSessions(t *testing.T) {
	push := &fakePusher{}
	ticker := &Ticker{Push: push, PlayerMap: true}
	sessions := []SessionSnapshot{
		{SessionID: "s1", CharacterID: 1, Appearance: &PeerAppearance{Name: "shown"}},
		{SessionID: "s2", CharacterID: 2, Appearance: &PeerAppearance{Name: "hidden"}, MapHidden: true},
		{SessionID: "s3", CharacterID: 3, Appearance: &PeerAppearance{Name: "other"}},
		{SessionID: "s4", CharacterID: 4},
	}
	state := &divisionTickState{}
	ticker.runPlayerMap(state, 10_000, sessions)
	if len(push.toSession) != 4 {
		t.Fatalf("%d roster pushes", len(push.toSession))
	}
	for _, pushed := range push.toSession {
		frame := pushed.frames[0]
		if frame.Opcode != wire.OpBetaPlayerMap {
			t.Fatalf("opcode %04x", frame.Opcode)
		}
		count := binary.LittleEndian.Uint16(frame.Payload)
		switch pushed.sessionID {
		case "s2":
			if count != 0 {
				t.Fatalf("the hidden session got %d markers", count)
			}
		default:
			if count != 2 {
				t.Fatalf("%s got %d markers, want the two shown", pushed.sessionID, count)
			}
			if string(frame.Payload) == "" || containsName(frame.Payload, "hidden") {
				t.Fatalf("%s sees the hidden session", pushed.sessionID)
			}
		}
	}
	// The roster is paced: nothing until the interval passes.
	ticker.runPlayerMap(state, 10_000+playerMapIntervalMs-1, sessions)
	if len(push.toSession) != 4 {
		t.Fatal("a roster before the interval")
	}
}

/*
================
containsName
================
*/
func containsName(payload []byte, name string) bool {
	for i := 0; i+len(name) <= len(payload); i++ {
		if string(payload[i:i+len(name)]) == name {
			return true
		}
	}
	return false
}
