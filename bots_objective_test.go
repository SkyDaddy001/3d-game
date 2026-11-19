package main

import (
	"math"
	"testing"
)

func botAt(id, team int, x, y float64) *Player {
	return &Player{ID: id, Name: "bot", Team: team, Alive: true, HP: 100, X: x, Y: y, bot: &botState{}}
}

// Bots on both teams take zones: after a minute, some zone belongs to a team.
func TestBotsTakeCaptureZones(t *testing.T) {
	m := newArena()
	m.Mode = "capture"
	m.started = true
	m.zones = newCaptureZones()
	m.players[1] = botAt(1, 1, 300, 300)
	m.players[2] = botAt(2, 1, 320, 280)
	m.players[3] = botAt(3, 2, 1700, 1700)
	m.players[4] = botAt(4, 2, 1680, 1720)
	for i := 0; i < 30*60; i++ {
		m.step(1.0 / 30)
	}
	owned := 0
	for _, z := range m.zones {
		if z.Owner != 0 {
			owned++
		}
	}
	if owned == 0 {
		t.Fatalf("after a minute no bot team held a zone: %+v", m.zones)
	}
}

// In plant and defuse, attackers head for the two sites and one of them plants.
func TestBotsPlantTheBomb(t *testing.T) {
	m := newArena()
	m.Mode = "rounds"
	m.started = true
	m.round = &roundState{num: 1, wins: map[int]int{}, phase: "fight", left: 100}
	m.bomb = newBomb()
	c := float64(worldSize / 2)
	m.players[1] = botAt(1, 1, c-380, c)
	m.players[2] = botAt(2, 1, c-380, c+20)
	m.players[3] = botAt(3, 2, c+380, c)
	m.players[4] = botAt(4, 2, c+380, c+20)
	closest := math.Inf(1)
	for i := 0; i < 30*30 && !m.bomb.planted && m.round.phase == "fight"; i++ {
		m.step(1.0 / 30)
		for _, p := range []*Player{m.players[1], m.players[2]} {
			for _, s := range m.bomb.sites {
				if p.Alive && p.Team == bombAttackers {
					closest = math.Min(closest, math.Hypot(p.X-s.X, p.Y-s.Y))
				}
			}
		}
	}
	if !m.bomb.planted && m.round.phase == "fight" {
		t.Fatalf("after thirty seconds the bomb was not planted and the round is still on (closest attacker %.0f from a site)", closest)
	}
}
