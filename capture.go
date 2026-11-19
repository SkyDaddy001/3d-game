package main

import "math"

// Capture mode: two teams contest three zones, A, B and C. A zone is taken by the team that is
// alone in it for captureTime seconds, and the team that holds a zone scores while it does.
// The first team to captureScoreLimit points wins.

const (
	captureRadius     = 90.0 // how close a player must be to count in a zone
	captureTime       = 4.0  // seconds one team must be alone in a zone to take it
	captureScoreLimit = 200  // points that win the match
	captureTick       = 1.0  // seconds of holding a zone per point
)

// captureZone is one of the three zones. Owner is the team holding it, 0 when nobody does.
type captureZone struct {
	Name     string  `json:"name"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Owner    int     `json:"owner"`
	Progress float64 `json:"progress"` // seconds the contesting team has been alone in the zone
	Team     int     `json:"-"`        // the team that is taking it, 0 when none is
}

// newCaptureZones lays out A, B and C across the middle of the map.
func newCaptureZones() []captureZone {
	c := worldSize / 2
	return []captureZone{
		{Name: "A", X: c - 500, Y: c},
		{Name: "B", X: c, Y: c},
		{Name: "C", X: c + 500, Y: c},
	}
}

// presentTeams reports which teams have living players inside a zone.
func (m *Match) presentTeams(z captureZone) (alpha, bravo int) {
	for _, p := range m.players {
		if p.gone || !p.Alive || p.Team == 0 {
			continue
		}
		if math.Hypot(p.X-z.X, p.Y-z.Y) > captureRadius {
			continue
		}
		if p.Team == 1 {
			alpha++
		} else {
			bravo++
		}
	}
	return alpha, bravo
}

// stepCapture advances zone control and scores. It does nothing outside capture mode.
func (m *Match) stepCapture(dt float64) {
	if m.Mode != "capture" || !m.started || m.over {
		return
	}
	if m.zones == nil {
		m.zones = newCaptureZones()
	}
	for i := range m.zones {
		z := &m.zones[i]
		a, b := m.presentTeams(*z)
		// Only a team that is alone in the zone can take it or hold it.
		switch {
		case a > 0 && b == 0:
			if z.Owner == 1 {
				z.Progress = 0
				break
			}
			if z.Team != 1 {
				z.Team, z.Progress = 1, 0
			}
			z.Progress += dt
			if z.Progress >= captureTime {
				z.Owner, z.Team, z.Progress = 1, 0, 0
			}
		case b > 0 && a == 0:
			if z.Owner == 2 {
				z.Progress = 0
				break
			}
			if z.Team != 2 {
				z.Team, z.Progress = 2, 0
			}
			z.Progress += dt
			if z.Progress >= captureTime {
				z.Owner, z.Team, z.Progress = 2, 0, 0
			}
		default:
			// Contested or empty: progress stalls and the owner keeps the zone.
			z.Team, z.Progress = 0, 0
		}
	}
	m.captureScore(dt)
}

// captureScore gives each team a share of points for every zone it holds.
func (m *Match) captureScore(dt float64) {
	for _, z := range m.zones {
		if z.Owner == 0 {
			continue
		}
		m.captureFrac[z.Owner-1] += dt / captureTick
		for m.captureFrac[z.Owner-1] >= 1-1e-9 { // the tolerance keeps summed steps from losing a point
			m.captureFrac[z.Owner-1]--
			m.captureScores[z.Owner-1]++
		}
	}
	for team := 0; team < 2; team++ {
		if m.captureScores[team] >= captureScoreLimit {
			m.captureWinner = team + 1
			m.over = true
			return
		}
	}
}
