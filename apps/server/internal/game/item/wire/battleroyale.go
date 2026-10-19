/*
===========================================================================

battleroyale.go - the battle royale state frame and build pick

Port-only, not native: no v1.150 client or v1.188 server knows these
opcodes; only this port's browser client consumes them (docs/
battle-royale.md, "Wire"). 0x3FB2 S->C carries a player's whole view of
the match; 0x7FB2 C->S picks a build; 0x3FB3 S->C is the scoreboard
(stage 2); 0x7FB4 C->S is the GM bot switch and 0x3FB4 S->C the bot
roster (stage 5, both version 2 since stage 7: difficulty and count);
0x3FB5 S->C lists the living bots' positions (stage 7); 0x3FB6 S->C
shows or removes the airdrop (stage 8b; version 2 since stage 9: its
flight line, landing point and time to landing). All integers
little-endian, coordinates in world grid units.

===========================================================================
*/

package wire

import (
	"errors"
	"math"
)

const (
	// OpBattleRoyaleState is the S->C match state frame.
	OpBattleRoyaleState uint16 = 0x3FB2
	// OpBattleRoyalePick is the C->S build pick.
	OpBattleRoyalePick uint16 = 0x7FB2
	// OpBattleRoyaleScoreboard is the S->C scoreboard (stage 2).
	OpBattleRoyaleScoreboard uint16 = 0x3FB3
	// BattleRoyaleScoreboardVersion is the scoreboard's version byte.
	BattleRoyaleScoreboardVersion uint8 = 1
	// battleRoyaleScoreboardMaxEntries bounds one frame (a u8 count).
	battleRoyaleScoreboardMaxEntries = 0xff
	// battleRoyaleScoreNameMaxBytes bounds an entry name: the client
	// refuses a longer one (character names hold at most 12 characters).
	battleRoyaleScoreNameMaxBytes = 64
	// battleRoyaleScoreEntryFixedBytes is one entry without its name.
	battleRoyaleScoreEntryFixedBytes = 2 + 2 + 1 + 1
	// OpBattleRoyaleBots is the C->S GM bot switch (stage 5).
	OpBattleRoyaleBots uint16 = 0x7FB4
	// OpBattleRoyaleBotRoster is the S->C bot roster (stage 5, v2 stage 7).
	OpBattleRoyaleBotRoster uint16 = 0x3FB4
	// BattleRoyaleBotRosterVersion is the roster's version byte.
	BattleRoyaleBotRosterVersion uint8 = 2
	// battleRoyaleBotRosterMaxEntries bounds one roster (a u16 count).
	battleRoyaleBotRosterMaxEntries = 0xffff
	// battleRoyaleBotRosterHeadBytes is the roster without its entries.
	battleRoyaleBotRosterHeadBytes = 1 + 1 + 1 + 2 + 2
	// battleRoyaleBotEntryFixedBytes is one roster entry without its name.
	battleRoyaleBotEntryFixedBytes = 4 + 2
	// battleRoyaleBotSwitchBytes is the stage-5 switch's body (one enabled
	// byte); battleRoyaleBotSwitchV2Bytes the stage-7 body.
	battleRoyaleBotSwitchBytes   = 1
	battleRoyaleBotSwitchV2Bytes = 1 + 1 + 2
	// The switch's difficulty byte and count range (docs/battle-royale.md
	// 0x7FB4 v2); a one-byte switch means Normal and the default count.
	BattleRoyaleBotEasy         uint8 = 0
	BattleRoyaleBotNormal       uint8 = 1
	BattleRoyaleBotHard         uint8 = 2
	BattleRoyaleBotDefaultCount       = 20
	BattleRoyaleBotMaxCount           = 200
	// OpBattleRoyaleBotPositions is the S->C living bots' positions
	// (stage 7), every 2 s.
	OpBattleRoyaleBotPositions uint16 = 0x3FB5
	// BattleRoyaleBotPositionsVersion is its version byte.
	BattleRoyaleBotPositionsVersion uint8 = 1
	// battleRoyaleBotPositionBytes is one position entry: gid, x, z, level.
	battleRoyaleBotPositionBytes = 4 + 4 + 4 + 1
	// BattleRoyaleNoticeChannel is the 0x3667 channel of the kill feed
	// (port-only, like JobKillNoticeChannel).
	BattleRoyaleNoticeChannel = 0x82

	// BattleRoyaleStateVersion is the frame's version byte.
	BattleRoyaleStateVersion uint8 = 1

	// The state byte.
	BattleRoyaleStateNone      uint8 = 0
	BattleRoyaleStateLobby     uint8 = 1
	BattleRoyaleStateCountdown uint8 = 2
	BattleRoyaleStateRunning   uint8 = 3
	BattleRoyaleStateFinished  uint8 = 4

	// The role byte.
	BattleRoyaleRoleSpectator  uint8 = 0
	BattleRoyaleRoleMember     uint8 = 1
	BattleRoyaleRoleAlive      uint8 = 2
	BattleRoyaleRoleEliminated uint8 = 3

	// The zone stage byte.
	BattleRoyaleStageWaiting   uint8 = 0
	BattleRoyaleStageShrinking uint8 = 1
	BattleRoyaleStageFinal     uint8 = 2

	// battleRoyaleStateFixedBytes is the frame without the winner name.
	battleRoyaleStateFixedBytes = 1 + 1 + 1 + 1 + 12 + 1 + 1 + 4 + 1 + 1 + 1 + 4 + 12 + 12 + 4 + 2
	// battleRoyaleWinnerMaxBytes bounds the winner name (a u16 length).
	battleRoyaleWinnerMaxBytes = 0xffff
	// battleRoyalePickBytes is the pick's body: one build byte.
	battleRoyalePickBytes = 1
)

// ErrBattleRoyalePick is the refusal of a malformed 0x7FB2 body.
var ErrBattleRoyalePick = errors.New("wire: malformed battle royale pick")

// ErrBattleRoyaleBots is the refusal of a malformed 0x7FB4 body.
var ErrBattleRoyaleBots = errors.New("wire: malformed battle royale bot switch")

/*
================
BattleRoyaleCircle

A circle in world grid units, as the frame carries it.
================
*/
type BattleRoyaleCircle struct {
	X, Z, Radius float32
}

/*
================
BattleRoyaleState

One player's view of the match (docs/battle-royale.md, 0x3FB2).
================
*/
type BattleRoyaleState struct {
	State        uint8
	Role         uint8
	Build        uint8
	Lobby        BattleRoyaleCircle
	Members      uint8
	Alive        uint8
	CountdownMs  uint32
	Phase        uint8
	Phases       uint8
	Stage        uint8
	StageMs      uint32
	Current      BattleRoyaleCircle
	Next         BattleRoyaleCircle
	DamagePerSec float32
	Winner       string
}

/*
================
BattleRoyaleState.Encode
================
*/
func (s BattleRoyaleState) Encode() []byte {
	winner := []byte(s.Winner)
	if len(winner) > battleRoyaleWinnerMaxBytes {
		winner = winner[:battleRoyaleWinnerMaxBytes]
	}
	w := NewWriter(battleRoyaleStateFixedBytes + len(winner))
	w.U8(BattleRoyaleStateVersion).U8(s.State).U8(s.Role).U8(s.Build)
	writeBattleRoyaleCircle(w, s.Lobby)
	w.U8(s.Members).U8(s.Alive).U32(s.CountdownMs)
	w.U8(s.Phase).U8(s.Phases).U8(s.Stage).U32(s.StageMs)
	writeBattleRoyaleCircle(w, s.Current)
	writeBattleRoyaleCircle(w, s.Next)
	w.U32(math.Float32bits(s.DamagePerSec))
	w.U16(uint16(len(winner))).Bytes(winner)
	return w.Payload()
}

/*
================
BattleRoyaleStateFrame
================
*/
func BattleRoyaleStateFrame(s BattleRoyaleState) Frame {
	return Frame{Opcode: OpBattleRoyaleState, Payload: s.Encode()}
}

/*
================
writeBattleRoyaleCircle
================
*/
func writeBattleRoyaleCircle(w *Writer, c BattleRoyaleCircle) {
	w.U32(math.Float32bits(c.X)).U32(math.Float32bits(c.Z)).U32(math.Float32bits(c.Radius))
}

/*
================
DecodeBattleRoyalePick

The build byte of a 0x7FB2 body.
================
*/
func DecodeBattleRoyalePick(payload []byte) (uint8, error) {
	if len(payload) != battleRoyalePickBytes {
		return 0, ErrBattleRoyalePick
	}
	return payload[0], nil
}

/*
================
BattleRoyaleScore

One scoreboard entry (docs/battle-royale.md, 0x3FB3): Placement is 0
while alive, else 1 for the winner, 2 for the second, ...; Self marks the
receiving player's own entry.
================
*/
type BattleRoyaleScore struct {
	Name      string
	Kills     uint16
	Placement uint8
	Self      bool
}

/*
================
EncodeBattleRoyaleScoreboard

[u8 version][u8 final][u8 count] then per entry [u16 nameLen][name][u16
kills][u8 placement][u8 self]. Entries past 255 are left out; a name past
64 bytes is cut.
================
*/
func EncodeBattleRoyaleScoreboard(final bool, entries []BattleRoyaleScore) []byte {
	if len(entries) > battleRoyaleScoreboardMaxEntries {
		entries = entries[:battleRoyaleScoreboardMaxEntries]
	}
	size := 3
	for _, entry := range entries {
		size += battleRoyaleScoreEntryFixedBytes + min(len(entry.Name), battleRoyaleScoreNameMaxBytes)
	}
	w := NewWriter(size)
	w.U8(BattleRoyaleScoreboardVersion).U8(boolByte(final)).U8(uint8(len(entries)))
	for _, entry := range entries {
		name := []byte(entry.Name)
		if len(name) > battleRoyaleScoreNameMaxBytes {
			name = name[:battleRoyaleScoreNameMaxBytes]
		}
		w.U16(uint16(len(name))).Bytes(name).U16(entry.Kills).U8(entry.Placement).U8(boolByte(entry.Self))
	}
	return w.Payload()
}

/*
================
BattleRoyaleScoreboardFrame
================
*/
func BattleRoyaleScoreboardFrame(final bool, entries []BattleRoyaleScore) Frame {
	return Frame{Opcode: OpBattleRoyaleScoreboard, Payload: EncodeBattleRoyaleScoreboard(final, entries)}
}

/*
================
BattleRoyaleNotificationFrame

NotificationFrame on BattleRoyaleNoticeChannel: the kill feed.
================
*/
func BattleRoyaleNotificationFrame(message string) Frame {
	return notificationFrameOn(BattleRoyaleNoticeChannel, message)
}

/*
================
BattleRoyaleBotSwitch

A decoded 0x7FB4: the switch, the difficulty (0 easy, 1 normal, 2 hard)
and the bot count (1..200).
================
*/
type BattleRoyaleBotSwitch struct {
	Enabled    bool
	Difficulty uint8
	Count      int
}

/*
================
DecodeBattleRoyaleBots

A 0x7FB4 body: version 2 [u8 enabled][u8 difficulty][u16 count], or the
stage-5 one byte [u8 enabled] (normal, 20 bots). enabled above 1,
difficulty above 2, a count outside 1..200 or any other length is
malformed.
================
*/
func DecodeBattleRoyaleBots(payload []byte) (BattleRoyaleBotSwitch, error) {
	switch len(payload) {
	case battleRoyaleBotSwitchBytes:
		if payload[0] > 1 {
			return BattleRoyaleBotSwitch{}, ErrBattleRoyaleBots
		}
		return BattleRoyaleBotSwitch{Enabled: payload[0] == 1, Difficulty: BattleRoyaleBotNormal, Count: BattleRoyaleBotDefaultCount}, nil
	case battleRoyaleBotSwitchV2Bytes:
		r := NewReader(payload)
		enabled, _ := r.U8()
		difficulty, _ := r.U8()
		count, _ := r.U16()
		if enabled > 1 || difficulty > BattleRoyaleBotHard || count < 1 || count > BattleRoyaleBotMaxCount {
			return BattleRoyaleBotSwitch{}, ErrBattleRoyaleBots
		}
		return BattleRoyaleBotSwitch{Enabled: enabled == 1, Difficulty: difficulty, Count: int(count)}, nil
	}
	return BattleRoyaleBotSwitch{}, ErrBattleRoyaleBots
}

/*
================
BattleRoyaleBot

One roster entry (docs/battle-royale.md, 0x3FB4): the bot's world object
id and its name.
================
*/
type BattleRoyaleBot struct {
	GID  uint32
	Name string
}

/*
================
BattleRoyaleBotRoster

What a 0x3FB4 carries besides its entries: the lobby's switch,
difficulty and configured count.
================
*/
type BattleRoyaleBotRoster struct {
	Enabled    bool
	Difficulty uint8
	Configured int
}

/*
================
EncodeBattleRoyaleBotRoster

Version 2: [u8 2][u8 botsEnabled][u8 difficulty][u16 configured][u16
count] then per bot [u32 gid][u16 nameLen][name]. The client treats a
difficulty above 2 or a configured count outside 1..200 as a protocol
error, so both are always sent valid: an unknown difficulty goes out as
normal and an out-of-range count as the default 20 (whatever the switch
says). A name past 64 bytes is cut, as on the scoreboard.
================
*/
func EncodeBattleRoyaleBotRoster(roster BattleRoyaleBotRoster, bots []BattleRoyaleBot) []byte {
	if len(bots) > battleRoyaleBotRosterMaxEntries {
		bots = bots[:battleRoyaleBotRosterMaxEntries]
	}
	difficulty := roster.Difficulty
	if difficulty > BattleRoyaleBotHard {
		difficulty = BattleRoyaleBotNormal
	}
	configured := roster.Configured
	if configured < 1 || configured > BattleRoyaleBotMaxCount {
		configured = BattleRoyaleBotDefaultCount
	}
	size := battleRoyaleBotRosterHeadBytes
	for _, bot := range bots {
		size += battleRoyaleBotEntryFixedBytes + min(len(bot.Name), battleRoyaleScoreNameMaxBytes)
	}
	w := NewWriter(size)
	w.U8(BattleRoyaleBotRosterVersion).U8(boolByte(roster.Enabled)).U8(difficulty).U16(uint16(configured)).U16(uint16(len(bots)))
	for _, bot := range bots {
		name := []byte(bot.Name)
		if len(name) > battleRoyaleScoreNameMaxBytes {
			name = name[:battleRoyaleScoreNameMaxBytes]
		}
		w.U32(bot.GID).U16(uint16(len(name))).Bytes(name)
	}
	return w.Payload()
}

/*
================
BattleRoyaleBotRosterFrame
================
*/
func BattleRoyaleBotRosterFrame(roster BattleRoyaleBotRoster, bots []BattleRoyaleBot) Frame {
	return Frame{Opcode: OpBattleRoyaleBotRoster, Payload: EncodeBattleRoyaleBotRoster(roster, bots)}
}

/*
================
BattleRoyaleBotPosition

One 0x3FB5 entry: a living bot, its grid position and its level.
================
*/
type BattleRoyaleBotPosition struct {
	GID   uint32
	X, Z  float32
	Level uint8
}

/*
================
EncodeBattleRoyaleBotPositions

[u8 1][u16 count] then per bot [u32 gid][f32 x][f32 z][u8 level].
================
*/
func EncodeBattleRoyaleBotPositions(bots []BattleRoyaleBotPosition) []byte {
	if len(bots) > battleRoyaleBotRosterMaxEntries {
		bots = bots[:battleRoyaleBotRosterMaxEntries]
	}
	w := NewWriter(1 + 2 + len(bots)*battleRoyaleBotPositionBytes)
	w.U8(BattleRoyaleBotPositionsVersion).U16(uint16(len(bots)))
	for _, bot := range bots {
		w.U32(bot.GID).U32(math.Float32bits(bot.X)).U32(math.Float32bits(bot.Z)).U8(bot.Level)
	}
	return w.Payload()
}

/*
================
BattleRoyaleBotPositionsFrame
================
*/
func BattleRoyaleBotPositionsFrame(bots []BattleRoyaleBotPosition) Frame {
	return Frame{Opcode: OpBattleRoyaleBotPositions, Payload: EncodeBattleRoyaleBotPositions(bots)}
}

//============================================================================

const (
	// OpBattleRoyaleAirdrop is the S->C airdrop frame (stage 8b, v2 stage 9).
	OpBattleRoyaleAirdrop uint16 = 0x3FB6
	// BattleRoyaleAirdropVersion is its version byte.
	BattleRoyaleAirdropVersion uint8 = 2
	// BattleRoyaleAirdropBytes is its exact length: version, state,
	// phase, the line (ax, az, bx, bz), the landing point (x, z), etaMs.
	BattleRoyaleAirdropBytes = 1 + 1 + 1 + 16 + 8 + 4

	// The airdrop states: nothing to show, the flight line (incoming),
	// the landed marker.
	BattleRoyaleAirdropNone     uint8 = 0
	BattleRoyaleAirdropIncoming uint8 = 1
	BattleRoyaleAirdropLanded   uint8 = 2
)

/*
================
BattleRoyaleAirdrop

One 0x3FB6 v2 frame: the state, the zone phase the airdrop belongs to,
its flight line from (AX, AZ) to (BX, BZ), its landing point (X, Z;
the sender zeroes it before landing) and the milliseconds left until it
lands (incoming only), all in grid units.
================
*/
type BattleRoyaleAirdrop struct {
	State          uint8
	Phase          uint8
	AX, AZ, BX, BZ float32
	X, Z           float32
	EtaMs          uint32
}

/*
================
EncodeBattleRoyaleAirdrop

[u8 2][u8 state][u8 phase][f32 ax][f32 az][f32 bx][f32 bz][f32 x][f32 z]
[u32 etaMs]: 31 bytes.
================
*/
func EncodeBattleRoyaleAirdrop(a BattleRoyaleAirdrop) []byte {
	w := NewWriter(BattleRoyaleAirdropBytes)
	w.U8(BattleRoyaleAirdropVersion).U8(a.State).U8(a.Phase)
	for _, f := range []float32{a.AX, a.AZ, a.BX, a.BZ, a.X, a.Z} {
		w.U32(math.Float32bits(f))
	}
	w.U32(a.EtaMs)
	return w.Payload()
}

/*
================
BattleRoyaleAirdropFrame
================
*/
func BattleRoyaleAirdropFrame(a BattleRoyaleAirdrop) Frame {
	return Frame{Opcode: OpBattleRoyaleAirdrop, Payload: EncodeBattleRoyaleAirdrop(a)}
}
