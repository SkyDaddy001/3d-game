package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"testing"
)

// A first visit creates a device account and cookie, and a returning device gets the same account.
func TestDeviceCookieIsTheLogin(t *testing.T) {
	srv, s := newTestServer(t)
	pl := newPlayer(t, srv.URL, "returner")

	u, _ := url.Parse(srv.URL)
	var token string
	for _, c := range pl.Jar.Cookies(u) {
		if c.Name == deviceCookie {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no device cookie after first visit")
	}
	acc, ok := s.store.ByToken(token)
	if !ok || acc.Name != "returner" {
		t.Fatalf("account for device: ok=%v name=%q", ok, acc.Name)
	}

	// The same cookie on a later visit still shows the name, so the lobby skips the form.
	resp, err := pl.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index: status %d", resp.StatusCode)
	}
}

// Without a name, a device cannot create or join a match.
func TestNameRequiredToPlay(t *testing.T) {
	srv, _ := newTestServer(t)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = c.Post(srv.URL+"/matches", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create without name: status %d, want 403", resp.StatusCode)
	}
}

func TestNameRules(t *testing.T) {
	srv, _ := newTestServer(t)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, _ := c.Get(srv.URL + "/")
	resp.Body.Close()

	setName := func(name string) int {
		t.Helper()
		resp, err := c.PostForm(srv.URL+"/profile/name", url.Values{"name": {name}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := setName("ab"); got != http.StatusUnprocessableEntity {
		t.Fatalf("too short: status %d, want 422", got)
	}
	if got := setName("bad name!"); got != http.StatusUnprocessableEntity {
		t.Fatalf("bad characters: status %d, want 422", got)
	}
	if got := setName("Valid_1"); got != http.StatusOK {
		t.Fatalf("valid name: status %d, want 200", got)
	}
	if got := setName("Other_2"); got != http.StatusUnprocessableEntity {
		t.Fatalf("second name on same device: status %d, want 422", got)
	}

	// Another device cannot take the same name, even with different capitals.
	other := newPlayerNoName(t, srv.URL)
	resp, err := other.PostForm(srv.URL+"/profile/name", url.Values{"name": {"VALID_1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("taken name: status %d, want 409", resp.StatusCode)
	}
}

func newPlayerNoName(t *testing.T, base string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return c
}

// Accounts and names survive a restart because they are saved to disk.
func TestAccountsPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.NewDevice()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetName(token, "keeper"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMatch("keeper", MatchResult{XP: 500, Kills: 2, Won: true}); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := reopened.ByToken(token)
	if !ok || acc.Name != "keeper" || acc.XP != 500 || acc.Kills != 2 || acc.Wins != 1 || acc.Matches != 1 {
		t.Fatalf("after restart: ok=%v acc=%+v", ok, acc)
	}
}
