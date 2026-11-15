package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// A simulated bot match plays to a winner and records the whole game.
func TestSimulatedBotMatchIsRecorded(t *testing.T) {
	rep := SimulateBotMatch("rep001", 8, "")
	if !rep.Finished || rep.Winner == "" || rep.Winner == "nobody" {
		t.Fatalf("finished %v winner %q after %.0fs", rep.Finished, rep.Winner, rep.Duration)
	}
	if len(rep.Frames) < 50 {
		t.Fatalf("only %d frames recorded", len(rep.Frames))
	}
	first, last := rep.Frames[0], rep.Frames[len(rep.Frames)-1]
	if len(first.Players) != 8 || len(first.Obstacles) == 0 || len(first.Loot) == 0 {
		t.Fatalf("first frame: %d players, %d obstacles, %d loot", len(first.Players), len(first.Obstacles), len(first.Loot))
	}
	alive := 0
	for _, p := range last.Players {
		if p.Alive {
			alive++
		}
	}
	if alive == 0 || alive > 2 {
		t.Fatalf("%d players alive in the last frame, want the winning team only", alive)
	}
	shots := 0
	for i, f := range rep.Frames {
		shots += len(f.Shots)
		if i > 0 && f.T < rep.Frames[i-1].T {
			t.Fatalf("frame %d goes back in time", i)
		}
	}
	if shots == 0 {
		t.Fatal("no shots were recorded in a whole match")
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%.0fs of play, %d frames, %d shots, %.1f MB of JSON", rep.Duration, len(rep.Frames), shots, float64(len(data))/1e6)
}

func TestSimulatedRoundsMatchIsRecorded(t *testing.T) {
	rep := SimulateBotMatch("rep002", 6, "rounds")
	if !rep.Finished || (rep.Winner != "Team Alpha" && rep.Winner != "Team Bravo") {
		t.Fatalf("finished %v winner %q", rep.Finished, rep.Winner)
	}
	if rep.Frames[len(rep.Frames)-1].Round == nil {
		t.Fatal("rounds replay frames should carry the round state")
	}
}

// The lobby button records a bot match and opens its replay page; the data is served as JSON.
func TestReplayRoutes(t *testing.T) {
	srv, _ := newTestServer(t)
	pl := newPlayer(t, srv.URL, "watcher_1")
	resp := postForm(t, pl, srv.URL+"/replays/bots", nil)
	path := resp.Header.Get("HX-Redirect")
	if !strings.HasPrefix(path, "/replays/") {
		t.Fatalf("record: status %d redirect %q", resp.StatusCode, path)
	}
	page := getBody(t, pl, srv.URL+path)
	if !strings.Contains(page, `id="rp-pause"`) {
		t.Fatal("replay page is missing its controls")
	}
	if live := getBody(t, pl, srv.URL+"/"); strings.Contains(live, `id="rp-pause"`) {
		t.Fatal("replay controls leaked into another page")
	}
	var rep Replay
	if err := json.Unmarshal([]byte(getBody(t, pl, srv.URL+path+"/data")), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Frames) == 0 || rep.FrameMS != replayFrameMS {
		t.Fatalf("replay data: %d frames, frame time %d", len(rep.Frames), rep.FrameMS)
	}
	if r2, _ := pl.Get(srv.URL + "/replays/000000"); r2.StatusCode != 404 {
		t.Fatalf("unknown replay: status %d, want 404", r2.StatusCode)
	}
}

// Only the most recent recordings are kept.
func TestOldReplaysAreDropped(t *testing.T) {
	srv, s := newTestServer(t)
	pl := newPlayer(t, srv.URL, "watcher_2")
	first := postForm(t, pl, srv.URL+"/replays/bots", url.Values{"bots": {"2"}}).Header.Get("HX-Redirect")
	for i := 0; i < replayKeep; i++ {
		postForm(t, pl, srv.URL+"/replays/bots", url.Values{"bots": {"2"}})
	}
	s.mu.Lock()
	kept := len(s.replays)
	s.mu.Unlock()
	if kept != replayKeep {
		t.Fatalf("%d replays kept, want %d", kept, replayKeep)
	}
	if s.getReplay(strings.TrimPrefix(first, "/replays/")) != nil {
		t.Fatal("the oldest replay should have been dropped")
	}
}
