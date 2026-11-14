package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func roomID(resp *http.Response) string {
	return strings.TrimPrefix(resp.Header.Get("HX-Redirect"), "/matches/")
}

// A custom room keeps the settings from the form.
func TestCustomRoomSettings(t *testing.T) {
	srv, s := newTestServer(t)
	s.botFill = 8
	host := newPlayer(t, srv.URL, "host_a")
	id := roomID(postForm(t, host, srv.URL+"/matches", url.Values{
		"size": {"4"}, "mode": {"solo"}, "bots": {"on"}, "private": {"on"},
	}))
	info := s.get(id).Info()
	if info.Max != 4 || info.Mode != "solo" || !info.Bots || !info.Private || info.Host != "host_a" {
		t.Fatalf("room info: %+v", info)
	}

	// No bots when the box is not ticked; an odd size falls back to the default.
	id = roomID(postForm(t, host, srv.URL+"/matches", url.Values{"size": {"7"}, "mode": {"duo"}}))
	info = s.get(id).Info()
	if info.Bots || info.Max != 8 || info.Mode != "duo" || info.Private {
		t.Fatalf("room info: %+v", info)
	}
}

// Private rooms stay out of the list and out of quick play, but open by number.
func TestPrivateRoomIsHiddenButJoinable(t *testing.T) {
	srv, _ := newTestServer(t)
	host := newPlayer(t, srv.URL, "host_b")
	guest := newPlayer(t, srv.URL, "guest_b")
	id := roomID(postForm(t, host, srv.URL+"/matches", url.Values{"size": {"4"}, "private": {"on"}}))

	if list := getBody(t, guest, srv.URL+"/matches"); strings.Contains(list, id) {
		t.Fatal("a private room appeared in the room list")
	}
	if quick := roomID(postForm(t, guest, srv.URL+"/matches/quick", nil)); quick == id {
		t.Fatal("quick play put a player into a private room")
	}
	if got := roomID(postForm(t, guest, srv.URL+"/rooms/join", url.Values{"matchid": {id}})); got != id {
		t.Fatalf("join by number: redirected to %q, want %q", got, id)
	}
	resp := postForm(t, guest, srv.URL+"/rooms/join", url.Values{"matchid": {"000000"}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown room number: status %d, want 404", resp.StatusCode)
	}
}

// A room does not take more players than its size.
func TestRoomSizeLimit(t *testing.T) {
	m := NewMatch("size01")
	m.mu.Lock()
	m.Max = 2
	m.mu.Unlock()
	if _, ok := m.Join("one"); !ok {
		t.Fatal("first join failed")
	}
	if _, ok := m.Join("two"); !ok {
		t.Fatal("second join failed")
	}
	if _, ok := m.Join("three"); ok {
		t.Fatal("a third player joined a two-player room")
	}
}

// The team mode decides teams whatever the player count.
func TestRoomTeamModes(t *testing.T) {
	teams := func(mode string, n int) map[int]int {
		m := &Match{players: map[int]*Player{}, Mode: mode}
		for i := 1; i <= n; i++ {
			m.players[i] = &Player{ID: i}
		}
		m.assignTeams()
		sizes := map[int]int{}
		for _, p := range m.players {
			sizes[p.Team]++
		}
		return sizes
	}
	if got := teams("solo", 6); len(got) != 6 {
		t.Fatalf("solo with 6 players: %d teams, want 6", len(got))
	}
	if got := teams("duo", 2); len(got) != 2 {
		t.Fatalf("duo with 2 players: %d teams, want 2 so there is someone to fight", len(got))
	}
	if got := teams("duo", 6); len(got) != 3 {
		t.Fatalf("duo with 6 players: %d teams, want 3", len(got))
	}
}
