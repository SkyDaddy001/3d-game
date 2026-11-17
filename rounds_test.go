package main

import (
	"net/http"
	"net/url"
	"testing"
)

// newRoundsMatch makes a started rounds match with n bots and no obstacles.
func newRoundsMatch(n int) *Match {
	m := newArena()
	m.Mode = "rounds"
	m.addBotsLocked(n)
	m.startRounds()
	m.started = true
	return m
}

func TestRoundsSplitIntoTwoEvenTeams(t *testing.T) {
	m := newRoundsMatch(6)
	sizes := map[int]int{}
	for _, p := range m.players {
		sizes[p.Team]++
		if p.credits != roundStartCredits {
			t.Fatalf("starting credits %d, want %d", p.credits, roundStartCredits)
		}
	}
	if len(sizes) != 2 || sizes[1] != 3 || sizes[2] != 3 {
		t.Fatalf("team sizes %v, want two teams of 3", sizes)
	}
}

// During the buy phase players can shop but cannot move or shoot.
func TestBuyPhaseFreezesPlayButAllowsBuying(t *testing.T) {
	m := newArena()
	m.Mode = "rounds"
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100}
	b := &Player{ID: 2, Name: "b", Alive: true, HP: 100}
	m.players[1], m.players[2] = a, b
	m.startRounds()
	x, y := a.X, a.Y
	a.DX, a.wantShoot, a.wantBuy = 1, true, 2 // tries to walk, shoot, and buy an SMG
	full := a.mags[0]
	m.step(0.1)
	if a.X != x || a.Y != y {
		t.Fatal("a player moved during the buy phase")
	}
	if a.mags[0] != full {
		t.Fatal("a player fired during the buy phase")
	}
	if !a.owned[1] || a.credits != roundStartCredits-weapons[1].Price {
		t.Fatalf("buying in the buy phase: owned %v credits %d", a.owned, a.credits)
	}
	for i := 0; i < int(buyTime*10)+2; i++ {
		m.step(0.1)
	}
	if m.round.phase != "fight" {
		t.Fatalf("phase after the buy time: %q, want fight", m.round.phase)
	}
}

// Wiping a team wins the round, pays both teams, and brings everyone back for the next one.
func TestRoundWinPaysAndRespawns(t *testing.T) {
	m := newArena()
	m.Mode = "rounds"
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100}
	b := &Player{ID: 2, Name: "b", Alive: true, HP: 100}
	m.players[1], m.players[2] = a, b
	m.startRounds()
	m.round.phase, m.round.left = "fight", fightTime
	b.owned[2] = true // b bought a rifle, then dies
	m.eliminate(b, a.Name, a, false)
	m.checkRound()
	if m.round.wins[1] != 1 || m.round.phase != "end" {
		t.Fatalf("after a wipe: wins %v phase %q", m.round.wins, m.round.phase)
	}
	if a.credits != roundStartCredits+killReward+roundWinBonus || b.credits != roundStartCredits+roundLossBonus {
		t.Fatalf("credits: winner %d loser %d", a.credits, b.credits)
	}
	for i := 0; i < int(roundEndPause*10)+2; i++ {
		m.step(0.1)
	}
	if m.round.num != 2 || m.round.phase != "buy" {
		t.Fatalf("next round: num %d phase %q", m.round.num, m.round.phase)
	}
	if !b.Alive || b.HP != 100 {
		t.Fatal("the eliminated player should be back for the next round")
	}
	if b.owned[2] {
		t.Fatal("a player who died should lose bought weapons")
	}
	if m.over {
		t.Fatal("one round should not end the match")
	}
}

// When time runs out, the team with more players standing takes the round.
func TestRoundTimeoutGoesToTheBiggerTeam(t *testing.T) {
	m := newRoundsMatch(4)
	m.round.phase, m.round.left = "fight", 0.01
	for _, p := range m.players {
		if p.Team == 2 {
			m.eliminate(p, "test", nil, false)
			break
		}
	}
	m.checkRound() // both teams still have someone, and there is time left
	if m.round.phase != "fight" {
		t.Fatal("the round ended early")
	}
	m.round.left = -1
	m.checkRound()
	if m.round.wins[1] != 1 {
		t.Fatalf("wins %v, want team 1 to take the timed-out round", m.round.wins)
	}
}

// The first team to roundsToWin wins the match; winners place first and losers second.
func TestMatchEndsAtRoundsToWin(t *testing.T) {
	m := newArena()
	m.Mode = "rounds"
	got := make(chan []PlayerResult, 1)
	m.OnEnd = func(r []PlayerResult) { got <- r }
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100}
	b := &Player{ID: 2, Name: "b", Alive: true, HP: 100}
	m.players[1], m.players[2] = a, b
	m.startRounds()
	for r := 0; r < roundsToWin; r++ {
		if m.over {
			t.Fatalf("match ended after %d rounds", r)
		}
		m.round.phase, m.round.left = "fight", fightTime
		m.eliminate(b, a.Name, a, false)
		m.checkRound()
		if !m.over {
			m.startRound()
		}
	}
	if !m.over || m.winner != "Team Alpha" {
		t.Fatalf("over %v winner %q", m.over, m.winner)
	}
	results := <-got
	for _, r := range results {
		if r.Name == "a" && (!r.Won || r.Place != 1 || r.Players != 2) {
			t.Fatalf("winner result %+v", r)
		}
		if r.Name == "b" && (r.Won || r.Place != 2) {
			t.Fatalf("loser result %+v", r)
		}
	}
}

// A room of bots plays a rounds match through to a champion.
func TestBotRoundsMatchFinishes(t *testing.T) {
	m := newRoundsMatch(6)
	const dt = 1.0 / 30
	for i := 0; i < 30*60*25 && !m.over; i++ {
		m.step(dt)
	}
	if !m.over {
		t.Fatalf("rounds match still running after 25 simulated minutes: round %d, score %v", m.round.num, m.round.wins)
	}
}

// Picked sides are honoured while there is room; everyone else balances the teams.
func TestRoundsHonourPickedSides(t *testing.T) {
	m := newArena()
	m.Mode = "rounds"
	for i := 1; i <= 6; i++ {
		m.players[i] = &Player{ID: i, Name: string(rune('a' + i)), Alive: true, HP: 100}
	}
	m.players[5].wantSide = 2
	m.players[6].wantSide = 2
	m.players[1].wantSide = 2
	m.players[2].wantSide = 2 // a fourth pick for Bravo: the side is full at three
	m.startRounds()
	sizes := map[int]int{}
	for _, p := range m.players {
		sizes[p.Team]++
	}
	if sizes[1] != 3 || sizes[2] != 3 {
		t.Fatalf("team sizes %v, want 3 and 3", sizes)
	}
	// Picks are honoured in player ID order while Bravo has room: 1, 2 and 5 take the three seats.
	if m.players[1].Team != 2 || m.players[2].Team != 2 || m.players[5].Team != 2 {
		t.Fatal("players who picked Bravo while it had room should be on Bravo")
	}
	if m.players[6].Team != 1 {
		t.Fatal("a pick for a full side should be moved to the other team")
	}
}

// Rounds quick play uses its own rooms, and sides can be picked before the start.
func TestRoundsQuickPlayAndSidePick(t *testing.T) {
	srv, s := newTestServer(t)
	a := newPlayer(t, srv.URL, "rounder_a")
	b := newPlayer(t, srv.URL, "rounder_b")
	br := roomID(postForm(t, a, srv.URL+"/matches/quick", nil))
	rr := roomID(postForm(t, a, srv.URL+"/matches/quick", url.Values{"mode": {"rounds"}}))
	if br == rr || s.get(rr).Info().Mode != "rounds" || s.get(br).Info().Mode == "rounds" {
		t.Fatalf("battle royale room %s, rounds room %s (mode %q)", br, rr, s.get(rr).Info().Mode)
	}
	if got := roomID(postForm(t, b, srv.URL+"/matches/quick", url.Values{"mode": {"rounds"}})); got != rr {
		t.Fatalf("second rounds player went to %s, want %s", got, rr)
	}
	s.get(rr).Join("rounder_a")
	if r := postForm(t, a, srv.URL+"/matches/"+rr+"/side", url.Values{"side": {"bravo"}}); r.StatusCode != http.StatusNoContent {
		t.Fatalf("pick side: status %d", r.StatusCode)
	}
	if r := postForm(t, a, srv.URL+"/matches/"+br+"/side", url.Values{"side": {"bravo"}}); r.StatusCode != http.StatusConflict {
		t.Fatalf("picking a side in a battle royale room: status %d", r.StatusCode)
	}
}
