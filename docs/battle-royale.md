# Battle Royale (port-only, not native)

A last-player-standing match in Roc Mountain. Nothing here exists in the
v1.150 game: every part is port-only, behind `SRO_BATTLE_ROYALE` (unset or
`off` = native, no match, no frames). The owner approved this design on
2026-10-18. Stage 1 is the playable core; loot polish, the zone wall and
rewards beyond the winner's silk come later.

## Match flow

1. **Lobby.** A red circle (radius 15 m) in Wind Town, Roc
   (region 0x5A74, local x 1710, z 330, game ≈ −3477, −351). A living
   player standing in it is a lobby member. Players reach Roc by flyship.
2. **Countdown.** With at least 2 lobby members a 30 s countdown starts.
   Members may join during it; below 2 it cancels. During lobby and
   countdown each member picks a build (see below) with the picker window;
   no pick means the default build.
3. **Drop.** At 0 the server allocates a layer of world 29
   (`INS_EVENT_RANDOMMATCH`), swaps each participant's real character into
   a level-1 match character of the chosen build (real record held in the
   same row, see Persistence) and relocates them to drop points inside the
   arena circle at least 50 m apart, on walkable ground.
4. **Farming.** Match monsters (spawned into the layer, no respawn of Roc
   natives): only the level-5 Baroi wolf (`MOB_EU_BARUSWOLF_CLON`), spread
   over all the walkable Roc ground (see "Server notes (stage 4)").
   Drops: one combined table of HP/MP/Vigor potions, pills and Seal of
   Moon or Sun equipment only (weapons of the build, armor and accessories
   of the race), every degree from the killer's level, never the monster's
   own drops; every equipment drop carries at least three blue options and
   white stats of 50 % or more. Bows and crossbows get
   unlimited ammunition. Experience: a fixed share of the level's
   requirement per kill (3.5 kills a level, about 106 kills from 1 to
   30), hard cap level 30. On every level-up the build's two masteries are set
   to the new level and every build skill whose mastery requirement is met
   is learned (no SP cost).
5. **Zone.** Six phases (internal/game/battleroyale schedule.go,
   `ScheduleFor(players, radius)`); the first circle covers all of Roc
   Mountain (2040 m) for every player count and every shrink lasts three
   minutes (stage 4). Outside the current circle a player loses max-HP %
   per second, growing with continuous time outside (1 %/s in phase 1 up
   to 12 %/s in the final phase).
6. **Elimination.** Death, leaving the arena world, or a disconnect longer
   than 60 s eliminates the player: the real character is restored at once
   and returned to the lobby. The kill feed names killer and victim.
7. **Winner.** The last player alive wins: shard announcement, fireworks,
   1000 silk, then restore. 0 alive is a draw.

## Balance (match characters only)

- Max HP × 2.5. Stats are fixed by the build (2:1 split, see presets).
- EU Wizard outgoing magic damage × 0.5; CH outgoing physical damage × 1.2.
- Player-vs-player is free-for-all inside the match layer with no murderer
  flag, no death penalty and no item loss on death beyond the match items.

## Builds

The build id fixes the race. Ids are shared by the server and the client.

| Id | Race | Build | Masteries | Weapon kinds (TID4) |
| --- | --- | --- | --- | --- |
| 1 | CH | Blade-Fire | Blade, Fire | 2 sword, 3 blade (+ shield) |
| 2 | CH | Glaive-Lightning | Spear, Lightning | 4 spear, 5 glaive |
| 3 | CH | Bow-Cold | Bow, Cold | 6 bow |
| 4 | CH | Blade-Heal | Blade, Force | 2 sword, 3 blade |
| 11 | EU | Warrior 2H | Warrior, Cleric | 8 two-handed sword |
| 12 | EU | Knight | Warrior, Cleric | 7 one-handed sword (+ shield) |
| 13 | EU | Ranger | Rogue, Warrior | 12 crossbow |
| 14 | EU | Assassin | Rogue, Warrior | 13 dagger |
| 15 | EU | Wizard | Wizard, Cleric | 11 staff |
| 16 | EU | Warlock | Warlock, Bard | 10 dark staff |

Default build: 11. Melee builds take 2 STR : 1 INT per level, casters
2 INT : 1 STR.

## Persistence

The real character is never copied to another row. On drop the server
writes, in one character-row commit, the match character fields and a
`battleRoyaleHeld` field holding the complete real record. Restore writes
the held record back and clears the field in one commit. At boot and at
world entry a non-empty `battleRoyaleHeld` is restored before the
character spawns, and the character is moved back to the default world,
so a crash at any moment returns the real character byte-identical.
Match items never leave the match: they vanish with the match character.

## Blocked during a match

Return and reverse-return scrolls, portals and flyships, exchange, stall,
storage and guild storage, pick-up and attack pets, party, letters, the
item mall, gold drop, and GM commands for participants.

## Wire (port-only opcodes)

All integers little-endian. Coordinates are world grid units
(sectorX·1920 + local, 1 m = 10 units); radii in grid units.

### 0x3FB2 server → client: match state

Sent to a player when its view of the match changes (entering or leaving
the lobby circle, countdown start/cancel, drop, each zone stage, alive
count change, end) and once at world entry while a match concerns them.
Times are milliseconds remaining at send time; the client counts down
locally, so no per-second frames are needed.

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 1 |
| state | u8 | 0 none (hide everything), 1 lobby, 2 countdown, 3 running, 4 finished |
| role | u8 | 0 spectator/outside, 1 lobby member, 2 alive participant, 3 eliminated |
| build | u8 | the player's chosen build id (0 = none yet) |
| lobbyX, lobbyZ, lobbyRadius | f32 ×3 | lobby circle |
| members | u8 | lobby members (lobby/countdown) or participants (running) |
| alive | u8 | participants alive (running/finished) |
| countdownMs | u32 | countdown remaining (state 2) |
| phase | u8 | zone phase index, 0-based |
| phases | u8 | phase count |
| stage | u8 | 0 waiting, 1 shrinking, 2 final |
| stageMs | u32 | time remaining in this stage |
| curX, curZ, curRadius | f32 ×3 | current circle at send time |
| nextX, nextZ, nextRadius | f32 ×3 | the circle this phase shrinks to |
| damagePerSec | f32 | % max HP per second outside, current phase base |
| winnerLen | u16 | byte length of the winner name (state 4), else 0 |
| winner | bytes | UTF-8 winner name |

While shrinking, the client interpolates the current circle linearly from
cur to next over stageMs.

### 0x7FB2 client → server: pick a build

| Field | Type | Meaning |
| --- | --- | --- |
| build | u8 | a build id from the table |

Accepted only from a lobby member during lobby or countdown; otherwise
ignored. The server answers with a 0x3FB2 carrying the stored build.

### Kill feed and banners

Announcements use the existing 0x3667 notification frame. The kill feed
uses a new port-only channel 0x82 with literal text ("Gina eliminated
test", "test left the zone", "7 players left"); countdown and stage banners
use the existing banner channel (7) with literal text.

## Server notes (stage 1, as built)

Where the server fills in what the sections above leave open; the wire
above is unchanged.

- **Where.** The arena is centred on the Roc Mountain field west of Wind
  Town (region 0x5A6F, local 960/960), the MOB_RM nest ground; the lobby
  centre is region 0x5A74, local 1710/330, radius 15 m. The match runs in
  the first free layer of world 29; 16 players at most.
- **Lobby frames.** A player who leaves the circle gets state 0. Lobby and
  countdown frames go only to members; running and finished frames only to
  participants (the eliminated keep receiving them until the match closes).
  A world entry appends the frame after the bootstrap when a match concerns
  the character.
- **Pick.** 0x7FB2 is accepted only while the sender is a lobby member, so
  a pick sent before the first lobby scan (at most 0.5 s after stepping in)
  is ignored; the client should pick after its first state frame.
- **Monsters.** Per participant: 6 Barus wolves (MOB_EU_BARUSWOLF[_CLON]),
  4 tigers (MOB_CH_TIGER[_CLON], MOB_CH_WHITETIGER[_CLON]), 2 dark/evil
  wolves (MOB_AM_WOLF, MOB_EV_WOLF) and 1 Manho (MOB_FW_WHITETIGER_010 to
  _030); a codename the population refuses is skipped. Spread uniformly
  over the arena, no respawn.
- **Loot.** A match kill drops 2-3 HP and 2-3 MP potions of the monster's
  degree (levels 1-7 degree 1, 8-15 degree 2, 16-23 degree 3, 24+ degree
  4), a vigor potion 40 % of the time, a pill 30 %, a Seal of Sun weapon
  of the build's kinds 25 % and, for the shield builds, a shield 10 %.
  Arrows and bolts are never spent by a match character.
- **Growth.** The match pace is 1.4 x the beta growth pace; at level 30 a
  kill grants no EXP (skill EXP still flows). Each level trains both build
  masteries to it and learns every build skill root whose mastery and
  prerequisite ranks are met (0x72CB's gates, no SP); the client receives
  the mastery and skill acks and a 0x343C stat refresh.
- **Balance.** HP x2.5, CH physical attack x1.2 and Wizard magical attack
  x0.5 are keeper writes on the match character, so the stat window shows
  them. Arena opponents are legal targets: no aggressor (purple) state, no
  murder, and every match death is a team death (kind 2): no EXP or item
  loss.
- **Reconnect.** A participant who logs out keeps its match character for
  the 60 s grace: a world entry inside the grace rejoins the running match
  without a restore; after the grace, or when no running match knows the
  character, the entry restores the real record first (so "restored at
  world entry" holds for every held row the match no longer owns).
- **End.** The finished arena lingers 8 s (the final frame and the shard
  notice), then the winner is restored, the arena's ground items purged
  and the layer released. 0 alive is a draw, announced as such.
- **Fireworks** are the client's to draw from the state-4 frame; the
  server sends no extra effect.
- **Telemetry.** History journal events `battle_royale_match_started`
  (players, monsters, world, arenaRadius, seed), `battle_royale_zone_phase`
  (phase, stage, remainingMs, radius, targetRadius, damagePerSec, alive),
  `battle_royale_player_eliminated` (killer, reason, placement, alive,
  elapsedMs) and `battle_royale_match_ended` (winner, draw, players,
  durationMs, standings, silk), all with the match id.
- **Setting.** `SRO_BATTLE_ROYALE` (deployment default on); unset or off
  installs nothing. Boot and world-entry recovery of a held row run whatever
  the setting says. Schema 28 adds `battleRoyaleHeld` (the real record, the
  build id, the match world and the hold time). The hold's gold reset and
  restore are tagged `battle_royale_hold` / `battle_royale_restore` in the
  economy ledger.

### Server notes (stage 2, as built)

Stage 2 changes no persisted shape: schema 28 stands, kill counts and
streaks are match-runtime state.

- **Build bug, root cause.** `Match.EnterLobby` created every lobby entry
  with the default build and the half-second lobby scan called
  `LeaveLobby` for anyone not standing in the circle at that instant, so a
  member who stepped over the 15 m edge and back (or was scanned out for
  one tick) re-entered with the default build and the pick was gone; and a
  refused 0x7FB2 got no reply, so the picker kept showing a build the
  server did not hold. Fixed in the core and the handler: a pick is
  remembered for the whole lobby (`Match.choices`) and re-enters with the
  member; a pick from a player the last scan did not admit scans the lobby
  synchronously and retries, so a pick sent right after the first lobby
  frame is never lost to the tick cadence; every 0x7FB2 is answered with
  the sender's own 0x3FB2 (a refused one with the view the server holds),
  and repeated picks are idempotent. The match_started journal event now
  lists `builds` (name:id per participant). Regression tests cover a pick
  before the first scan, during the lobby, across a leave and re-entry,
  a re-pick during the countdown and a pick in the last half second.
- **Lobby.** Hotan, region 0x5C87, local 1049/846 (25 m from
  Storage-Keeper Auisan at 1134.28/610.79, bearing 110 degrees), radius
  15 m; chosen against the exported navmesh (every tile of the disc is
  walkable terrain, height spread 0.5 m, so no building or the storage
  approach is covered). `battleroyale/places.go` owns the stands; the
  licensed test re-checks them against the shipped data. A restored
  character stands at the circle's centre.
- **Arena** (superseded by stage 4). Centre region 0x5A6C local 640/640
  (the stage-1 centre, 0x5A6F 960/960, sat on a blocked tile with a
  half-walkable 250 m circle); radius 500 m at 2 players, 700 m at 5,
  900 m at 10+, linear between. Its 500/700/900 m circles are 84/78/71 %
  walkable terrain, the best of every candidate on the Roc regions.
  Every zone target centre is vetoed against walkable ground
  (`Config.WalkableTarget`, redrawn up to 32 times), so the final circle
  closes where a player can stand; drop points and monster points off
  walkable ground are redrawn (16 tries).
- **Zone pacing** (superseded by stage 4). `ScheduleFor` is unchanged:
  every wait and shrink scales with arena radius / 300 m and the edge
  cap holds the radius shrink at or below `MaxShrinkSpeed` = 2 m/s
  (every player count and radius is tested). The doubled arena therefore
  roughly triples the match: about 14 min for 2 players at 500 m, 34 min
  for 10 at 900 m (first wait 9 min).
- **Monsters.** Per participant at the drop: 18 Barus wolves, 12 tigers, 6
  dark/evil wolves, 3 Manho and 6 thieves (3x stage 1 plus the thieves).
  At the start of every zone phase (the drop included) a top-up spawns
  inside the next circle: 4 wolves, 3 tigers, 2 dark wolves and 2 thieves
  per player alive, plus the supply-drop elite at the next circle's centre
  and the banner "Supply drop at the next zone!". Journal event
  `battle_royale_zone_replenished` (phase, monsters, elite, targetRadius).
- **Thieves.** The game's caravan thief job rows `MOB_THIEF_NPC_NNNN`
  (characterdata: ten rows per level, row (level-1)*10+1 first of its
  level; type word 0x10C6, the same rows `Caravan_SpawnBandits` draws).
  The match uses the first row of levels 3, 7, 11, 15, 19, 23 and 27
  (_0021, _0061, _0101, _0141, _0181, _0221, _0261) so the band 1-30 is
  covered; nothing is scaled. A thief kill drops `ThiefLootPlan`: HP and
  MP potions (3-4 each) of one degree above its level band (capped at 4),
  then up to two extras in the order Sun weapon 50 %, vigor 60 %, pill
  50 %, shield 25 % (shield builds): 2-4 stacks.
- **Supply drop.** `MOB_THIEF_NPC_0151` (level 16) spawned as a champion
  (`QuestMonsterSpawn.Rarity` 1, x1.5 HP) at the next circle's centre;
  recognized by codename plus rarity. Its kill drops `SupplyDropLoot` for
  the killer's build: a degree-4 Sun weapon, 5 HP and 5 MP potions and 3
  vigor potions of degree 4, two pills (50 %), and a degree-4 shield or a
  second weapon kind (50 %): 4-6 stacks. The degree-4 weapon needs level
  24; until then it waits in the bag (see auto-equip).
- **Beta player map.** `movement.Runtime.MapHidden` ->
  `Runtime.BattleRoyaleMapHidden`: a held (match) character and every
  lobby member (the mode's hidden set, refreshed by each scan) is left out
  of everyone's 0x3FB0 roster and receives an empty roster, so a
  participant sees no markers and shows none. An eliminated player keeps
  hiding until the lobby reopens and it leaves the circle. Off, nobody
  hides.
- **Auto-equip.** The hold writes the build's kit into the match record
  before the re-entry publishes it (so the spawn carries it natively): the
  degree-1 Sun weapon of the build's creation kind in socket 6 (blade for
  Blade-Fire and Blade-Heal, glaive for Glaive-Lightning), a degree-1 Sun
  shield (Blade-Fire, Knight) or 250 creation arrows/bolts (Bow, Ranger)
  in socket 7, 20 HP and 20 MP degree-1 potions in the bag. A picked-up
  Sun weapon of the build's kinds with a higher degree than socket 6 is
  moved there through the ordinary inventory move (`applyInventoryMove`)
  in the pickup's own burst: the client gets the native 0xB034 move
  result, the socket visual and the 0x343C stats, observers the visual. A
  weapon the equip gates refuse (level) stays in the bag; the match tick
  after each level gained re-checks the bag.
- **Kills, streaks, scoreboard.** Kills are the core's `Participant.Kills`
  (a participant dies once, so a streak is its kill count). Banners
  "<name> is on a rampage" at 3 and "<name> is unstoppable" at 5, once
  each, on channel 7 to every participant. 0x3FB3 goes to every
  participant after each elimination (final 0) and at the end (final 1),
  one entry per participant in name order, kills and placement (0 while
  alive), the receiver's own entry marked; names are cut at 64 bytes.
  0x3FB2 is unchanged.
- **Fireworks** remain the client's (item 8).

The stage-2 Monsters, Thieves and Supply drop notes above are superseded
by stage 3 below; the stage-1 Monsters, Loot and Growth notes likewise.
The stage-2 Arena and Zone pacing notes are superseded by stage 4.

### Server notes (stage 3: one monster, Sun-only loot, as built)

Owner change after the stage-2 playtest. No persisted shape changes.

- **Monsters** (counts superseded by stage 4). The match spawns one
  monster only: the Baroi wolf `MOB_EU_BARUSWOLF_CLON`, level 5
  (characterdata 5854; the spawn path accepts it, it already stood in
  stage 2). Tigers, dark/evil wolves, Manho and thieves are gone from
  the drop, the top-ups and the supply drop. Per participant at the
  drop: 450 wolves (10x the stage-2 45), uniform over the arena kept off
  its outer 10 %, capped at 4500 (the 10-player count: the arena stops
  growing at 900 m, so the density never climbs past it). Counts: 2
  players 900 (500 m), 5 players 2250 (700 m), 10+ players 4500 (900 m),
  about one wolf per 550-870 m2 of arena. The quest-monster spawn path
  has no population cap (only the shard's gid counter); points off
  walkable ground are redrawn up to 16 times and dropped after that.
  Every zone phase tops the next circle up with 40 wolves per player
  alive (at most 400) and stands the supply elite at its centre.
- **Supply drop.** A giant (rarity 4, x3 body) of the same wolf at the next
  circle's centre, recognized by codename plus rarity. Its kill drops
  `SupplyDropLoot`: a Sun weapon of the killer's build one degree above
  the killer's current weapon degree (capped at 4), 5 HP and 5 MP potions
  and 3 vigor potions of the killer's degree, then a wearable Sun armor
  piece (50 %) and a wearable Sun accessory (50 %): 4-6 stacks.
- **Loot** (table and seal superseded by stage 4). Every wolf drops from
  ONE combined match table (`battleroyale.LootPlan`): one stack per
  kill, a second at 50 % (1.5 stacks a kill on average). Each stack is a
  weighted draw:

  | Entry | Weight | Item |
  | --- | --- | --- |
  | HP potion | 26 % | `ITEM_ETC_HP_POTION_0d`, 2-3 |
  | MP potion | 22 % | `ITEM_ETC_MP_POTION_0d`, 2-3 |
  | Vigor potion | 14 % | `ITEM_ETC_ALL_POTION_0d`, 1 |
  | Pill | 10 % | `ITEM_ETC_CURE_ALL_0d` (degree 1-3), 1 |
  | Sun weapon | 8 % | `ITEM_{CH,EU}_{kind}_0d_C_RARE` of the build's kinds |
  | Sun armor | 12 % | `ITEM_{CH,EU}_{M,W}_{HEAVY,LIGHT,CLOTHES}_0d_{HA,CA,SA,BA,LA,AA,FA}_C_RARE` |
  | Sun accessory | 6 % | `ITEM_{CH,EU}_{EARRING,NECKLACE,RING}_0d_C_RARE` |
  | Sun shield | 2 % | shield builds; armor for the others |

  About 11.7 % of kills carry a weapon (0.12 weapons a kill), 17 % armor,
  9 % an accessory. No non-Sun equipment ever drops. Degrees follow the
  KILLER's level, never the monster's: weapons, shields, potions, pills
  and rings by the weapon bands (degree 1 at 1-7, 2 at 8-15, 3 at 16-23,
  4 at 24-30); armor, earrings and necklaces at the highest degree whose
  itemdata ReqLevel the killer meets (armor 1/11/19/27, earrings
  1/10/18/26, necklaces 1/12/20/28), so every armor or accessory drop is
  wearable at once. The armor is the killer's race and gender (from its
  model) and the build's class (`Build.Armor`): heavy for Blade-Fire,
  Glaive-Lightning, Blade-Heal, Warrior 2H and Knight, light for Bow-Cold,
  Ranger and Assassin, clothes (robes) for Wizard and Warlock; the EU
  classes are the ones the build's masteries admit (itemdata ReqLevelType
  513 heavy, 513/515/518 light, 514/516/517/518 clothes), and the match
  trains those masteries to the character level. Only the weapon is
  auto-equipped; armor and accessories go to the bag.
- **Growth.** The native reward formula's level-gap scale (x0.1 at seven
  or more levels above the monster) would starve a match character on a
  level-5 wolf, so a credited kill (the formula paid the character any
  EXP) pays `MatchKillExp`: the leveldata requirement of the character's
  level / 3.5, rounded up, instead of the formula's amount. It is
  committed through the stat authority's exact updater
  (`Runtime.CommitExperience`, `progression.ExactExperienceUpdater`), so
  the beta growth rate never scales it again. On the shipped curve that is
  34 EXP a kill at level 1 (118 needed), 6,715 at 10, 80,572 at 20,
  350,519 at 29; levels 1 to 30 take 106 credited kills, about 8.8
  minutes at one kill every 5 s (8-10 minutes at 4.5-5.7 s a kill). A
  kill never crosses two levels; level 30 earns no EXP (skill EXP still
  flows, unscaled). Party members credited by the native distributor
  each get their own share.

The stage-3 Monsters counts and the Loot table and seal are superseded by
stage 4 below (the wolf, the supply elite, the degree rules and Growth
stand).

### Server notes (stage 4: all of Roc, three-minute shrinks, Moon/Sun loot, as built)

Owner change after the stage-3 playtest. No persisted shape and no wire
change: 0x3FB2 is byte-identical (its radii are f32, 20400 units fits; the
phase count field now says 6).

- **Arena = all of Roc.** The Roc Mountain regions are x 104..118,
  y 85..98 plus the strip y 84, x 113..117 (`battleroyale.InRoc`; grid X
  199680-228480, Z 161280-190080, a 2.88 km square). The arena circle is
  centred on that square, region 0x5B6F local 960/960 (grid
  214080/175680; blocked ground, which a zone centre may be), radius
  2040 m, just over the half-diagonal (2036.5 m): every Roc corner is
  inside, for every player count (`ArenaCircle()` takes no count any
  more; the countdown no longer resizes the arena).
- **Walkable ground mask.** `battleroyale.GroundMask` samples the Roc
  regions on a 9.6 m grid (20 cells per region edge, 300 x 300 cells),
  asks the world's ground test at each cell centre and keeps the largest
  4-connected component. On the shipped navmesh that is 4.97 km2 of the
  5.29 km2 walkable Roc ground (94 %; the rest are cut-off plateaus and
  islands); the ground outside Roc that the circle's corners overlap is
  never used. The wiring builds it once on first use
  (`battleRoyaleGroundMask`, about 90 000 ground tests) and
  `battleRoyaleGround` now means "in the mask and walkable at the exact
  point". Drop points (`DropPointsFiltered`, spacing kept), every zone
  target centre (`PlanZoneFiltered`, so the final circle closes on
  reachable ground) and every monster point stand on it; a point whose
  exact spot is refused is redrawn on the mask's cells. The licensed test
  plans 40 seeds: every target centre and drop point is mask ground, and
  1967 of 2000 planned monster points are walkable at the exact spot (the
  wiring redraws the rest).
- **Zone.** Six phases, every shrink 3:00 (`minPhaseShrinkMs`, also the
  floor for smaller test arenas). Each shrink closes the radius by at most
  360 m: 2 m/s (`MaxShrinkSpeed`, unchanged), worst-case edge 4 m/s, 80 %
  of the 5 m/s run speed (`playerRunSpeed` = 50 units/s), so a player who
  starts running when a shrink starts is never overtaken, and the radii do
  not depend on the player count. Waits scale x0.5 at 2 players, x0.75 at
  5, x1 at 10+.

  | Phase | Wait (10+) | Wait (2) | Shrink | End radius | Damage | Ramp |
  | --- | --- | --- | --- | --- | --- | --- |
  | 1 | 2:00 | 1:00 | 3:00 | 1680 m | 1 %/s | +0.05 |
  | 2 | 1:30 | 0:45 | 3:00 | 1320 m | 2 %/s | +0.10 |
  | 3 | 1:00 | 0:30 | 3:00 | 960 m | 3.5 %/s | +0.18 |
  | 4 | 0:45 | 0:22.5 | 3:00 | 600 m | 5 %/s | +0.25 |
  | 5 | 0:30 | 0:15 | 3:00 | 260 m | 8 %/s | +0.40 |
  | 6 | 0:20 | 0:10 | 3:00 | 0 (a point) | 12 %/s | +0.60 |

  Total: 24:05 at 10 or more players, 22:33.75 at 5, 21:02.5 at 2.
- **Monsters.** 2000 Baroi wolves at the drop whatever the player count
  (`ArenaMonsters`; about one per 2 500 m2 of the 5 km2 ground), the
  same top-ups as stage 3 (40 per player alive, at most 400, on the next
  circle's ground kept off its edge, 50 m around the point of the last
  phase) plus the supply giant, but a top-up never takes the match past
  `MaxLiveMonsters` = 3000 living monsters (the wiring counts the layer's
  living monsters over the Roc regions before each top-up). Stage 3 stood
  up to 4500.
- **Loot: the match table only.** A match kill never drops the monster's
  own table (the owner reverted the "native drops too" idea: it is a
  battle royale). The combined table now drops more equipment, 45 % of
  the stacks (1.5 stacks a kill, so about 0.68 equipment items a kill and
  a weapon in about 20 % of kills):

  | Entry | Weight | Item |
  | --- | --- | --- |
  | HP potion | 20 % | `ITEM_ETC_HP_POTION_0d`, 2-3 |
  | MP potion | 17 % | `ITEM_ETC_MP_POTION_0d`, 2-3 |
  | Vigor potion | 10 % | `ITEM_ETC_ALL_POTION_0d`, 1 |
  | Pill | 8 % | `ITEM_ETC_CURE_ALL_0d` (degree 1-3), 1 |
  | Weapon | 14 % | `ITEM_{CH,EU}_{kind}_0d_{B,C}_RARE` of the build's kinds |
  | Armor | 18 % | `ITEM_{CH,EU}_{M,W}_{HEAVY,LIGHT,CLOTHES}_0d_{part}_{B,C}_RARE` |
  | Accessory | 10 % | `ITEM_{CH,EU}_{EARRING,NECKLACE,RING}_0d_{B,C}_RARE` |
  | Shield | 3 % | shield builds; armor for the others |

  Every equipment drop (the supply drop's too) draws its seal on its
  own: Seal of Moon (`_B_RARE`) two times in three, Seal of Sun
  (`_C_RARE`) one in three. The Moon rows gate at the same levels as the
  Sun rows, so the degree rules of stage 3 stand. The auto-equip ranks a
  weapon by degree first, then Sun over Moon (`WeaponRank`: a degree-2
  Moon replaces a degree-1 Sun, a Moon never replaces the Sun of its
  degree). The starter kit stays a degree-1 Sun weapon.
- **Blue options (at least three).** The native producer
  (`loot.EquipmentMagic`, 7276A0) gives a rare item one to six options,
  mostly one or two (the least of four six-way draws, duplicates spend a
  slot). `loot.EquipmentMagicAtLeast(code, 3, DropRoll)` (port-only)
  reruns it up to 16 times and keeps the richest result; if none reached
  three it adds distinct options from the item's own native pool
  (magicoptionassign rows at the item's degree, allowed on its type and
  body part, probability above zero, never the nonrepair option), each
  valued by the native `alchemy.RollMagicValue`. Over all 848 Moon/Sun
  rows of degrees 1-4 the result is three options 91 % of the time, four
  8 %, five 1 %. The options sit in `inventory.Item.MagicOptions`: the
  ground item keeps them and the pickup's 0xB034 item body
  (`wire.ItemBody.MagicOptions`) hands them to the client, exactly as a
  native blue.
- **White stats (50 % or more).** `rollBattleRoyaleVariance` takes the
  native dropped-equipment roll (`rollLowBiasedVarianceField`, the least
  of three draws, 0-31 per field: 7 fields for weapons, 6 for armor and
  shields, 2 for accessories) and folds each field to
  16 + v x 15 / 31: every stat lands at 16-31 of 31 (51.6-100 %) with the
  native low bias kept inside that half; the durability follows the
  first field as natively. The client prints each grade as
  trunc((stat - lo) / (hi - lo) x 100) of the rounded stat, which is
  50 % or more for every range at 16 or above (tested over ranges 1-400).
- **Tests.** Core: the Roc arena covers every Roc corner and InRoc's
  regions; the six-phase table, three-minute shrinks, 2 m/s radius and
  4 m/s edge bounds for 1-16 players, 21-25 minute totals; the mask keeps
  the largest component and draws inside a circle; filtered drops;
  `ArenaMonsters`, top-ups and the live cap; the combined table's rates
  and Moon/Sun ratio (Sun 30-37 % over 6000 kills); `WeaponRank`.
  Licensed: the mask is 4.5-5.5 km2 and at least 90 % of walkable Roc,
  zone centres and drops on it for 40 seeds, monster points walkable.
  Loot package: three or more distinct, allowed, valued options on every
  Moon/Sun row of degrees 1-4. Wiring: match kills drop only the match
  table, every equipment drop carries 3+ blues and every white field
  16+, Sun about a third, a pickup keeps the options; the white fold;
  the live monster count after the drop and first top-up.

## Stage 2 (owner feedback after the first playtest, 2026-10-18)

1. **Build bug (highest priority).** The match must start with the build
   the player picked before the countdown ended. Reproduce the real flow
   (picker -> 0x7FB2 -> stored pick -> drop uses it, also when picked during
   lobby, re-picked, or picked in the last second) and fix the cause on the
   side where it lies; add a regression test for every path.
2. **Lobby moves to Hotan, beside the storage keeper** (NPC_KT_WAREHOUSE,
   Storage-Keeper Auisan, region 23687 = 0x5C87, local x 1134.28,
   y 243.75, z 610.79). Centre the 15 m circle on walkable flat ground
   about 25-30 m from the NPC so it does not cover the NPC or the
   storage approach. Hotan is a town: players reach it normally. The drop
   still goes to the Roc arena (world 29 layer).
3. **Bigger arena.** Start radius ×2 (500 m for 2 players, 700 m for 5,
   900 m for 10+), still centred on walkable Roc ground; zone timings
   scaled so the zone never shrinks faster than ~2 m/s.
4. **More monsters.** About 3× the stage-1 density, spread over the whole
   arena, plus new monsters spawned near the next circle as the zone
   shrinks so the end game still has farming.
5. **Thieves.** Spawn thief-job monsters (the game's own thief/bandit
   monster rows, e.g. caravan bandits — pick level-appropriate ones) that
   drop more and better loot: 2-4 items per kill, higher chance of Sun
   weapons and degree +1 over the level band, Vigor potions and pills.
6. **Hidden on the map.** While a player is in the lobby, a match, or
   eliminated-but-spectating, they must not appear on other players'
   world map/minimap player markers (the port's beta player map,
   SRO_BETA_PLAYER_MAP 0x3FB0) — and participants must not see others.
7. **Auto equip.** At the drop each player starts with a degree-1 Seal of
   Sun weapon of their build already equipped (and a shield for builds
   that use one, ammo for bow/crossbow), plus 20 HP and 20 MP potions.
   A newly looted better weapon of the player's build (higher degree)
   is auto-equipped if the slot holds a worse one.
8. **Fireworks.** While a match runs, fireworks go off automatically at
   random points inside the current circle (client-side, every 6-12 s,
   using the game's firework effects, e.g. system/firework_star_shower.efp,
   system/item_firework_bomb_r/g/b/v.efp), and a burst over the winner.
9. **Supply drop.** At the start of every zone phase an elite (champion
   or giant) thief or wolf spawns at the next circle's centre with a
   banner "Supply drop at the next zone!"; it drops 4-6 items including a
   Sun weapon of degree 4 for one of the killers' builds.
10. **Kills, streaks and scoreboard.** The server counts kills per
    participant; banners on streaks of 3 ("<name> is on a rampage") and 5
    ("<name> is unstoppable"); the HUD shows the player's own kills; at the
    end a scoreboard window lists every participant with placement and
    kills.

### 0x3FB3 server -> client: scoreboard (stage 2)

Sent to every participant on each elimination and at the end.

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 1 |
| final | u8 | 1 at match end (open the scoreboard window), else 0 |
| count | u8 | entries |
| per entry: nameLen | u16 | byte length |
| name | bytes | UTF-8 |
| kills | u16 | kills |
| placement | u8 | 0 while alive, else 1 = winner, 2 = second, ... |
| self | u8 | 1 for the receiving player's own entry |

## Stage 5: bots (owner request, 2026-10-18)

A GM-only switch in the lobby adds 20 bots to the next match, so a match
can start and be fun with few real players.

- **Switch.** Only a GM standing in the lobby (role member) sees a
  "Bots: ON/OFF" button (on the lobby hint and in the build picker). It
  toggles the lobby's bot setting for the next match; the server refuses
  the toggle from non-GMs. While bots are on, ONE real player in the lobby
  is enough to start the countdown (the 2-player minimum counts bots).
- **Bots.** 20 server-driven fighters. Each is a monster-backed actor
  (a human/bandit-looking monster model from the game, level-scaled like
  the match characters, HP comparable to a match character of its level)
  carrying a player-like name ("Ares", "Mira", …, unique per match). Bots
  drop at walkable points like players, level up over the match (their
  stats follow a 1->30 curve with time/kills), farm wolves, fight players
  and other bots when close, move toward the safe zone when the zone
  closes, take zone damage outside it, and die like monsters.
- **Players and bots.** Real players click a bot to select it and attack
  it like any monster; bots attack players. A bot counts as a participant:
  alive count, eliminations, kill feed ("Ares eliminated test"),
  streaks, the scoreboard (with a bot flag), and the winner can be a bot.
  A bot's kill of a player is a normal match elimination.
- **Names in red.** Every client shows a bot's name above its head in red
  (nameplate), and in the scoreboard/kill feed.

### 0x7FB4 client -> server: GM bot switch

| Field | Type | Meaning |
| --- | --- | --- |
| enabled | u8 | 1 = bots on, 0 = off |

Accepted only from a GM lobby member during lobby or countdown.

### 0x3FB4 server -> client: bot roster and lobby options

Sent to lobby members whenever the switch changes (and with each 0x3FB2
lobby/countdown frame to a newly joined member), and to everyone in the
match world when bots spawn or die.

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 1 |
| botsEnabled | u8 | the lobby's switch |
| count | u8 | bots listed |
| per bot: gid | u32 | the bot's world object id |
| nameLen | u16 | byte length |
| name | bytes | UTF-8 name |

The client overrides the nameplate of each listed gid with the name, drawn
in red. The 0x3FB3 scoreboard gains no new field: bot rows are ordinary
rows whose names are in the roster.

### Server notes (stage 5, as built)

The wire above is built as specified; 0x3FB2 and 0x3FB3 keep their
layouts. No persisted shape changes: bots never touch the store.

- **Switch.** 0x7FB4 is accepted from a lobby member whose character
  carries the GM privilege (`GMPrivilege`, the `SRO_GM_CHARACTERS`
  allowlist), during the lobby or the countdown; a sender the last scan
  did not admit is scanned for at once, as for the pick. The sender is
  always answered with a 0x3FB4 (a refused switch with the setting the
  server holds); an accepted one also goes to every other lobby member.
  A body other than one byte 0 or 1 is refused. The switch belongs to
  the division's lobby and **carries over** into the lobbies after a
  match until a GM turns it off. With it on, one real member starts the
  countdown and switching off during a one-member countdown cancels it.
- **0x3FB4 sends.** On an accepted switch (lobby members), to each newly
  joined lobby member after its 0x3FB2, to every participant when the
  bots stand at the drop and again on every bot's death, and at world
  entry after the state frame whenever the entry gets a state frame.
  The roster lists every bot that stood in the match, dead ones too, so
  the scoreboard can keep flagging their rows; in the lobby it lists
  none. Names are cut at 64 bytes like the scoreboard's.
- **Roster of a drop.** 20 bots (`battleroyale.MaxBots`), named from a
  fixed list of 40 player-like names ("Ares", "Mira", "Kael", "Luna",
  ...) shuffled by the match seed, never a lobby member's name
  (case-insensitive). They are core participants (`Participant.Bot`):
  their drop points follow the players' (a player lands where it would
  have without bots), the zone schedule counts them, the alive count,
  eliminations, kills, streak banners, placements, the scoreboard and
  the kill feed treat them as players, and 0x3FB2's `members` counts
  them. A bot can win; a bot winner gets the shard announcement but no
  silk. When the last real player is out the match ends at once instead
  of running the zone down for the bots: the standing bots are placed
  by kills (ties by name) and the first wins.
- **Body.** Each bot is a quest-path monster of the match layer
  (`SpawnQuestMonsterActor`), one of the ten level-1 caravan thief rows
  `MOB_THIEF_NPC_0001..0010` (the bandit body with its trade look; the
  rows stage 2 already spawned), on mask ground at its drop point (a
  refused point is redrawn). Players click, select, strike and kill it
  through the ordinary monster paths; no PvP rule is involved. The
  thief row's 20 s idle departure is re-armed every tick
  (`RefreshJobMonster`), so a bot never vanishes.
- **Level and balance.** `battleroyale.BotLevel`: 1 + one level per 30 s
  alive + one per 4 wolves + two per participant killed, capped at 30.
  The client always sees the level-1 row (its level, model and HP bar);
  the bot fights through a port-only combat projection
  (`monster.PortCombat`, read by `combat.MonsterInstanceStats`): the
  first thief row of its level (`MOB_THIEF_NPC_0001` at 1 ...
  `_0291` at 30) supplies its attack, defence, hit and evasion; the hits
  it takes are scaled by visible HP / `BotMaxHP(level)`, so its bar holds
  a match character's HP of that level (the native curve over 2 STR a
  level, x2.5: 500 at 1, 3462 at 30); its hits on players are doubled
  (the thief rows were authored against unboosted HP). A nil projection
  (every other monster) is native; an actor with one never goes to
  dormant storage.
- **Brain** (every 0.5 s match tick, in this order). Take the zone's
  damage (the players' rule on the visible HP; a zone death is "died
  outside the zone"). Outside the next circle (the one the zone closes
  to, at least 10 m) run toward its centre. Else, a living player within
  25 m: the bot is handed it as a retaliation target
  (`ArmRetaliation`) and the native chase and monster attack (its
  skills, cast results and the player death path) do the rest; a bot already
  chasing or attacking is left alone. Else another bot within 25 m or
  the nearest wolf within 80 m: run to 4 m and strike every 1.5 s for a
  twelfth of its own level HP (against a bot, scaled to that bot's bar).
  Else roam to a random point inside the next circle every 4 s. Movement
  is one ordinary run leg of at most 40 m per decision through the
  monster mover (`MonsterMoverOps.SteerMonster`, wired from the ticker's
  movement owner), clipped by the navmesh like a wander leg; it also
  ends a dormant bot's spawn hold. A bot's strike on a monster is an HP
  debit with the HP refresh and, when fatal, the dead state and the
  ordinary corpse retirement; it has no swing animation (no native path
  lets a monster hit a monster).
- **Deaths and loot.** A bot whose body dies through any settled kill
  (`settleMonsterInsideDoor`, every player kill lane) is eliminated and
  credited to the killing blow's participant; a bot's strike credits
  that bot; a player killed by a bot's body is credited to the bot
  ("Ares eliminated test"). A player's kill of a bot pays the match EXP
  and drops two `LootPlan` rolls (2-4 stacks). A body that disappears or
  dies unsettled is eliminated uncredited.
- **Cleanup.** Bodies live only in the match layer: the layer's release
  at close retires them, and the gid index (`botGIDs`) forgets them.
  Telemetry `battle_royale_match_started` gains `bots`.
- **Tests.** Core: quorum on/off and the countdown cancel, 20 unique
  names avoiding a member's, players' drop points unchanged, bot
  eliminations and kills, a bot winner, the bots-alone end, the level,
  HP and row curves. Wire: the 0x3FB4 bytes and the 0x7FB4 decoding.
  Wiring: non-GM refused, the switch heard by the lobby, mode off
  refuses; switch off is the native match; one GM starts with bots, 20
  bodies on ground with unique names and their combat projection, the
  roster and `members`; level-up installs the row; a player's settled
  kill of a bot (feed, kill credit, two loot rolls, scoreboard, roster)
  and a bot's strikes killing a bot; a bot killing the last player wins
  without silk; a bot outside the next circle is steered toward it; the
  close releases the layer and bodies and keeps the switch.

### Server notes (stage 6: economy)

Owner-approved economy pass, proven by a whole-match simulation. No wire
change (0x3FB2/0x3FB3/0x3FB4 byte-identical) and no persisted shape
change. It supersedes the stage-4 **Monsters** counts and top-ups, the
stage-3/4 loot degree rules and stack count, and the stage-3 supply-drop
weapon "one degree above".

**Model (from the shipped data and the code).**

- EXP: leveldata column 1, levels 1-29 (118 ... 1 226 815 EXP);
  `MatchKillExp` pays a level's requirement / 3.5, so 106 credited kills
  take a character from 1 to 30 (unchanged).
- The wolf: `MOB_EU_BARUSWOLF_CLON`, level 5, 119 HP (characterdata
  columns 57/59), the only match monster.
- Required levels of every Moon/Sun row (itemdata ReqLevel quads; the EU
  armor's mastery quad holds the same values): degree 1 is level 1 for
  every row; degree d > 1 is 8 (d - 1) + the part offset: weapons,
  shields, rings and hands (AA) 0, shoulders (SA) 1, feet (FA) and
  earrings 2, head (HA) and chest (CA) 3, legs (LA) and necklaces 4, body
  (BA) 5. So degree 2 spans 8-13, 3 spans 16-21, 4 spans 24-29.
  `battleroyale.RequiredLevel` encodes it; the licensed test checks all
  848 Moon/Sun rows of degrees 1-4 against itemdata.
- Potions: HP/MP/vigor tiers 1-5 heal 120/220/370/570/820 (itemdata
  column 118/122, tier column 61); pills (cure-all) tiers 1-4.
- Zone: six phases, three-minute shrinks; with the 20 bots every
  match counts 22+ participants, so the waits are the 10-player ones and
  phase 5 starts at 17:15 (1 035 s) for every player count.
- Ground: the largest walkable Roc component, 4.97 km2 (`GroundMask`).
- Bots: `BotLevel` = 1 + one level per 30 s + one per 4 wolves; a strike
  every 1.5 s for BotMaxHP(level)/12 (a level-1 bot kills a wolf in three
  strikes, from level 14 in one).

**Loot rules (`battleroyale.LootPlan`, `SupplyDropLoot`).**

- Every kill drops 3 stacks, a fourth at 50 % (3.5 on average); a bot
  body two plans (6-8), the supply giant 3-6.
- Equipment only from the build's rows (weapon kinds, shield of the
  shield builds, race/gender/class armor, race accessories) whose
  required level lies in **[killer level - 3, killer level]** at the
  kill, drawn uniformly among the family's rows in that window (any
  equipment family of the window when the drawn family has none). A
  level-20 player never gets a level-1 item. Levels 5-7 have no row in
  their window (degree 1 needs 1, degree 2 starts at 8): their
  equipment draws become HP potions.
- HP, MP and vigor potions by `PotionTier`: the largest tier that heals
  at most 30 % of the match max HP of the level: tier 1 at 1-4, 2 at
  5-11, 3 at 12-18, 4 at 19-25, 5 at 26-30. Pills by the degree bands
  (1 at 1-7, 2 at 8-15, 3 at 16-23, 4 at 24-30; tier 4 exists).
- Kept: the stage-4 table weights (equipment 45 % of the stacks), Moon
  2/3 and Sun 1/3, at least three blues, whites 50 % or more, the match
  table only. The supply drop's weapon is now the build's weapon of the
  window (any window equipment when none).

**Monster supply (`battleroyale/economy.go`, wired in
`action/battleroyale_arena.go`).**

| Parameter | Value | Why |
| --- | --- | --- |
| `ArenaMonsters` | 1 500 | the drop's shared stock, one per 3 300 m2 |
| `MaxLiveMonsters` | 3 000 | unchanged; the simulation peaks at 2 621 |
| Zone supply | every 15 s and at each phase start | retire, then restock |
| Retire | untouched wolves > 30 m outside the current circle | they only held the cap |
| Zone target | max(16 per participant alive, next-circle ground / 3 000 m2), at most ground / 400 m2, batch 600 | stock inside the next circle |
| Local supply | every 5 s, 10 wolves within 35 m (new ones 12-35 m away, on the zone's side) of each participant below level 30 | the farming pace |
| Floor (simulated) | 8 living wolves inside the current circle per participant alive, every second before phase 5 | |
| Ground items | vanish after 75 s; at most 2 000 per match layer, oldest first | `Registry.ExpirePopulation`, swept every 5 s with the 0x36AB despawn |
| Bots | a level-30 bot no longer hunts wolves | the wolves stay with the players |

Why a local supply: uniform wolves cannot give a realistic pace under
the cap. The first simulation (stage-4 rules plus the zone supply)
showed 2 000 wolves over 5 km2 put the nearest wolf about 25 m away and
players at about 13 s a kill; almost nobody reached 30 before phase 5.
Spawning a few wolves beside each participant who still levels costs
360 living monsters at most and brings the kill to about 5.7 s; spawning
them on the zone's side walks the player into the zone.

**Proof (`battleroyale/economy_sim_test.go`).** Seeded whole matches on
an injected clock (no sleeps), three seeds per player count, through the
real `ScheduleFor`, `PlanZoneFiltered`/`CircleAt`, `DropPointsFiltered`,
`MonsterPlan`, `SupplyPlan`, `LocalSupplyPlan`, `RetireOutside`,
`LootPlan`, `MatchKillExp` over the leveldata curve, `BotLevel` and
`BotMaxHP`, and the ground constants. Policies: a player farms the
nearest unclaimed wolf on the zone's side, runs at 5 m/s over 1.25 x the
straight line, fights 3.5 s at level 1 down to 2 s from 16, picks up for
0.5 s, keeps farming at 30 and never picks a drop up (worst case for the
ground cap); a bot follows its live brain; nobody dies (worst case for
the supply). Every kill is checked: at least three stacks, every
equipment row inside [level-3, level], only consumables otherwise, level
never past 30. The table below is the licensed run on the real Roc mask
(`TestEconomySimulationOnRoc`, which also checks the EXP curve against
leveldata); kills and stacks are per real player over the whole match,
peaks are the worst seed, bot kills and retirements the mean per match.

| Players | Lvl 30 before phase 5 | Lvl 30 median / p95 (min) | Phase 5 at (min) | Kills / player | Stacks / player | Peak ground items | Peak live monsters | Min wolves in zone / participant | Bot wolf kills | Wolves retired |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2 + 20 bots | 6/6 | 10.3 / 10.5 | 17.25 | 192 | 670 | 139 | 1934 | 15.9 | 1200 | 2215 |
| 5 + 20 bots | 15/15 | 10.3 / 15.1 | 17.25 | 189 | 663 | 286 | 2080 | 15.6 | 1142 | 2338 |
| 10 + 20 bots | 30/30 | 10.0 / 10.4 | 17.25 | 194 | 678 | 539 | 2299 | 15.6 | 1230 | 2323 |
| 16 + 20 bots | 48/48 | 10.1 / 10.8 | 17.25 | 189 | 664 | 850 | 2621 | 15.4 | 1182 | 2508 |

The unlicensed run (`TestEconomySimulation`, a stand-in 5 km2 mask with
cliffs) gives the same picture: 100 % at level 30 before phase 5,
median 10.0-10.2 min, p95 at most 14.9 min, live peak 2 522, ground peak
819.

**Tests.** Core: `TestRequiredLevel`, `TestPotionTiers`,
`TestLootPlanWindow` (every build, level and gender), `TestSupplyLoot`,
`TestZoneSupply`, `TestLocalSupplyPlan`, `TestEconomySimulation`
(invariants, the 95 % rule, caps, determinism); licensed:
`TestRequiredLevelMatchesItemdata`, `TestEconomySimulationOnRoc`.
Ground items: `TestExpirePopulationLifetimeAndCap`. Wiring:
`TestBattleRoyaleZoneSupplyRetiresAndRestocks`,
`TestBattleRoyaleLocalSupplyFollowsLevellers`,
`TestBattleRoyaleGroundItemsExpire`,
`TestBattleRoyaleCappedBotLeavesWolves`, and the stage-2/4 loot and
live-cap tests updated to 3-4 stacks and the supply.

## Stage 7: smarter bots, difficulty, count and map (owner request, 2026-10-18)

### Owner requirements
1. **Behave like real players.** Bots must actively hunt: notice real
   players and bots within a difficulty-dependent sense range (Easy 40 m,
   Normal 70 m, Hard 110 m) and come to fight them; keep farming wolves
   between fights; drink potions when hurt (heal when HP < 40 % with a
   cooldown, Easy rarely, Hard reliably); flee into the safe zone well
   before the zone reaches them.
2. **Gear up.** Bots collect loot from what they kill (simulated: each kill
   rolls the same match loot table and the bot keeps the best weapon /
   armor degree reached). Their attack and defence grow with that gear and
   their level, so a late-game bot is a real threat. Expose the bot's gear
   degree in the server notes/telemetry.
3. **Never get stuck.** Movement must follow walkable ground: use the
   server's navmesh/path planning (or the walkable ground mask with an A*
   over its 9.6 m grid) to plan multi-leg paths around cliffs; detect a bot
   that has not progressed for ~5 s and re-plan or pick a new goal.
4. **Spawn near the players.** Bots drop in a ring 150-400 m around the
   real players (spread among them), on walkable ground, not all over Roc.
5. **Difficulty**, chosen with the switch: Easy, Normal, Hard. Suggested
   scales (tune in the simulation/tests): damage ×0.6 / ×1.0 / ×1.5,
   HP ×0.8 / ×1.0 / ×1.3, attack interval 1.8 s / 1.3 s / 0.9 s, level-up
   pace ×0.8 / ×1.0 / ×1.3, potion use 30 % / 70 % / 100 %.
6. **Count**, chosen with the switch: 1-200 (default 20). Keep the server
   light: total living wolves cap is reduced as bots grow so that wolves +
   bots never exceed the current live cap (3000); bot AI must stay cheap
   (stagger decisions across ticks, e.g. each bot decides every 0.5-1 s,
   path planning amortised).
7. **On the map.** Every participant (alive real player or eliminated
   spectator in the match) sees all living bots as red dots on the world
   map (M) and the minimap, with the name on hover/near.

### 0x7FB4 client -> server: GM bot switch (version 2, replaces stage 5)

| Field | Type | Meaning |
| --- | --- | --- |
| enabled | u8 | 1 = bots on, 0 = off |
| difficulty | u8 | 0 easy, 1 normal, 2 hard |
| count | u16 | 1..200 |

A one-byte body (stage 5 client) is accepted as enabled with normal / 20.

### 0x3FB4 server -> client: bot roster (version 2, replaces stage 5)

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 2 |
| botsEnabled | u8 | the lobby's switch |
| difficulty | u8 | 0 easy, 1 normal, 2 hard |
| configured | u16 | the chosen bot count |
| count | u16 | bots listed (may exceed 255) |
| per bot: gid | u32 | the bot's world object id |
| nameLen | u16 | byte length |
| name | bytes | UTF-8 name |

### 0x3FB5 server -> client: bot positions (new)

Sent every 2 s to every participant of a running match (alive or
eliminated) while bots are alive. Positions are world grid units.

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 1 |
| count | u16 | living bots listed |
| per bot: gid | u32 | the bot (names come from 0x3FB4) |
| x, z | f32 ×2 | grid position |
| level | u8 | the bot's current level |

### Server notes (stage 7, as built)

The three wire changes are built exactly as specified above; 0x3FB2 and
0x3FB3 are byte-identical. No persisted shape changes.

**Why nobody found a bot in the first live match (root cause, with
tests).**

1. **The bots could not hurt an ordinary player and walked home.** A bot
   body is a caravan thief row (`MOB_THIEF_NPC_*`, TID4 2). A native
   thief monster refuses every target outside job mode
   (`JobTargetRefused`, 529110: job state 4 is an ordinary player), so a
   bot handed a player by `ArmRetaliation` chased it, had every strike
   refused and took the native return leg to its anchor, its drop point.
   `TestBattleRoyaleBotKeepsAttackingAnOrdinaryPlayer` (real division
   transaction, simulation tick, chase and damage owner) dropped the
   target on the first tick with the thief row and fought to the death
   with an ordinary row; it now passes with the thief row.
2. **They were dropped all over Roc.** The bots took drop points over the
   whole 5 km2 arena and noticed players only within 25 m.
3. **They got stuck behind cliffs.** One straight 40 m clipped leg per
   tick toward the goal: on the shipped Roc navmesh that rule reaches
   **0 of 40** random goals 800 m+ apart (`TestBotPathsCrossRoc`, the
   same pairs as the new planner), so a bot outside the next circle stood
   at the first cliff until the zone killed it (most likely the two
   eliminations seen).

**Fixes outside the battle royale package (port-only, nil = native).**
`monster.Instance.JobTargetRefused` refuses nobody for an actor with a
`PortCombat` projection; `acquireSightTarget` never picks a sight target
for one (the brain picks its targets); `simulation` gains
`SteerPortActor` (ends a native chase, attack, recovery, return or
follow at the live pose, then `SteerMonster`), `ReleasePortActor` and
`HealPortActor`, all refused for an actor without `PortCombat`.

**Switch.** 0x7FB4 v2 `[u8 enabled][u8 difficulty][u16 count]`; a
one-byte body is enabled/off with Normal / 20. enabled > 1,
difficulty > 2, a count outside 1..200 or any other length is malformed
and refused without an answer (as stage 5). GM-only, lobby or countdown,
as stage 5. The switch, difficulty and count carry over into the next
lobbies (`battleRoyaleMatch.botDifficulty/botCount`,
`Match.SetBotOptions`). **0x3FB4 v2** always carries a valid difficulty
(0-2) and configured count (1-200), also while the switch is off (the
lobby's stored count, default 20): the encoder replaces anything else
with Normal / 20, because the client ends the session on an invalid head
(`TestBattleRoyaleBotRosterAlwaysValid`). **0x3FB5** goes every 2 s
while at least one bot lives to every participant of the running match,
alive or eliminated, listing every living bot at the pose the brain
sampled that tick, with its level.

**Names.** Up to 200 unique names: the 40 plain names first (shuffled by
the seed), then the same names with a number ("Ares2" ... "Ares6"), each
tier shuffled; a lobby member's name is never taken.

**Drop (requirement 4).** Real players keep their stage-4 drop points;
bot i lands in a ring 150-400 m (area-uniform) around player i mod n, on
the walkable ground filter, never nearer than 150 m to any player, 20 m
apart when the ring has room (`battleroyale.BotRingPoints`). A refused
exact point is redrawn within 60 m first, then anywhere in the arena.
The bots stand after the players have dropped.

**Difficulty (requirement 5, `battleroyale/botprofile.go`).**

| | Sense | Damage | HP | Strike | Level pace | Potion | Thinks every | Roams toward a player |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Easy | 40 m | x0.6 | x0.8 | 1.8 s | x0.8 | 30 % | 1.0 s | 20 % |
| Normal | 70 m | x1.0 | x1.0 | 1.3 s | x1.0 | 70 % | 1.0 s | 40 % |
| Hard | 110 m | x1.5 | x1.3 | 0.9 s | x1.3 | 100 % | 0.5 s | 60 % |

Damage and strike apply to a bot's strikes on monsters directly; against
players the native attack keeps the row's cadence, so its hits are
scaled instead by damage x 1.3 s / strike (x0.43 / x1 / x2.17), on top of
stage 5's x2. HP scales `BotMaxHP` (`BotScaledMaxHP`, through the hits
taken). Level: `BotLevelPaced` = 1 + pace x (seconds alive / 30 + wolves /
4) + 2 per participant killed, at most 30.

**Brain (requirement 1, `battleroyale/botbrain.go`, a pure function of
the senses).** In order: (1) outside the circle the zone closes to (at
least 10 m), or inside it with the current edge reaching it within 60 s
(sampled every 2 s, only when the bot stands within 240 m of the edge):
go into it, to 60 % of its radius from the centre on the bot's side,
dropping any fight; (2) a player the native fight holds stays the target
while it lives within 1.5 x the sense range, past that the fight is
released; (3) the bot or wolf it is already fighting, by the same rule;
(4) the nearest player or bot within the sense range: a player is handed
to the native chase and attack (`ArmRetaliation`), a bot is chased and
struck; (5) below level 30 the nearest wolf within 80 m; (6) roam: toward
the nearest real player (scattered 30 m) for the profile's share of the
picks, else a random point of the circle's inner 70 %, a new goal on
arrival or after 20 s. Strikes on monsters land from 4 m every strike
interval for `BotMaxHP(level)/12 x damage x gear attack` (against a bot,
scaled to its bar over its scaled HP and gear defence). Potions: under
40 % HP, one attempt every 8 s with the profile's chance, healing
`PotionHeal(level)` (120/220/370/570/820 by `PotionTier`) of match HP.

**Gear (requirement 2).** Each bot is dealt a random build (and gender).
Every kill it makes (a wolf, a bot or a player) rolls `LootPlan` at its
level for that build; it keeps the best weapon of its build's kinds and
the best armor or shield, ranked 2 x degree + 1 for Sun. Start: the kit's
degree-1 Sun weapon (rank 3), no armor. Formula:

	attack  x (1 + 0.08 x (weapon rank - 3))   degree-4 Sun: x1.48
	defence x (1 + 0.06 x armor rank)          degree-4 Sun: hits / 1.54

The combat projection is re-installed when the level or the gear
changes: `DamageDealt = 2 x PlayerHitScale x attack`, `DamageTaken =
visible HP / (BotScaledMaxHP x defence)`. Telemetry: `match_started`
gains `botDifficulty`, `botCount` (configured) and `bots` (stood); a
bot's `player_eliminated` gains `bot`, `botLevel`, `weaponDegree`,
`armorDegree`, `wolfKills` and `potions`.

**Paths (requirement 3, `battleroyale/botnav.go`).** The server's own
monster planner is a 256-probe local detour search (3.2 m grid) for a
chase's last metres, so the bots plan their own: A* on the ground mask's
9.6 m cells (8-connected, a diagonal only between two open cells, octile
heuristic). Every cell edge a search steps over is probed once, centre
to centre, with the ordinary monster movement clip
(`MonsterSteer.PlanPath`, the clip `SteerPortActor`'s leg uses), and the
answer is kept for the match; a mask-only plan (no probe) reached only
26 of 40 goals on Roc, because thin walls and cliff lips sit between
walkable cell centres. The path is walked leg by leg (runs of one
direction, at most 30 m), so every metre walked was probed. A bot less
than 2 m nearer its waypoint for 5 s is stuck: the edge is blocked and
it re-plans from the last waypoint it reached; after 8 stucks the goal
is given up (one straight leg, and the mind picks a new roam goal).
Licensed run on the shipped Roc navmesh (40 pairs 800 m+ apart, native
run speed, 0.5 s ticks, every leg clipped by the real probe): **40/40
arrive** within twice the planned route's run time + 60 s (mean 0.99x,
worst 1.04x the planned time; Roc's detours make routes up to 3.2x the
straight line); the stage-5 rule: 0/40. One edge probe costs 6.6 us.

**Count and cost (requirement 6).** 1-200 bots. Each bot decides on its
profile's think period, staggered over the 0.5 s match ticks (Normal:
half the bots a tick); every tick still applies the zone damage. One
snapshot per tick (every bot's pose, every player's position) feeds all
the decisions; the wolf search runs only when no foe is in sense. The
planner spends at most 60 000 A* expansions (a 600 m wall detour costs
about 8 400) and 600 new edge probes (at most about 4 ms) per match
tick; a plan that does not fit waits for the next tick with its probes
kept. `TestBattleRoyale200BotsTickCost`: 200 Hard bots (every bot
thinking every tick) beside 1 500 wolves cost **0.91 ms per brain tick**
on the fixture (no navmesh; live, add at most the probe budget). Wolves:
the bots are living monsters of the layer, so the 3 000 live cap already
holds wolves + bots; what they may claim of it is lowered: a bot weighs
4 wolves in the zone target (a player 16), and the local supply gives
the bots 200 wolves between them (10 each up to 20 bots, at least 2),
the players served first.

**Economy (`TestEconomySimulation`, `TestEconomySimulationOnRoc`).** The
simulation now takes the bot count and difficulty (the profile's strike,
damage and pace, gear from `LootPlan`, the local share, the ring drop).
Licensed run on the Roc mask, three seeds per line-up:

| Line-up | Lvl 30 before phase 5 | Lvl 30 median / p95 (min) | Kills / player | Peak live monsters | Min wolves in zone / participant | Bot wolf kills | Best bot weapon / armor degree |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2 + 20 Normal | 6/6 | 10.0 / 10.4 | 186 | 1940 | 10.9 | 1227 | 4 / 4 |
| 5 + 20 Normal | 15/15 | 10.1 / 10.7 | 179 | 2071 | 11.0 | 1223 | 4 / 4 |
| 10 + 20 Normal | 30/30 | 10.1 / 11.2 | 191 | 2357 | 7.9 | 1220 | 4 / 4 |
| 16 + 20 Normal | 48/48 | 10.0 / 10.9 | 186 | 2535 | 9.0 | 1232 | 4 / 4 |
| 10 + 100 Normal | 30/30 | 10.0 / 10.7 | 187 | 2096 | 5.0 | 5683 | 4 / 4 |
| 10 + 200 Normal | 30/30 | 10.1 / 11.5 | 201 | 2151 | 4.5 | 11523 | 4 / 4 |
| 10 + 20 Hard | 30/30 | 10.1 / 12.1 | 188 | 2280 | 7.9 | 1034 | 4 / 4 |
| 10 + 100 Hard | 30/30 | 10.0 / 10.9 | 184 | 2029 | 5.0 | 4665 | 4 / 4 |
| 10 + 200 Hard | 30/30 | 10.1 / 11.6 | 202 | 2135 | 4.5 | 9356 | 4 / 4 |

Phase 5 starts at 17.25 min in every line-up. The floor the simulation
asserts every second before phase 5 is 8 living wolves in the zone per
player plus 2 per bot. The simulation's bots farm the whole match and
never fight (the supply's worst case).

**Tests.** Core: `TestBotProfiles`, `TestBotGear`,
`TestBotOptionsAndNames` (200 unique names), `TestBotRingPoints`,
`TestBotBrainRules` (every rule in order, the 60 s edge lead, the 1.5 x
disengage), `TestBotBrainHuntsPlayers`, `TestBotBrainPotion`,
`TestBotNavAroundAWall` (a mask wall and a wall only the probe knows),
`TestBotNavBudget`, `TestBotSupplyShares`, the economy simulation;
licensed `TestBotPathsCrossRoc` and the Roc economy. Wire: the v2 roster
bytes, the always-valid head, the switch decoding (v1 and v2), the
0x3FB5 bytes. Monster: `TestScriptedJobMonsterRefusesNobody`.
Simulation: `TestPortActorReleaseSteerAndHeal`. Wiring:
`TestBattleRoyaleBotKeepsAttackingAnOrdinaryPlayer`,
`TestBattleRoyaleBotSwitchV2`, `TestBattleRoyaleBotsDropAroundThePlayers`,
`TestBattleRoyaleBotMapEveryTwoSeconds` (the spectator included),
`TestBattleRoyaleBotHuntsAndReleasesAPlayer`,
`TestBattleRoyaleBotGearAndPotion`, `TestBattleRoyale200BotsTickCost`,
and the stage-5/6 bot tests on the v2 roster.

## Stage 8: bots as real characters, full sets (owner request, 2026-10-18)

1. **Bots are characters, not thieves.** Each bot is a real-looking
   player character of a random build (race, gender, model from the
   build table) with visible equipment, shown to clients exactly like a
   player (character spawn, not a monster spawn), with its name in red
   (the 0x3FB4 roster keeps naming its gid).
2. **Real-player behaviour.** Bots use their build's skills (the same
   auto-granted skills match characters get, with cooldowns and MP),
   farm monsters and level up from them, pick up drops from the ground
   (their own kills' loot and anything near), equip better items and
   build sets, drink potions, and fight players and other bots with the
   same combat rules as players (no special damage formulas beyond the
   difficulty scale).
3. **Damage between players and bots** must follow the normal
   player-vs-player pipeline both ways (skills, defence, parry/crit,
   balance), tuned so a fight lasts roughly as long as player vs player
   at the same level and gear.
4. **Full sets.** Every build must be able to complete a whole set during
   a match: all armor parts of its class (head, shoulders, chest, legs,
   hands, feet — shoulders were missing in play), weapon, shield for
   shield builds, earrings, necklace and two rings. Drops are biased
   towards the killer's missing or outdated slots so sets complete
   naturally while levelling (a bad-luck protection), and stay within the
   killer's level window.

### Server notes (stage 8: full sets)

Owner report: "you are missing shoulders for each character". No wire
change and no persisted shape change.

**What was wrong.** The shoulder rows were never filtered out: every
CH/EU heavy, light and garment set ships `_SA_` rows of degrees 1-4 in
both seals (itemdata TID4 2), the required levels match
`RequiredLevel` (9/17/25 above degree 1) and the wiring finishes and
equips them like any part. The loot simply had no notion of a set:

- it drew armor uniformly over seven codename parts per degree, but
  the body has six sockets: `HA` (helm) and `CA` (crown/diadem) are
  both TID4 1, the head. The code called `CA` the "chest" and `BA` the
  "body", so the head took two draws in seven and every other part
  one;
- each part's row is in the [level-3, level] window for only four
  levels of every eight (shoulders at 1-4, 9-12, 17-20, 25-28; never at
  29-30, where players spend the second half of the match), and
  nothing remembered which slots a player still lacked, so a run of
  draws could pass a slot's whole window by. In the set-blind
  simulation (the new slot model without the bias) only 54-83 % of the
  players held a complete set at phase 5, shoulders 83-100 %, the
  shield 33-100 %.

**Set slots (`battleroyale/lootset.go`).** Twelve slots: head (HA or
CA), shoulders (SA), chest (BA), legs (LA), hands (AA), feet (FA),
weapon (the build's kinds), shield (shield builds only), earring,
necklace, ring and second ring. `SlotOf` maps a Moon/Sun codename to
its slot and degree for the build's race, gender and class;
`HeldSet` reads the best degree per slot from everything a character
holds (worn and bagged, the two best rings in the ring slots). A slot
is current at a level when it holds degree 4 or the newest degree its
rows offer at that level (`SetState.SlotCurrent`; nothing newer can
drop), and a set is complete when every slot of the build is current.

**Bias and pity (`LootPlan`; removed by stage 9, see its server notes).** The table is unchanged: 3 stacks a
kill plus one at 50 %, equipment 45 % of the stacks, Moon 2/3 and Sun
1/3, every row inside [level-3, level], at least three blues and whites
of 50 % or more (the wiring). Every equipment draw now picks a SLOT
among the build's slots that have a row in the window, weighted by the
stage-4 family share split over the family's slots (weapon 14, shield
3, armor 18 over six slots plus the shield's 3 for a build without
one, accessories 10 over four slots) times the slot's state:

| State | Factor |
| --- | --- |
| missing (holds nothing) | x3 |
| outdated (holds a degree below the window's row) | x2 |
| current | x1 |

Pity: a per-participant `LootLedger` counts, per slot, the kills that
dropped equipment while that slot was missing or outdated and had a row
in the window, without dropping it. A slot at `PityKills` (4) or more
is owed: the kill's next equipment draw drops it (most starved first,
one owed slot per draw); dropping a slot resets its count. The supply
drop's armor and accessory draws pick their slot the same way (no
pity). The wiring reads the killer's set from its bag at every kill and
keeps the ledger in `battleRoyaleRuntime.ledgers` keyed by name, reset
when the hold's `HeldAtMs` changes (a new match); server memory only,
never persisted. A bot's loot (no set given) weighs every slot as
missing, i.e. by its family share.

**Auto-equip and discard (`battleroyale/autoequip.go`,
`action/battleroyale_autoequip.go`).** One score per held item
(`GearScore`), lexicographic by construction:

| Term | Weight |
| --- | --- |
| usable: the build, race, gender and class wear it (the level is the native equip gate's) | else no score |
| degree | x 100 000 000 |
| Seal of Sun | + 10 000 000 |
| plus (+N) | x 1 000 000 |
| white-stat percent points, summed over the fields | x 1 000 |
| blue options | x 100 each |
| blue values, summed | + up to 99 |

`PlanGear` wears the best usable item per slot (a worn item wins a
tie; the two best rings take both ring sockets) and discards every
unworn equipment item that is not chosen (unusable, or no better than
the worn one), never an item at `UltimatePlus` (+8). The wiring seats
each planned item through the native inventory move
(`applyInventoryMove`: the move result, visual and stat refresh to the
client, the visual to observers; an item the equip gates refuse waits
in the bag), right after every pickup and level gain, and every 5 s
(with the ground sweep) runs the full pass: equip, then drop the
discards through the native ground drop (`applyGroundDrop`, at the
character's feet, then the match's 75 s ground policy). Only TID 3.1
equipment enters the plan (never consumables, job suits or avatars),
worn items are never dropped, and socket 7 of a build without a shield
(its ammunition) is never planned. The shield slot exists only for the
shield builds, whose weapon kinds are all one-handed, so a two-handed
or ranged build never wears a shield (a shield it picks up is
dropped). `battleroyale.PlanGear` is pure and exported for the bots.

**Proof.** `TestEverySlotDropsEveryDegree` (every build, gender and
level 1-30: every window row drops, every slot including the shoulders
at every degree 1-4) and, licensed, `TestSetSlotsMatchItemdata` (7 840
window rows: Moon and Sun exist, TID3/TID4 of the slot, the class of
the build, ReqGender of the body, required level in the window);
`TestLootBiasTowardsMissing` (missing x3, outdated x2), `TestLootPity`,
`TestHeldSetAndComplete`, `TestSlotOfArmorParts`. The economy
simulation (`economy_sim_test.go` with `fullset_sim_test.go`) gives
every real player a set: it picks up and wears every upgrade of its
own drops and loots with its set and ledger; at the start of phase 5
at least 95 % of every line-up must hold a complete set. The licensed
run on the Roc mask (the unlicensed stand-in gives the same):

| Line-up | Complete set at phase 5 | Median kills to a degree-4 set | Head | Shoulders | Chest | Legs | Hands | Feet | Weapon | Shield | Earring | Necklace | Ring | Ring 2 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2 + 20 bots | 6/6 | 105 | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % |
| 5 + 20 bots | 15/15 | 105 | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % |
| 10 + 20 bots | 30/30 | 104 | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % |
| 16 + 20 bots | 48/48 | 105 | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % | 100 % |

The degree-4 set completes at about kill 105, i.e. at level 29-30 (the
degree-4 chest needs 29); every player is at level 30 before phase 5
(the stage-6 table is unchanged in shape: 100 % at 30, median 10.0-10.2
min). Wiring: `TestBattleRoyaleFullAutoEquip` (a better shoulder is
seated with one move and its visual, a worse Moon blade stays off the
socket, the discard drops the worse shoulder and blade, keeps the +8
weapon and every potion, a two-handed Warrior drops a shield and never
wears it), `TestPlanGearScoreOrder`.

### Server notes (stage 8: bots as characters, items 1-3, as built)

No wire change: 0x3FB2/0x3FB3/0x3FB4 v2/0x3FB5 are byte-identical (the
roster and the map now carry player object ids). No persisted shape
change. The stage-5/7 monster bodies, their combat projection
(`monster.PortCombat`), the simulated gear (`BotGear`) and the brain's
own strikes and heals are gone from the wiring.

- **A bot is a headless character.** At the drop each bot gets a blank
  record (id from the band 99000-99999, gid 199000-199999: the top of the
  player object band; an id a real record holds is skipped) of a build
  drawn from the match seed (race, gender), dressed by the same
  `battleroyale.Hold` a player's drop runs (the store's creation seeds:
  model, racial masteries, base skills, starter inventory), the build's
  skill grant and the kit. Its name avoids every character name of the
  division (`Config.NameTaken`).
- **Held by the store, never stored.** The record is an *ephemeral*
  record of the authority store (`data/store/ephemeral.go`, through
  `enterworld.Deps.Ephemeral`): name and id lookups resolve it (after any
  real row), every commit door runs its mutations under the store lock,
  but nothing marks it dirty: no character row, no id watermark, no
  ledger entry; a door that touched only bots commits only when the
  ground moved with it (a bot's pickup). It is never in
  `CharactersForDivision`, a character list or a login; creation refuses
  its name and skips its id while it lives. Test:
  `TestEphemeralCharacterNeverPersists` (no row after every door kind and
  after a reopen).
- **In the world without a session.** The bot is admitted to the match
  layer's population under a synthetic session id (`1<<62 + id`; the
  bots pass the layer's 16-player capacity) and gets the natural and
  potion recovery of a resident. The tick sees it through
  `simulation.Ticker.Headless`: the movement lane builds its snapshot
  exactly as a bound session's provider does
  (`movement.Runtime.HeadlessSnapshot`: live world, peer appearance with
  the worn set, body radius, combat eligibility), marked `Headless`, so
  it is a peer, a mover and a monster target like any session but never
  a viewer (no frame is built for it; the peer, pet, player-map, NPC and
  monster-scope viewer loops skip it). Every real player's client
  therefore spawns a bot with the ordinary 0x30D7 player row (CICUser,
  model, name, worn equipment), follows its moves, sees its casts,
  hits, pickups, equipment changes and death through the ordinary
  observer routing; peer frames of a session-less character go to the
  sessions observing its gid (`BattleRoyaleHeadlessGID`). The client
  needs no change: it already colours every 0x3FB4 roster gid red,
  whatever the entity kind (`ui.ts` nameplate, target window).
- **Driven through the client's own commands.** Decisions are made under
  the division lock on the 0.5 s match tick; the commands they queue run
  right after the lock is released, through the handlers a packet
  reaches: ground clicks (0x7738, `movement.Runtime.HandleMove`, wired as
  `BattleRoyaleSetup.Move`), presses (0x7074 `HandleTargetInteract`:
  skill, basic attack, pickup, cancel) and item use (0x75BD
  `HandleItemUse`). Their public frames are routed around the bot like
  any tick batch.
- **Brain** (`battleroyale.BotMind.Decide`, priority order): the zone
  (unchanged); the player or bot its attack holds, within 1.5 x sense;
  the nearest player or bot in sense; the monster its attack holds; a
  drop within 30 m it wants (when no foe holds it); below 30 the nearest
  wolf within 80 m; roam (hunting the nearest player for the profile's
  share). A target past 25 m is walked to on the A* route (stage 7
  planner, stuck re-plan unchanged); nearer, the bot presses at most
  once per the profile's StrikeMs (1.8 / 1.3 / 0.9 s): a heal of its build
  on itself under 60 % HP, a buff of its build that is not up, the
  strongest ready attack skill (by required mastery level, then rank;
  readiness is the server's own command admission: cooldown, MP,
  weapon, ammunition), else the basic attack. The server's approach,
  casting, continuation, PvP and death rules do the rest. A fight the
  mind lets go is cancelled. Potions: HP under 40 %, MP under 25 %, once
  per 8 s each, with the profile's chance, the best degree in the bag
  (an all-round potion when none).
- **Growth and gear are the players'.** A credited kill pays the match
  EXP (`MatchKillExp`) at the difficulty's pace (x0.8 / x1 / x1.3, still
  under one level a kill) and levels trains the masteries and learns
  the build skills exactly as for a player. A bot wants a potion or pill
  while it carries fewer than 60 of the kind, and equipment the set
  planner (`battleroyale.PlanGear`, the auto-equip's own) would wear; the
  pickup's auto-equip seats it and the periodic gear pass discards the
  rest, as for a player. Its own kills drop the ordinary match loot.
- **Damage (item 3).** Bots and players hurt each other only through the
  PvP pipeline (`battleRoyaleOpponents`: both held, same layer): the
  stage-5/7 scaling (x2 damage, the strike-pace factor, the gear
  formula, the HP projection) is gone. The difficulty is the one rule on
  top, as keeper writes on the bot's own stats, multiplied into the
  match's own percents: Easy HP x0.8 and every attack range x0.6, Normal
  nothing, Hard x1.3 and x1.5. Measured on the fixture (same build,
  level, gear; `TestBattleRoyaleBotPvPMatchesAPlayer`): Normal bot and
  player hit each other for 20, both hold 500 HP: 25 hits to kill either
  way, exactly player vs player. Hard: the bot hits for 32 and holds 650
  (player needs 32.5 hits, bot 15.6); Easy: 10 and 400 (50 vs 20). A hit
  is attack less defence, so it moves a little more than the attack's
  scale (x1.6 / x0.5 for x1.5 / x0.6).
- **Death and cleanup.** A bot dies like any match character (its death
  is a team death; the killer is credited; the kill feed, streaks,
  scoreboard, roster and bot-winner rules are unchanged). Its corpse lies
  5 s, then the body leaves the tick (observers get the despawn), the
  population and every runtime plane (the session close's path) and the
  store. The close retires every bot left before the layer is released.
  Telemetry `player_eliminated` of a bot: `bot`, `botLevel`, `build`,
  `weaponDegree` and `armorDegree` (worn), `wolfKills`, `potions`.
- **Cost (requirement 6 of stage 7, re-measured).**
  `TestBattleRoyale200BotsTickCost`: 200 Hard bots (characters, every bot
  thinking every step, the worst case) cost 15.3 ms per brain step
  (decisions and every command they issue) and 0.23 ms per tick for the
  200 tick snapshots, on the fixture (no navmesh; live adds the
  planner's probe budget, at most about 4 ms a step, and each move's
  movement clip). At Normal half the bots think a step. 200 stays the
  cap; wolves + bots stay under the 3 000 live cap (the census counts
  every standing bot beside the living monsters) and the bots weigh in
  the supply plan as before.
- **Tests.** Store: `TestEphemeralCharacterNeverPersists`. Simulation:
  `TestTickerHeadlessIsAPeerNeverAViewer`. Core: `TestBotBrainRules`,
  `TestBotBrainPotion` (stage 8 rules). Wiring:
  `TestBattleRoyaleBotsStandAsCharacters`,
  `TestBattleRoyaleBotSpawnsForAPlayerAsACharacter` (a real player's
  tick gets the bot's 0x30D7 row with its weapon, then its walk; nothing
  is built for a bot), `TestBattleRoyaleBotLevelsOnAWolf`,
  `TestBattleRoyaleBotPicksUpAndWears`, `TestBattleRoyaleBotDiesAndLeaves`,
  `TestBattleRoyaleBotKillsTheLastPlayerAndWins`,
  `TestBattleRoyaleBotsWalkIntoTheZone`,
  `TestBattleRoyaleBotsLeaveWithTheLayer`,
  `TestBattleRoyaleBotHuntsAndReleasesAPlayer`,
  `TestBattleRoyaleBotDrinksAPotion`, `TestBattleRoyaleBotPvPMatchesAPlayer`,
  `TestBattleRoyale200BotsTickCost`, and the stage-5/7 switch, ring,
  map and capped-farming tests on characters.
- **Left in place.** The monster-side stage-7 hooks (`monster.PortCombat`,
  `SteerPortActor`, `ReleasePortActor`, `HealPortActor`, the
  `JobTargetRefused` exemption) are nil-is-native and now unused by the
  mode; the core's `BotLevel`, `BotMaxHP`, `BotCodename`, `BotModel` and
  `BotGear` remain for the economy simulation's bot model.

## Stage 8c: bots that farm, gear up and fight like players (owner request, 2026-10-18)

Owner report after the live matches: bots never farm, gather items,
build sets or use skills like real players; they kill each other at once;
a stuck bot should be relocated. No wire change and no persisted shape
change.

### Live evidence and root causes

The `battle_royale_player_eliminated` events of the last matches (100 and
200 Hard bots; 1-2 players): 90-194 bots out within three minutes, the
first at 5 s, almost all killed by other bots, every one at botLevel 1
with weapon and armor degree 1 and wolfKills 0. `zone_replenished`
reported `monsters=0` at phases 1-3.

1. **The brain was a priority list with fights first.** Rule 3 of the
   stage-7/8 brain ("the nearest player or bot within sense") outranked
   farming, and the ring drop kept bots only 20 m apart inside a
   150-400 m ring, well inside each other's 70/110 m sense. Every bot
   started a fight on its first decision; the wolf search did not even
   run while a foe was in sense.
2. **Buffs ate the MP.** On the shipped skill data a level-20 Blade-Fire
   bot pressed five fire buffs in a row (568 MP down to 64) and fought on
   with its basic attack: 1 attack skill in 40 s.
3. **`zone_replenished monsters=0` is not a starved zone.** The record
   reports only the pass at the phase start; the 15 s passes before it
   had already stocked the next circle to its target (measured: 1 622
   inside against a target of 1 585 at the phase-1 start, nobody farming
   because the bots were fighting). The local supply, which feeds the
   levellers, was never reported. One real defect was found in it: a
   pass did not count the wolves it had just stood, so bots standing
   together each stood a full share (three bots on one spot: 30 wolves
   instead of 10), which filled the 3 000 live cap with wolves nobody
   farmed. Fixed and tested (`TestBattleRoyaleLocalSupplyCountsItsOwnWolves`).

### Research summary

- Battle royale and MMO bots are mostly built with **utility AI** (the
  "infinite axis utility system", Dave Mark, GDC AI Summit 2013/2015,
  shipped in Guild Wars 2: Heart of Thorns): every candidate action is
  scored as a product of considerations (response curves over distance,
  HP, cooldowns, threat) and the best wins; designers pick the actions an
  agent has. A small behaviour tree or fixed override sits on top for
  absolute rules (the zone). Learned AI is considered unreliable.
- **Phase-dependent behaviour**: players expect bots to loot and gear
  early, run the circle mid-game and fight late; PUBG's bots (7.1) were
  tuned on live data for what players carry in each match phase and
  their loot was targeted per phase. Bots should not be much stronger
  than players early.
- **Aggression escalation**: a per-bot alert/aggression meter that rises
  with combat events (being hit, a player near, fights nearby) and
  decays over time; above a threshold the bot switches to hunting.
- **Threat assessment and retreat**: weigh a target by its strength
  against the bot's own (level, gear, HP); real players criticise bots
  that never retreat or heal and move aimlessly.
- **Anti-dogpile**: limit how many agents attack one target (the
  attack-token idea), and prefer targets nobody is fighting.
- **Hysteresis**: give the current action a momentum bonus and a short
  commitment so the agent does not flip between two close scores.

Sources: gameai.com/iaus.php; gamedeveloper.com "(User Research) AI
designs in battle royale games"; techraptor.net and legitreviews.com on
PUBG update 7.1 bots.

### Server notes (stage 8c, as built)

- **Utility brain** (`battleroyale/botbrain.go`, file header has the full
  model). The zone stays absolute. Candidates and score:

  | Candidate | Score |
  | --- | --- |
  | farm (below 30) | Farm x distance |
  | loot | Loot x distance |
  | player | Player x aggression x distance x threat x own HP x foe HP |
  | bot | Bot x aggression x distance x threat x own HP x foe HP / (1 + bots attacking it) |
  | retaliate | Retaliate replaces Player/Bot for a foe attacking the bot |
  | retreat | 0.8-1.0 under RetreatBelow HP while a healthier foe attacks it |
  | roam | 0.1 (floor) |

  Stage weights by zone phase (`BotStageOf`):

  | Stage | Farm | Loot | Player | Bot | Retaliate |
  | --- | --- | --- | --- | --- | --- |
  | early (phases 0-1) | 0.70 | 0.75 | 1.10 | 0.05 | 1.20 |
  | mid (phases 2-3) | 0.50 | 0.60 | 1.10 | 0.40 | 1.20 |
  | late (phase 4 on, final) | 0.20 | 0.45 | 1.20 | 0.85 | 1.20 |

  Distance is 1 within 25 m, falling linearly to 0.5 at the candidate's
  range (sense; 1.5 x sense for the engaged foe or an attacker; 80 m for
  a wolf; 30 m for a drop). Threat is 0.4 + 0.6 x logistic((own power -
  foe power + bravery) / 4), power = level + worn degrees / 2. Own HP is
  0.3 + 0.7 x HP share; a wounded foe scores up to x1.3. The last choice
  and the target the attack holds get +0.15 momentum; a choice holds 2 s
  unless the zone, a retreat or the loss of its target ends it. A retreat
  walks 40 m away from the threat (into the circle) for at least 4 s.
- **Alert meter** (coordinator request): every decision it decays by
  exp(-elapsed / AlertDecayMs) and rises +0.5 per foe attacking the bot,
  +1.0 x HP share lost, +0.35 x distance per real player in sense and
  +0.1 per bot fight in sense (max 2). At or above the threshold the bot
  is aggressive: Player x1.5, Farm x0.5, Loot x0.7, Bot at least 0.3.
  A real player near a Normal bot turns it on him within 2-3 decisions
  (`TestBotAlertMeter`; in the acceptance run a farming bot attacked the
  player 4 s after he walked up to it).
- **Difficulty** (`botprofile.go`), beside the stage-7 numbers:

  | | Aggression | Bravery | Retreats below | Alert threshold | Alert decay |
  | --- | --- | --- | --- | --- | --- |
  | Easy | x0.8 | -3 levels | 15 % HP | 0.9 | 10 s |
  | Normal | x1.0 | 0 | 30 % HP | 0.6 | 20 s |
  | Hard | x1.2 | +3 levels | 35 % HP | 0.4 | 30 s |

- **Senses** (`action/battleroyale_botbrain.go`): every sighted
  participant carries its level, summed worn degrees, HP share and the
  object its attack holds; a bot knows which foes attack it and how many
  bots attack each foe. The wolf is searched whatever foe is in sense and
  drops are sensed while fighting (the brain weighs them).
- **Skill presses**: a buff is pressed only while the MP pool is at least
  half full (the rest is kept for attack skills); heals, attack-skill
  ranking and the server's own readiness (cooldown, MP, weapon, ammo)
  are unchanged.
- **Spawn** (`battleroyale/drop.go`): bots drop at least 120 m apart
  (`BotRingSpacingM`), 150 m+ from every player; the 150-400 m ring holds
  about 34; a full ring widens by 100 m at a time (at most 12 times,
  `BotRingReach` 1 600 m).
- **Stuck relocation** (`action/battleroyale_botwatch.go`): a bot whose
  decision is a walk (zone, roam, chase, retreat, pickup; a goal already
  within 5 m does not count) and that has not come 5 m from where it
  stood for 10 s, or that stands off the walkable ground mask for 3 s,
  is relocated to a random walkable point of the inner 70 % of the circle
  the zone closes to, the first of 24 draws out of every real player's
  interest when one exists (so the peer visibility despawns it where it
  stood and spawns it where it lands). The relocation is the in-layer
  form of the relocation door for a session-less character (world stand
  and record world under the character's door, pending pickup, combat
  intent and finalizes cleared, resident region re-bound), counted and
  recorded as `battle_royale_bot_relocated` (reason no-progress /
  off-ground, from, to, count).
- **Telemetry**: a bot's `player_eliminated` gains `kills`,
  `skillsCast` (non-buff skills, never the basic attack, counted where
  every cast starts), `buffsCast`, `itemsPicked` (counted where a pickup
  lands), `relocations` and `gearDegrees` (summed worn degrees);
  `wolfKills` stays. Every elimination by a bot carries `killerBot=1`.
  New `battle_royale_wolf_supply` every 60 s: zone and local wolves stood
  and retired since the start, the last zone pass's stock inside the
  next circle against its target, the live count, and how many levelling
  bots have no wolf within 80 m.
- **Tests**. Core: `TestBotStageOf`, `TestBotBrainRules` (rewritten),
  `TestBotUtilityByStage`, `TestBotUtilityNoDogpileAndThreat`,
  `TestBotUtilityRetreat`, `TestBotUtilityHysteresis`, `TestBotAlertMeter`,
  `TestBotRingPoints` (120 m spacing, widening). Wiring:
  `TestBattleRoyaleBotsDropAroundThePlayers` (spacing),
  `TestBattleRoyaleLocalSupplyCountsItsOwnWolves`,
  `TestBattleRoyaleStuckBotIsRelocated`, `TestBattleRoyaleOffGroundBotIsRelocated`,
  `TestBattleRoyaleFightingBotIsNotRelocated`. Licensed (shipped
  skilldata, itemdata, leveldata, the progression's EXP commit):
  `TestBattleRoyaleBotsCastTheirBuildSkills` (one bot of every build at
  level 20, 40 s: attack skills cast 2-11 per build, buffs cast by every
  build whose kit admits one; build 4's only buffs at level 20 are
  shield buffs and its kit carries no shield) and `TestBattleRoyaleBotsFarmFirstAcceptance`.
- **Acceptance** (1 player + 20 Normal bots, the whole action tick every
  100 ms for 5 minutes, shipped skills; the fixture wolf has 300 HP and
  does not strike back, the player's HP is refilled every second):
  median bot level **11** (8-13), **0** bot deaths (0 to bots in the
  first 3 minutes), every bot cast skills (about 870 attack skills and
  80 buffs in all), about 1 190 pickups, 745-758 wolf kills, 0
  relocations, at every 30 s sample **0** levelling bots without a wolf
  within 80 m, and the bot the player walked up to attacked him 4 s
  later.

## Stage 8b: airdrop with an ultimate weapon (owner request, 2026-10-18)

- The supply drop becomes an **airdrop**: at each phase start the server
  picks the airdrop point (the next circle's centre, on walkable ground),
  announces it with a banner ("Airdrop incoming at the next zone - check
  your map!") and marks it for every participant on the minimap and the
  world map (M) until it is looted (the guarding giant dies) or the phase
  ends.
- The airdrop guardian's loot can contain the **ultimate weapon**: a
  Seal of Sun weapon of the highest degree the killer can use (level
  window), of the killer's build, at **+8**, with the **maximum number of
  blue options** the item's native pool allows (all distinct, values at
  their top roll) and white stats at 100 %. Chance per airdrop: 35 %
  (guaranteed on the final phase's airdrop). When it drops, a shard-wide
  banner names the killer ("<name> found the ultimate weapon!").

### 0x3FB6 server -> client: airdrop marker

Sent to every participant when an airdrop appears, is looted or expires.

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 1 |
| active | u8 | 1 = show the marker, 0 = remove it |
| x, z | f32 ×2 | grid position |
| phase | u8 | the zone phase it belongs to |

### Server notes (stage 8b: airdrop, as built)

- **Airdrop.** At every phase start `replenishBattleRoyaleArena`
  removes the last phase's marker, stands the guardian (the supply-drop
  giant wolf, `SupplyElite`) at the next circle's centre (walkable
  ground nearby when the centre is not), registers the airdrop by the
  guardian's gid (`action/battleroyale_airdrop.go`) and sends every
  participant 0x3FB6 active 1 at the guardian's grid position, then the
  channel-7 banner "Airdrop incoming at the next zone - check your
  map!". The guardian's death (the loot hook) or the next phase start
  sends 0x3FB6 active 0 for that phase. An expired airdrop's guardian
  keeps its loot.
- **Ultimate weapon.** Rolled when the airdrop drops
  (`RollAirdropUltimate`): 35 %, always on the final phase. Its loot is
  `SupplyDropLoot` plus a Seal of Sun weapon of the killer's build
  (`UltimateWeaponCodename`, a kind drawn uniformly) at the newest
  degree the killer can wear (`DegreeForLevel`; the [level-3, level]
  window holds no weapon row at levels such as 29-30, so the window
  rule would give none), with `Plus` 8 (the item row's plus byte, as
  every enhanced item carries it), every white field at 31 (100 %) and
  `loot.EquipmentMagicMaximum`: every distinct option of the item's own
  native pool the generator could roll on it (allowed, probability
  above zero, not nonrepair), one per magic tag, up to its MaxMagic,
  each at its top value. The finder is named on channel 7 to the whole
  shard: "<name> found the ultimate weapon!". The auto-equip seats it
  (it outscores everything of its degree) and never discards it.
- **Tests.** `TestAirdropUltimateChance`, `TestUltimateWeaponCodename`,
  `TestEquipmentMagicMaximum` (every Sun weapon of degrees 1-4),
  `TestBattleRoyaleAirdropEncoding` (11 bytes), and
  `TestBattleRoyaleAirdrop` (marker on drop at the guardian, the
  ultimate weapon's properties, the shard banner, removal on the kill,
  once only, removal at the next phase).

## Stage 9: random loot, airdrop flight line, compass, skins (owner, 2026-10-18)

1. **Random loot (replaces the stage-8 set bias).** Equipment drops are
   random across ALL builds, races and genders (still Moon/Sun, ≥3 blues,
   whites ≥50 %, level window by the killer's level), so players build a
   set by collecting what they find. Remove the per-killer slot bias and
   the pity rule. Auto-equip keeps wearing the best usable piece; bag
   items the player cannot use are not auto-dropped any more (players may
   want to drop or keep them); worse usable duplicates are still dropped.
2. **Fireworks only at the airdrop.** No more random fireworks in the
   zone. While an airdrop is landed and its guardian alive, fireworks go
   off at the airdrop point (client-side, every 3-6 s), and stop when the
   guardian dies.
3. **Compass (client).** A HUD compass strip at the top showing the
   player's facing (N, NE, E, SE, S, SW, W, NW with degree ticks) and the
   player's coordinates (game X/Y in metres, as the original shows them).
4. **Skins with buffs.** A rare random drop (~1 % of equipment draws):
   an avatar dress/hat/attachment item of any gender, carrying ≥2 blue
   options from its native pool (if the avatar has a pool; otherwise the
   plain avatar).
5. **Airdrop flight line, always in the safe zone.** 60 s before each
   airdrop lands, every participant sees on the minimap and world map a
   straight diagonal line (the flight path) crossing the NEXT circle,
   with an "Airdrop incoming" banner and a timer. The landing point is a
   random point ON that line INSIDE the next circle, on walkable ground.
   At landing the line disappears, the marker (and the guardian) appear
   at the point.

### 0x3FB6 server -> client: airdrop (version 2, replaces version 1)

| Field | Type | Meaning |
| --- | --- | --- |
| version | u8 | 2 |
| state | u8 | 0 none (hide), 1 incoming (show the line), 2 landed (show the marker) |
| phase | u8 | zone phase it belongs to |
| ax, az | f32 ×2 | flight line start (grid units) |
| bx, bz | f32 ×2 | flight line end |
| x, z | f32 ×2 | landing point (state 2; 0 otherwise) |
| etaMs | u32 | ms until landing (state 1), else 0 |

### Server notes (stage 9: random loot, skins, flight line; items 1, 4, 5 as built)

0x3FB6 is version 2 exactly as the table above (31 bytes); 0x3FB2-0x3FB5
are byte-identical. No persisted shape change. Items 2 and 3 are the
client's.

- **Random loot (item 1, `battleroyale/lootrandom.go`).** The stage-8
  slot bias, the pity ledger (`LootLedger`, `PityKills`, the
  `battleRoyaleRuntime.ledgers` map) and `HeldSet` are gone; `Killer`
  keeps only build, level and body. The table is unchanged (3 stacks a
  kill plus one at 50 %, equipment 45 %: weapon 14, armor 18, accessory
  10, shield 3; Moon 2/3, Sun 1/3; [level-3, level] window; three or more
  blues, whites 50 % or more), but an equipment draw now picks its row
  uniformly among the family's window rows of **every** build: the union
  of what every build of the table wears, 356 rows over degrees 1-4 (per
  degree the eleven weapon kinds of the builds, both shields, the 70
  armor rows of the ten bodies: CH male/female heavy and light, EU
  male/female heavy, light and clothes, and both races' earrings,
  necklaces and rings). Only the killer's level matters: two killers of a
  level draw the same plan from the same stream
  (`TestLootIgnoresTheKiller`). The supply drop is random the same way;
  the airdrop's ultimate weapon stays the killer's build (stage 8b).
  A Blade-Fire male can wear about a quarter of what drops at level 18.
- **Auto-equip.** `PlanGear` still wears the best usable piece per slot
  and still discards a usable piece no better than the worn one (the
  periodic pass drops it at the character's feet); a piece the character
  can never use (another build, race, gender or class, a shield of a
  build without one) is neither worn nor discarded: it stays in the bag.
  Bots are unchanged: they pick up only what `PlanGear` would wear.
- **Skins (item 4).** An equipment draw becomes a skin at 1 in 100, at any
  level (levels 5-7 included): one of 26 item-mall avatars
  (`SkinCodenames`: the EU wedding, Halloween, pirate, fur and knight hats
  and dresses, the angel and devil wing dresses and the Halloween
  attachment, male and female; TID 3/1/13/1-3, the licensed test checks
  each). The event avatars and the job flag (TID4 4) and the MABT hats
  (MaxMagic 0) are left out. A skin carries no whites and 2 to MaxMagic
  (4 for dresses, 2 for hats and the attachment) distinct blue options
  of its part's native avatar pool (`loot.AvatarMagic`: the
  magicoptionassign row of TID3 13 and its TID4 joined to the degree-1
  option rows, one per tag, valued as the smith values them,
  `alchemy.RollSingleRange` clamped to 1..0x6A4; hats draw from
  STR/INT/ER/MP/DARA/MPRG, dresses from STR/INT/HR/HP/DRUA/HPRG/MDIA,
  the attachment from HR/HPRG). The attachment's itemdata MaxMagic is 1;
  the owner's two options win. The auto-equip never plans an avatar, so
  a skin waits in the bag for the player to wear.
- **Flight line (item 5, `battleroyale/airdrop.go`,
  `action/battleroyale_airdrop.go`).** At every phase start the last
  airdrop goes (state 0) and the new one is announced: state 1 to every
  participant with its line and `etaMs` = 60 000, and the channel-7
  banner "Airdrop incoming - watch the flight line on your map!". It
  lands `AirdropLeadMs` (60 s) later on the match tick (every phase
  lasts at least 3:10, so it always lands inside its phase): the
  guardian giant stands at the landing point, the ultimate weapon is
  rolled (rules unchanged), and state 2 goes out with the landing point
  (the guardian's position) and the line kept. The guardian's death or
  the next phase start sends state 0. Algorithm (`PlanFlightLine`): the
  landing circle is the phase's next circle (at least 50 m around the
  final phase's point target); a direction within 15 degrees of one of
  the four diagonals; a perpendicular offset from the centre uniform in
  ±60 % of the radius; ends 500 m beyond the circle on both sides; the
  landing point uniform on the chord (kept within 95 % of its half
  length) and, when that ground is not walkable (`battleRoyaleGround`:
  the Roc mask and the exact navmesh test), slid along the line in
  9.6 m steps alternating both ways to the nearest walkable point of the
  chord; a chord with none draws a new line (32 lines; then the last
  line is kept and the guardian's spawn finds ground within 50 m). The
  frame of state 1 carries x, z = 0; state 0 keeps the line. Telemetry:
  `battle_royale_airdrop_landed` (phase, x, z, ultimate);
  `zone_replenished` loses its `elite` field.
- **Set completion under random loot (honest numbers).** The economy
  simulation (`fullset_sim_test.go`, each player picks up its own drops
  and wears what its build can use; nobody picks up another's drops) on
  the licensed Roc mask, three seeds per line-up, complete set at the
  start of phase 5 (every slot at the newest degree its rows offer, i.e.
  degree 4 everywhere at level 30):

  | Line-up | Complete set | Head | Shoulders | Chest | Legs | Hands | Feet | Weapon | Shield | Earring | Necklace | Ring | Ring 2 |
  | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
  | 2 + 20 bots | 0/6 | 100 % | 33 % | 83 % | 83 % | 17 % | 17 % | 83 % | 67 % | 100 % | 100 % | 83 % | 50 % |
  | 5 + 20 bots | 0/15 | 80 % | 40 % | 67 % | 87 % | 20 % | 20 % | 73 % | 67 % | 67 % | 100 % | 93 % | 80 % |
  | 10 + 20 bots | 0/30 | 87 % | 27 % | 80 % | 80 % | 20 % | 27 % | 47 % | 50 % | 87 % | 100 % | 90 % | 43 % |
  | 16 + 20 bots | 0/48 | 98 % | 35 % | 69 % | 67 % | 38 % | 25 % | 65 % | 42 % | 88 % | 100 % | 81 % | 48 % |

  Nobody completes a set (the unlicensed stand-in mask gives the same
  0 %). Two causes: a player wears one body of ten, so a given armor row
  is one of up to 70 armor rows of its window; and the [level-3, level]
  window offers the degree-4 hands (24), shoulders (25) and feet (26)
  only at levels 24-29, which a player crosses in about 15 kills, while
  it spends the ~85 kills after level 30 in a window ([27, 30]) without
  them. The levelling pace is untouched (100 % at level 30 before phase
  5, median 10.0-10.1 min).

  **Proposal (shipped 2026-10-18 as +5 stacks with the cap window; see
  "Server notes (stage 9b)" below).** Keep the
  randomness, raise the supply: `LootTuning{ExtraEquipment: n,
  TopDegreeAtCap: true}` adds n equipment-only stacks to every kill
  (families by the table's equipment weights) and gives a level-30
  killer every top-degree row (required 24-29) instead of [27, 30].
  Measured (licensed, `runLootProposals`, 5 / 16 players + 20 bots):

  | Tuning | Complete set (5 players) | Complete set (16 players) | Peak ground items (16) |
  | --- | --- | --- | --- |
  | cap window only | 1/15 | 0/48 | 836 |
  | +3 stacks, cap window | 1/15 | 18/48 | 1537 |
  | +5 stacks, cap window | 7/15 | 31/48 | 2025 |
  | +8 stacks, cap window | 12/15 | 37/48 | 2071 |

  About +5 equipment stacks a kill with the cap window reaches half the
  players (47-65 %), +8 about 80 %; the simulated ground then reaches
  the 2 000-item cap (its players never pick anything up; the cap drops
  the oldest first). Switching it on is one line (`LootPlan` passing the
  tuning) once the owner chooses a value.
- **Tests.** Core: `TestLootIgnoresTheKiller`,
  `TestRandomLootSpansEveryBuild` (47 window rows at level 18, 356 in
  all, skins about 1 % of equipment draws, all 26 seen),
  `TestEverySlotDropsEveryDegree` (every build's own window rows still
  drop at every level and every slot at every degree),
  `TestSkinCodenames`, `TestSetStateAndComplete`, `TestLootPlanWindow`,
  `TestSupplyLoot` (random weapon of the window), `TestPlanFlightLine`
  (400 lines: diagonal, ends 500 m out, landing on the line inside the
  circle; sliding onto a 20 m walkable band; a new line for a half-plane;
  none over unwalkable ground), `TestAirdropTiming`; licensed
  `TestRandomLootAndSkinsMatchItemdata` (every row Moon and Sun, every
  skin an avatar hat/dress/attachment of one gender with a pool of two or
  more). Wire: `TestBattleRoyaleAirdropEncoding` (31 bytes, every field).
  Wiring: `TestBattleRoyaleAirdrop` (state 1 with the line crossing the
  next circle and the timer, no guardian before landing, not a tick
  early, state 2 at the guardian on the line inside the circle on
  walkable ground, state 0 on the kill with the ultimate weapon and the
  shard banner, state 0 at the next phase),
  `TestBattleRoyaleAirdropFollowsThePhases` (every phase including the
  point target), `TestBattleRoyaleDropsCarryBluesAndHighWhites` (other
  builds' rows, skins with 2-4 pool options and no whites),
  `TestBattleRoyaleFullAutoEquip` (unusable shield and shoulder kept,
  worse usable items dropped), `TestPlanGearScoreOrder`,
  `TestBattleRoyaleMonstersTopUpsAndSupplyDrop` (the guardian a minute
  after the phase start at the landing point).

### Server notes (stage 9b: loot tuning and the Devil's Spirit airdrop, as built)

Owner-approved 2026-10-18. No wire or persisted shape change.

- **Loot tuning (live).** `LootPlan` now rolls `LiveLootTuning()` =
  `LootTuning{ExtraEquipment: 5, TopDegreeAtCap: true}`
  (`battleroyale/lootrandom.go`): every match kill drops the table's 3
  stacks (a 4th at 50 %) and then 5 equipment-only stacks (family by the
  table's equipment weights, a skin at 1 %), so about 8.5 stacks a kill;
  a level-30 killer draws from every top-degree row (required 24-29)
  instead of [27, 30]. At levels 5-7 the window holds no row, so the
  extra draws give only the odd skin. A bot's body is still two
  `LootPlan` rolls (about 17 stacks). The supply drop
  (`SupplyDropLoot`) keeps its own 3-6 stacks but uses the live window,
  so its level-30 weapon is a degree-4 row. `LootPlanTuned(killer,
  LootTuning{}, rng)` is the stage-9 table the tests and the simulation
  compare against. The ground cap (`MaxGroundItems` 2000) and the 75 s
  expiry are unchanged.
- **Set completion and the ground, live tuning** (licensed Roc mask,
  three seeds per line-up, 20 Normal bots; players never pick anything
  up, the ground's worst case):

  | Line-up | Complete set at phase 5 | Median kills to a degree-4 set | Stacks / player | Peak ground items | Evicted by the cap / match | Time at the cap |
  | --- | --- | --- | --- | --- | --- | --- |
  | 2 + 20 bots | 6/6 | 127 | 1495 | 289 | 0 | 0 % |
  | 5 + 20 bots | 6/15 | never (median) | 1497 | 682 | 0 | 0 % |
  | 10 + 20 bots | 24/30 | 130 | 1553 | 1302 | 0 | 0 % |
  | 16 + 20 bots | 32/48 | 137 | 1533 | 2025 | 24 | 0.1 % |

  Per slot at phase 5 (5 / 16 players): head 100 / 100 %, shoulders
  87 / 94 %, chest 73 / 85 %, legs 87 / 98 %, hands 93 / 92 %, feet
  93 / 96 %, weapon, shield and every accessory 100 %. The levelling
  pace is unchanged (every player at level 30 before phase 5, median
  10.0-10.2 min); the 10-player line-ups with 100 and 200 bots peak at
  about 1 300 ground items and never reach the cap. The cap is touched
  only by 16 players at the end of their farming (24 items a match,
  0.1 % of the match), so it is not hit constantly. The alternatives,
  re-measured in the same run (5 / 16 players): stage-9 table 0/15 and
  1/48 (peak 862); cap window only 0/15 and 2/48 (862); +3 stacks 2/15
  and 15/48 (1539); +8 stacks 12/15 and 45/48, but 16 players then sit
  at the cap 38 % of the match and lose about 16 000 items a match to
  it. (The earlier proposal table differs slightly: the bots now draw
  the live table from the simulation's shared stream.)
- **Devil's Spirit in the airdrop** (`action/battleroyale_loot.go`,
  `game/item/devilspirit`). Only when the Devil's Spirit rows are loaded
  (`devilspirit.Loaded`: `SRO_DEVIL_SPIRIT` on, so the GameWorld's
  wiring applied them to itemdata; with the flag off nothing rolls and
  the loot stream is untouched): when an airdrop's guardian dies, its
  killer rolls `devilspirit.AirdropReward` on the match's loot stream
  (one in 40; of those, the S grade one in 8, otherwise crimson,
  golden or blue A grade evenly), for the sex of the killer's match
  character. A hit lies with the guardian's loot, owned by the killer
  like the rest, and the whole shard gets the channel-7 banner
  "<name> found a Devil's Spirit!" (`DevilSpiritBanner`). It is rolled
  whether or not the airdrop also holds the ultimate weapon; the
  guardian of an airdrop already taken or expired never rolls it.
- **Worn without a dress.** The Devil's Spirit takes the avatar
  attachment socket (TID4 3) but, as in the later client it comes from,
  needs no avatar dress: `action/avatartransfer.go` `needsDress` exempts
  `devilspirit.Is` rows from the v1.150 rule that an attachment needs a
  worn dress, and taking the dress off leaves it worn. The client's equip
  gate (`foundation/gameplay/avatar-equip.ts`) reads the same ids, and a
  retail attachment refused for want of a dress now says so
  ("Can only be equipped when avatar dress is worn.",
  `UIIT_STT_AVATAR_MAIL_ONLY_WEAR`) instead of doing nothing. Tests:
  `TestDevilSpiritIsWornWithoutAnAvatarDress`,
  `TestDressRemovalLeavesTheDevilSpiritWorn`, and the client's
  `tests/runtime/devil-spirit.test.mjs`. Its awakening, options, devil
  skills, scroll and extension are ported too (see "Devil's Spirit:
  awakening, options and skills" below).
- **The airdrop's Devil's Spirit.** `prepareBattleRoyaleDrop` makes it a
  new instance (`makeDevilSpirit`): awakened for good (a match is shorter
  than any 28-day period, so it never expires; documented choice), its
  level-0 MATTR_NASRUN_HPNA / _MPNA (15 %, the S grade 20 % and block 1)
  and a MATTR_NASRUN_UMDU drawn from 2/4/6/8/10 %, no whites.
- **Tests.** `TestLiveLootTuning` (the tuning, 8.4-8.6 stacks a kill at
  level 18 with five or more equipment, lowest required level 26 at
  level 29 and 24 at level 30, top-degree supply weapons at 30),
  `TestLootPlanWindow` (8-9 stacks every level outside 5-7, the window
  or the cap's top degree; the stage-9 table's shares at level 18),
  `TestBattleRoyaleDropsCarryBluesAndHighWhites` (8-9 drops a kill),
  `TestLoaded` and the licensed `TestApplyAddsAttachmentRowsOnShippedData`
  (`Loaded` false on the native data, true once applied),
  `TestBattleRoyaleAirdropDevilSpirit` (a forced roll: no Devil's Spirit
  and no banner without the rows; with them, the killer's-sex row with
  exactly its port-only options and no whites, the shard banner, the
  banner text; a plain wolf never drops one), and the economy
  simulations above (`TestEconomySimulation`, licensed
  `TestEconomySimulationOnRoc`, which now report the ground cap instead
  of asserting the pre-sweep peak).

### Server and client notes: Devil's Spirit, awakening, options and skills (port-only)

Owner-approved 2026-10-18, behind `SRO_DEVIL_SPIRIT` (off = native: no
row, no option, no skill, no packet). Values are the later client's
(import `Client_695_extracted`), checked by the licensed
`scripts/test/assets/devilSpirit.test.mjs` (`devilSpiritImport.mjs`,
`SRO_IMPORT_ROOT`).

- **Data** (`game/item/devilspirit/devilspirit.json`): per row the eleven
  effective-OptLevel abilities (_RefAbilityByItemOptLevel ->
  _RefMagicOptByItemOptLevel / _RefSkillByItemOptLevel): A grade HP/MP
  15..25 %, block 0 (0-2), 1 (3-5), 2 (6-10); S grade 20..30 %, block
  1/2/3; skills: stage 1 (0-2) Malicious Devil Skill, stage 2 (3-5)
  Wicked Devil Skill + Ignore Monster's defense 1, stage 3 (6-10)
  Vallainous Devil Skill + Ignore Monster's defense 2 (each colour its
  own buff row, the S grade the red ones). The MATTR_NASRUN_UMDU / HPNA /
  MPNA / BLOCKRATE magicoption rows (353-356), the
  AVATAR_SPECIAL_OPTLV_RATIO table (10, 15, 15, 15, 15, 15, 5, 4, 3, 2, 1),
  28-day awakening, the scroll (`ITEM_ETC_NASRUN_UPGRADE_SCROLL` 39738,
  3 h) and the extension (`ITEM_MALL_NASRUN_EXTENSION` 24395, 28 days),
  and the eleven devil skill rows (31146-31156).
- **Instance state** (`state.go`): kept in fields every item path already
  carries: durability = the awakening's expiry (unix s; 0 not awakened,
  0xFFFFFFFF never), variance = binding expiry | level << 32 | UMDU << 40,
  plus = base OptLevel; the magic options are derived (`Sync`) from the
  effective level (plus + live binding, max 10); an expired instance
  carries none. No converter, store or wire shape changed.
- **Stats** (`combat/magicoptions.go`, `uniquemonster.go`): hpna / mpna
  raise max HP / MP by their percent (params 3/4 PercentSum), brna adds a
  flat block rate (param 10), umdu fills param 0x103, which adds its
  percent of a player's hit on a unique monster (rarity low nibble 3,
  `UniqueMonsterDamage`, `resolvePlayerImpactBehindWall`).
- **Wearing** (`action/avatartransfer.go`, `action/devilspirit.go`): the
  first wear awakens it (the later client asks "Do you want to awaken
  this item?" on use; v1.150 has no use action for an avatar, inference);
  an expired one is refused (server and client gate); the worn state
  reaches the client in 0x3078 kind 4 (port-only, the 0x3645 body layout,
  since 0x3645 addresses bag slots only).
- **Scroll and extension** (`useDevilSpiritEtc`): clones of the v1.150
  gender tool (3/3/13/8), so the client's drag-onto-an-item use sends
  0x75BD with the target bag slot. Scroll: awakened, unexpired target in
  the bag; draw % 101 against the ratio table; 0 fails (0xB5BD [2][0xDD],
  the later 0x18DD), the scroll is spent either way (inference); success
  binds the level for 3 h (0x3645). Extension: awakened target; expiry =
  max(current, now) + 28 days; 0x3078 kind 2. Both makeable by GM
  (`/makeitem ITEM_ETC_NASRUN_UPGRADE_SCROLL`, `ITEM_MALL_NASRUN_EXTENSION`).
- **Sweep** (`advanceResidentDevilSpirit`, once a second per resident,
  first at admission): expired bindings and awakenings are applied and
  persisted; a worn expiry sends 0x3078 kind 3 then 4 and the stats.
- **Devil skills** (`skills.go`, `enterworld/portonlyskills.go`): the
  later rows are parsed by the server's own skill parser as one more
  shard; a port-only row may join hst3 and dru on its caster alone
  (`skilltimedeffect.go`, port-only rows only). Their reqi (kind 14, the
  later avatar socket 4) reads a worn live Devil's Spirit
  (`equipmentreqi.go`). The worn instance's level skills are learned
  (0xB2CB) and withdrawn (0x3078 kind 5) on wear, removal, level change
  and expiry. Malicious/Wicked/Vallainous: 10 min, +20/25/30 % damage,
  +10/15/20 % speed, 30 min cooldown. Ignore Monster's defense (msid) has
  no v1.150 meaning: learned, no effect.
- **Client** (`foundation/gameplay/devil-spirit.ts`): tooltip lines
  (options, awakening period, enhancement and its time, from the wall
  clock), notices, 0x3078 kinds 2-5, the expired equip gate, a learned
  devil buff takes the shortcut of the one it replaced or the first free
  page slot (stand-in for the later client's Devil's Spirit window
  "Shortcut" button; the skills have no skill-window board position
  there either), and the look: a worn Devil's Spirit is drawn only while
  its wearer's devil buff runs (caster and observers, from the attached
  effects); one look for every stage (the stage-specific shoulder models
  and the avatar_nasrun particles are not built).
- **Asset build**: skillData.json gains the devil skill rows, the SN_*
  catalogue their names and the scroll's/extension's descriptions, the
  icons the scroll, extension and skill icons.
- **Own avatar socket: not done.** The v1.150 avatar storage is four
  records (server container, client `AVATAR_STORAGE_SLOTS`, the avatar
  window's four sockets) and TID3 14 is unknown to every client item
  owner; the attachment mapping stays.
- **Tests:** `item/devilspirit` (`state_test.go`, `devilspirit_test.go`),
  `combat` (`uniquemonster_test.go`, `devilspirit_test.go`), `action`
  (`devilspirit_test.go`: GM make, awakening on wear, dress rules, expired
  refusal, scroll success/failure, extension, sweep, skills follow the
  worn item, the cast needs the worn item and installs the buff;
  `battleroyale_devilspirit_test.go`), client
  `devil-spirit-state.test.mjs`, `devil-spirit-skills.test.mjs`,
  `devil-spirit.test.mjs`, scripts `devilSpirit.test.mjs`.
