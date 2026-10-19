/*
===========================================================================

chainhop.go - the Chain Lightning hop a world-entry bootstrap carries

Port-only, not native. Under SRO_CHAIN_LIGHTNING_HOP_MS
(action/chainlightning.go) the client starts each next hop of a Chain
Lightning bolt one period after the last (chain-cadence.ts) instead of
flying straight on at the mover's speed. The bootstrap names the period
and the family's skill ids, so the client keeps no codename of its own. A
shard with the setting off sends nothing, and the client flies the native
chain.

===========================================================================
*/
package enterworld

/*
================
ChainLightningHop

The hop period in milliseconds and the skill ids whose chain it times.
================
*/
type ChainLightningHop struct {
	HopMs  uint32   `json:"hopMs"`
	Skills []uint32 `json:"skills"`
}
