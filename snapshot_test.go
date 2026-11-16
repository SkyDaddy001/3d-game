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
