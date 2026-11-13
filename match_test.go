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
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 200, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, target

	m.fire(shooter)
	if target.HP != 100 {
		t.Fatalf("shot through a wall hit the target: HP %v", target.HP)
	}
}

func TestEliminationPlacement(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 25}
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

func TestHeadshotVsBodyDamage(t *testing.T) {
	newPair := func(targetY float64) (*Match, *Player, *Player) {
		m := &Match{players: map[int]*Player{}}
		shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
		target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: targetY, Alive: true, HP: 100}
		m.players[1], m.players[2] = shooter, target
		return m, shooter, target
	}

	_, shooter, target := newPair(50) // dead center: headshot
	m := &Match{players: map[int]*Player{1: shooter, 2: target}}
	m.fire(shooter)
	if target.Alive || target.HP != 0 {
		t.Fatalf("headshot should eliminate: HP %v alive %v", target.HP, target.Alive)
	}
	if shooter.hitKind != "head" || shooter.hitSeq != 1 {
		t.Fatalf("hit info: kind %q seq %d", shooter.hitKind, shooter.hitSeq)
	}

	_, shooter, target = newPair(58) // 8 units off center: body shot
	m = &Match{players: map[int]*Player{1: shooter, 2: target}}
	m.fire(shooter)
	if target.HP != 100-shotDamage {
		t.Fatalf("body shot damage: HP %v, want %v", target.HP, 100-shotDamage)
	}
	if shooter.hitKind != "body" {
		t.Fatalf("hit kind: %q, want body", shooter.hitKind)
	}
}

func TestSpreadGrowsWhileFiring(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	m.players[1] = shooter
	for i := 0; i < 3; i++ {
		m.fire(shooter)
	}
	if shooter.bloom <= 0 {
		t.Fatal("bloom should grow after shots")
	}
	if shooter.bloom > bloomMax+1e-9 {
		t.Fatalf("bloom %v exceeds max %v", shooter.bloom, bloomMax)
	}
}

func TestKillRevealsKiller(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 25}
	m.players[1], m.players[2] = shooter, target

	m.fire(shooter)
	if shooter.revealLeft != bountyTime {
		t.Fatalf("killer should be revealed for %v seconds, got %v", bountyTime, shooter.revealLeft)
	}
}

func TestTeamFormation(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	for i := 1; i <= 4; i++ {
		m.players[i] = &Player{ID: i, Name: "p", Alive: true, HP: 100}
	}
	m.assignTeams()
	if m.players[1].Team != m.players[2].Team || m.players[3].Team != m.players[4].Team || m.players[1].Team == m.players[3].Team {
		t.Fatalf("four players should form two duos: %d %d %d %d",
			m.players[1].Team, m.players[2].Team, m.players[3].Team, m.players[4].Team)
	}

	m2 := &Match{players: map[int]*Player{1: {ID: 1}, 2: {ID: 2}}}
	m2.assignTeams()
	if m2.players[1].Team == m2.players[2].Team {
		t.Fatal("two players should be on separate solo teams")
	}
}

func TestTeammatesCannotBeShot(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	mate := &Player{ID: 2, Name: "b", Team: 1, X: 100, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, mate

	m.fire(shooter)
	if mate.HP != 100 {
		t.Fatalf("teammate was damaged: HP %v", mate.HP)
	}
}
