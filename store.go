package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Accounts are device-bound, like a village in a mobile strategy game: the first visit creates
// an account and a secret cookie, and that cookie is the login. Only a hash of the cookie is saved.
//
// Storage is a JSON file written atomically, using only the standard library so the build has no
// C dependency. It is fine for one server process; a database can replace it later.

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

var (
	ErrNameInvalid = errors.New("names are 3-16 letters, digits or underscores")
	ErrNameTaken   = errors.New("that name is taken")
	ErrHasName     = errors.New("this device already has a name")
	ErrUnknown     = errors.New("unknown device")
)

// Account is a player's saved progression.
type Account struct {
	TokenHash    string   `json:"token_hash"`
	Name         string   `json:"name,omitempty"`
	EmailHash    string   `json:"email_hash,omitempty"`
	Verified     bool     `json:"verified,omitempty"`
	PendingEmail string   `json:"pending_email,omitempty"`
	OTPHash      string   `json:"otp_hash,omitempty"`
	OTPSentAt    int64    `json:"otp_sent_at,omitempty"`
	OTPExpires   int64    `json:"otp_expires,omitempty"`
	OTPTries     int      `json:"otp_tries,omitempty"`
	Friends      []string `json:"friends,omitempty"`
	XP           int      `json:"xp"`
	Matches      int      `json:"matches"`
	Wins         int      `json:"wins"`
	Kills        int      `json:"kills"`
	Created      int64    `json:"created"`
	Trophies     int      `json:"trophies"`
	DailyDay     int64    `json:"daily_day,omitempty"` // UTC day number of the last daily reward
	Look         Look     `json:"look"`
	Devices      []string `json:"devices,omitempty"` // token hashes of other devices signed in to this account
	Blocked      []string `json:"blocked,omitempty"` // usertags this player has blocked
	LoginTarget  string   `json:"-"`                 // on a new device: the account a sign-in code was sent for
}

// Store holds all accounts. Methods return copies, so callers never share state with it.
type Store struct {
	mu       sync.Mutex
	path     string              // empty means in-memory only
	accounts map[string]*Account // keyed by token hash
	names    map[string]string   // lowercase usertag -> token hash
	emails   map[string]string   // email hash -> token hash, verified accounts only
	devices  map[string]string   // token hash of an extra device -> key of its account
}

// OpenStore loads accounts from path. A missing file means a new, empty store.
func OpenStore(path string) (*Store, error) {
	s := &Store{
		path:     path,
		accounts: map[string]*Account{},
		names:    map[string]string{},
		emails:   map[string]string{},
		devices:  map[string]string{},
	}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []*Account
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	for _, a := range list {
		s.accounts[a.TokenHash] = a
		if a.Name != "" {
			s.names[strings.ToLower(a.Name)] = a.TokenHash
		}
		if a.Verified {
			s.emails[a.EmailHash] = a.TokenHash
		}
		for _, d := range a.Devices {
			s.devices[d] = a.TokenHash
		}
	}
	return s, nil
}

// saveLocked writes the whole store to a temporary file, then renames it into place.
// The caller must hold s.mu.
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	// Devices that have not chosen a usertag yet are kept in memory only, so visitors who never
	// sign up cannot grow the file.
	list := make([]*Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		if a.Name != "" {
			list = append(list, a)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewDevice creates an account for a new device. It returns the secret token for the cookie.
func (s *Store) NewDevice() (string, Account, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", Account{}, err
	}
	token := hex.EncodeToString(b)
	a := &Account{TokenHash: hashToken(token), Created: time.Now().Unix()}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Not saved yet: a device is written to disk once it has a usertag.
	s.accounts[a.TokenHash] = a
	return token, copyAccount(a), nil
}

// ByToken returns the account for a device token.
func (s *Store) ByToken(token string) (Account, bool) {
	if token == "" {
		return Account{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[s.keyLocked(token)]
	if !ok {
		return Account{}, false
	}
	return copyAccount(a), true
}

// copyAccount returns a copy that shares no slices with the stored account.
func copyAccount(a *Account) Account {
	c := *a
	c.Friends = append([]string(nil), a.Friends...)
	c.Devices = append([]string(nil), a.Devices...)
	c.Blocked = append([]string(nil), a.Blocked...)
	return c
}

// SetName gives a device's account its one and only name. Names are unique, ignoring case.
func (s *Store) SetName(token, name string) (Account, error) {
	if !nameRE.MatchString(name) {
		return Account{}, ErrNameInvalid
	}
	if nameBlocked(name) {
		return Account{}, ErrNameBlocked
	}
	key := strings.ToLower(name)

	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.keyLocked(token)
	a, ok := s.accounts[h]
	if !ok {
		return Account{}, ErrUnknown
	}
	if a.Name != "" {
		return copyAccount(a), ErrHasName
	}
	if owner, taken := s.names[key]; taken && owner != h {
		return Account{}, ErrNameTaken
	}
	a.Name = name
	s.names[key] = h
	if err := s.saveLocked(); err != nil {
		a.Name = ""
		delete(s.names, key)
		return Account{}, err
	}
	return copyAccount(a), nil
}

// RecordMatch adds one finished match to a player's progression, found by name.
func (s *Store) RecordMatch(name string, r MatchResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.names[strings.ToLower(name)]
	if !ok {
		return ErrUnknown
	}
	a := s.accounts[h]
	a.XP += r.XP
	a.Matches++
	a.Kills += r.Kills
	if r.Won {
		a.Wins++
	}
	a.Trophies = max(0, a.Trophies+r.Trophies)
	return s.saveLocked()
}

// MatchResult is one player's outcome for a finished match.
type MatchResult struct {
	XP       int
	Kills    int
	Won      bool
	Trophies int // change in trophies; may be negative
}

// LevelFor returns the level for a total XP amount. Level L starts at 100*(L-1)^2 XP,
// so each level takes a little longer than the one before.
func LevelFor(xp int) int {
	return int(math.Sqrt(float64(xp)/100)) + 1
}

// LevelProgress returns the level, the XP earned into it, and the XP needed to reach the next.
func LevelProgress(xp int) (level, into, need int) {
	level = LevelFor(xp)
	start := 100 * (level - 1) * (level - 1)
	next := 100 * level * level
	return level, xp - start, next - start
}

// ComputeXP rewards kills, placement, and damage. Damage is worth a little, so a player who
// fights and loses still progresses.
func ComputeXP(kills, place, damage int) int {
	xp := kills*100 + damage/25
	switch {
	case place == 1:
		xp += 500
	case place == 2:
		xp += 300
	case place == 3:
		xp += 180
	case place > 0 && place <= 10:
		xp += 60
	}
	return xp
}
