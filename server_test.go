package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	mux, err := newMux(s)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

// clearObstacles removes static obstacles so movement checks are not affected by random layout.
func clearObstacles(t *testing.T, s *Server, id string) {
	t.Helper()
	m := s.get(id)
	if m == nil {
		t.Fatal("match not found")
	}
	m.mu.Lock()
	m.obstacles = nil
	m.mu.Unlock()
}

// createMatch posts to /matches and returns the new match ID from the HX-Redirect header.
func createMatch(t *testing.T, base, name string) string {
	t.Helper()
	resp, err := http.PostForm(base+"/matches", url.Values{"name": {name}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("HX-Redirect")
	if loc == "" {
		t.Fatalf("no HX-Redirect from create, status %d", resp.StatusCode)
	}
	return strings.TrimPrefix(strings.SplitN(loc, "?", 2)[0], "/matches/")
}

func dialMatch(t *testing.T, base, id, name string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/ws/" + id + "?name=" + url.QueryEscape(name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", name, err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

// nextSnapshot reads the next state message from a connection.
func nextSnapshot(t *testing.T, c *websocket.Conn) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return snap
}

// waitFor reads snapshots until pred returns true or the deadline passes.
func waitFor(t *testing.T, c *websocket.Conn, pred func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := nextSnapshot(t, c)
		if pred(snap) {
			return snap
		}
	}
	t.Fatal("condition not reached before timeout")
	return Snapshot{}
}

func TestMatchFlow(t *testing.T) {
	srv, s := newTestServer(t)
	id := createMatch(t, srv.URL, "alice")
	clearObstacles(t, s, id)

	alice := dialMatch(t, srv.URL, id, "alice")
	bob := dialMatch(t, srv.URL, id, "bob")

	// Both players are in the lobby and the match has not started.
	lobby := waitFor(t, alice, func(s Snapshot) bool { return s.Alive == 2 })
	if lobby.Started {
		t.Fatal("match should not be started yet")
	}

	// A late joiner is rejected once the match is not waiting anymore.
	resp, err := http.Post(srv.URL+"/matches/"+id+"/start", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("start: got status %d, want 204", resp.StatusCode)
	}

	started := waitFor(t, alice, func(s Snapshot) bool { return s.Started })
	if started.Zone.R <= 0 {
		t.Fatalf("zone radius should be positive, got %v", started.Zone.R)
	}
	startX := started.Me.X

	// Move alice toward the world center so she always has room to move, and check
	// her position changes on the server.
	dx := 1.0
	if startX > worldSize/2 {
		dx = -1.0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := []byte(fmt.Sprintf(`{"t":"input","dx":%v,"dy":0,"aim":0,"shoot":false}`, dx))
	if err := alice.Write(ctx, websocket.MessageText, input); err != nil {
		t.Fatal(err)
	}
	moved := waitFor(t, alice, func(s Snapshot) bool { return math.Abs(s.Me.X-startX) > 5 })
	if math.Abs(moved.Me.X-startX) <= 5 {
		t.Fatalf("alice did not move: start %v, now %v", startX, moved.Me.X)
	}

	// Bob sees alice only if she is inside his area of interest; he should see the match running.
	bobSnap := waitFor(t, bob, func(s Snapshot) bool { return s.Started })
	if bobSnap.Alive != 2 {
		t.Fatalf("bob should count 2 alive, got %d", bobSnap.Alive)
	}
}

func TestJoinRejectedAfterStart(t *testing.T) {
	srv, _ := newTestServer(t)
	id := createMatch(t, srv.URL, "alice")
	alice := dialMatch(t, srv.URL, id, "alice")
	dialMatch(t, srv.URL, id, "bob")
	waitFor(t, alice, func(s Snapshot) bool { return s.Alive == 2 })

	resp, err := http.Post(srv.URL+"/matches/"+id+"/start", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/matches/"+id+"/join", "application/x-www-form-urlencoded", strings.NewReader("name=carol"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("join after start: got status %d, want 409", resp.StatusCode)
	}
}

func TestIdleMatchCloses(t *testing.T) {
	s, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	mux, err := newMux(s)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	id := createMatch(t, srv.URL, "solo")
	m := s.get(id)
	if m == nil {
		t.Fatal("match not found after create")
	}
	m.mu.Lock()
	m.idleLimit = 200 * time.Millisecond
	m.mu.Unlock()

	// Nobody connects, so the match should close once the idle limit passes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.Closed() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("empty match did not close after idle limit")
}
