/*
===========================================================================

brlobby.go - the Battle Royale lobby a world-entry bootstrap carries

Port-only, not native. Under SRO_BATTLE_ROYALE every player learns where
the lobby circle stands at world entry, so the client draws it for
everyone, not only for the members its 0x3FB2 frames reach. A shard with
the mode off sends nothing.

===========================================================================
*/
package enterworld

/*
================
BattleRoyaleLobby

The lobby circle: its region, local centre (world units) and radius in
metres.
================
*/
type BattleRoyaleLobby struct {
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Z        float64 `json:"z"`
	RadiusM  float64 `json:"radiusM"`
}
