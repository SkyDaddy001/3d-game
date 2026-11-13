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
	"net/url"
	"sort"
	"sync"

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
}

type inputMsg struct {
	T     string  `json:"t"`
	DX    float64 `json:"dx"`
	DY    float64 `json:"dy"`
	Aim   float64 `json:"aim"`
	Shoot bool    `json:"shoot"`
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

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Println("template:", err)
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.render(w, "index.html", nil)
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

func (s *Server) createMatch(w http.ResponseWriter, r *http.Request) {
	id := fmt.Sprintf("%06d", rand.Intn(1_000_000))
	s.mu.Lock()
	s.matches[id] = NewMatch(id)
	s.mu.Unlock()
	redirectTo(w, id, r.FormValue("name"))
}

func (s *Server) joinMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m := s.get(id)
	if m == nil || m.Started() {
		http.Error(w, "match unavailable", http.StatusConflict)
		return
	}
	redirectTo(w, id, r.FormValue("name"))
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
	if s.get(id) == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "match.html", struct{ ID, Name string }{id, nameOr(r.URL.Query().Get("name"))})
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	m := s.get(r.PathValue("id"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	p, ok := m.Join(nameOr(r.URL.Query().Get("name")))
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
		m.Input(p, clamp(in.DX, -1, 1), clamp(in.DY, -1, 1), in.Aim, in.Shoot)
	}
}

// redirectTo tells htmx to navigate the full page to the match, carrying the player's name.
func redirectTo(w http.ResponseWriter, id, name string) {
	w.Header().Set("HX-Redirect", "/matches/"+id+"?"+url.Values{"name": {nameOr(name)}}.Encode())
	w.WriteHeader(http.StatusOK)
}

func nameOr(s string) string {
	if s == "" {
		return "anon"
	}
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func main() {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	s := &Server{
		matches: map[string]*Match{},
		tmpl:    template.Must(template.ParseFS(templateFS, "templates/*.html")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /matches", s.listMatches)
	mux.HandleFunc("POST /matches", s.createMatch)
	mux.HandleFunc("GET /matches/{id}", s.matchPage)
	mux.HandleFunc("POST /matches/{id}/join", s.joinMatch)
	mux.HandleFunc("POST /matches/{id}/start", s.startMatch)
	mux.HandleFunc("GET /ws/{id}", s.serveWS)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
