package main

import (
	"math"
	"testing"
)

func TestRayRect(t *testing.T) {
	wall := Rect{X: 100, Y: 0, W: 10, H: 100}

	if d, ok := rayRect(0, 50, 1, 0, wall); !ok || math.Abs(d-100) > 1e-9 {
		t.Fatalf("ray through wall: got (%v, %v), want (100, true)", d, ok)
	}
	if _, ok := rayRect(0, 200, 1, 0, wall); ok {
		t.Fatal("ray passing above the wall should miss")
	}
	if _, ok := rayRect(0, 50, -1, 0, wall); ok {
		t.Fatal("ray pointing away from the wall should miss")
	}
}

func TestWallBlocksShot(t *testing.T) {
	m := &Match{
		players:   map[int]*Player{},
		obstacles: []Rect{{X: 100, Y: 0, W: 10, H: 100}},
	}
	shooter := &Player{ID: 1, Name: "a", X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", X: 200, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, target

	m.fire(shooter)
	if target.HP != 100 {
		t.Fatalf("shot through a wall hit the target: HP %v", target.HP)
	}
}

func TestEliminationPlacement(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", X: 100, Y: 50, Alive: true, HP: 25}
	m.players[1], m.players[2] = shooter, target

	m.fire(shooter)
	if target.Alive {
		t.Fatal("target should be eliminated")
	}
	if target.Place != 2 {
		t.Fatalf("placement: got #%d, want #2 with one survivor", target.Place)
	}
	if len(m.events) != 1 {
		t.Fatalf("expected one kill feed event, got %d", len(m.events))
	}
}
