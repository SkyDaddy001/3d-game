package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// Usertag rules, blocking, and reports.

var ErrNameBlocked = errors.New("that name is not allowed, choose another")

// reservedNames could be mistaken for staff or for a bot. rudeNames is a short starter list; add
// more, one per line, in the file named by ARENA_BLOCKED_NAMES.
var (
	reservedNames = []string{"admin", "moderator", "staff", "official", "support", "system", "arenaroyale", "developer"}
	rudeNames     = []string{"fuck", "shit", "bitch", "cunt", "dick", "cock", "pussy", "asshole", "bastard", "slut", "whore", "rape", "nazi", "hitler", "porn"}
	extraNames    []string
	extraOnce     sync.Once
)

// plainName folds a usertag so simple disguises still match: case, underscores, digits for letters.
func plainName(name string) string {
	r := strings.NewReplacer("_", "", "0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "8", "b")
	return r.Replace(strings.ToLower(name))
}

// nameBlocked reports whether a usertag is reserved or contains a blocked word.
func nameBlocked(name string) bool {
	extraOnce.Do(func() {
		path := os.Getenv("ARENA_BLOCKED_NAMES")
		if path == "" {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if w := plainName(strings.TrimSpace(sc.Text())); w != "" && !strings.HasPrefix(w, "#") {
				extraNames = append(extraNames, w)
			}
		}
	})
	p := plainName(name)
	if strings.HasPrefix(p, "bot") { // bots are named "Bot ..."; keep players from posing as one
		return true
	}
	for _, list := range [][]string{reservedNames, rudeNames, extraNames} {
		for _, w := range list {
			if strings.Contains(p, w) {
				return true
			}
		}
	}
	return false
}

// Block hides another player: it ends any friendship, and neither can add or invite the other.
func (s *Store) Block(token, usertag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	if a.Name == "" {
		return ErrNoName
	}
	otherKey, ok := s.names[strings.ToLower(strings.TrimSpace(usertag))]
	if !ok {
		return ErrNoUser
	}
	other := s.accounts[otherKey]
	if other == a {
		return ErrSelf
	}
	if !contains(a.Blocked, other.Name) {
		a.Blocked = append(a.Blocked, other.Name)
	}
	a.Friends = without(a.Friends, other.Name)
	other.Friends = without(other.Friends, a.Name)
	return s.saveLocked()
}

// Unblock removes a block. It does not restore a friendship.
func (s *Store) Unblock(token, usertag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	for _, b := range a.Blocked {
		if strings.EqualFold(b, strings.TrimSpace(usertag)) {
			a.Blocked = without(a.Blocked, b)
			return s.saveLocked()
		}
	}
	return ErrNoUser
}

// RemoveFriend ends a friendship on both sides.
func (s *Store) RemoveFriend(token, usertag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return ErrUnknown
	}
	for _, f := range a.Friends {
		if strings.EqualFold(f, strings.TrimSpace(usertag)) {
			a.Friends = without(a.Friends, f)
			if key, ok := s.names[strings.ToLower(f)]; ok {
				s.accounts[key].Friends = without(s.accounts[key].Friends, a.Name)
			}
			return s.saveLocked()
		}
	}
	return ErrNoUser
}

// HasBlocked reports whether the player called owner has blocked the player called other.
func (s *Store) HasBlocked(owner, other string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.names[strings.ToLower(owner)]
	return ok && contains(s.accounts[key].Blocked, other)
}

// Exists reports whether a usertag belongs to a player.
func (s *Store) Exists(usertag string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.names[strings.ToLower(strings.TrimSpace(usertag))]
	if !ok {
		return "", false
	}
	return s.accounts[key].Name, true
}

// Report is one player's complaint about another, kept for the operator to read.
type Report struct {
	Time     string `json:"time"`
	Reporter string `json:"reporter"`
	Target   string `json:"target"`
	Reason   string `json:"reason"`
	Note     string `json:"note,omitempty"`
}

var reportReasons = map[string]bool{"cheating": true, "offensive name": true, "abusive behaviour": true, "other": true}

// reportLog appends reports to a file, one JSON object per line.
type reportLog struct {
	mu   sync.Mutex
	path string // empty keeps reports in memory only
	mem  []Report
}

func (l *reportLog) add(r Report) error {
	r.Time = time.Now().UTC().Format(time.RFC3339)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mem = append(l.mem, r)
	if len(l.mem) > 200 {
		l.mem = l.mem[len(l.mem)-200:]
	}
	if l.path == "" {
		return nil
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, _ := json.Marshal(r)
	_, err = f.Write(append(line, '\n'))
	return err
}
