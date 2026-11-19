package main

import "testing"

// A fight in a plant-and-defuse round: attacker Alpha at site A, defender Bravo far away.
func bombFight() (*Match, *Player, *Player) {
	m := newArena()
	m.Mode = "rounds"
	m.started = true
	m.zoneR = 3000
	m.round = &roundState{num: 1, wins: map[int]int{}, phase: "fight", left: 100}
	m.bomb = newBomb()
	c := worldSize / 2
	att := &Player{ID: 1, Name: "att", Team: 1, Alive: true, HP: 100, X: c - 250, Y: c}
	def := &Player{ID: 2, Name: "def", Team: 2, Alive: true, HP: 100, X: 200, Y: 200}
	m.players[1], m.players[2] = att, def
	return m, att, def
}

func stepSeconds(m *Match, s float64) {
	for i := 0; i < int(s*30); i++ {
		m.stepBomb(1.0 / 30)
	}
}

// Standing alone at a site plants the bomb after bombPlantTime.
func TestBombPlantsAtASite(t *testing.T) {
	m, _, _ := bombFight()
	stepSeconds(m, bombPlantTime+0.5)
	if !m.bomb.planted || m.bomb.site != 1 {
		t.Fatalf("bomb should be planted at site A: planted %v, site %d", m.bomb.planted, m.bomb.site)
	}
}

// A defender at the site stops the plant.
func TestBombPlantBlockedByDefender(t *testing.T) {
	m, _, def := bombFight()
	def.X, def.Y = worldSize/2-250, worldSize/2
	stepSeconds(m, bombPlantTime*2)
	if m.bomb.planted {
		t.Fatal("a defender at the site should stop the plant")
	}
}

// Once planted, a defender alone at the bomb defuses it, and the defenders win the round.
func TestBombDefusedWinsForDefenders(t *testing.T) {
	m, att, def := bombFight()
	stepSeconds(m, bombPlantTime+0.5)
	att.X, att.Y = 300, 1700 // the attacker leaves
	def.X, def.Y = worldSize/2-250, worldSize/2
	stepSeconds(m, bombDefuseTime+0.5)
	if !m.bomb.defused {
		t.Fatal("the bomb should be defused")
	}
	m.checkRound()
	if m.round.phase != "end" || m.round.wins[2] != 1 {
		t.Fatalf("defenders should win the round: phase %s, wins %v", m.round.phase, m.round.wins)
	}
}

// If the fuse runs out, the attackers win the round.
func TestBombExplodesForAttackers(t *testing.T) {
	m, _, def := bombFight()
	def.X, def.Y = 200, 200
	stepSeconds(m, bombPlantTime+0.5)
	stepSeconds(m, bombFuseTime+0.5)
	if !m.bomb.exploded {
		t.Fatal("the bomb should explode when the fuse runs out")
	}
	m.checkRound()
	if m.round.phase != "end" || m.round.wins[1] != 1 {
		t.Fatalf("attackers should win the round: phase %s, wins %v", m.round.phase, m.round.wins)
	}
}

// Attackers wiped out before any plant lose the round.
func TestBombAttackersWipedBeforePlant(t *testing.T) {
	m, att, _ := bombFight()
	att.Alive = false
	m.checkRound()
	if m.round.phase != "end" || m.round.wins[2] != 1 {
		t.Fatalf("defenders should win when attackers are wiped: phase %s, wins %v", m.round.phase, m.round.wins)
	}
}
