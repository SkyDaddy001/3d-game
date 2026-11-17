package main

import (
	"math"
	"testing"
)

// A new map has lakes and bushes, nothing solid stands in a lake, and nobody spawns in water.
func TestMapTerrain(t *testing.T) {
	m := newMatchState("terra1")
	if len(m.lakes) == 0 || len(m.bushes) < 20 {
		t.Fatalf("%d lakes and %d bushes", len(m.lakes), len(m.bushes))
	}
	for _, o := range m.obstacles {
		if m.inWater(o.X+o.W/2, o.Y+o.H/2) {
			t.Fatalf("a %q obstacle stands in a lake", o.K)
		}
	}
	for _, l := range m.lakes {
		if math.Hypot(l.X-worldSize/2, l.Y-worldSize/2) < 520+l.R {
			t.Fatal("a lake reaches into the rounds arena")
		}
	}
	for i := 0; i < 200; i++ {
		if x, y := m.spawnPoint(); m.inWater(x, y) {
			t.Fatal("a spawn point is in a lake")
		}
	}
}

// In water a player moves at half speed, cannot shoot, and is a low target.
func TestSwimming(t *testing.T) {
	m, shooter, target := duel()
	m.started = true
	m.lakes = []Circle{{X: 0, Y: 50, R: 60}} // the shooter is in the lake
	shooter.DX, shooter.wantShoot = 0, true
	m.step(1.0 / 30)
	if !shooter.swim || shooter.mags[0] != weapons[0].Mag {
		t.Fatalf("swimming: swim %v, magazine %d", shooter.swim, shooter.mags[0])
	}
	shooter.DX = 1
	x := shooter.X
	m.step(0.2)
	if moved := shooter.X - x; math.Abs(moved-moveSpeed*swimSpeed*0.2) > 0.5 {
		t.Fatalf("swam %.1f in 0.2s", moved)
	}
	// A level shot from the bank passes over a swimmer; aimed at the water line it hits.
	shooter.X, shooter.swim = 0, true
	target.Pitch = 0
	target.Aim = math.Pi
	shoot(m, target)
	if shooter.HP != 100 {
		t.Fatal("a level shot hit a swimmer")
	}
	target.Pitch = pitchTo(target, shooter)
	shoot(m, target)
	if shooter.HP == 100 || target.hitKind != "body" {
		t.Fatalf("a shot at the water line: HP %v kind %q", shooter.HP, target.hitKind)
	}
}

// A bush hides a quiet player from a distance, but not up close, and not after they fire.
func TestBushConcealment(t *testing.T) {
	m := newMatchState("bush01")
	m.obstacles, m.lakes = nil, nil
	m.bushes = []Circle{{X: 500, Y: 500, R: 25}}
	hider, _ := m.Join("hider_1")
	seeker, _ := m.Join("seeker_1")
	m.mu.Lock()
	defer m.mu.Unlock()
	hider.Team, seeker.Team = 1, 2
	hider.X, hider.Y, hider.quiet = 500, 500, 5
	seeker.X, seeker.Y = 700, 500

	sees := func() bool {
		for len(seeker.send) > 0 {
			<-seeker.send
		}
		for len(hider.send) > 0 {
			<-hider.send
		}
		m.broadcast()
		for _, p := range readSnap(t, seeker).Players {
			if p.Name == "hider_1" {
				return true
			}
		}
		return false
	}
	if sees() {
		t.Fatal("a quiet player in a bush was visible from 200 away")
	}
	if !readSnap(t, hider).Me.Hidden {
		t.Fatal("the hider should be told they are hidden")
	}
	seeker.X = 540
	if !sees() {
		t.Fatal("a player in a bush should be visible from 40 away")
	}
	seeker.X = 700
	hider.quiet = 0 // just fired
	if !sees() {
		t.Fatal("firing should give away a player in a bush")
	}
	hider.quiet, hider.X = 5, 560 // stepped out of the bush
	if !sees() {
		t.Fatal("a player outside the bush should be visible")
	}
}

// The fixed terrain is sent once.
func TestWorldSentOnce(t *testing.T) {
	m := newMatchState("world1")
	p, _ := m.Join("world_p")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcast()
	if w := readSnap(t, p).World; w == nil || len(w.Lakes) == 0 {
		t.Fatal("the first snapshot should carry the lakes and bushes")
	}
	m.broadcast()
	if readSnap(t, p).World != nil {
		t.Fatal("the terrain was sent twice")
	}
}
