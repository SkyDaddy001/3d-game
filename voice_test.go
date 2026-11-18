package main

import (
	"encoding/binary"
	"testing"
	"time"
)

// listen drains a player's voice queue without blocking and returns what arrived.
func listen(p *Player) [][]byte {
	var got [][]byte
	for {
		select {
		case f := <-p.voice:
			got = append(got, f)
		default:
			return got
		}
	}
}

func voiceFrame(mode byte, packet ...byte) []byte {
	return append([]byte{mode}, packet...)
}

// Team voice reaches teammates from anywhere, and never reaches the other side.
func TestVoiceTeamAudience(t *testing.T) {
	m := newArena()
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100, Team: 1, X: 100, Y: 100, voice: make(chan []byte, 8)}
	mate := &Player{ID: 2, Name: "mate", Alive: true, HP: 100, Team: 1, X: 3000, Y: 3000, voice: make(chan []byte, 8)}
	foe := &Player{ID: 3, Name: "foe", Alive: true, HP: 100, Team: 2, X: 110, Y: 100, voice: make(chan []byte, 8)}
	m.players[1], m.players[2], m.players[3] = a, mate, foe

	if !m.Voice(a, voiceFrame(voiceTeam, 9, 9, 9)) {
		t.Fatal("team voice was refused")
	}
	got := listen(mate)
	if len(got) != 1 {
		t.Fatalf("teammate heard %d frames, want 1", len(got))
	}
	if got[0][0] != voiceKind || got[0][1]&flagTeam == 0 || binary.BigEndian.Uint16(got[0][2:4]) != 1 {
		t.Fatalf("bad relay header %v", got[0][:4])
	}
	if len(listen(foe)) != 0 {
		t.Fatal("enemy heard team voice")
	}
}

// Nearby voice reaches players within hearing range; an enemy behind a wall hears it muffled.
func TestVoiceNearbyMuffling(t *testing.T) {
	m := newArena()
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100, Team: 1, X: 500, Y: 500, voice: make(chan []byte, 8)}
	openFoe := &Player{ID: 2, Name: "open", Alive: true, HP: 100, Team: 2, X: 600, Y: 500, voice: make(chan []byte, 8)}
	wallFoe := &Player{ID: 3, Name: "wall", Alive: true, HP: 100, Team: 2, X: 500, Y: 700, voice: make(chan []byte, 8)}
	far := &Player{ID: 4, Name: "far", Alive: true, HP: 100, Team: 2, X: 2500, Y: 2500, voice: make(chan []byte, 8)}
	m.players[1], m.players[2], m.players[3], m.players[4] = a, openFoe, wallFoe, far
	m.obstacles = []Rect{{X: 430, Y: 580, W: 140, H: 40}} // a block between a and wallFoe

	if !m.Voice(a, voiceFrame(voiceNear, 7)) {
		t.Fatal("nearby voice was refused")
	}
	if got := listen(openFoe); len(got) != 1 || got[0][1]&flagMuffle != 0 {
		t.Fatalf("open enemy should hear clearly, got %v", got)
	}
	if got := listen(wallFoe); len(got) != 1 || got[0][1]&flagMuffle == 0 {
		t.Fatalf("enemy behind a wall should hear it muffled, got %v", got)
	}
	if len(listen(far)) != 0 {
		t.Fatal("a player out of hearing range heard nearby voice")
	}
}

// A player can send only voiceRate packets a second; bad modes and empty frames are refused.
func TestVoiceLimitsAndRejects(t *testing.T) {
	m := newArena()
	a := &Player{ID: 1, Name: "a", Alive: true, HP: 100, Team: 1, voice: make(chan []byte, 256)}
	m.players[1] = a
	accepted := 0
	for i := 0; i < voiceRate*2; i++ {
		if m.Voice(a, voiceFrame(voiceNear, 1)) {
			accepted++
		}
	}
	if accepted != voiceRate {
		t.Fatalf("accepted %d packets in one second, want %d", accepted, voiceRate)
	}
	a.voiceWindow = time.Now().Add(-2 * time.Second) // a new second starts
	if !m.Voice(a, voiceFrame(voiceNear, 1)) {
		t.Fatal("a packet in a new second was refused")
	}
	if m.Voice(a, []byte{5, 1}) || m.Voice(a, []byte{voiceNear}) {
		t.Fatal("a bad mode or an empty packet was accepted")
	}
}
