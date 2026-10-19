/*
===========================================================================

botnav.go - bot paths over the walkable Roc ground

Port-only, not native (see zone.go; docs/battle-royale.md "Stage 7").
The stage-5 brain ran a bot straight at its goal, one clipped 40 m leg
at a time: a cliff between the two clipped every leg at the same spot
and the bot stood there until the zone killed it. The server's own
monster planner (movement.findMonsterRoute) is a local detour search of
256 probes on a 3.2 m grid, built for a chase's last metres, not for a
kilometre across Roc. So the bots plan here, over the ground mask
(groundmask.go): A* on its 9.6 m cells, 8-connected (a diagonal step only
when both cells beside it are open), octile heuristic, and the path is
cut into straight legs of at most botNavLegM whose every 2.4 m sample is
mask ground. The wiring walks each leg with the ordinary clipped monster
movement (SteerPortActor), so the navmesh still has the last word.

The mask joins two walkable cell centres 9.6 m apart; a thin wall, a
cliff lip or an object between them is invisible to it (on the shipped
Roc navmesh a mask-only plan reached 26 of 40 far goals). So a nav may
carry a walk test (SetWalkTest: the wiring and the licensed test pass the
server's own clipped monster probe): every cell edge a search steps over
is probed once, centre to centre, and the answer is kept for the match;
a tick probes at most BotNavTickProbes new edges, and a search that
needed more waits for the next tick with what it learned kept. The mask
alone (no walk test) is the fallback.

Even so, a BotRoute that makes no progress
for BotStuckMs (less than botStuckProgressM nearer its waypoint) marks the
cell edge it was crossing as blocked for this nav (learned, kept for the
match) and plans again; after botRouteMaxStucks it gives the goal up and
the brain picks another.

Cost: one search expands at most botNavMaxExpansions cells, and a tick
spends at most the budget BeginTick grants (BotNavTickBudget); a route
whose plan does not fit waits for the next tick (RouteWaiting). The
scratch arrays are stamped, not cleared, so a search costs its own
expansions only.

===========================================================================
*/
package battleroyale

import (
	"container/heap"
	"math"
)

const (
	// botNavMaxExpansions bounds one A* search (about 40 % of the 54 000
	// mask cells; a 2 km detour across Roc takes a few thousand).
	botNavMaxExpansions = 24000
	// BotNavTickBudget is the expansions one match tick may spend, and
	// BotNavTickProbes the new edges it may probe with the walk test.
	BotNavTickBudget = 60000
	BotNavTickProbes = 600
	// The edge states the nav keeps.
	edgeOpen    = 1
	edgeBlocked = 2
	// botNavSnapCells is how far (cells) a point off the mask snaps to the
	// nearest open cell.
	botNavSnapCells = 8
	// botNavLegM is the longest straight leg of a path; botNavSampleStep
	// the spacing of its ground samples (a quarter cell).
	botNavLegM       = 30
	botNavSampleStep = GroundMaskStep / 4
	// BotStuckMs is how long a route may make no progress before its edge
	// is blocked and it plans again; botStuckProgressM the progress that
	// counts; botWaypointReachM how near a waypoint counts as reached.
	BotStuckMs        = 5000
	botStuckProgressM = 2
	botWaypointReachM = 3
	botRouteMaxStucks = 8
	// botNavBlockBand is how many parallel crossings to either side a
	// stuck blocks with its own (BotNav.Block).
	botNavBlockBand    = 2
	botRouteResumeGapM = 3000 // a route not advanced for 3 s restarts its progress clock
)

/*
================
RouteStatus

What BotNav.Advance says of a route: walk the returned leg, the goal is
reached, the plan waits for the next tick's budget, or the goal is given
up (unreachable, or stuck too often).
================
*/
type RouteStatus int

const (
	RouteMoving RouteStatus = iota
	RouteArrived
	RouteWaiting
	RouteFailed
)

/*
================
BotNav

One match's planner over a ground mask: the learned blocked edges and
the reusable A* scratch. Not safe for concurrent use (the wiring holds
the division lock).
================
*/
type BotNav struct {
	mask    *GroundMask
	edges   map[uint64]uint8
	walk    func(a, b Point) bool
	probes  int
	starved bool
	g       []float32
	parent  []int32
	seen    []uint32
	closed  []uint32
	gen     uint32
	open    navHeap
	budget  int
	// Expansions, Plans and Probes count the work done (telemetry, tests).
	Expansions int64
	Plans      int64
	Probes     int64
}

/*
================
NewBotNav
================
*/
func NewBotNav(mask *GroundMask) *BotNav {
	n := &BotNav{mask: mask, edges: map[uint64]uint8{}, budget: BotNavTickBudget, probes: BotNavTickProbes}
	if mask != nil {
		total := mask.cols * mask.rows
		n.g, n.parent = make([]float32, total), make([]int32, total)
		n.seen, n.closed = make([]uint32, total), make([]uint32, total)
	}
	return n
}

/*
================
BotNav.SetWalkTest

Installs the walk test every new cell edge is probed with (nil: the mask
alone).
================
*/
func (n *BotNav) SetWalkTest(walk func(a, b Point) bool) {
	n.walk = walk
}

/*
================
BotNav.BeginTick

Grants the tick's expansion budget and BotNavTickProbes new probes.
================
*/
func (n *BotNav) BeginTick(budget int) {
	n.budget, n.probes = budget, BotNavTickProbes
}

/*
================
BotNav.Budget

The expansions left this tick.
================
*/
func (n *BotNav) Budget() int {
	return n.budget
}

//============================================================================

/*
================
navNode
================
*/
type navNode struct {
	cell     int32
	priority float32
}

/*
================
navHeap

A binary min-heap of open cells by priority (container/heap).
================
*/
type navHeap []navNode

func (h navHeap) Len() int           { return len(h) }
func (h navHeap) Less(i, j int) bool { return h[i].priority < h[j].priority }
func (h navHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *navHeap) Push(v any)        { *h = append(*h, v.(navNode)) }
func (h *navHeap) Pop() any {
	old := *h
	v := old[len(old)-1]
	*h = old[:len(old)-1]
	return v
}

/*
================
edgeKey
================
*/
func edgeKey(a, b int) uint64 {
	if a > b {
		a, b = b, a
	}
	return uint64(a)<<32 | uint64(b)
}

/*
================
BotNav.openCell

Whether a cell index is kept ground.
================
*/
func (n *BotNav) openCell(cell int) bool {
	return cell >= 0 && cell < len(n.mask.cells) && n.mask.cells[cell]
}

/*
================
BotNav.cellOf

The cell under p, -1 off the grid.
================
*/
func (n *BotNav) cellOf(p Point) int {
	m := n.mask
	if p.X < m.minX || p.Z < m.minZ {
		return -1
	}
	col, row := int((p.X-m.minX)/GroundMaskStep), int((p.Z-m.minZ)/GroundMaskStep)
	if col >= m.cols || row >= m.rows {
		return -1
	}
	return col*m.rows + row
}

/*
================
BotNav.snap

The open cell under p, or the nearest open cell centre within
botNavSnapCells rings; -1 when none.
================
*/
func (n *BotNav) snap(p Point) int {
	if cell := n.cellOf(p); n.openCell(cell) {
		return cell
	}
	m := n.mask
	col, row := int(math.Floor((p.X-m.minX)/GroundMaskStep)), int(math.Floor((p.Z-m.minZ)/GroundMaskStep))
	best, bestD := -1, math.Inf(1)
	for ring := 1; ring <= botNavSnapCells && best < 0; ring++ {
		for dc := -ring; dc <= ring; dc++ {
			for dr := -ring; dr <= ring; dr++ {
				if max(abs(dc), abs(dr)) != ring {
					continue
				}
				c, r := col+dc, row+dr
				if c < 0 || r < 0 || c >= m.cols || r >= m.rows || !m.cells[c*m.rows+r] {
					continue
				}
				centre := m.cellCentre(c*m.rows + r)
				if d := math.Hypot(centre.X-p.X, centre.Z-p.Z); d < bestD {
					best, bestD = c*m.rows+r, d
				}
			}
		}
	}
	return best
}

/*
================
abs
================
*/
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

/*
================
BotNav.step

Whether a bot may step from cell to its neighbour (dc, dr): both open,
the edge not learned blocked, and a diagonal only between two open
orthogonal cells.
================
*/
func (n *BotNav) step(cell, dc, dr int) (int, bool) {
	m := n.mask
	col, row := cell/m.rows, cell%m.rows
	c, r := col+dc, row+dr
	if c < 0 || r < 0 || c >= m.cols || r >= m.rows {
		return -1, false
	}
	next := c*m.rows + r
	if !m.cells[next] {
		return -1, false
	}
	if dc != 0 && dr != 0 {
		side1, side2 := c*m.rows+row, col*m.rows+r
		if !m.cells[side1] || !m.cells[side2] || n.edges[edgeKey(cell, side1)] == edgeBlocked || n.edges[edgeKey(cell, side2)] == edgeBlocked {
			return -1, false
		}
	}
	return next, n.passable(cell, next)
}

/*
================
BotNav.passable

Whether the edge between two neighbouring open cells may be walked: its
kept state, else (with a walk test) one probe centre to centre, kept;
without a probe left this tick the edge is closed for this search and
the search is marked starved.
================
*/
func (n *BotNav) passable(a, b int) bool {
	switch n.edges[edgeKey(a, b)] {
	case edgeOpen:
		return true
	case edgeBlocked:
		return false
	}
	if n.walk == nil {
		return true
	}
	if n.probes <= 0 {
		n.starved = true
		return false
	}
	n.probes--
	n.Probes++
	state := uint8(edgeBlocked)
	if n.walk(n.mask.cellCentre(a), n.mask.cellCentre(b)) {
		state = edgeOpen
	}
	n.edges[edgeKey(a, b)] = state
	return state == edgeOpen
}

/*
================
octile

The octile distance in cells between two cells.
================
*/
func (n *BotNav) octile(a, b int) float32 {
	rows := n.mask.rows
	dc, dr := float32(abs(a/rows-b/rows)), float32(abs(a%rows-b%rows))
	return max(dc, dr) + (math.Sqrt2-1)*min(dc, dr)
}

/*
================
BotNav.search

A* from cell start to cell goal; the cell path start..goal, or nil when
the goal is unreachable or the search ran out (exhausted true).
================
*/
func (n *BotNav) search(start, goal int) (path []int32, exhausted bool) {
	n.gen++
	if n.gen == 0 {
		clear(n.seen)
		clear(n.closed)
		n.gen = 1
	}
	n.open = n.open[:0]
	n.starved = false
	n.g[start], n.parent[start], n.seen[start] = 0, -1, n.gen
	heap.Push(&n.open, navNode{cell: int32(start), priority: n.octile(start, goal)})
	expansions := 0
	steps := [8][3]float32{{1, 0, 1}, {-1, 0, 1}, {0, 1, 1}, {0, -1, 1}, {1, 1, math.Sqrt2}, {1, -1, math.Sqrt2}, {-1, 1, math.Sqrt2}, {-1, -1, math.Sqrt2}}
	for n.open.Len() > 0 {
		node := heap.Pop(&n.open).(navNode)
		cell := int(node.cell)
		if n.closed[cell] == n.gen {
			continue
		}
		n.closed[cell] = n.gen
		if cell == goal {
			break
		}
		expansions++
		if expansions > botNavMaxExpansions {
			n.charge(expansions)
			return nil, true
		}
		for _, s := range steps {
			next, ok := n.step(cell, int(s[0]), int(s[1]))
			if !ok || n.closed[next] == n.gen {
				continue
			}
			cost := n.g[cell] + s[2]
			if n.seen[next] == n.gen && n.g[next] <= cost {
				continue
			}
			n.g[next], n.parent[next], n.seen[next] = cost, int32(cell), n.gen
			heap.Push(&n.open, navNode{cell: int32(next), priority: cost + n.octile(next, goal)})
		}
	}
	n.charge(expansions)
	if n.closed[goal] != n.gen {
		return nil, false
	}
	for cell := int32(goal); cell >= 0; cell = n.parent[cell] {
		path = append(path, cell)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, false
}

/*
================
BotNav.charge
================
*/
func (n *BotNav) charge(expansions int) {
	n.budget -= expansions
	n.Expansions += int64(expansions)
	n.Plans++
}

/*
================
BotNav.Plan

The waypoints from from to goal over the mask: straight legs of at most
botNavLegM over mask ground, the last one goal itself (or the open cell
it snapped to). RouteWaiting when this tick's budget is spent,
RouteFailed when either end has no ground near it or no path joins them.
================
*/
func (n *BotNav) Plan(from, goal Point) ([]Point, RouteStatus) {
	return n.plan(from, goal, nil)
}

/*
================
BotNav.plan

Plan, starting from via when given (a bot that got stuck walks back to
the last waypoint it reached, on the probed graph, and plans on from
there).
================
*/
func (n *BotNav) plan(from, goal Point, via *Point) ([]Point, RouteStatus) {
	if n == nil || n.mask == nil || len(n.mask.open) == 0 {
		return []Point{goal}, RouteMoving
	}
	if n.budget <= 0 {
		return nil, RouteWaiting
	}
	origin := from
	if via != nil {
		origin = *via
	}
	start, end := n.snap(origin), n.snap(goal)
	if start < 0 || end < 0 {
		return nil, RouteFailed
	}
	if !n.openCell(n.cellOf(goal)) {
		goal = n.mask.cellCentre(end)
	}
	var head []Point
	if via != nil {
		head = []Point{*via}
	}
	if start == end || n.walk == nil && via == nil && n.ClearLine(from, goal) && distance(from, goal) <= botNavLegM*UnitsPerMeter {
		return append(head, goal), RouteMoving
	}
	cells, _ := n.search(start, end)
	if cells == nil {
		if n.starved {
			return nil, RouteWaiting
		}
		return nil, RouteFailed
	}
	return append(head, n.legs(origin, goal, cells)...), RouteMoving
}

/*
================
BotNav.legs

Cuts a cell path into straight legs. With a walk test the legs follow
the probed centre-to-centre edges only: the bot first steps onto its own
cell's centre, then each leg is a run of steps in one direction (at most
botNavLegM), so every metre it walks was probed. Without one, a leg runs
from the current point to the farthest path cell (or the goal) within
botNavLegM whose straight line is clear mask ground.
================
*/
func (n *BotNav) legs(from, goal Point, cells []int32) []Point {
	if n.walk != nil {
		return n.probedLegs(from, goal, cells)
	}
	points := make([]Point, 0, len(cells)/2+1)
	at := from
	for i := 0; i < len(cells); {
		best := -1
		for j := i; j < len(cells); j++ {
			target := n.mask.cellCentre(int(cells[j]))
			if j == len(cells)-1 {
				target = goal
			}
			if distance(at, target) > botNavLegM*UnitsPerMeter {
				break
			}
			if n.ClearLine(at, target) {
				best = j
			}
		}
		if best < 0 {
			// Not even the next cell is a clear line (a corner cut):
			// step to it anyway; the mask joins the two.
			best = i
		}
		next := n.mask.cellCentre(int(cells[best]))
		if best == len(cells)-1 {
			next = goal
		}
		points = append(points, next)
		at = next
		i = best + 1
	}
	if len(points) == 0 || points[len(points)-1] != goal {
		points = append(points, goal)
	}
	return points
}

/*
================
BotNav.probedLegs

The walk-test legs of legs (see there).
================
*/
func (n *BotNav) probedLegs(from, goal Point, cells []int32) []Point {
	points := make([]Point, 0, len(cells)/2+2)
	first := n.mask.cellCentre(int(cells[0]))
	if distance(from, first) > botWaypointReachM*UnitsPerMeter {
		points = append(points, first)
	}
	rows := n.mask.rows
	maxSteps := int(math.Floor(botNavLegM * UnitsPerMeter / (GroundMaskStep * math.Sqrt2)))
	for i := 0; i < len(cells)-1; {
		dc, dr := int(cells[i+1])/rows-int(cells[i])/rows, int(cells[i+1])%rows-int(cells[i])%rows
		j := i + 1
		for j+1 < len(cells) && j-i < maxSteps &&
			int(cells[j+1])/rows-int(cells[j])/rows == dc && int(cells[j+1])%rows-int(cells[j])%rows == dr {
			j++
		}
		points = append(points, n.mask.cellCentre(int(cells[j])))
		i = j
	}
	return append(points, goal)
}

/*
================
BotNav.ClearLine

Whether every botNavSampleStep sample of the segment a-b lies on kept
ground and no blocked edge separates two consecutive samples (with a walk
test, every crossed edge must be one probed open).
================
*/
func (n *BotNav) ClearLine(a, b Point) bool {
	d := distance(a, b)
	samples := max(int(math.Ceil(d/botNavSampleStep)), 1)
	previous := n.cellOf(a)
	for i := 0; i <= samples; i++ {
		t := float64(i) / float64(samples)
		cell := n.cellOf(Point{X: a.X + (b.X-a.X)*t, Z: a.Z + (b.Z-a.Z)*t})
		if !n.openCell(cell) {
			return false
		}
		if cell != previous && previous >= 0 {
			state := n.edges[edgeKey(previous, cell)]
			if state == edgeBlocked || n.walk != nil && state != edgeOpen {
				return false
			}
		}
		previous = cell
	}
	return true
}

/*
================
BotNav.Block

Learns that the ground from at toward toward cannot be walked: the cell
edge the line crosses first is blocked for every later plan, and with it
the parallel crossings botNavBlockBand cells to either side (a wall seen
at one cell is usually longer than one cell; learning it a cell at a time
would cost a stuck each).
================
*/
func (n *BotNav) Block(at, toward Point) {
	if n == nil || n.mask == nil {
		return
	}
	d := distance(at, toward)
	if d <= 0 {
		return
	}
	ux, uz := (toward.X-at.X)/d, (toward.Z-at.Z)/d
	band := botNavBlockBand
	if n.walk != nil {
		// Probed edges already show the walls; a stuck names one edge.
		band = 0
	}
	for k := -band; k <= band; k++ {
		origin := Point{X: at.X - uz*GroundMaskStep*float64(k), Z: at.Z + ux*GroundMaskStep*float64(k)}
		n.blockCrossing(origin, ux, uz, d)
	}
}

/*
================
BotNav.blockCrossing

Blocks the first cell edge the ray from origin along (ux, uz) crosses
within length.
================
*/
func (n *BotNav) blockCrossing(origin Point, ux, uz, length float64) {
	from := n.cellOf(origin)
	if from < 0 {
		return
	}
	for step := botNavSampleStep; step <= length+botNavSampleStep; step += botNavSampleStep {
		t := min(step, length)
		cell := n.cellOf(Point{X: origin.X + ux*t, Z: origin.Z + uz*t})
		if cell >= 0 && cell != from {
			n.edges[edgeKey(from, cell)] = edgeBlocked
			return
		}
	}
}

/*
================
BotNav.BlockedEdges

How many edges the nav knows blocked.
================
*/
func (n *BotNav) BlockedEdges() int {
	blocked := 0
	for _, state := range n.edges {
		if state == edgeBlocked {
			blocked++
		}
	}
	return blocked
}

/*
================
distance
================
*/
func distance(a, b Point) float64 {
	return math.Hypot(a.X-b.X, a.Z-b.Z)
}

//============================================================================

/*
================
BotRoute

One bot's walk to a goal: the planned legs, the leg it is on, and the
progress clock that detects a stuck bot.
================
*/
type BotRoute struct {
	Goal    Point
	points  []Point
	next    int
	planned bool
	reached bool
	last    Point
	bestD   float64
	sinceMs int64
	lastMs  int64
	stucks  int
	// Stucks counts every stuck this route detected (telemetry).
	Stucks int
}

/*
================
BotRoute.Reset

Starts a route to goal (planned on the next Advance).
================
*/
func (r *BotRoute) Reset(goal Point) {
	*r = BotRoute{Goal: goal, Stucks: r.Stucks}
}

/*
================
BotRoute.backtrack

Where a re-plan after a stuck starts on a probed graph (a walk test):
the last waypoint the bot reached (it walked from there, so it can walk
back), or nil before any. A mask-only nav plans from where the bot
stands, its learned band already around it.
================
*/
func (r *BotRoute) backtrack(probed bool) *Point {
	if !probed || r.stucks == 0 || !r.reached {
		return nil
	}
	at := r.last
	return &at
}

/*
================
BotNav.Advance

The leg a bot standing at at walks next on route r at nowMs (see the
file header): plans when needed, moves to the next waypoint once within
botWaypointReachM, and on BotStuckMs without botStuckProgressM of
progress blocks the edge toward the waypoint and plans again from the
last waypoint it reached (at most botRouteMaxStucks times).
================
*/
func (n *BotNav) Advance(r *BotRoute, at Point, nowMs int64) (Point, RouteStatus) {
	if distance(at, r.Goal) <= botWaypointReachM*UnitsPerMeter {
		return r.Goal, RouteArrived
	}
	if nowMs-r.lastMs > botRouteResumeGapM {
		// The bot did something else meanwhile (a fight): its progress
		// clock restarts instead of reading the pause as a stuck.
		r.bestD, r.sinceMs = math.Inf(1), nowMs
	}
	r.lastMs = nowMs
	if !r.planned {
		points, status := n.plan(at, r.Goal, r.backtrack(n.walk != nil))
		if status != RouteMoving {
			return at, status
		}
		r.points, r.next, r.planned = points, 0, true
		r.bestD, r.sinceMs = math.Inf(1), nowMs
	}
	for r.next < len(r.points)-1 && distance(at, r.points[r.next]) <= botWaypointReachM*UnitsPerMeter {
		r.last, r.reached = r.points[r.next], true
		r.next++
		r.bestD, r.sinceMs = math.Inf(1), nowMs
	}
	waypoint := r.points[r.next]
	if d := distance(at, waypoint); d < r.bestD-botStuckProgressM*UnitsPerMeter {
		r.bestD, r.sinceMs = d, nowMs
	} else if nowMs-r.sinceMs >= BotStuckMs {
		n.Block(at, waypoint)
		r.stucks++
		r.Stucks++
		if r.stucks > botRouteMaxStucks {
			return at, RouteFailed
		}
		r.planned = false
		points, status := n.plan(at, r.Goal, r.backtrack(n.walk != nil))
		if status != RouteMoving {
			return at, status
		}
		r.points, r.next, r.planned = points, 0, true
		r.bestD, r.sinceMs = math.Inf(1), nowMs
		waypoint = r.points[0]
	}
	return waypoint, RouteMoving
}
