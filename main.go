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
}

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
}

type matchRow struct {
	ID      string
	Players int
	Started bool
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
	s.render(w, "index.html", struct {
		Name     string
		Verified bool
		Friends  []string
	}{acc.Name, acc.Verified, acc.Friends})
}

func (s *Server) listMatches(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ms := make([]*Match, 0, len(s.matches))
	for _, m := range s.matches {
		ms = append(ms, m)
	}
	s.mu.Unlock()

	rows := make([]matchRow, 0, len(ms))
	for _, m := range ms {
		n, started := m.Summary()
		rows = append(rows, matchRow{ID: m.ID, Players: n, Started: started})
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

func (s *Server) createMatch(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	id := fmt.Sprintf("%06d", rand.Intn(1_000_000))
	s.mu.Lock()
	s.matches[id] = NewMatch(id)
	s.mu.Unlock()
	redirectTo(w, id)
}

func (s *Server) joinMatch(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	id := r.PathValue("id")
	m := s.get(id)
	if m == nil || m.Started() {
		http.Error(w, "match unavailable", http.StatusConflict)
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
		http.Error(w, "need at least 2 players", http.StatusConflict)
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
		})
	}
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
	return &Server{matches: map[string]*Match{}, tmpl: tmpl, store: store, mailer: mailer, invites: map[string][]invite{}}, nil
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
	mux, err := newMux(s)
	if err != nil {
		log.Fatal(err)
	}
	go s.sweepClosed(context.Background())
	log.Println("listening on :8080, accounts in", dataPath)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
