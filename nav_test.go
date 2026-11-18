package main

import (
	"math"
	"math/rand"
	"testing"
)

// A route goes around a wall instead of through it.
func TestPathGoesAroundAWall(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 480, Y: 200, W: 40, H: 600}}
	path := m.findPath(300, 500, 700, 500)
	if len(path) == 0 {
		t.Fatal("no route found around the wall")
	}
	px, py := 312.5, 512.5 // the centre of the starting cell
	for _, w := range path {
		if m.blocked(w.X, w.Y) {
			t.Fatalf("waypoint (%.0f, %.0f) is inside the wall", w.X, w.Y)
		}
		if math.Hypot(w.X-px, w.Y-py) > navCell*1.5 {
			t.Fatal("the route has a gap")
		}
		px, py = w.X, w.Y
	}
	if math.Hypot(px-700, py-500) > navCell {
		t.Fatalf("the route ends at (%.0f, %.0f), not at the goal", px, py)
	}
}

func TestNoPathIntoASealedBox(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 600, Y: 400, W: 200, H: 20}, {X: 600, Y: 580, W: 200, H: 20}, {X: 600, Y: 400, W: 20, H: 200}, {X: 780, Y: 400, W: 20, H: 200}}
	if path := m.findPath(300, 500, 700, 500); path != nil {
		t.Fatalf("found a route into a sealed box: %d waypoints", len(path))
	}
}

// Routes prefer the long way round a lake to swimming straight across.
func TestPathAvoidsWater(t *testing.T) {
	m := newArena()
	m.lakes = []Circle{{X: 500, Y: 500, R: 100}}
	wet := 0
	for _, w := range m.findPath(300, 500, 700, 500) {
		if m.inWater(w.X, w.Y) {
			wet++
		}
	}
	if wet > 0 {
		t.Fatalf("%d waypoints are in the lake", wet)
	}
}

// The grid is rebuilt when a crate is destroyed, so the gap it leaves becomes walkable.
func TestNavRebuildsWhenACrateBreaks(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 480, Y: 480, W: 42, H: 42, K: "crate", HP: 10}}
	cx, cy := navCellOf(500, 500)
	if !m.navLocked().closed[cy*navSize+cx] {
		t.Fatal("the crate's cell should be closed")
	}
	m.damageCrate(0, 50)
	if m.navLocked().closed[cy*navSize+cx] {
		t.Fatal("the cell should open once the crate is gone")
	}
}

// A bot whose goal is behind a long wall walks around it and arrives.
func TestBotWalksAroundAWallToItsGoal(t *testing.T) {
	m := newArena()
	m.rng = rand.New(rand.NewSource(1)) // a fixed seed keeps the bot's aim and wandering the same every run
	m.started = true
	m.zoneR = 3000
	m.obstacles = []Rect{{X: 480, Y: 250, W: 40, H: 500}}
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 400, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	mate := &Player{ID: 2, Name: "mate", Team: 1, X: 600, Y: 500, Alive: true, downed: true, bleed: 1e9}
	far := &Player{ID: 3, Name: "far", Team: 2, X: 1900, Y: 1900, Alive: true, HP: 100}
	m.players[1], m.players[2], m.players[3] = bot, mate, far
	// The detour takes about ten seconds and a revive three more, so allow thirty.
	for i := 0; i < 30*30 && mate.downed; i++ {
		mate.bleed = 1e9
		m.step(1.0 / 30)
	}
	if mate.downed {
		t.Fatalf("the bot never got round the wall to revive its teammate (it is at %.0f, %.0f)", bot.X, bot.Y)
	}
}
