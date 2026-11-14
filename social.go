package main

import (
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"strings"
)

// invite is a pending friend invitation to a match. Invites live in memory and expire when the
// match closes or starts.
type invite struct {
	MatchID string
	From    string
}

// quickMatch puts the player into the first open match, or creates one. Teammates are assigned at
// random when the match starts.
func (s *Server) quickMatch(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	s.mu.Lock()
	var pick *Match
	for _, m := range s.matches {
		info := m.Info()
		if info.Started || info.Private || info.Players >= info.Max || m.Closed() {
			continue
		}
		if pick == nil || m.ID < pick.ID {
			pick = m
		}
	}
	id := ""
	if pick != nil {
		id = pick.ID
	} else {
		id = fmt.Sprintf("%06d", rand.Intn(1_000_000))
		s.matches[id] = s.newMatch(id)
	}
	s.mu.Unlock()
	redirectTo(w, id)
}

// sendInvite invites a friend to a waiting match. Only friends can be invited.
func (s *Server) sendInvite(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	me := sessionFrom(r).acc
	id := r.FormValue("matchid")
	target := strings.TrimSpace(r.FormValue("usertag"))

	if !containsFold(me.Friends, target) {
		http.Error(w, "you can only invite friends", http.StatusForbidden)
		return
	}
	m := s.get(id)
	if m == nil || m.Started() {
		http.Error(w, "match unavailable", http.StatusConflict)
		return
	}
	s.mu.Lock()
	key := strings.ToLower(target)
	s.invites[key] = append(s.invites[key], invite{MatchID: id, From: me.Name})
	s.mu.Unlock()
	fmt.Fprintf(w, "Invite sent to %s.", html.EscapeString(target))
}

// invitesList shows the player's live invitations, with a Join button for each.
func (s *Server) invitesList(w http.ResponseWriter, r *http.Request) {
	name := sessionFrom(r).acc.Name
	key := strings.ToLower(name)

	s.mu.Lock()
	var live []invite
	for _, inv := range s.invites[key] {
		if m := s.matches[inv.MatchID]; m != nil && !m.Closed() && !m.Started() {
			live = append(live, inv)
		}
	}
	s.invites[key] = live
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(live) == 0 {
		fmt.Fprint(w, `<p class="empty">No invites right now.</p>`)
		return
	}
	var b strings.Builder
	for _, inv := range live {
		fmt.Fprintf(&b, `<div class="row-item"><span>%s invited you to match %s</span> `+
			`<button hx-post="/matches/%s/join" hx-swap="none">Join</button></div>`,
			html.EscapeString(inv.From), html.EscapeString(inv.MatchID), html.EscapeString(inv.MatchID))
	}
	fmt.Fprint(w, b.String())
}

// containsFold reports whether list holds v, ignoring case.
func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func (s *Server) terms(w http.ResponseWriter, r *http.Request)   { s.render(w, "terms.html", nil) }
func (s *Server) privacy(w http.ResponseWriter, r *http.Request) { s.render(w, "privacy.html", nil) }
