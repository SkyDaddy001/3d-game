package main

import (
	"testing"
	"time"
)

// A team message reaches teammates only; a proximity message reaches nearby players of either team.
func TestQuickChatAudience(t *testing.T) {
	m := newArena()
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100, Team: 1, X: 500, Y: 500}
	mate := &Player{ID: 2, Name: "mate", Alive: true, HP: 100, Team: 1, X: 2000, Y: 2000}
	foeNear := &Player{ID: 3, Name: "foe", Alive: true, HP: 100, Team: 2, X: 600, Y: 500}
	foeFar := &Player{ID: 4, Name: "far", Alive: true, HP: 100, Team: 2, X: 1900, Y: 2000}
	m.players[1], m.players[2], m.players[3], m.players[4] = a, mate, foeNear, foeFar

	if !m.Chat(a, 1, true) {
		t.Fatal("team chat was refused")
	}
	if got := m.chatsFor(mate); len(got) != 1 || !got[0].Team {
		t.Fatalf("teammate should hear team chat, got %v", got)
	}
	if got := m.chatsFor(foeNear); len(got) != 0 {
		t.Fatalf("enemy should not hear team chat, got %v", got)
	}
	m.chats = m.chats[:0]

	a.chatAt = time.Time{} // the cooldown starts again for the second message
	// The cooldown blocks an immediate second message.
	if !m.Chat(a, 0, false) {
		t.Fatal("proximity chat was refused")
	}
	if m.Chat(a, 0, false) {
		t.Fatal("second message inside the cooldown was accepted")
	}
	if got := m.chatsFor(foeNear); len(got) != 1 || got[0].Text != quickChats[0] {
		t.Fatalf("nearby enemy should hear proximity chat, got %v", got)
	}
	if got := m.chatsFor(foeFar); len(got) != 0 {
		t.Fatalf("distant enemy should not hear proximity chat, got %v", got)
	}
}

// An index outside the preset list is refused.
func TestQuickChatRejectsBadIndex(t *testing.T) {
	m := newArena()
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100, Team: 1}
	m.players[1] = a
	if m.Chat(a, len(quickChats), true) || m.Chat(a, -1, true) {
		t.Fatal("out-of-range preset was accepted")
	}
}
