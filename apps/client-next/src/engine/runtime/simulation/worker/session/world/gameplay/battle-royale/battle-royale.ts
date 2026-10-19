/*
===========================================================================

battle-royale.ts - the Battle Royale match as this player sees it (port-only 0x3FB2, not native)

When the operator enables SRO_BATTLE_ROYALE, the server pushes the match
state whenever this player's view of it changes, the scoreboard (0x3FB3)
on each elimination and at the end, the bot roster (0x3FB4) and the
living bots' positions (0x3FB5, every two seconds of a running match), the
airdrop marker (0x3FB6, Stage 8b), and takes build picks in 0x7FB2 and a GM's bot settings in 0x7FB4
(docs/battle-royale.md). This owner keeps the latest state with its
deadlines on the simulation clock and the latest scoreboard, keeps the bot
roster with every bot name seen this match (the results window marks a dead
bot's row too), the latest bot positions while the match runs and the
marked airdrop until the server removes it or the match ends, sends a
pick or the bot settings only while the server would accept them, and
schedules the native countdown sounds. Nothing arrives while the flag is off, so the owner stays empty and
the client shows nothing.

The pick is the player's choice, not one frame: the owner remembers it and
sends it again while the server's frames name another build. The server
answers a pick with the stored build, but it drops a pick it refuses
without an answer, and a member who steps out of the lobby circle and back
in is stored with the default build again; either way the match would
start with a build the player did not choose. A pick made before the first
member frame is held and sent when that frame arrives.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import {
	type BattleRoyaleBotPositions,
	type BattleRoyaleBots,
	type BattleRoyaleBotSettings,
	battleRoyaleBotSwitchFrame,
	battleRoyaleCanPick,
	battleRoyaleCanSwitchBots,
	battleRoyalePickFrame,
	type BattleRoyaleScoreboard,
	type BattleRoyaleState,
	decodeBattleRoyaleBotPositions,
	decodeBattleRoyaleBots,
	decodeBattleRoyaleScoreboard,
	decodeBattleRoyaleState,
	OP_BATTLE_ROYALE_BOT_POSITIONS,
	OP_BATTLE_ROYALE_BOTS,
	OP_BATTLE_ROYALE_SCOREBOARD,
	OP_BATTLE_ROYALE_STATE
} from "@/engine/foundation/gameplay/battle-royale";
import {
	type BattleRoyaleAirdrop,
	decodeBattleRoyaleAirdrop,
	OP_BATTLE_ROYALE_AIRDROP
} from "@/engine/foundation/gameplay/battle-royale-airdrop";
import type { UiSoundHandle } from "@/engine/foundation/ui/sound-catalog";

// countdown.wav runs 4.9 s: it starts five seconds before the drop and
// ends on it. A countdown learned later than this into its last five
// seconds stays silent rather than overrunning the start.
const COUNTDOWN_CUE_MS = 5000;
const COUNTDOWN_CUE_LATEST_MS = 4000;
// A sent pick the server's frames still contradict this long after it left
// goes again (the answer is one round trip away), at most this many times
// a choice; a confirming frame or a new membership grants the tries again.
// INFERENCE: the spec names no retry; a second covers any round trip the
// countdown leaves room for, and the cap keeps a server that keeps
// refusing from hearing the pick forever.
const PICK_RETRY_MS = 1000;
const MAX_PICK_RESENDS = 5;

/*
================
createBattleRoyale
================
*/
export function createBattleRoyale() {
	let state: BattleRoyaleState | undefined;
	let scoreboard: BattleRoyaleScoreboard | undefined;
	let bots: BattleRoyaleBots | undefined;
	let positions: BattleRoyaleBotPositions | undefined;
	let airdrop: BattleRoyaleAirdrop | undefined;
	let sequence = 0;
	// The countdown deadline whose five-second cue and start cue were taken,
	// and a cue a frame raised for the next step.
	let countdownCued = -1, startCued = -1;
	let pending: UiSoundHandle | null = null;
	// The build the player last chose (0: none yet), when a pick last left,
	// and the sends the owner made of it on its own since the player chose
	// or the server confirmed.
	let chosen = 0, pickSentAtMs = -Infinity, pickResends = 0;
	return {
		/*
================
receive

Consumes a match, scoreboard, bot roster, bot position or airdrop frame;
false for any other opcode. A malformed frame is a protocol error, never a
partial match. Positions are kept only while a match runs. The airdrop is
kept whatever the state (the server may mark the first phase's drop just
before the frame that starts the match) and dropped when the match ends.
================
		*/
		receive( frame: WireFrame, now: number ): boolean {
			if ( frame.opcode === OP_BATTLE_ROYALE_SCOREBOARD ) {
				scoreboard = decodeBattleRoyaleScoreboard( frame.payload, ++sequence );
				return true;
			}
			if ( frame.opcode === OP_BATTLE_ROYALE_BOTS ) {
				const roster = decodeBattleRoyaleBots( frame.payload ), names = [ ...bots?.names ?? [] ];
				for ( const bot of roster.bots ) if ( !names.includes( bot.name ) ) names.push( bot.name );
				bots = { ...roster, names, sequence: ++sequence };
				return true;
			}
			if ( frame.opcode === OP_BATTLE_ROYALE_BOT_POSITIONS ) {
				const decoded = decodeBattleRoyaleBotPositions( frame.payload, now, ++sequence );
				positions = state?.phase === "running" ? decoded : undefined;
				return true;
			}
			if ( frame.opcode === OP_BATTLE_ROYALE_AIRDROP ) {
				airdrop = decodeBattleRoyaleAirdrop( frame.payload, now ) ?? undefined;
				return true;
			}
			if ( frame.opcode !== OP_BATTLE_ROYALE_STATE ) return false;
			const previous = state;
			state = decodeBattleRoyaleState( frame.payload, now, ++sequence ) ?? undefined;
			const gathering = state?.phase === "lobby" || state?.phase === "countdown";
			// State 0 ends the match: no bot stands in the world or switches on
			// any more, but this match's bot names outlive it with the results.
			// A new match's lobby retires the last match's scoreboard and bot
			// names; the switch and any roster that came first stay.
			if ( gathering ) scoreboard = undefined;
			if ( state?.phase !== "running" ) positions = undefined;
			// State 0, the result and a new lobby end the match's airdrop.
			if ( !state || state.phase === "finished" || state.phase === "lobby" ) airdrop = undefined;
			if ( !state && bots ) {
				bots = bots.names.length ?
					{ ...bots, enabled: false, bots: [], sequence: ++sequence } :
					undefined;
			} else if ( gathering && bots && previous?.phase !== "lobby" && previous?.phase !== "countdown" ) {
				bots = { ...bots, names: bots.bots.map( bot => bot.name ), sequence: ++sequence };
			}
			if ( battleRoyaleCanPick( state ) ) {
				// A fresh membership was stored with the default build: the
				// remembered choice goes at once, with its tries renewed.
				if ( !battleRoyaleCanPick( previous ) ) {
					pickSentAtMs = -Infinity;
					pickResends = 0;
				} else if ( state!.build === chosen ) pickResends = 0;
			}
			// The drop arrived before the local countdown ran out (clock skew):
			// the start still sounds once.
			if (
				previous?.phase === "countdown" && state?.phase === "running" &&
				startCued !== previous.countdownEndsAtMs
			) {
				startCued = previous.countdownEndsAtMs;
				pending = "SND_COUNTSTART";
			}
			if ( state?.phase === "finished" && previous?.phase !== "finished" ) pending = "SND_COUNTEND";
			return true;
		},
		/*
================
state
================
		*/
		state: () => state,
		/*
================
scoreboard
================
		*/
		scoreboard: () => scoreboard,
		/*
================
bots

The latest bot roster with this match's bot names; undefined before the
first 0x3FB4. After the match ends (state 0) only the names remain, for
the results window, until a new lobby or session.
================
		*/
		bots: () => bots,
		/*
================
botPositions

The latest 0x3FB5 of the running match with its receipt time; undefined
outside a running match and before the first frame.
================
		*/
		botPositions: () => positions,
		/*
================
airdrop

The marked airdrop (0x3FB6 active) with its receipt time; undefined
before the first mark, after the server removes it, after the match ends
and in a new session.
================
		*/
		airdrop: () => airdrop,
		/*
================
switchBots

A GM asked for the lobby's bot settings (on or off, difficulty, count):
0x7FB4 version 2, or null while the server would refuse it (not a GM, not
a lobby member, no lobby or countdown running). The roster frame the
server answers with shows the settings; nothing is assumed before it.
Settings the wire cannot carry are a caller defect and throw.
================
		*/
		switchBots( settings: BattleRoyaleBotSettings, gm: boolean ): WireFrame | null {
			if ( !battleRoyaleCanSwitchBots( state, gm ) ) return null;
			return battleRoyaleBotSwitchFrame( settings );
		},
		/*
================
step

The sound due at now, if any: the countdown's last five seconds, the
drop, the result.
================
		*/
		step( now: number ): UiSoundHandle | null {
			if ( state?.phase === "countdown" ) {
				const left = state.countdownEndsAtMs - now;
				if ( left <= COUNTDOWN_CUE_MS && countdownCued !== state.countdownEndsAtMs ) {
					countdownCued = state.countdownEndsAtMs;
					if ( left > COUNTDOWN_CUE_LATEST_MS ) return "SND_COUNTDOWN";
				}
				if ( left <= 0 && startCued !== state.countdownEndsAtMs ) {
					startCued = state.countdownEndsAtMs;
					return "SND_COUNTSTART";
				}
			}
			const cue = pending;
			pending = null;
			return cue;
		},
		/*
================
pick

The player chose a build: 0x7FB2 now, or null while the server would
ignore it (not a lobby member, or no lobby or countdown running). The
choice is remembered either way; pickDue sends it when the server would
take it. An id outside the build table is a caller defect and throws.
================
		*/
		pick( build: number, now: number ): WireFrame | null {
			const frame = battleRoyalePickFrame( build );
			chosen = build;
			pickResends = 0;
			if ( !battleRoyaleCanPick( state ) ) return null;
			pickSentAtMs = now;
			return frame;
		},
		/*
================
pickDue

The remembered choice's 0x7FB2 when it must go again at now: the player
is a lobby member whose stored build differs from the choice, the last
pick left PICK_RETRY_MS ago or more (or never, for this membership), and
the tries are not spent. Null otherwise.
================
		*/
		pickDue( now: number ): WireFrame | null {
			if ( !chosen || !battleRoyaleCanPick( state ) || state!.build === chosen ) return null;
			if ( now - pickSentAtMs < PICK_RETRY_MS || pickResends >= MAX_PICK_RESENDS ) return null;
			pickResends++;
			pickSentAtMs = now;
			return battleRoyalePickFrame( chosen );
		},
		/*
================
clear

A new session: no match, scoreboard, bots, positions, airdrop, cue or
remembered choice.
================
		*/
		clear() {
			state = undefined;
			scoreboard = undefined;
			bots = undefined;
			positions = undefined;
			airdrop = undefined;
			countdownCued = -1;
			startCued = -1;
			pending = null;
			chosen = 0;
			pickSentAtMs = -Infinity;
			pickResends = 0;
		}
	};
}
