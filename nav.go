package main

import (
	"container/heap"
	"math"
)

// Path finding for bots. The map is cut into square cells; a cell is closed if a player standing
// at its centre would touch an obstacle. Bots find a route across open cells with A* and then
// walk it, cutting corners wherever the straight line is clear.

const (
	navCell   = 25.0
	navSize   = int(worldSize / navCell) // cells per side
	navReplan = 0.6                      // seconds between route searches for one bot
	waterCost = 4.0                      // swimming is slow and defenceless, so routes avoid it
)

type navGrid struct {
	closed []bool
	water  []bool
	obstN  int // obstacle count the grid was built for; crates breaking changes it
}

// navLocked returns the grid, rebuilding it if the obstacles have changed.
func (m *Match) navLocked() *navGrid {
	if m.nav != nil && m.nav.obstN == len(m.obstacles) {
		return m.nav
	}
	g := &navGrid{closed: make([]bool, navSize*navSize), water: make([]bool, navSize*navSize), obstN: len(m.obstacles)}
	for cy := 0; cy < navSize; cy++ {
		for cx := 0; cx < navSize; cx++ {
			x, y := (float64(cx)+0.5)*navCell, (float64(cy)+0.5)*navCell
			g.closed[cy*navSize+cx] = m.blocked(x, y)
			g.water[cy*navSize+cx] = m.inWater(x, y)
		}
	}
	m.nav = g
	return g
}

func navCellOf(x, y float64) (int, int) {
	cx := int(clamp(x, 0, worldSize-1) / navCell)
	cy := int(clamp(y, 0, worldSize-1) / navCell)
	return cx, cy
}

// walkClear reports whether a player can walk the straight line between two points.
func (m *Match) walkClear(x1, y1, x2, y2 float64) bool {
	d := math.Hypot(x2-x1, y2-y1)
	steps := int(d/8) + 1
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x, y := x1+(x2-x1)*t, y1+(y2-y1)*t
		// A little wider than the player, so straight runs do not scrape along corners.
		for _, r := range m.obstacles {
			if x > r.X-playerRadius-3 && x < r.X+r.W+playerRadius+3 && y > r.Y-playerRadius-3 && y < r.Y+r.H+playerRadius+3 {
				return false
			}
		}
	}
	return true
}

type navNode struct {
	cell int
	f    float64
}
type navHeap []navNode

func (h navHeap) Len() int           { return len(h) }
func (h navHeap) Less(i, j int) bool { return h[i].f < h[j].f }
func (h navHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *navHeap) Push(x any)        { *h = append(*h, x.(navNode)) }
func (h *navHeap) Pop() any          { o := *h; n := o[len(o)-1]; *h = o[:len(o)-1]; return n }

// findPath returns waypoints from (x1, y1) to (x2, y2), or nil if there is no route. If the goal
// is inside something solid, the route ends at the nearest open cell to it.
func (m *Match) findPath(x1, y1, x2, y2 float64) []Point {
	g := m.navLocked()
	sx, sy := navCellOf(x1, y1)
	gx, gy := navCellOf(x2, y2)
	start, goal := sy*navSize+sx, gy*navSize+gx
	if g.closed[goal] {
		best, bestD := -1, math.Inf(1)
		for r := 1; r <= 4 && best < 0; r++ {
			for cy := max(0, gy-r); cy <= min(navSize-1, gy+r); cy++ {
				for cx := max(0, gx-r); cx <= min(navSize-1, gx+r); cx++ {
					if d := math.Hypot(float64(cx-gx), float64(cy-gy)); !g.closed[cy*navSize+cx] && d < bestD {
						best, bestD = cy*navSize+cx, d
					}
				}
			}
		}
		if best < 0 {
			return nil
		}
		goal = best
		gx, gy = goal%navSize, goal/navSize
	}
	cost := make([]float64, navSize*navSize)
	from := make([]int32, navSize*navSize)
	for i := range cost {
		cost[i] = math.Inf(1)
		from[i] = -1
	}
	cost[start] = 0
	open := &navHeap{{start, 0}}
	for open.Len() > 0 {
		cur := heap.Pop(open).(navNode).cell
		if cur == goal {
			break
		}
		cx, cy := cur%navSize, cur/navSize
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				nx, ny := cx+dx, cy+dy
				if (dx == 0 && dy == 0) || nx < 0 || ny < 0 || nx >= navSize || ny >= navSize {
					continue
				}
				n := ny*navSize + nx
				if g.closed[n] {
					continue
				}
				// No squeezing diagonally between two closed cells.
				if dx != 0 && dy != 0 && (g.closed[cy*navSize+nx] || g.closed[ny*navSize+cx]) {
					continue
				}
				step := math.Hypot(float64(dx), float64(dy))
				if g.water[n] {
					step *= waterCost
				}
				if c := cost[cur] + step; c < cost[n] {
					cost[n] = c
					from[n] = int32(cur)
					heap.Push(open, navNode{n, c + math.Hypot(float64(nx-gx), float64(ny-gy))})
				}
			}
		}
	}
	if from[goal] < 0 && goal != start {
		return nil
	}
	var path []Point
	for c := goal; c != start && c >= 0; c = int(from[c]) {
		path = append(path, Point{X: (float64(c%navSize) + 0.5) * navCell, Y: (float64(c/navSize) + 0.5) * navCell})
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// steer returns the direction a bot should walk to reach (gx, gy): straight there when the way
// is clear, otherwise along a route that is searched for again every so often.
func (m *Match) steer(p *Player, gx, gy, dt float64) (float64, float64) {
	b := p.bot
	dir := func(x, y float64) (float64, float64) {
		d := math.Hypot(x-p.X, y-p.Y)
		if d < 1 {
			return 0, 0
		}
		return (x - p.X) / d, (y - p.Y) / d
	}
	if m.walkClear(p.X, p.Y, gx, gy) {
		b.path = nil
		return dir(gx, gy)
	}
	b.replan -= dt
	if b.replan <= 0 || b.path == nil || math.Hypot(gx-b.pathGoal.X, gy-b.pathGoal.Y) > 60 {
		b.path = m.findPath(p.X, p.Y, gx, gy)
		b.pathGoal = Point{X: gx, Y: gy}
		b.replan = navReplan
	}
	for len(b.path) > 0 && math.Hypot(b.path[0].X-p.X, b.path[0].Y-p.Y) < 14 {
		b.path = b.path[1:]
	}
	if len(b.path) == 0 {
		return dir(gx, gy)
	}
	// Head for the furthest of the next few waypoints that can be walked to in a straight line.
	next := b.path[0]
	for i := min(5, len(b.path)-1); i > 0; i-- {
		if m.walkClear(p.X, p.Y, b.path[i].X, b.path[i].Y) {
			next = b.path[i]
			b.path = b.path[i:]
			break
		}
	}
	return dir(next.X, next.Y)
}
