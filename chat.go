package main

import (
	"math"
	"time"
)

// Quick chat: players pick from a fixed wheel of preset messages. There is no free text, so
// there is nothing to moderate. A message goes to the sender's team, or to everyone nearby.

// quickChats are the presets, by index. The client shows the same list; the server only checks the index.
var quickChats = []string{
	"Enemy spotted",
	"Need help",
	"On my way",
	"Hold this position",
	"Nice shot",
	"Going for loot",
	"Cover me",
	"Thanks",
}

const chatCooldown = 2 * time.Second // one message per player this often

// ChatView is one preset message as a recipient sees it.
type ChatView struct {
	From string  `json:"from"`
	Team bool    `json:"team"` // true for team messages, false for proximity messages
	Text string  `json:"text"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// chatMsg is what the match keeps until it has been broadcast.
type chatMsg struct {
	by   *Player
	team bool
	text string
}

// Chat queues a preset message from p. team picks the audience: true for teammates only,
// false for everyone within hearing range. It reports whether the message was accepted.
func (m *Match) Chat(p *Player, index int, team bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < 0 || index >= len(quickChats) || p.gone || p.bot != nil || !p.Alive {
		return false
	}
	now := time.Now()
	if !p.chatAt.IsZero() && now.Sub(p.chatAt) < chatCooldown {
		return false
	}
	p.chatAt = now
	m.chats = append(m.chats, chatMsg{by: p, team: team, text: quickChats[index]})
	return true
}

// chatsFor returns the messages this player should hear in the next snapshot.
func (m *Match) chatsFor(me *Player) []ChatView {
	var out []ChatView
	for _, c := range m.chats {
		if c.by == me {
			continue
		}
		if c.team {
			if c.by.Team == 0 || c.by.Team != me.Team {
				continue
			}
		} else if math.Hypot(c.by.X-me.X, c.by.Y-me.Y) > hearRange {
			continue
		}
		out = append(out, ChatView{From: c.by.Name, Team: c.team, Text: c.text, X: c.by.X, Y: c.by.Y})
	}
	return out
}
