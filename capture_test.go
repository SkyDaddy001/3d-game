package main

import "testing"

func captureMatch() (*Match, *Player, *Player) {
	m := newArena()
	m.Mode = "capture"
	m.started = true
	m.zones = newCaptureZones()
	a := &Player{ID: 1, Name: "a", Team: 1, Alive: true, HP: 100, X: m.zones[0].X, Y: m.zones[0].Y}
	b := &Player{ID: 2, Name: "b", Team: 2, Alive: true, HP: 100, X: 100, Y: 100}
	m.players[1], m.players[2] = a, b
	return m, a, b
}

// A team alone in a zone takes it after captureTime, and an enemy in the zone stops that.
func TestCaptureTakenWhenAloneAndContestedWhenNot(t *testing.T) {
	m, a, b := captureMatch()
	for i := 0; i < int(captureTime*30)+1; i++ {
		m.stepCapture(1.0 / 30)
	}
	if m.zones[0].Owner != 1 {
		t.Fatalf("zone A should belong to team 1, owner %d", m.zones[0].Owner)
	}

	m, a, b = captureMatch()
	b.X, b.Y = m.zones[0].X+10, m.zones[0].Y // an enemy inside zone A
	for i := 0; i < int(captureTime*30)*3; i++ {
		m.stepCapture(1.0 / 30)
	}
	if m.zones[0].Owner != 0 {
		t.Fatalf("a contested zone should not be taken, owner %d", m.zones[0].Owner)
	}
	_ = a
}

// A team earns one point a second for each zone it holds; the first to the limit wins.
func TestCaptureScoresAndWins(t *testing.T) {
	m, _, _ := captureMatch()
	m.zones[1].Owner = 2 // team 2 already holds zone B
	for i := 0; i < 30*10; i++ {
		m.stepCapture(1.0 / 30)
	}
	if m.captureScores[1] != 10 {
		t.Fatalf("team 2 should have 10 points after ten seconds, has %d", m.captureScores[1])
	}
	m.captureScores[1] = captureScoreLimit - 1
	for i := 0; i < 60 && !m.over; i++ {
		m.stepCapture(1.0 / 30)
	}
	if !m.over || m.captureWinner != 2 {
		t.Fatalf("team 2 should win at the score limit: over %v, winner %d", m.over, m.captureWinner)
	}
}
