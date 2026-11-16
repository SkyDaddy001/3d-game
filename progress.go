package main

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Trophies, the daily reward, and avatar looks.

const dailyXP = 150

var (
	ErrDailyClaimed = errors.New("today's reward is already claimed, come back tomorrow")
	ErrLookInvalid  = errors.New("that look is not available")
	ErrLookLocked   = errors.New("that item unlocks at a higher level")
)

// ComputeTrophies returns the trophy change for one match: a gain for placing well, a loss for
// placing badly or leaving, and a little extra per elimination.
func ComputeTrophies(place, kills, players int) int {
	if players < 2 {
		return 0
	}
	d := kills * 2
	switch {
	case place == 1:
		d += 30
	case place == 2:
		d += 18
	case place == 3:
		d += 10
	case place > 0 && place <= players/2:
		d += 4
	case place == 0: // left before being placed
		d -= 10
	default:
		d -= 8
	}
	return d
}

// Look is a player's chosen avatar. Each field indexes one of the option lists below.
type Look struct {
	Custom bool `json:"custom,omitempty"` // false until the player picks a look
	Hair   int  `json:"hair"`
	Skin   int  `json:"skin"`
	Shirt  int  `json:"shirt"`
	Hat    int  `json:"hat"`
}

// LookOption is one choice in the avatar editor. Level is the level that unlocks it.
type LookOption struct {
	Name  string
	Color string // CSS colour for the lobby preview; empty for hats
	Level int
}

// The client keeps the same lists in the same order (static/game3d.js).
var (
	hairOptions = []LookOption{
		{"Dark brown", "#3b2a20", 1}, {"Chestnut", "#6b3f22", 1}, {"Blonde", "#d9a13a", 1},
		{"Red", "#b8432e", 1}, {"Black", "#22232b", 1}, {"Violet", "#7a5cc9", 1},
	}
	skinOptions = []LookOption{
		{"Light", "#ffd2ae", 1}, {"Tan", "#f2b48a", 1}, {"Brown", "#c98a5e", 1}, {"Deep", "#8d5a3b", 1},
	}
	shirtOptions = []LookOption{
		{"Red", "#e5483f", 1}, {"Orange", "#f08a2e", 1}, {"Purple", "#b04fd0", 1}, {"Pink", "#e04f8a", 1},
		{"Blue", "#3f7fe0", 1}, {"Gold", "#f5c518", 3}, {"Midnight", "#1d2233", 5}, {"Neon", "#39ffb0", 8},
	}
	hatOptions = []LookOption{
		{"None", "", 1}, {"Cap", "", 2}, {"Headband", "", 4}, {"Crown", "", 6}, {"Halo", "", 10},
	}
)

// Code is the compact form sent to game clients, for example "2.0.5.3". Empty means no custom look.
func (l Look) Code() string {
	if !l.Custom {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", l.Hair, l.Skin, l.Shirt, l.Hat)
}

// checkLook reports whether a look is valid and unlocked at the given level.
func checkLook(l Look, level int) error {
	for _, c := range []struct {
		i    int
		opts []LookOption
	}{{l.Hair, hairOptions}, {l.Skin, skinOptions}, {l.Shirt, shirtOptions}, {l.Hat, hatOptions}} {
		if c.i < 0 || c.i >= len(c.opts) {
			return ErrLookInvalid
		}
		if c.opts[c.i].Level > level {
			return ErrLookLocked
		}
	}
	return nil
}

// SetLook saves a device's avatar look, if every item is unlocked at the account's level.
func (s *Store) SetLook(token string, l Look) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.keyLocked(token)
	a, ok := s.accounts[h]
	if !ok {
		return ErrUnknown
	}
	if err := checkLook(l, LevelFor(a.XP)); err != nil {
		return err
	}
	l.Custom = true
	a.Look = l
	return s.saveLocked()
}

// ClaimDaily gives the once-a-day XP bonus. Days are counted in UTC.
func (s *Store) ClaimDaily(token string, now time.Time) (int, error) {
	day := now.Unix() / 86400
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.keyLocked(token)
	a, ok := s.accounts[h]
	if !ok {
		return 0, ErrUnknown
	}
	if !a.Verified {
		return 0, ErrNotVerified
	}
	if a.DailyDay == day {
		return 0, ErrDailyClaimed
	}
	a.DailyDay = day
	a.XP += dailyXP
	return dailyXP, s.saveLocked()
}

// DailyReady reports whether the account can claim today's reward.
func (a Account) DailyReady(now time.Time) bool {
	return a.Verified && a.DailyDay != now.Unix()/86400
}

// Top returns the n named players with the most trophies, best first.
func (s *Store) Top(n int) []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, 0, len(s.names))
	for _, h := range s.names {
		if a := s.accounts[h]; a.Verified {
			out = append(out, copyAccount(a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Trophies != out[j].Trophies {
			return out[i].Trophies > out[j].Trophies
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
