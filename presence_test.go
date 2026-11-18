package main

import (
	"strings"
	"testing"
)

// A friend in a public waiting room shows as joinable; one in a private room does not.
func TestFriendPresence(t *testing.T) {
	_, s := newTestServer(t)
	pub := s.newMatch("pubroom")
	pub.Join("alice")
	priv := s.newMatch("privroom")
	priv.Private = true
	priv.Join("bob")
	s.mu.Lock()
	s.matches["pubroom"], s.matches["privroom"] = pub, priv
	s.mu.Unlock()

	if got := s.roomOf("ALICE"); got != pub {
		t.Fatalf("alice found in %v, want the public room", got)
	}
	if line := s.presenceLine("alice"); !strings.Contains(line, "/matches/pubroom/join") {
		t.Fatalf("alice should be joinable, got %s", line)
	}
	if line := s.presenceLine("bob"); strings.Contains(line, "join") || !strings.Contains(line, "private room") {
		t.Fatalf("bob is in a private room, got %s", line)
	}
	if line := s.presenceLine("carol"); !strings.Contains(line, "offline") {
		t.Fatalf("carol is not in any room, got %s", line)
	}
}
