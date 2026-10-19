/*
===========================================================================

groundmask.go - the walkable Roc ground a match may use

Port-only, not native (see zone.go). The stage-4 arena circle covers all
of Roc Mountain and, at its corners, parts of the regions around it; Roc
itself holds blocked cliffs and small walkable islands no path reaches.
A GroundMask samples the Roc regions on a GroundMaskStep grid (twenty
cells to a region edge), asks the caller's walkable test at every cell
centre and keeps only the largest 4-connected set of walkable cells: the
ground a player can actually walk. Drop points, monster points and zone
centres are kept on it, so nobody lands on an unreachable plateau and the
final circle closes where players can meet.

The mask is pure data built once from a predicate (the wiring passes the
navmesh test); a nil or empty mask contains nothing. Connectivity is a
cell-level approximation of the navmesh (two walkable cell centres 9.6 m
apart are taken as joined), which the licensed test checks against the
shipped data: 4.97 km2 in one component of the 5.29 km2 walkable Roc
ground (94 %).

===========================================================================
*/
package battleroyale

import "math/rand/v2"

// GroundMaskStep is the mask's cell edge in world units (9.6 m): twenty
// cells to a region edge, so no cell straddles two regions.
const GroundMaskStep = 96.0

/*
================
GroundMask

The largest walkable component of Roc on a GroundMaskStep grid: cells
holds the column-major flags, open lists the kept cell indexes in order
so a uniform cell draw is one random index.
================
*/
type GroundMask struct {
	minX, minZ float64
	cols, rows int
	cells      []bool
	open       []int32
}

/*
================
BuildGroundMask

Samples every Roc cell centre with walkable and keeps the largest
4-connected component (ties keep the first found, scanning by column).
================
*/
func BuildGroundMask(walkable func(Point) bool) *GroundMask {
	minX, minZ, maxX, maxZ := RocBounds()
	mask := &GroundMask{minX: minX, minZ: minZ,
		cols: int((maxX - minX) / GroundMaskStep), rows: int((maxZ - minZ) / GroundMaskStep)}
	total := mask.cols * mask.rows
	raw := make([]bool, total)
	for index := range total {
		p := mask.cellCentre(index)
		raw[index] = InRoc(p) && (walkable == nil || walkable(p))
	}
	component := make([]int32, total)
	var best []int32
	var stack []int32
	label := int32(0)
	for seed := range total {
		if !raw[seed] || component[seed] != 0 {
			continue
		}
		label++
		members := []int32{int32(seed)}
		component[seed] = label
		stack = append(stack[:0], int32(seed))
		for len(stack) > 0 {
			cell := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, next := range mask.neighbours(int(cell)) {
				if next >= 0 && raw[next] && component[next] == 0 {
					component[next] = label
					members = append(members, int32(next))
					stack = append(stack, int32(next))
				}
			}
		}
		if len(members) > len(best) {
			best = members
		}
	}
	mask.cells = make([]bool, total)
	for _, cell := range best {
		mask.cells[cell] = true
	}
	for index, kept := range mask.cells {
		if kept {
			mask.open = append(mask.open, int32(index))
		}
	}
	return mask
}

/*
================
GroundMask.neighbours

The four edge neighbours of a cell, -1 past the grid.
================
*/
func (m *GroundMask) neighbours(index int) [4]int {
	col, row := index/m.rows, index%m.rows
	out := [4]int{-1, -1, -1, -1}
	if col > 0 {
		out[0] = index - m.rows
	}
	if col+1 < m.cols {
		out[1] = index + m.rows
	}
	if row > 0 {
		out[2] = index - 1
	}
	if row+1 < m.rows {
		out[3] = index + 1
	}
	return out
}

/*
================
GroundMask.cellCentre
================
*/
func (m *GroundMask) cellCentre(index int) Point {
	col, row := index/m.rows, index%m.rows
	return Point{X: m.minX + (float64(col)+0.5)*GroundMaskStep, Z: m.minZ + (float64(row)+0.5)*GroundMaskStep}
}

/*
================
GroundMask.Contains

Whether p lies in a kept cell.
================
*/
func (m *GroundMask) Contains(p Point) bool {
	if m == nil || len(m.cells) == 0 || p.X < m.minX || p.Z < m.minZ {
		return false
	}
	col, row := int((p.X-m.minX)/GroundMaskStep), int((p.Z-m.minZ)/GroundMaskStep)
	if col >= m.cols || row >= m.rows {
		return false
	}
	return m.cells[col*m.rows+row]
}

/*
================
GroundMask.Area

The kept ground in square metres.
================
*/
func (m *GroundMask) Area() float64 {
	if m == nil {
		return 0
	}
	cell := GroundMaskStep / UnitsPerMeter
	return float64(len(m.open)) * cell * cell
}

/*
================
GroundMask.Within

The kept cells whose centre lies inside within, scanned over the circle's
bounding box clipped to the grid (at most the whole Roc grid, 300 x 300
cells), so a small circle costs little and a uniform draw of the result
is uniform over the kept ground inside it.
================
*/
func (m *GroundMask) Within(within Circle) GroundCells {
	if m == nil || len(m.open) == 0 {
		return GroundCells{}
	}
	firstCol := max(int((within.Center.X-within.Radius-m.minX)/GroundMaskStep), 0)
	lastCol := min(int((within.Center.X+within.Radius-m.minX)/GroundMaskStep), m.cols-1)
	firstRow := max(int((within.Center.Z-within.Radius-m.minZ)/GroundMaskStep), 0)
	lastRow := min(int((within.Center.Z+within.Radius-m.minZ)/GroundMaskStep), m.rows-1)
	out := GroundCells{mask: m}
	for col := firstCol; col <= lastCol; col++ {
		for row := firstRow; row <= lastRow; row++ {
			index := col*m.rows + row
			if m.cells[index] && !Outside(within, m.cellCentre(index)) {
				out.cells = append(out.cells, int32(index))
			}
		}
	}
	return out
}

/*
================
GroundCells

A set of kept mask cells (GroundMask.Within) to draw points from.
================
*/
type GroundCells struct {
	mask  *GroundMask
	cells []int32
}

/*
================
GroundCells.Len
================
*/
func (g GroundCells) Len() int {
	return len(g.cells)
}

/*
================
GroundCells.Sample

A point uniform over the cells (uniform inside its cell); false when the
set is empty.
================
*/
func (g GroundCells) Sample(rng *rand.Rand) (Point, bool) {
	if len(g.cells) == 0 {
		return Point{}, false
	}
	centre := g.mask.cellCentre(int(g.cells[rng.IntN(len(g.cells))]))
	return Point{X: centre.X + (rng.Float64()-0.5)*GroundMaskStep, Z: centre.Z + (rng.Float64()-0.5)*GroundMaskStep}, true
}
