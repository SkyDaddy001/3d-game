package main

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Clans are stored on each member's account (Clan, ClanTag, ClanLeader), so the save file keeps
// its format. A clan exists while at least one account is in it; the leader leaving disbands it.

var (
	clanNameRE = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	clanTagRE  = regexp.MustCompile(`^[A-Za-z0-9]{2,4}$`)
)

var (
	ErrClanName  = errors.New("clan names are 3-16 letters, digits or underscores")
	ErrClanTag   = errors.New("clan tags are 2-4 letters or digits")
	ErrClanTaken = errors.New("that clan name or tag is taken")
	ErrInClan    = errors.New("you are already in a clan")
	ErrNoClan    = errors.New("you are not in a clan")
	ErrNotLeader = errors.New("only the clan leader can do that")
	ErrNoInvite  = errors.New("you have no clan invite")
)

// ClanView is what a player sees of their own clan.
type ClanView struct {
	Name    string
	Tag     string
	Leader  string
	Members []string
	Leading bool
}

// ClanOf returns the clan the account behind token is in.
func (s *Store) ClanOf(token string) (ClanView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok || a.Clan == "" {
		return ClanView{}, false
	}
	v := ClanView{Name: a.Clan, Tag: a.ClanTag, Leading: a.ClanLeader}
	for _, m := range s.accounts {
		if m.Clan != a.Clan {
			continue
		}
		v.Members = append(v.Members, m.Name)
		if m.ClanLeader {
			v.Leader = m.Name
		}
	}
	return v, true
}

// CreateClan makes a new clan with the account behind token as its leader.
func (s *Store) CreateClan(token, name, tag string) error {
	name, tag = strings.TrimSpace(name), strings.TrimSpace(tag)
	if !clanNameRE.MatchString(name) {
		return ErrClanName
	}
	if !clanTagRE.MatchString(tag) {
		return ErrClanTag
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	if !a.Verified {
		return ErrNotVerified
	}
	if a.Clan != "" {
		return ErrInClan
	}
	for _, o := range s.accounts {
		if strings.EqualFold(o.Clan, name) || strings.EqualFold(o.ClanTag, tag) {
			return ErrClanTaken
		}
	}
	a.Clan, a.ClanTag, a.ClanLeader, a.ClanInvite = name, tag, true, ""
	return s.saveLocked()
}

// InviteToClan lets the leader invite a verified player by usertag. A new invite replaces the last.
func (s *Store) InviteToClan(token, usertag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	if a.Clan == "" {
		return ErrNoClan
	}
	if !a.ClanLeader {
		return ErrNotLeader
	}
	otherHash, ok := s.names[strings.ToLower(strings.TrimSpace(usertag))]
	if !ok {
		return ErrNoUser
	}
	other := s.accounts[otherHash]
	if other == a {
		return ErrSelf
	}
	if other.Clan != "" {
		return ErrInClan
	}
	other.ClanInvite = a.Clan
	return s.saveLocked()
}

// AcceptClanInvite joins the clan that invited the account behind token.
func (s *Store) AcceptClanInvite(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	if a.Clan != "" {
		return ErrInClan
	}
	if a.ClanInvite == "" {
		return ErrNoInvite
	}
	tag := ""
	for _, m := range s.accounts {
		if m.Clan == a.ClanInvite {
			tag = m.ClanTag
			break
		}
	}
	if tag == "" { // the clan has disbanded since the invite
		a.ClanInvite = ""
		return s.saveLocked()
	}
	a.Clan, a.ClanTag, a.ClanLeader, a.ClanInvite = a.ClanInvite, tag, false, ""
	return s.saveLocked()
}

// LeaveClan takes the account out of its clan. The leader leaving disbands the clan for everyone.
func (s *Store) LeaveClan(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	if a.Clan == "" {
		return ErrNoClan
	}
	name := a.Clan
	if a.ClanLeader {
		for _, m := range s.accounts {
			if m.Clan == name {
				m.Clan, m.ClanTag, m.ClanLeader = "", "", false
			}
		}
	} else {
		a.Clan, a.ClanTag, a.ClanLeader = "", "", false
	}
	return s.saveLocked()
}

// clanCreate handles the create form.
func (s *Server) clanCreate(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "clan", 20, time.Minute) {
		return
	}
	if err := s.store.CreateClan(sessionFrom(r).token, r.FormValue("name"), r.FormValue("tag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// clanInvite handles the invite form for leaders.
func (s *Server) clanInvite(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "clan", 20, time.Minute) {
		return
	}
	if err := s.store.InviteToClan(sessionFrom(r).token, r.FormValue("usertag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// clanAccept joins the clan that has invited this account.
func (s *Server) clanAccept(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "clan", 20, time.Minute) {
		return
	}
	if err := s.store.AcceptClanInvite(sessionFrom(r).token); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// clanLeave takes this account out of its clan.
func (s *Server) clanLeave(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "clan", 20, time.Minute) {
		return
	}
	if err := s.store.LeaveClan(sessionFrom(r).token); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// clanPanel renders the clan section of the lobby. Every value is escaped before it is written.
func (s *Server) clanPanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c, ok := s.store.ClanOf(sessionFrom(r).token)
	if !ok {
		fmt.Fprint(w, `<p class="empty">No clan yet. Create one below, or accept an invite.</p>`)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<p><strong>[%s] %s</strong> · %d member%s</p><ul class="friends">`,
		html.EscapeString(c.Tag), html.EscapeString(c.Name), len(c.Members), plural(len(c.Members)))
	for _, m := range c.Members {
		tag := ""
		if m == c.Leader {
			tag = " (leader)"
		}
		fmt.Fprintf(&b, `<li>%s%s</li>`, html.EscapeString(m), tag)
	}
	b.WriteString("</ul>")
	if c.Leading {
		b.WriteString(`<p class="small">Invite a player by usertag from the form below. Leaving as leader disbands the clan.</p>`)
	}
	fmt.Fprint(w, b.String())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
