package main

import "testing"

// Starting a capture room sets up the zones and plain teams; starting a bomb room sets up the bomb and rounds.
func TestCaptureAndBombRoomsStart(t *testing.T) {
	m := newArena()
	m.Mode = "capture"
	m.players[1] = &Player{ID: 1, Name: "a", Alive: true, HP: 100}
	m.players[2] = &Player{ID: 2, Name: "b", Alive: true, HP: 100}
	if !m.Start() {
		t.Fatal("capture room did not start")
	}
	if len(m.zones) != 3 || m.round != nil {
		t.Fatalf("capture room should have three zones and no rounds: zones %d, round %v", len(m.zones), m.round)
	}

	b := newArena()
	b.Mode = "bomb"
	b.players[1] = &Player{ID: 1, Name: "a", Alive: true, HP: 100}
	b.players[2] = &Player{ID: 2, Name: "b", Alive: true, HP: 100}
	if !b.Start() {
		t.Fatal("bomb room did not start")
	}
	if b.bomb == nil || b.round == nil {
		t.Fatalf("bomb room should have a bomb and rounds: bomb %v, round %v", b.bomb, b.round)
	}
}
