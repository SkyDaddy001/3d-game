package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// captureMailer keeps the last email instead of sending it, so tests can read the code.
type captureMailer struct {
	mu   sync.Mutex
	body string
}

func (c *captureMailer) Send(to, subject, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body = body
	return nil
}

var codeRE = regexp.MustCompile(`code is (\d{6})`)

func (c *captureMailer) code(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	m := codeRE.FindStringSubmatch(c.body)
	if m == nil {
		t.Fatalf("no code in last email: %q", c.body)
	}
	return m[1]
}

var testMail = &captureMailer{}

func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s, err := newServer(t.TempDir()+"/accounts.json", testMail)
	if err != nil {
		t.Fatal(err)
	}
	s.botFill = 0 // these tests count real players; bots have their own tests
	mux, err := newMux(s)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s
}

// newPlayer makes a browser-like client: it gets a device cookie, picks a usertag, and verifies
// its email with the code from the capture mailer. Each call is a separate device.
func newPlayer(t *testing.T, base, name string) *http.Client {
	t.Helper()
	c := newPlayerNoName(t, base)
	resp, err := c.PostForm(base+"/profile/name", url.Values{"name": {name}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set name %q: status %d", name, resp.StatusCode)
	}
	resp, err = c.PostForm(base+"/auth/email", url.Values{"email": {name + "@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send code for %q: status %d", name, resp.StatusCode)
	}
	resp, err = c.PostForm(base+"/auth/otp", url.Values{"code": {testMail.code(t)}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify %q: status %d", name, resp.StatusCode)
	}
	return c
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

// createMatch posts to /matches as the player and returns the new match ID.
func createMatch(t *testing.T, base string, c *http.Client) string {
	t.Helper()
	resp, err := c.Post(base+"/matches", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("HX-Redirect")
	if loc == "" {
		t.Fatalf("no HX-Redirect from create, status %d", resp.StatusCode)
	}
	return strings.TrimPrefix(loc, "/matches/")
}

// dialMatch opens the player's WebSocket for a match, sending the device cookie.
func dialMatch(t *testing.T, base, id string, c *http.Client) *websocket.Conn {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	var cookies []string
	for _, ck := range c.Jar.Cookies(u) {
		cookies = append(cookies, ck.String())
	}
	header := http.Header{}
	header.Set("Cookie", strings.Join(cookies, "; "))

	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/ws/" + id
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
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
	alicePl := newPlayer(t, srv.URL, "alice")
	bobPl := newPlayer(t, srv.URL, "bob")
	id := createMatch(t, srv.URL, alicePl)
	clearObstacles(t, s, id)

	alice := dialMatch(t, srv.URL, id, alicePl)
	bob := dialMatch(t, srv.URL, id, bobPl)

	// Both players are in the lobby and the match has not started.
	lobby := waitFor(t, alice, func(s Snapshot) bool { return s.Alive == 2 })
	if lobby.Started {
		t.Fatal("match should not be started yet")
	}

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

	// Move alice toward the world center so she always has room to move.
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

	bobSnap := waitFor(t, bob, func(s Snapshot) bool { return s.Started })
	if bobSnap.Alive != 2 {
		t.Fatalf("bob should count 2 alive, got %d", bobSnap.Alive)
	}
}

func TestJoinRejectedAfterStart(t *testing.T) {
	srv, _ := newTestServer(t)
	alicePl := newPlayer(t, srv.URL, "alice")
	bobPl := newPlayer(t, srv.URL, "bobby")
	id := createMatch(t, srv.URL, alicePl)
	alice := dialMatch(t, srv.URL, id, alicePl)
	dialMatch(t, srv.URL, id, bobPl)
	waitFor(t, alice, func(s Snapshot) bool { return s.Alive == 2 })

	resp, err := http.Post(srv.URL+"/matches/"+id+"/start", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	carolPl := newPlayer(t, srv.URL, "carol")
	resp, err = carolPl.Post(srv.URL+"/matches/"+id+"/join", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("join after start: got status %d, want 409", resp.StatusCode)
	}
}

func TestIdleMatchCloses(t *testing.T) {
	srv, s := newTestServer(t)
	pl := newPlayer(t, srv.URL, "solo")
	id := createMatch(t, srv.URL, pl)
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
