package main

import (
	"encoding/json"
	"testing"
)

func readSnap(t *testing.T, p *Player) Snapshot {
	t.Helper()
	select {
	case b := <-p.send:
		var s Snapshot
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		return s
	default:
		t.Fatal("no snapshot was sent")
		return Snapshot{}
	}
}

// Building and pickup lists go out once, then only when they change.
func TestWorldListsSentOnlyWhenChanged(t *testing.T) {
	m := newMatchState("slim01")
	p, _ := m.Join("slim_player")
	m.mu.Lock()
	defer m.mu.Unlock()

	m.broadcast()
	first := readSnap(t, p)
	if first.Obstacles == nil || first.Loot == nil {
		t.Fatal("the first snapshot must carry the obstacle and pickup lists")
	}
	m.broadcast()
	second := readSnap(t, p)
	if second.Obstacles != nil || second.Loot != nil {
		t.Fatal("an unchanged world should not be sent again")
	}

	// A pickup appears next to the player: only the pickup list is sent again.
	m.loot[9999] = &Loot{ID: 9999, X: p.X, Y: p.Y}
	m.broadcast()
	third := readSnap(t, p)
	if third.Loot == nil || third.Obstacles != nil {
		t.Fatalf("after a new pickup: loot sent %v, obstacles sent %v", third.Loot != nil, third.Obstacles != nil)
	}
}

// If a snapshot is dropped because the player's connection is slow, the lists are sent again.
func TestDroppedSnapshotResendsWorld(t *testing.T) {
	m := newMatchState("slim02")
	p, _ := m.Join("slow_player")
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(p.send) < cap(p.send) { // fill the queue so the next send is dropped
		p.send <- []byte("{}")
	}
	m.broadcast() // dropped
	for len(p.send) > 0 {
		<-p.send
	}
	m.broadcast()
	if s := readSnap(t, p); s.Obstacles == nil || s.Loot == nil {
		t.Fatal("the lists were lost with the dropped snapshot")
	}
}

// Players hear and see other players' shots nearby, once, and not their own.
func TestNearbyShotsAreSent(t *testing.T) {
	m := newMatchState("shots1")
	m.obstacles, m.lakes, m.bushes = nil, nil, nil
	a, _ := m.Join("shooter_x")
	b, _ := m.Join("listener_y")
	c, _ := m.Join("far_away_z")
	m.mu.Lock()
	defer m.mu.Unlock()
	a.Team, b.Team, c.Team = 1, 2, 3
	a.X, a.Y, b.X, b.Y, c.X, c.Y = 500, 500, 700, 500, 1900, 1900
	m.fire(a)
	m.broadcast()
	if got := readSnap(t, b).Shots; len(got) != 1 || got[0].Weapon != "Pistol" || got[0].X != 500 {
		t.Fatalf("listener got %+v", got)
	}
	if len(readSnap(t, a).Shots) != 0 {
		t.Fatal("the shooter was sent their own shot")
	}
	if len(readSnap(t, c).Shots) != 0 {
		t.Fatal("a player out of earshot was sent the shot")
	}
	m.broadcast()
	if len(readSnap(t, b).Shots) != 0 {
		t.Fatal("the shot was sent twice")
	}
}
