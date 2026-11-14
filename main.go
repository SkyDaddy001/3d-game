package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	mu      sync.Mutex
	matches map[string]*Match
	tmpl    *template.Template
	store   *Store
	mailer  Mailer
	invites map[string][]invite // lowercase usertag -> pending invites
	botFill int                 // room size that bots fill up to when a match starts; 0 for no bots
}

// defaultBotFill is the room size bots fill up to unless ARENA_BOT_FILL says otherwise.
const defaultBotFill = 8

type inputMsg struct {
	T      string  `json:"t"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Aim    float64 `json:"aim"`
	Shoot  bool    `json:"shoot"`
	Ping   bool    `json:"ping"`
	PX     float64 `json:"px"`
	PY     float64 `json:"py"`
	Reload bool    `json:"reload"`
	Switch int     `json:"switch"`
	Revive bool    `json:"revive"`
	Buy    int     `json:"buy"`
}

func (s *Server) get(id string) *Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matches[id]
}

// sweepClosed removes matches that have stopped, once a minute.
func (s *Server) sweepClosed(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			for id, m := range s.matches {
				if m.Closed() {
					delete(s.matches, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Println("template:", err)
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	acc := sessionFrom(r).acc
	level, into, need := LevelProgress(acc.XP)
	s.render(w, "index.html", struct {
		Name     string
		Verified bool
		Friends  []string
		Level    int
		XPInto   int
		XPNeed   int
		Percent  int
		Matches  int
		Wins     int
		Kills    int
	}{acc.Name, acc.Verified, acc.Friends, level, into, need, into * 100 / need, acc.Matches, acc.Wins, acc.Kills})
}

func (s *Server) listMatches(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ms := make([]*Match, 0, len(s.matches))
	for _, m := range s.matches {
		ms = append(ms, m)
	}
	s.mu.Unlock()

	rows := make([]RoomInfo, 0, len(ms))
	for _, m := range ms {
		if info := m.Info(); !info.Private && !m.Closed() {
			rows = append(rows, info)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	s.render(w, "matches.html", rows)
}

// requirePlayer stops players who have not chosen a usertag and verified their email.
func requirePlayer(w http.ResponseWriter, r *http.Request) bool {
	if !ready(r) {
		http.Error(w, "choose a usertag and verify your email first", http.StatusForbidden)
		return false
	}
	return true
}

// roomSizes are the sizes a custom room may have.
var roomSizes = map[int]bool{2: true, 4: true, 8: true, 12: true, 16: true}

// createMatch makes a custom room from the lobby form: size, team mode, bots, and privacy.
// Missing or invalid fields fall back to the defaults: 8 players, automatic teams, bots on, public.
func (s *Server) createMatch(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	size := s.botFill
	if n, err := strconv.Atoi(r.FormValue("size")); err == nil && roomSizes[n] {
		size = n
	}
	if size < 2 {
		size = defaultBotFill
	}
	mode := r.FormValue("mode")
	if mode != "solo" && mode != "duo" {
		mode = ""
	}
	// The lobby form always sends "size"; with it present, an unticked bots box means no bots.
	bots := s.botFill > 0 && (r.FormValue("bots") == "on" || r.FormValue("size") == "")

	id := fmt.Sprintf("%06d", rand.Intn(1_000_000))
	m := s.newMatch(id)
	m.mu.Lock()
	m.Max = size
	m.Mode = mode
	m.Private = r.FormValue("private") == "on"
	m.Host = sessionFrom(r).acc.Name
	m.FillTo = 0
	if bots {
		m.FillTo = size
	}
	m.mu.Unlock()
	s.mu.Lock()
	s.matches[id] = m
	s.mu.Unlock()
	redirectTo(w, id)
}

// joinByNumber joins a room from its six-digit number, which is how private rooms are shared.
func (s *Server) joinByNumber(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("matchid"))
	m := s.get(id)
	if m == nil {
		http.Error(w, "no room with that number", http.StatusNotFound)
		return
	}
	if info := m.Info(); info.Started || info.Players >= info.Max {
		http.Error(w, "that room is full or already playing", http.StatusConflict)
		return
	}
	redirectTo(w, id)
}

func (s *Server) joinMatch(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	id := r.PathValue("id")
	m := s.get(id)
	if m == nil {
		http.Error(w, "match unavailable", http.StatusConflict)
		return
	}
	if info := m.Info(); info.Started || info.Players >= info.Max {
		http.Error(w, "that room is full or already playing", http.StatusConflict)
		return
	}
	redirectTo(w, id)
}

func (s *Server) startMatch(w http.ResponseWriter, r *http.Request) {
	m := s.get(r.PathValue("id"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	if !m.Start() {
		http.Error(w, "the match has already started or has no players", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) matchPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ready(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	name := sessionFrom(r).acc.Name
	if s.get(id) == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "match.html", struct{ ID, Name string }{id, name})
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	m := s.get(r.PathValue("id"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	p, ok := m.Join(sessionFrom(r).acc.Name)
	if !ok {
		http.Error(w, "match full or already started", http.StatusConflict)
		return
	}
	c, err := websocket.Accept(w, r, nil) // same-origin check enforced by default
	if err != nil {
		m.Leave(p)
		return
	}
	defer c.CloseNow()
	defer m.Leave(p)
	c.SetReadLimit(512)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		for {
			select {
			case msg := <-p.send:
				if err := c.Write(ctx, websocket.MessageText, msg); err != nil {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var in inputMsg
		if json.Unmarshal(data, &in) != nil || in.T != "input" {
			continue
		}
		m.Input(p, Controls{
			DX:     clamp(in.DX, -1, 1),
			DY:     clamp(in.DY, -1, 1),
			Aim:    in.Aim,
			Shoot:  in.Shoot,
			Ping:   in.Ping,
			PX:     clamp(in.PX, 0, worldSize),
			PY:     clamp(in.PY, 0, worldSize),
			Reload: in.Reload,
			Switch: in.Switch,
			Revive: in.Revive,
			Buy:    in.Buy,
		})
	}
}

// newMatch creates a match whose results are saved to each player's profile when it ends.
func (s *Server) newMatch(id string) *Match {
	m := NewMatch(id)
	m.mu.Lock()
	m.FillTo = s.botFill
	m.OnEnd = func(results []PlayerResult) {
		for _, r := range results {
			err := s.store.RecordMatch(r.Name, MatchResult{
				XP: ComputeXP(r.Kills, r.Place, r.Damage), Kills: r.Kills, Won: r.Won,
			})
			if err != nil {
				log.Println("record match:", r.Name, err)
			}
		}
	}
	m.mu.Unlock()
	return m
}

// redirectTo tells htmx to navigate the full page to the match.
func redirectTo(w http.ResponseWriter, id string) {
	w.Header().Set("HX-Redirect", "/matches/"+id)
	w.WriteHeader(http.StatusOK)
}

// newServer opens the account store at storePath. An empty path keeps accounts in memory.
func newServer(storePath string, mailer Mailer) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	store, err := OpenStore(storePath)
	if err != nil {
		return nil, err
	}
	return &Server{matches: map[string]*Match{}, tmpl: tmpl, store: store, mailer: mailer, invites: map[string][]invite{}, botFill: defaultBotFill}, nil
}

func newMux(s *Server) (http.Handler, error) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /profile/name", s.setName)
	mux.HandleFunc("POST /auth/email", s.sendCode)
	mux.HandleFunc("POST /auth/otp", s.verifyCode)
	mux.HandleFunc("GET /friends", s.friendsList)
	mux.HandleFunc("GET /invites", s.invitesList)
	mux.HandleFunc("POST /invites", s.sendInvite)
	mux.HandleFunc("POST /matches/quick", s.quickMatch)
	mux.HandleFunc("GET /terms", s.terms)
	mux.HandleFunc("GET /privacy", s.privacy)
	mux.HandleFunc("POST /friends", s.addFriend)
	mux.HandleFunc("GET /matches", s.listMatches)
	mux.HandleFunc("POST /matches", s.createMatch)
	mux.HandleFunc("POST /rooms/join", s.joinByNumber)
	mux.HandleFunc("GET /matches/{id}", s.matchPage)
	mux.HandleFunc("POST /matches/{id}/join", s.joinMatch)
	mux.HandleFunc("POST /matches/{id}/start", s.startMatch)
	mux.HandleFunc("GET /ws/{id}", s.serveWS)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	return s.withAccount(mux), nil
}

func main() {
	dataPath := os.Getenv("ARENA_DATA")
	if dataPath == "" {
		dataPath = "arena-data.json"
	}
	s, err := newServer(dataPath, mailerFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	if v := os.Getenv("ARENA_BOT_FILL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxPlayers {
			log.Fatalf("ARENA_BOT_FILL must be a number from 0 to %d", maxPlayers)
		}
		s.botFill = n
	}
	mux, err := newMux(s)
	if err != nil {
		log.Fatal(err)
	}
	go s.sweepClosed(context.Background())
	log.Println("listening on :8080, accounts in", dataPath)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
