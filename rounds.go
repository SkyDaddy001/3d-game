package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// Rounds mode: two teams play short rounds in a small arena. Each round opens with a buy phase,
// credits carry over, and the first team to win roundsToWin rounds takes the match.

const (
	roundsToWin       = 5
	buyTime           = 12.0  // seconds to shop before each round; nobody can move or shoot
	fightTime         = 100.0 // seconds a round lasts before it is decided on who is left
	roundEndPause     = 4.0
	roundZoneR        = 520.0
	roundShrinkEvery  = 25.0
	roundSpawnOffset  = 380.0 // how far each team starts from the centre
	roundStartCredits = 1000
	roundWinBonus     = 1400
	roundLossBonus    = 900
)

var teamNames = map[int]string{1: "Alpha", 2: "Bravo"}

type roundState struct {
	num   int
	wins  map[int]int
	phase string  // "buy", "fight", or "end"
	left  float64 // seconds left in the phase
}

// RoundView is what clients see of the round state.
type RoundView struct {
	Num    int     `json:"num"`
	Phase  string  `json:"phase"`
	Left   float64 `json:"left"`
	Mine   int     `json:"mine"`   // rounds won by the viewer's team
	Theirs int     `json:"theirs"` // rounds won by the other team
	ToWin  int     `json:"toWin"`
	Team   string  `json:"team"` // the viewer's team name
}

// startRounds splits the room into two teams and opens the first round.
func (m *Match) startRounds() {
	ids := make([]int, 0, len(m.players))
	for id := range m.players {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for i, id := range ids {
		p := m.players[id]
		p.Team = i%2 + 1
		p.ensureAmmo()
		p.credits = roundStartCredits
	}
	m.round = &roundState{wins: map[int]int{}}
	m.startRound()
}

// startRound brings everyone back, resets the arena, and opens the buy phase.
func (m *Match) startRound() {
	r := m.round
	r.num++
	r.phase, r.left = "buy", buyTime
	center := worldSize / 2.0
	m.zoneX, m.zoneY, m.zoneR, m.zoneLeft = center, center, roundZoneR, roundShrinkEvery
	m.pings = nil
	m.grenades = nil

	for _, p := range m.players {
		if p.gone {
			continue
		}
		p.ensureAmmo()
		if !p.Alive { // died last round: bought weapons are lost, the pistol stays
			p.owned = [5]bool{true}
			p.weapon = 0
			p.armor, p.grenades = 0, 0
		}
		for i, w := range weapons {
			if p.owned[i] {
				p.mags[i], p.reserves[i] = w.Mag, w.Reserve
			}
		}
		p.Alive, p.HP, p.downed, p.Place = true, 100, false, 0
		p.bleed, p.reviveProg, p.hurtCool, p.bloom, p.reloadLeft, p.revealLeft = 0, 0, 0, 0, 0, 0
		p.DX, p.DY, p.wantShoot = 0, 0, false
		p.Z, p.vz, p.crouch, p.wantJump = 0, 0, false, false
		side := -1.0
		if p.Team == 2 {
			side = 1.0
		}
		for try := 0; try < 50; try++ {
			p.X = center + side*roundSpawnOffset + (rand.Float64()*2-1)*50
			p.Y = center + (rand.Float64()*2-1)*150
			if !m.blocked(p.X, p.Y) {
				break
			}
		}
		p.Aim = math.Atan2(0, -side) // face the other team
		if p.bot != nil {
			p.bot.lastX, p.bot.lastY, p.bot.seen = p.X, p.Y, 0
		}
	}
	m.logEvent(fmt.Sprintf("Round %d: buy phase", r.num))
}

// stepRound runs the round clock before each tick. It reports whether play is frozen.
func (m *Match) stepRound(dt float64) bool {
	r := m.round
	r.left -= dt
	switch r.phase {
	case "buy":
		if r.left <= 0 {
			r.phase, r.left = "fight", fightTime
			m.zoneLeft = roundShrinkEvery
			m.logEvent(fmt.Sprintf("Round %d: fight!", r.num))
			return false
		}
		return true
	case "end":
		if r.left <= 0 {
			m.startRound()
		}
		return true
	}
	return false
}

// present counts a team's members who are still in the room, and how many of them are alive.
func (m *Match) teamCount(team int) (present, alive int, hp float64) {
	for _, p := range m.players {
		if p.Team != team || p.gone {
			continue
		}
		present++
		if p.Alive {
			alive++
			hp += p.HP
		}
	}
	return
}

// checkRound decides the round once a team is wiped out or time runs out, and the match once a
// team reaches roundsToWin or the other team has left.
func (m *Match) checkRound() {
	r := m.round
	if m.over || r.phase != "fight" {
		return
	}
	p1, a1, hp1 := m.teamCount(1)
	p2, a2, hp2 := m.teamCount(2)
	if a1 > 0 && a2 > 0 && r.left > 0 {
		return
	}
	winner := 0
	switch {
	case a1 > 0 && a2 == 0:
		winner = 1
	case a2 > 0 && a1 == 0:
		winner = 2
	case a1 != a2: // time ran out: more players standing wins, then more health
		winner = 1
		if a2 > a1 {
			winner = 2
		}
	case hp1 != hp2:
		winner = 1
		if hp2 > hp1 {
			winner = 2
		}
	}
	if winner == 0 {
		m.logEvent(fmt.Sprintf("Round %d is a draw", r.num))
	} else {
		r.wins[winner]++
		m.logEvent(fmt.Sprintf("Team %s wins round %d (%d-%d)", teamNames[winner], r.num, r.wins[1], r.wins[2]))
		for _, p := range m.players {
			if p.Team == winner {
				p.credits += roundWinBonus
			} else {
				p.credits += roundLossBonus
			}
		}
	}
	r.phase, r.left = "end", roundEndPause

	// The match ends at roundsToWin, or when a whole team has left the room.
	champion := 0
	switch {
	case r.wins[1] >= roundsToWin || p2 == 0:
		champion = 1
	case r.wins[2] >= roundsToWin || p1 == 0:
		champion = 2
	}
	if champion == 0 {
		return
	}
	for _, p := range m.players {
		p.Place = 2
		if p.Team == champion {
			p.Place = 1
		}
	}
	m.finish("Team "+teamNames[champion], 2)
}

// roundView builds the round state as one player sees it.
func (m *Match) roundView(me *Player) *RoundView {
	r := m.round
	other := 3 - me.Team
	return &RoundView{
		Num: r.num, Phase: r.phase, Left: math.Max(0, r.left),
		Mine: r.wins[me.Team], Theirs: r.wins[other], ToWin: roundsToWin, Team: teamNames[me.Team],
	}
}
