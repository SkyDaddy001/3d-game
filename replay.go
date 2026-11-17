package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// Replays: the server can play a whole match between bots in one go and keep a recording of it.
// A recording is a list of frames, ten per second of game time, that the spectator page plays back.

const (
	replayFrameMS  = 100
	replayMaxSecs  = 20 * 60 // a simulated match is cut off after this much game time
	replayKeep     = 12      // recordings kept in memory; the oldest is dropped first
	replayMaxBots  = 16
	replayMinBots  = 2
	replayDefaults = 8
)

// ReplayShot is one trigger pull: who fired, in which direction, and how far the shot travelled.
type ReplayShot struct {
	ID    int     `json:"id"`
	Angle float64 `json:"a"`
	Reach float64 `json:"r"`
}

// ReplayFrame is the whole world at one moment. Loot and Obstacles are only present in frames
// where they changed; a player carries the last ones forward.
type ReplayFrame struct {
	T         float64      `json:"t"`
	Zone      Zone         `json:"zone"`
	Players   []PlayerView `json:"players"`
	Loot      []Loot       `json:"loot,omitempty"`
	Obstacles []Rect       `json:"obstacles,omitempty"`
	Events    []string     `json:"events"`
	Shots     []ReplayShot `json:"shots,omitempty"`
	Round     *RoundView   `json:"round,omitempty"`
}

// Replay is a recorded match.
type Replay struct {
	ID       string        `json:"id"`
	Mode     string        `json:"mode"`
	Players  int           `json:"players"`
	Winner   string        `json:"winner"`
	Finished bool          `json:"finished"` // false if the match hit the time limit undecided
	Duration float64       `json:"duration"` // seconds of game time
	FrameMS  int           `json:"frameMs"`
	Created  int64         `json:"created"`
	World    *WorldView    `json:"world"`
	Frames   []ReplayFrame `json:"frames"`
}

// recorder collects frames while a match runs.
type recorder struct {
	frames   []ReplayFrame
	shots    []ReplayShot
	clock    float64
	nextAt   float64
	lootSig  int
	obstSig  int
	hasFirst bool
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// recordShot notes a trigger pull for the next frame. It is a no-op when nothing is recording.
func (m *Match) recordShot(p *Player, reach float64) {
	dx, dy := math.Cos(p.Aim), math.Sin(p.Aim)
	for _, r := range m.obstacles {
		if t, ok := rayRect(p.X, p.Y, dx, dy, r); ok && t < reach {
			reach = t
		}
	}
	angle := math.Round(p.Aim*1000) / 1000
	// For live players: nearby clients hear the shot and see its tracer.
	if len(m.shots) < 256 {
		m.shots = append(m.shots, ShotView{by: p, X: round1(p.X), Y: round1(p.Y), Angle: angle, Reach: round1(reach), Weapon: weapons[p.weapon].Name})
	}
	if m.rec != nil {
		m.rec.shots = append(m.rec.shots, ReplayShot{ID: p.ID, Angle: angle, Reach: round1(reach)})
	}
}

// ShotView is one shot fired since the last snapshot, for sound and tracers.
type ShotView struct {
	by     *Player
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Angle  float64 `json:"a"`
	Reach  float64 `json:"r"`
	Weapon string  `json:"w"`
}

const hearRange = 900.0 // how far away a shot can be heard

// capture adds a frame when one is due. force records one regardless, for the final moment.
func (m *Match) capture(dt float64, force bool) {
	r := m.rec
	if r == nil {
		return
	}
	r.clock += dt
	if !force && r.clock < r.nextAt {
		return
	}
	r.nextAt += float64(replayFrameMS) / 1000

	f := ReplayFrame{
		T:      round1(r.clock),
		Zone:   Zone{X: round1(m.zoneX), Y: round1(m.zoneY), R: round1(m.zoneR)},
		Events: append([]string{}, m.events...),
		Shots:  r.shots,
	}
	r.shots = nil
	for _, p := range m.players {
		v := p.view()
		v.X, v.Y, v.HP, v.Bleed = round1(v.X), round1(v.Y), round1(v.HP), round1(v.Bleed)
		v.Aim = math.Round(v.Aim*1000) / 1000
		v.Bloom = 0
		f.Players = append(f.Players, v)
	}
	// Loot and obstacles change rarely, so they are stored only when they do.
	lootSig := len(m.loot)
	for id := range m.loot {
		lootSig += id * 31
	}
	if !r.hasFirst || lootSig != r.lootSig {
		r.lootSig = lootSig
		f.Loot = make([]Loot, 0, len(m.loot))
		for _, l := range m.loot {
			f.Loot = append(f.Loot, Loot{ID: l.ID, X: round1(l.X), Y: round1(l.Y), K: l.K})
		}
	}
	if !r.hasFirst || len(m.obstacles) != r.obstSig {
		r.obstSig = len(m.obstacles)
		f.Obstacles = append([]Rect{}, m.obstacles...)
	}
	if m.round != nil {
		// A neutral view of the round: "mine" is Alpha's score and "theirs" is Bravo's.
		f.Round = &RoundView{
			Num: m.round.num, Phase: m.round.phase, Left: round1(math.Max(0, m.round.left)),
			Mine: m.round.wins[1], Theirs: m.round.wins[2], ToWin: roundsToWin, Team: teamNames[1],
		}
	}
	r.hasFirst = true
	r.frames = append(r.frames, f)
}

// SimulateBotMatch plays a full match between bots as fast as the machine allows and returns
// the recording. mode is "" (battle royale, automatic teams), "solo", "duo", or "rounds".
func SimulateBotMatch(id string, bots int, mode string) *Replay {
	bots = max(replayMinBots, min(bots, replayMaxBots))
	m := newMatchState(id)
	m.Mode = mode
	m.rec = &recorder{}
	m.addBotsLocked(bots)
	if mode == "rounds" {
		m.startRounds()
	} else {
		m.assignTeams()
		m.groupTeams()
	}
	m.started = true

	const dt = 1.0 / tickRate
	m.capture(0, true)
	for i := 0; i < replayMaxSecs*tickRate && !m.over; i++ {
		m.step(dt)
		m.capture(dt, false)
	}
	m.capture(0, true)
	return &Replay{
		ID: id, Mode: mode, Players: bots, Winner: m.winner, Finished: m.over,
		Duration: round1(m.rec.clock), FrameMS: replayFrameMS, Created: time.Now().Unix(), World: m.worldView(), Frames: m.rec.frames,
	}
}

// recordBotMatch plays a bot match and opens its replay.
func (s *Server) recordBotMatch(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "replay", 4, time.Minute) {
		return
	}
	if !requirePlayer(w, r) {
		return
	}
	bots := replayDefaults
	fmt.Sscan(r.FormValue("bots"), &bots)
	mode := r.FormValue("mode")
	if mode != "solo" && mode != "duo" && mode != "rounds" {
		mode = ""
	}
	id := fmt.Sprintf("%06d", rand.Intn(1_000_000))
	rep := SimulateBotMatch(id, bots, mode)

	s.mu.Lock()
	s.replays[id] = rep
	s.replayOrder = append(s.replayOrder, id)
	for len(s.replayOrder) > replayKeep {
		delete(s.replays, s.replayOrder[0])
		s.replayOrder = s.replayOrder[1:]
	}
	s.mu.Unlock()
	w.Header().Set("HX-Redirect", "/replays/"+id)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) getReplay(id string) *Replay {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replays[id]
}

// replayPage serves the spectator page for a recording.
func (s *Server) replayPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.getReplay(id) == nil {
		http.Error(w, "that replay is no longer kept; record a new one from the lobby", http.StatusNotFound)
		return
	}
	s.render(w, "match.html", struct {
		ID, Name string
		Replay   bool
	}{id, sessionFrom(r).acc.Name, true})
}

// replayData serves the recording itself as JSON, compressed when the browser accepts it.
func (s *Server) replayData(w http.ResponseWriter, r *http.Request) {
	rep := s.getReplay(r.PathValue("id"))
	if rep == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		json.NewEncoder(gz).Encode(rep)
		return
	}
	json.NewEncoder(w).Encode(rep)
}
