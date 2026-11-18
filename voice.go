package main

import (
	"encoding/binary"
	"math"
	"time"
)

// Voice relays short Opus packets from a talking player to the people who should hear them.
// The server only forwards: it never decodes audio, and it keeps no recordings.
//
// A frame from the client is one byte of mode followed by one Opus packet.
// Mode voiceTeam sends it to teammates from anywhere; voiceNear to players within hearing range.
// A frame to a listener is: kind (voiceKind), flags, the sender's ID as two bytes, then the packet.
// The muffled flag is set when a nearby enemy is behind cover, so the client can dull the sound.

const (
	voiceTeam  byte = 0
	voiceNear  byte = 1
	voiceKind  byte = 1
	flagTeam   byte = 1 << 0
	flagMuffle byte = 1 << 1

	voiceMaxPacket  = 400 // bytes; Opus at speech quality is far below this
	voiceRate       = 60  // packets per second accepted from one player
	voiceQueueDepth = 64  // per listener; a slow listener drops voice rather than blocking the match
)

// voiceLimit lets a player send at most voiceRate packets a second. It reports whether this packet fits.
func (p *Player) voiceLimit(now time.Time) bool {
	if now.Sub(p.voiceWindow) >= time.Second {
		p.voiceWindow = now
		p.voiceCount = 0
	}
	if p.voiceCount >= voiceRate {
		return false
	}
	p.voiceCount++
	return true
}

// Voice forwards one talking frame from p. It reports whether the frame was accepted for relay.
func (m *Match) Voice(p *Player, frame []byte) bool {
	if len(frame) < 2 || len(frame) > voiceMaxPacket+1 {
		return false
	}
	mode := frame[0]
	if mode != voiceTeam && mode != voiceNear {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.gone || p.bot != nil || !p.voiceLimit(time.Now()) {
		return false
	}
	if mode == voiceTeam && p.Team == 0 {
		return false
	}
	packet := frame[1:]
	for _, q := range m.players {
		if q == p || q.gone || q.bot != nil {
			continue
		}
		var flags byte
		if mode == voiceTeam {
			if q.Team != p.Team {
				continue
			}
			flags |= flagTeam
		} else {
			if math.Hypot(p.X-q.X, p.Y-q.Y) > hearRange {
				continue
			}
			// Teammates are always clear; enemies behind cover are muffled.
			if q.Team != p.Team && !m.canSee(q, p) {
				flags |= flagMuffle
			}
		}
		out := make([]byte, 4+len(packet))
		out[0], out[1] = voiceKind, flags
		binary.BigEndian.PutUint16(out[2:4], uint16(p.ID))
		copy(out[4:], packet)
		select {
		case q.voice <- out:
		default: // queue full: drop this packet, voice is allowed to lose frames
		}
	}
	return true
}
