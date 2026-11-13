package main

import (
	"math"
	"testing"
)

// newArena makes a match with a zone that covers everything, so zone damage never interferes.
func newArena() *Match {
	return &Match{
		players: map[int]*Player{},
		zoneX:   1000, zoneY: 1000, zoneR: 5000,
		zoneLeft: 1000,
		loot:     map[int]*Loot{},
	}
}

// A teammate holding revive beside a downed player brings them back after reviveTime.
func TestTeammateRevivesDownedPlayer(t *testing.T) {
	m := newArena()
	downed := &Player{ID: 1, Name: "down", Team: 1, X: 500, Y: 500, Alive: true, downed: true, bleed: downedTime}
	medic := &Player{ID: 2, Name: "medic", Team: 1, X: 510, Y: 500, Alive: true, HP: 100, wantRevive: true}
	m.players[1], m.players[2] = downed, medic

	for elapsed := 0.0; elapsed < reviveTime+0.5; elapsed += 0.1 {
		medic.wantRevive = true
		m.updateRevives(0.1)
	}
	if downed.downed {
		t.Fatal("holding revive for reviveTime should revive the teammate")
	}
	if downed.HP != revivedHP || !downed.Alive {
		t.Fatalf("revived player: HP %v alive %v, want HP %v alive", downed.HP, downed.Alive, revivedHP)
	}
}

// Revive progress resets if the medic lets go, so it must be held continuously.
func TestReviveNeedsContinuousHold(t *testing.T) {
	m := newArena()
	downed := &Player{ID: 1, Name: "down", Team: 1, X: 500, Y: 500, Alive: true, downed: true, bleed: downedTime}
	medic := &Player{ID: 2, Name: "medic", Team: 1, X: 510, Y: 500, Alive: true, HP: 100}
	m.players[1], m.players[2] = downed, medic

	medic.wantRevive = true
	m.updateRevives(reviveTime / 2)
	m.updateRevives(0.1) // medic not holding revive this tick
	medic.wantRevive = true
	m.updateRevives(reviveTime / 2)
	if !downed.downed {
		t.Fatal("releasing revive should reset progress, so half a hold must not revive")
	}
}

// A downed player who is not revived in time bleeds out and is eliminated.
func TestBleedoutEliminates(t *testing.T) {
	m := newArena()
	downed := &Player{ID: 1, Name: "down", Team: 1, X: 500, Y: 500, Alive: true, downed: true, bleed: downedTime}
	other := &Player{ID: 2, Name: "other", Team: 2, X: 100, Y: 100, Alive: true, HP: 100}
	m.players[1], m.players[2] = downed, other

	m.step(downedTime + 0.5)
	if downed.Alive {
		t.Fatal("bleed-out should eliminate the downed player")
	}
}

// Players recover health only after a quiet spell with no damage.
func TestHealthRegeneratesAfterDelay(t *testing.T) {
	m := newArena()
	p := &Player{ID: 1, Name: "p", Team: 1, X: 500, Y: 500, Alive: true, HP: 50, hurtCool: regenDelay}
	m.players[1] = p

	for i := 0; i < 4; i++ {
		m.step(1)
	}
	if p.HP != 50 {
		t.Fatalf("health should not regenerate while the delay runs, got %v", p.HP)
	}
	m.step(1) // the delay ends on this tick, so recovery starts
	if math.Abs(p.HP-(50+regenRate*1)) > 1e-6 {
		t.Fatalf("after the delay, health should regenerate at %v per second, got HP %v", regenRate, p.HP)
	}
}

// A bullet hit resets the regeneration timer.
func TestDamageResetsRegeneration(t *testing.T) {
	m := newArena()
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = shooter, target

	m.fire(shooter)
	if target.hurtCool != regenDelay {
		t.Fatalf("a hit should start the regen delay, got %v", target.hurtCool)
	}
}
