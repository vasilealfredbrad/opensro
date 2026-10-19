/*
===========================================================================

headless_test.go - a character no session drives (port-only)

Ticker.Headless characters (the battle royale bots) join a tick as peers
and movers: a real viewer spawns them through the ordinary 0x30D7 peer
row and follows their moves, while nothing is ever built for them as a
viewer.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestTickerHeadlessIsAPeerNeverAViewer
================
*/
func TestTickerHeadlessIsAPeerNeverAViewer(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source := &fakeSource{sessions: []SessionSnapshot{peerSession("s1", "DIV_A", 1, "Viewer")}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	bot := peerSession("~bot/Ares", "DIV_A", 99001, "Ares")
	bot.Appearance.Equipment = []wire.PlayerEquipItem{{RefObjID: 3638, TypeFlags: wire.PackTypeFlags(3, 1, 6, 2)}}
	ticker.Headless = func() []SessionSnapshot { return []SessionSnapshot{bot} }

	ticker.RunTick(startMs)
	// The bot walks: the viewer follows the move like any peer's.
	from := bot.World.Spawn
	bot.World.Spawn.X += 40
	bot.World.MoveSegment = &MoveSegment{From: from, StartedAtMs: startMs + 100, ArrivesAtMs: startMs + 5000}
	ticker.RunTick(startMs + 200)

	rows := peerFramesTo(push, "s1", wire.OpSingleObjectSpawn)
	if len(rows) != 1 {
		t.Fatalf("the viewer received %d spawn rows, want the bot's one", len(rows))
	}
	decoded, err := wire.DecodePlayerSpawnRow(rows[0], map[uint32]uint16{3638: wire.PackTypeFlags(3, 1, 6, 2)}, true)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Gid != PlayerObjectID(99001) || decoded.Name != "Ares" || len(decoded.Equipment) != 1 || decoded.Equipment[0].RefObjID != 3638 {
		t.Fatalf("the bot spawned as %+v", decoded)
	}
	if len(peerFramesTo(push, "s1", wire.OpObjectSourceMove)) == 0 {
		t.Fatal("the viewer never saw the bot move")
	}
	for _, pushed := range push.toSession {
		if pushed.sessionID == bot.SessionID {
			t.Fatalf("frames were built for the headless bot: %+v", pushed.frames)
		}
	}
}
