package main

import (
	"fmt"
	"html"
	"strings"
)

// Friend presence: a friend counts as online while they are in a room. Players in a public
// waiting room can be joined from the friends list; private rooms show as busy and need the number.

// HasPlayer reports whether a real (not bot) player with this usertag is in the room.
func (m *Match) HasPlayer(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.players {
		if !p.gone && p.bot == nil && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// roomOf finds the room a player with this usertag is in, if any.
func (s *Server) roomOf(name string) *Match {
	s.mu.Lock()
	ms := make([]*Match, 0, len(s.matches))
	for _, m := range s.matches {
		ms = append(ms, m)
	}
	s.mu.Unlock()
	for _, m := range ms {
		if !m.Closed() && m.HasPlayer(name) {
			return m
		}
	}
	return nil
}

// presenceLine describes where a friend is: a joinable room, a busy one, or offline.
func (s *Server) presenceLine(name string) string {
	m := s.roomOf(name)
	if m == nil {
		return `<span class="presence off">offline</span>`
	}
	info := m.Info()
	switch {
	case info.Private:
		return `<span class="presence busy">in a private room</span>`
	case info.Started:
		return `<span class="presence busy">in a match</span>`
	case info.Players >= info.Max:
		return `<span class="presence busy">in a full room</span>`
	}
	return fmt.Sprintf(`<span class="presence on">in room %s</span> <button class="mini" hx-post="/matches/%s/join" hx-swap="none">Join</button>`,
		html.EscapeString(info.ID), html.EscapeString(info.ID))
}
