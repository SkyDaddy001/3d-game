package main

import (
	"math"
	"math/rand"
)

// Terrain features that are not obstacles: lakes to swim across and bushes to hide in.

const (
	lakeN        = 3
	bushN        = 45
	swimSpeed    = 0.5  // movement speed factor in water
	swimTop      = 12.0 // only the head and shoulders show above the water
	swimEye      = 8.0
	bushSpotDist = 70.0 // an enemy this close sees a player in a bush anyway
	bushQuiet    = 1.0  // seconds after firing before a bush hides the player again
)

// Circle is a round area on the map: a lake or a bush.
type Circle struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	R float64 `json:"r"`
}

// WorldView is the fixed terrain, sent to each player once.
type WorldView struct {
	Lakes  []Circle `json:"lakes"`
	Bushes []Circle `json:"bushes"`
}

// addTerrain places lakes and bushes on a new map. Lakes keep clear of the middle, where rounds
// are played, and anything standing where a lake goes is removed.
func (m *Match) addTerrain() {
	center := worldSize / 2.0
	for try := 0; try < 200 && len(m.lakes) < lakeN; try++ {
		l := Circle{R: 110 + rand.Float64()*60}
		l.X = l.R + 60 + rand.Float64()*(worldSize-2*l.R-120)
		l.Y = l.R + 60 + rand.Float64()*(worldSize-2*l.R-120)
		if math.Hypot(l.X-center, l.Y-center) < 620+l.R {
			continue
		}
		clear := true
		for _, o := range m.lakes {
			if math.Hypot(l.X-o.X, l.Y-o.Y) < l.R+o.R+120 {
				clear = false
			}
		}
		if clear {
			m.lakes = append(m.lakes, l)
		}
	}
	kept := m.obstacles[:0]
	for _, o := range m.obstacles {
		wet := false
		for _, l := range m.lakes {
			if math.Hypot(o.X+o.W/2-l.X, o.Y+o.H/2-l.Y) < l.R+math.Max(o.W, o.H)/2+12 {
				wet = true
			}
		}
		if !wet {
			kept = append(kept, o)
		}
	}
	m.obstacles = kept

	for try := 0; try < 400 && len(m.bushes) < bushN; try++ {
		b := Circle{X: 40 + rand.Float64()*(worldSize-80), Y: 40 + rand.Float64()*(worldSize-80), R: 20 + rand.Float64()*10}
		if m.inWater(b.X, b.Y) || m.blocked(b.X, b.Y) {
			continue
		}
		m.bushes = append(m.bushes, b)
	}
}

func (m *Match) inWater(x, y float64) bool {
	for _, l := range m.lakes {
		if math.Hypot(x-l.X, y-l.Y) < l.R {
			return true
		}
	}
	return false
}

func (m *Match) inBush(x, y float64) bool {
	for _, b := range m.bushes {
		if math.Hypot(x-b.X, y-b.Y) < b.R {
			return true
		}
	}
	return false
}

// concealed reports whether q is hidden from viewer by a bush: q is in one, has not fired in the
// last moment, and the viewer is not right next to them.
func (m *Match) concealed(viewer, q *Player) bool {
	return q.quiet >= bushQuiet && !q.swim && m.inBush(q.X, q.Y) &&
		math.Hypot(q.X-viewer.X, q.Y-viewer.Y) > bushSpotDist
}

func (m *Match) worldView() *WorldView {
	w := &WorldView{Lakes: m.lakes, Bushes: m.bushes}
	if w.Lakes == nil {
		w.Lakes = []Circle{}
	}
	if w.Bushes == nil {
		w.Bushes = []Circle{}
	}
	return w
}
