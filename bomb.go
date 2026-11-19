package main

import "math"

// Plant and defuse, as a variant of rounds. The attacking team (Alpha) plants a bomb at one of
// two sites by standing alone there for bombPlantTime. Once planted, the bomb explodes after
// bombFuseTime unless the defending team (Bravo) stands alone at the bomb for bombDefuseTime.
// Attackers win on an explosion, or when the defenders are wiped before a defuse. Defenders win
// on a defuse, when the attackers are wiped before a plant, or when time runs out with no plant.

const (
	bombPlantTime  = 3.0
	bombDefuseTime = 5.0
	bombFuseTime   = 40.0
	bombRadius     = 60.0
	bombAttackers  = 1 // Alpha plants
	bombDefenders  = 2 // Bravo defuses
)

type bombSite struct {
	Name string
	X, Y float64
}

type bombState struct {
	sites     [2]bombSite
	plantSite int     // index+1 of the site the attackers are planting at, 0 for none
	planting  float64 // seconds the attackers have been alone at plantSite
	planted   bool
	site      int     // index+1 of the planted site
	fuse      float64 // seconds until the bomb explodes
	defusing  float64 // seconds the defenders have been alone at the bomb
	defused   bool
	exploded  bool
}

// newBomb lays out sites A and B on either side of the middle of the map.
func newBomb() *bombState {
	c := worldSize / 2
	return &bombState{sites: [2]bombSite{{Name: "A", X: c - 250, Y: c}, {Name: "B", X: c + 250, Y: c}}}
}

// teamsNear counts living players of each team within bombRadius of a point.
func (m *Match) teamsNear(x, y float64) (attackers, defenders int) {
	for _, p := range m.players {
		if p.gone || !p.Alive || p.Team == 0 {
			continue
		}
		if math.Hypot(p.X-x, p.Y-y) > bombRadius {
			continue
		}
		if p.Team == bombAttackers {
			attackers++
		} else {
			defenders++
		}
	}
	return attackers, defenders
}

// stepBomb advances the plant, the fuse and the defuse. It does nothing outside a fight.
func (m *Match) stepBomb(dt float64) {
	b := m.bomb
	if b == nil || m.round == nil || m.round.phase != "fight" || m.over {
		return
	}
	if b.planted {
		if b.defused || b.exploded {
			return
		}
		b.fuse -= dt
		if b.fuse <= 0 {
			b.exploded = true
			m.logEvent("The bomb exploded")
			return
		}
		s := b.sites[b.site-1]
		a, d := m.teamsNear(s.X, s.Y)
		if d > 0 && a == 0 {
			b.defusing += dt
			if b.defusing >= bombDefuseTime {
				b.defused = true
				m.logEvent("The bomb was defused")
			}
		} else {
			b.defusing = 0
		}
		return
	}
	for i, s := range b.sites {
		a, d := m.teamsNear(s.X, s.Y)
		if a == 0 || d > 0 {
			continue
		}
		if b.plantSite != i+1 {
			b.plantSite, b.planting = i+1, 0
		}
		b.planting += dt
		if b.planting >= bombPlantTime {
			b.planted, b.site, b.fuse = true, i+1, bombFuseTime
			m.logEvent("Bomb planted at site " + s.Name)
		}
		return
	}
	b.plantSite, b.planting = 0, 0
}

// bombWinner says who wins the round from the bomb's state and the living players, and whether
// the round is decided yet. a1 and a2 are the attackers and defenders still alive.
func (m *Match) bombWinner(a1, a2 int) (int, bool) {
	b := m.bomb
	switch {
	case b.defused:
		return bombDefenders, true
	case b.exploded:
		return bombAttackers, true
	case b.planted:
		return 0, false // the fuse is running; the round goes on
	case a1 == 0:
		return bombDefenders, true
	case a2 == 0:
		return bombAttackers, true
	case m.round.left <= 0:
		return bombDefenders, true // time ran out with no plant
	}
	return 0, false
}
