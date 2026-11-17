package main

import (
	"math"
	"testing"
)

func duel() (*Match, *Player, *Player) {
	m := newArena()
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 200, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, target
	return m, shooter, target
}

func shoot(m *Match, p *Player) {
	p.bloom = 0
	m.fire(p)
}

// A level shot lands in a standing player's head; aimed at the chest it is a body shot.
func TestAimHeightDecidesHeadOrBody(t *testing.T) {
	m, shooter, target := duel()
	shooter.Pitch = pitchTo(shooter, target) // chest
	shoot(m, shooter)
	if target.HP != 100-weapons[0].Damage || shooter.hitKind != "body" {
		t.Fatalf("chest shot: HP %v kind %q", target.HP, shooter.hitKind)
	}
	target.HP = 100
	shooter.Pitch = 0 // level, at eye height
	shoot(m, shooter)
	if target.Alive || shooter.hitKind != "head" {
		t.Fatalf("level shot at a standing target: alive %v kind %q", target.Alive, shooter.hitKind)
	}
}

// Aiming too high or too low misses.
func TestShotsCanGoOverOrIntoTheGround(t *testing.T) {
	m, shooter, target := duel()
	shooter.Pitch = 0.2 // 40 units up at 200 away: over the head
	shoot(m, shooter)
	shooter.Pitch = -0.5 // meets the ground about 55 units out
	shoot(m, shooter)
	if target.HP != 100 || shooter.hitSeq != 0 {
		t.Fatalf("both shots should miss: HP %v hits %d", target.HP, shooter.hitSeq)
	}
}

// Crouching drops a player under a level shot.
func TestCrouchDucksALevelShot(t *testing.T) {
	m, shooter, target := duel()
	target.crouch = true
	shoot(m, shooter)
	if target.HP != 100 {
		t.Fatalf("a level shot hit a crouching player: HP %v", target.HP)
	}
	shooter.Pitch = pitchTo(shooter, target)
	shoot(m, shooter)
	if target.HP == 100 {
		t.Fatal("aiming down at a crouching player should hit")
	}
}

// A small crate hides a crouching player but not a standing one.
func TestSmallCrateIsCoverOnlyWhenCrouched(t *testing.T) {
	m, shooter, target := duel()
	m.obstacles = []Rect{{X: 170, Y: 41, W: 18, H: 18, K: "crate", HP: 1000}}

	shooter.Pitch = 0
	shoot(m, shooter) // level: over the crate, into the head that shows above it
	if target.Alive {
		t.Fatal("a standing player's head shows over a small crate and should be hit")
	}

	target.Alive, target.HP, target.crouch = true, 100, true
	for _, pitch := range []float64{0, pitchTo(shooter, target), -0.05, -0.1} {
		shooter.Pitch = pitch
		shoot(m, shooter)
	}
	if target.HP != 100 {
		t.Fatalf("a crouching player behind a small crate was hit: HP %v", target.HP)
	}
}

// A large crate is taller than a standing player and blocks every line from the ground.
func TestLargeCrateHidesAStandingPlayer(t *testing.T) {
	m, shooter, target := duel()
	m.obstacles = []Rect{{X: 150, Y: 29, W: 42, H: 42, K: "crate", HP: 1000}}
	for _, pitch := range []float64{0, pitchTo(shooter, target), 0.02, -0.05} {
		shooter.Pitch = pitch
		shoot(m, shooter)
	}
	if target.HP != 100 {
		t.Fatalf("a player behind a large crate was hit: HP %v", target.HP)
	}
}

// A jump leaves the ground, peaks, and lands again; crouching slows movement.
func TestJumpArcAndCrouchSpeed(t *testing.T) {
	m := newArena()
	m.started = true
	p := &Player{ID: 1, Name: "p", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, wantJump: true}
	other := &Player{ID: 2, Name: "o", Team: 2, X: 1500, Y: 1500, Alive: true, HP: 100}
	m.players[1], m.players[2] = p, other
	peak, ticks := 0.0, 0
	for i := 0; i < 60; i++ {
		m.step(1.0 / 30)
		peak = math.Max(peak, p.Z)
		if p.Z > 0 {
			ticks++
		}
	}
	if peak < 10 || peak > 18 || p.Z != 0 || ticks < 15 || ticks > 26 {
		t.Fatalf("jump: peak %.1f, airborne for %d ticks, ends at %.1f", peak, ticks, p.Z)
	}

	x := p.X
	p.DX = 1
	m.step(1)
	walked := p.X - x
	x = p.X
	p.crouch = true
	m.step(1)
	if crouched := p.X - x; math.Abs(crouched-walked*crouchSpeed) > 1 {
		t.Fatalf("crouch speed: walked %.0f, crouched %.0f", walked, crouched)
	}
}

// A player in the air is above a shot aimed at where their legs were.
func TestJumpingMovesTheBody(t *testing.T) {
	m, shooter, target := duel()
	shooter.Pitch = math.Atan2(4-shooter.eye(), 200) // at the shins
	shoot(m, shooter)
	if target.HP == 100 {
		t.Fatal("a shot at the shins should hit a standing player")
	}
	target.HP, target.Z = 100, 12
	shoot(m, shooter)
	if target.HP != 100 {
		t.Fatal("the same shot should pass under a player in mid-jump")
	}
}

// Bots aim up and down, so they still hit crouching players, and they cannot see through tall cover.
func TestBotsHandleHeight(t *testing.T) {
	m := newArena()
	m.started = true
	m.elapsed = botLootPhase + 1
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	enemy := &Player{ID: 2, Name: "e", Team: 2, X: 650, Y: 500, Alive: true, HP: 100, crouch: true}
	m.players[1], m.players[2] = bot, enemy
	for i := 0; i < 30*8 && enemy.HP == 100; i++ {
		enemy.crouch = true
		m.step(1.0 / 30)
	}
	if enemy.HP == 100 {
		t.Fatal("the bot never hit a crouching enemy in the open")
	}
	m.obstacles = []Rect{{X: 560, Y: 479, W: 42, H: 42, K: "crate", HP: 1000}}
	bot.X, bot.Y, enemy.X, enemy.Y = 500, 500, 650, 500
	if m.canSee(bot, enemy) {
		t.Fatal("a bot should not see a crouching player behind a large crate")
	}
}
