package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func postForm(t *testing.T, c *http.Client, u string, form url.Values) *http.Response {
	t.Helper()
	resp, err := c.PostForm(u, form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func getBody(t *testing.T, c *http.Client, u string) string {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// Random team puts players into the same open match, so they can fill a team together.
func TestRandomTeamJoinsOpenMatch(t *testing.T) {
	srv, _ := newTestServer(t)
	a := newPlayer(t, srv.URL, "quick_a")
	b := newPlayer(t, srv.URL, "quick_b")

	ra := postForm(t, a, srv.URL+"/matches/quick", nil)
	rb := postForm(t, b, srv.URL+"/matches/quick", nil)
	if ra.Header.Get("HX-Redirect") == "" || ra.Header.Get("HX-Redirect") != rb.Header.Get("HX-Redirect") {
		t.Fatalf("quick match: %q vs %q, want the same match", ra.Header.Get("HX-Redirect"), rb.Header.Get("HX-Redirect"))
	}
}

// Friends can invite each other, and the invite shows in the recipient's lobby. Strangers cannot.
func TestInviteFriendsOnly(t *testing.T) {
	srv, _ := newTestServer(t)
	host := newPlayer(t, srv.URL, "host_1")
	friend := newPlayer(t, srv.URL, "buddy_2")
	stranger := newPlayer(t, srv.URL, "stranger_3")

	id := strings.TrimPrefix(postForm(t, host, srv.URL+"/matches", nil).Header.Get("HX-Redirect"), "/matches/")
	postForm(t, host, srv.URL+"/friends", url.Values{"usertag": {"buddy_2"}})

	// A stranger cannot invite someone who is not on their list.
	resp, err := stranger.PostForm(srv.URL+"/invites", url.Values{"matchid": {id}, "usertag": {"host_1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger invite: status %d, want 403", resp.StatusCode)
	}

	// The host, who has added the friend, can invite them.
	postForm(t, host, srv.URL+"/invites", url.Values{"matchid": {id}, "usertag": {"buddy_2"}})
	inbox := getBody(t, friend, srv.URL+"/invites")
	if !strings.Contains(inbox, "host_1") || !strings.Contains(inbox, id) {
		t.Fatalf("friend inbox missing invite: %s", inbox)
	}
}

// The legal pages are public and carry the copyright notice.
func TestLegalPagesServe(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/terms", "/privacy"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "Tarak B Patel") {
			t.Fatalf("%s: status %d, copyright notice present=%v", path, resp.StatusCode, strings.Contains(string(b), "Tarak B Patel"))
		}
	}
}
