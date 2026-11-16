package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTrophiesRewardPlacement(t *testing.T) {
	if got := ComputeTrophies(1, 2, 8); got != 34 {
		t.Fatalf("win with 2 kills: %d trophies, want 34", got)
	}
	if got := ComputeTrophies(8, 0, 8); got >= 0 {
		t.Fatalf("last place should lose trophies, got %d", got)
	}
	if got := ComputeTrophies(0, 0, 8); got >= 0 {
		t.Fatalf("leaving early should lose trophies, got %d", got)
	}
	if got := ComputeTrophies(1, 5, 1); got != 0 {
		t.Fatalf("a one-player match should not change trophies, got %d", got)
	}
}

func TestTrophiesNeverGoBelowZero(t *testing.T) {
	s, _ := OpenStore("")
	newVerifiedAccount(t, s, "loser_1", "l@example.test")
	if err := s.RecordMatch("loser_1", MatchResult{Trophies: -8}); err != nil {
		t.Fatal(err)
	}
	if top := s.Top(10); len(top) != 1 || top[0].Trophies != 0 {
		t.Fatalf("top: %+v", top)
	}
}

func TestLeaderboardOrder(t *testing.T) {
	s, _ := OpenStore("")
	newVerifiedAccount(t, s, "aaa_low", "a@example.test")
	newVerifiedAccount(t, s, "bbb_high", "b@example.test")
	s.RecordMatch("aaa_low", MatchResult{Trophies: 10})
	s.RecordMatch("bbb_high", MatchResult{Trophies: 40})
	top := s.Top(1)
	if len(top) != 1 || top[0].Name != "bbb_high" {
		t.Fatalf("top player: %+v", top)
	}
}

func TestDailyRewardOncePerDay(t *testing.T) {
	s, _ := OpenStore("")
	tok := newVerifiedAccount(t, s, "daily_1", "d@example.test")
	day1 := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if xp, err := s.ClaimDaily(tok, day1); err != nil || xp != dailyXP {
		t.Fatalf("first claim: xp %d err %v", xp, err)
	}
	if _, err := s.ClaimDaily(tok, day1.Add(5*time.Hour)); !errors.Is(err, ErrDailyClaimed) {
		t.Fatalf("second claim the same day: %v, want ErrDailyClaimed", err)
	}
	if _, err := s.ClaimDaily(tok, day1.Add(24*time.Hour)); err != nil {
		t.Fatalf("claim the next day: %v", err)
	}
	if acc, _ := s.ByToken(tok); acc.XP != 2*dailyXP {
		t.Fatalf("XP after two claims: %d", acc.XP)
	}
}

func TestLookUnlocksByLevel(t *testing.T) {
	s, _ := OpenStore("")
	tok := newVerifiedAccount(t, s, "style_1", "s@example.test")
	if err := s.SetLook(tok, Look{Hair: 2, Skin: 1, Shirt: 4, Hat: 0}); err != nil {
		t.Fatalf("free look: %v", err)
	}
	if err := s.SetLook(tok, Look{Hat: 3}); !errors.Is(err, ErrLookLocked) {
		t.Fatalf("crown at level 1: %v, want ErrLookLocked", err)
	}
	if err := s.SetLook(tok, Look{Hair: 99}); !errors.Is(err, ErrLookInvalid) {
		t.Fatalf("bad index: %v, want ErrLookInvalid", err)
	}
	s.RecordMatch("style_1", MatchResult{XP: 2500}) // level 6
	if err := s.SetLook(tok, Look{Hat: 3, Shirt: 5}); err != nil {
		t.Fatalf("crown at level 6: %v", err)
	}
	acc, _ := s.ByToken(tok)
	if acc.Look.Code() != "0.0.5.3" {
		t.Fatalf("look code %q", acc.Look.Code())
	}
}

// Private rooms are unranked, so friends cannot trade wins for trophies.
func TestPrivateRoomsAreUnranked(t *testing.T) {
	srv, s := newTestServer(t)
	host := newPlayer(t, srv.URL, "rank_host")
	pub := roomID(postForm(t, host, srv.URL+"/matches", url.Values{"size": {"4"}}))
	priv := roomID(postForm(t, host, srv.URL+"/matches", url.Values{"size": {"4"}, "private": {"on"}}))
	if !s.get(pub).IsRanked() || s.get(priv).IsRanked() {
		t.Fatalf("ranked: public %v private %v", s.get(pub).IsRanked(), s.get(priv).IsRanked())
	}
}

func TestLobbyPagesForProgress(t *testing.T) {
	srv, _ := newTestServer(t)
	pl := newPlayer(t, srv.URL, "lobby_1")
	page := getBody(t, pl, srv.URL+"/")
	for _, want := range []string{"Daily reward", "Your look", "Top players", "Crown (Level 6)"} {
		if !strings.Contains(page, want) {
			t.Fatalf("lobby is missing %q", want)
		}
	}
	if resp := postForm(t, pl, srv.URL+"/daily", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("daily claim: status %d", resp.StatusCode)
	}
	if resp := postForm(t, pl, srv.URL+"/daily", nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second daily claim: status %d, want 409", resp.StatusCode)
	}
	if resp := postForm(t, pl, srv.URL+"/profile/look", url.Values{"hair": {"1"}, "skin": {"2"}, "shirt": {"7"}, "hat": {"0"}}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("locked shirt: status %d, want 403", resp.StatusCode)
	}
	if board := getBody(t, pl, srv.URL+"/leaderboard"); !strings.Contains(board, "lobby_1") {
		t.Fatalf("leaderboard missing the player: %s", board)
	}
}

// Shooting a crate wears it down; when it breaks it disappears and leaves a pickup.
func TestCrateBreaksAndDropsLoot(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 100, Y: 41, W: crateSize, H: crateSize, K: "crate", HP: crateHP}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	behind := &Player{ID: 2, Name: "b", Team: 2, X: 200, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, behind

	shooter.bloom = 0 // keep every shot dead straight
	m.fire(shooter)
	if len(m.obstacles) != 1 || behind.HP != 100 || shooter.hitSeq != 1 {
		t.Fatalf("first shot: crates %d, target HP %v, hit marker %d", len(m.obstacles), behind.HP, shooter.hitSeq)
	}
	shooter.bloom = 0 // keep every shot dead straight
	m.fire(shooter)
	shooter.bloom = 0 // keep every shot dead straight
	m.fire(shooter)   // 3 x 25 damage breaks a 60 HP crate
	if len(m.obstacles) != 0 {
		t.Fatal("the crate should be gone after three pistol shots")
	}
	if len(m.loot) != 1 {
		t.Fatalf("a broken crate should leave one pickup, got %d", len(m.loot))
	}
	shooter.bloom = 0 // keep every shot dead straight
	m.fire(shooter)
	if behind.HP == 100 {
		t.Fatal("with the crate gone, the shot should reach the player behind it")
	}
}

// Buildings and trees stay solid however much they are shot.
func TestBuildingsCannotBeDestroyed(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 100, Y: 0, W: 30, H: 100}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	m.players[1] = shooter
	for i := 0; i < 10; i++ {
		m.fire(shooter)
	}
	if len(m.obstacles) != 1 || shooter.hitSeq != 0 {
		t.Fatalf("building: obstacles %d, hit markers %d", len(m.obstacles), shooter.hitSeq)
	}
}

// Bigger crates take more shots, and shipping containers cannot be broken at all.
func TestBigCratesAndContainers(t *testing.T) {
	m := newArena()
	m.obstacles = []Rect{{X: 100, Y: 29, W: 42, H: 42, K: "crate", HP: 170}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	m.players[1] = shooter
	shots := 0
	for len(m.obstacles) > 0 && shots < 20 {
		shooter.bloom = 0
		m.fire(shooter)
		shots++
	}
	if shots != 7 { // 170 health at 25 per pistol shot
		t.Fatalf("a large crate broke after %d pistol shots, want 7", shots)
	}

	m.obstacles = []Rect{{X: 100, Y: 0, W: containerWid, H: containerLen, K: "container"}}
	for i := 0; i < 30; i++ {
		shooter.bloom = 0
		m.fire(shooter)
	}
	if len(m.obstacles) != 1 {
		t.Fatal("a container was destroyed")
	}
}

// A new map has every crate size and some containers, and no cover overlaps other obstacles.
func TestMapHasVariedCover(t *testing.T) {
	m := newMatchState("cover1")
	sizes, containers := map[float64]int{}, 0
	for i, o := range m.obstacles {
		switch o.K {
		case "crate":
			sizes[o.W]++
		case "container":
			containers++
		default:
			continue
		}
		for j, q := range m.obstacles {
			if i != j && o.X < q.X+q.W && o.X+o.W > q.X && o.Y < q.Y+q.H && o.Y+o.H > q.Y {
				t.Fatalf("%s at (%.0f, %.0f) overlaps another obstacle", o.K, o.X, o.Y)
			}
		}
	}
	if len(sizes) != 3 || containers == 0 {
		t.Fatalf("crate sizes %v, containers %d", sizes, containers)
	}
}
