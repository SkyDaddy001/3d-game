package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A voice report names a real player in the match and is saved with the match ID.
func TestVoiceReportIsSaved(t *testing.T) {
	srv, s := newTestServer(t)
	s.reports.path = filepath.Join(t.TempDir(), "reports.jsonl")
	a := newPlayer(t, srv.URL, "listener_1")
	rid := roomID(postForm(t, a, srv.URL+"/matches/quick", nil))
	m := s.get(rid)
	m.Join("loud_one")
	var speaker int
	for id, p := range m.players {
		if p.Name == "loud_one" {
			speaker = id
		}
	}
	resp := postForm(t, a, srv.URL+"/matches/"+rid+"/voice-report", url.Values{"speaker": {strconv.Itoa(speaker)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("voice report: status %d", resp.StatusCode)
	}
	data, _ := os.ReadFile(s.reports.path)
	for _, want := range []string{`"reporter":"listener_1"`, `"target":"loud_one"`, "voice in match " + rid} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("report file is missing %s: %s", want, data)
		}
	}
	if r := postForm(t, a, srv.URL+"/matches/"+rid+"/voice-report", url.Values{"speaker": {"999999"}}); r.StatusCode != http.StatusNotFound {
		t.Fatalf("report of an unknown speaker: status %d", r.StatusCode)
	}
}
