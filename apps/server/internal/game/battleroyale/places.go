/*
===========================================================================

places.go - where the lobby and the arena stand

Port-only, not native (see zone.go). The two places of the mode as data
the wiring and the licensed tests share: the lobby circle in Hotan beside
the storage keeper, and the arena circle over all of Roc Mountain.
Both were chosen against the exported navmesh
(places_licensed_test.go re-checks them against the shipped data):

  - Lobby. Storage-Keeper Auisan (NPC_KT_WAREHOUSE) stands at region
    0x5C87 local 1134.28/610.79. The circle's centre is 25 m from the NPC
    at 110 degrees (local 1049/846): every tile of its 15 m disc is
    walkable terrain and its height varies by half a metre, so the circle
    covers neither the NPC nor the storage approach.
  - Arena (stage 4). All of Roc Mountain: the outdoor regions x 104..118,
    y 85..98 and the strip y 84, x 113..117 (grid X 199680..228480, Z
    161280..190080, a 2.88 km square). The circle is centred on that
    square (region 0x5B6F local 960/960, grid 214080/175680; the centre
    itself is blocked ground, which a zone centre may be) and its radius,
    2040 m, is just over the half-diagonal (2036.5 m), so the first circle
    covers every Roc region whatever the player count. Only the largest
    walkable component of Roc (about 5 km2 of the 5.3 km2 walkable Roc
    ground; groundmask.go) receives drop points, monsters and later zone
    centres; InRoc keeps everything off the regions around Roc that the
    corners of the circle overlap.

===========================================================================
*/
package battleroyale

import worldgeom "opensro.online/server/internal/game/world"

const (
	// The lobby circle: Hotan, beside the storage keeper (stage 2).
	LobbyRegion  uint16  = 0x5C87
	LobbyLocalX  float64 = 1049
	LobbyLocalZ  float64 = 846
	LobbyRadiusM         = 15

	// The arena: all of Roc Mountain (stage 4). The centre is the middle
	// of the Roc square; the radius covers its corners.
	ArenaRegion  uint16  = 0x5B6F
	ArenaLocalX  float64 = 960
	ArenaLocalZ  float64 = 960
	ArenaRadiusM         = 2040

	// The Roc Mountain regions: sectors x RocMinSectorX..RocMaxSectorX,
	// y RocMinSectorY..RocMaxSectorY, plus the northern strip y
	// RocStripSectorY at x RocStripMinSectorX..RocStripMaxSectorX.
	RocMinSectorX      = 104
	RocMaxSectorX      = 118
	RocMinSectorY      = 85
	RocMaxSectorY      = 98
	RocStripSectorY    = 84
	RocStripMinSectorX = 113
	RocStripMaxSectorX = 117
)

/*
================
LobbyCircle

The lobby circle in world grid units.
================
*/
func LobbyCircle() Circle {
	center := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: LobbyRegion, X: LobbyLocalX, Z: LobbyLocalZ})
	return Circle{Center: center, Radius: LobbyRadiusM * UnitsPerMeter}
}

/*
================
ArenaCircle

The arena circle: all of Roc Mountain, for every player count.
================
*/
func ArenaCircle() Circle {
	center := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: ArenaRegion, X: ArenaLocalX, Z: ArenaLocalZ})
	return Circle{Center: center, Radius: ArenaRadiusM * UnitsPerMeter}
}

/*
================
RocBounds

The grid rectangle the Roc regions span: min X, min Z, max X, max Z
(exclusive), in world units.
================
*/
func RocBounds() (minX, minZ, maxX, maxZ float64) {
	return RocMinSectorX * worldgeom.OutdoorRegionSize, RocStripSectorY * worldgeom.OutdoorRegionSize,
		(RocMaxSectorX + 1) * worldgeom.OutdoorRegionSize, (RocMaxSectorY + 1) * worldgeom.OutdoorRegionSize
}

/*
================
InRoc

Whether a grid point lies in a Roc Mountain region.
================
*/
func InRoc(p Point) bool {
	if p.X < 0 || p.Z < 0 {
		return false
	}
	x, y := int(p.X/worldgeom.OutdoorRegionSize), int(p.Z/worldgeom.OutdoorRegionSize)
	if y == RocStripSectorY {
		return x >= RocStripMinSectorX && x <= RocStripMaxSectorX
	}
	return x >= RocMinSectorX && x <= RocMaxSectorX && y >= RocMinSectorY && y <= RocMaxSectorY
}
