package main

import (
	"math"
	"testing"
)

func TestArmourHalvesBodyDamageUntilItRunsOut(t *testing.T) {
	m, shooter, target := duel()
	target.ensureAmmo()
	target.credits = 5000
	if !target.buy(armorSlot) || target.armor != armorMax || target.credits != 5000-armorPrice {
		t.Fatalf("buying armour: armour %v credits %d", target.armor, target.credits)
	}
	if target.buy(armorSlot) {
		t.Fatal("full armour should not be sold again")
	}
	shooter.Pitch = pitchTo(shooter, target)
	shoot(m, shooter) // 25 damage: 12.5 to armour, 12.5 to health
	if target.HP != 87.5 || target.armor != 37.5 {
		t.Fatalf("armoured body shot: HP %v armour %v", target.HP, target.armor)
	}
	shooter.Pitch = 0 // a headshot ignores armour
	shoot(m, shooter)
	if target.Alive {
		t.Fatal("armour should not stop a headshot")
	}
}

func TestGrenadeFliesExplodesAndIsLimited(t *testing.T) {
	m, thrower, target := duel()
	m.started = true
	thrower.ensureAmmo()
	thrower.credits = 5000
	for i := 0; i < 3; i++ {
		thrower.buy(grenadeSlot)
	}
	if thrower.grenades != grenadeMax {
		t.Fatalf("carrying %d grenades, want the limit of %d", thrower.grenades, grenadeMax)
	}
	thrower.wantThrow = true
	m.step(1.0 / 30)
	if len(m.grenades) != 1 || thrower.grenades != 1 {
		t.Fatalf("after a throw: %d in the air, %d left", len(m.grenades), thrower.grenades)
	}
	thrower.wantThrow = true // too soon: the cooldown blocks a second throw
	m.step(1.0 / 30)
	if len(m.grenades) != 1 {
		t.Fatal("the throw cooldown was ignored")
	}
	g := m.grenades[0]
	for i := 0; i < 60 && len(m.grenades) > 0; i++ {
		m.step(1.0 / 30)
	}
	if len(m.grenades) != 0 {
		t.Fatal("the grenade never exploded")
	}
	if d := math.Hypot(g.X-thrower.X, g.Y-thrower.Y); d < 120 || d > 260 {
		t.Fatalf("the grenade landed %.0f away", d)
	}
	if target.HP >= 100 {
		t.Fatalf("a player %.0f from the blast took no damage", math.Hypot(g.X-target.X, g.Y-target.Y))
	}
	if thrower.HP != 100 {
		t.Fatal("the thrower was outside the blast and should be unhurt")
	}
}

func TestBlastRespectsCoverTeammatesAndCrates(t *testing.T) {
	m := newArena()
	owner := &Player{ID: 1, Name: "o", Team: 1, X: 0, Y: 0, Alive: true, HP: 100}
	mate := &Player{ID: 2, Name: "m", Team: 1, X: 520, Y: 500, Alive: true, HP: 100}
	open := &Player{ID: 3, Name: "e", Team: 2, X: 500, Y: 540, Alive: true, HP: 100}
	hidden := &Player{ID: 4, Name: "h", Team: 2, X: 570, Y: 500, Alive: true, HP: 100}
	for _, p := range []*Player{owner, mate, open, hidden} {
		m.players[p.ID] = p
	}
	m.obstacles = []Rect{
		{X: 535, Y: 480, W: 20, H: 40},                          // a wall between the blast and "hidden"
		{X: 485, Y: 470, W: 18, H: 18, K: "crate", HP: crateHP}, // a crate close to the blast
	}
	m.explode(&Grenade{X: 500, Y: 500, owner: owner})
	if mate.HP != 100 {
		t.Fatal("a grenade hurt the thrower's teammate")
	}
	if open.HP >= 100 {
		t.Fatal("an enemy in the open was not hurt")
	}
	if hidden.HP != 100 {
		t.Fatal("an enemy behind a wall was hurt")
	}
	if len(m.obstacles) != 1 {
		t.Fatal("the crate in the blast should have broken")
	}
	if owner.hitSeq == 0 {
		t.Fatal("the thrower should get a hit marker")
	}
}

func TestOwnGrenadeCanEliminateYou(t *testing.T) {
	m := newArena()
	owner := &Player{ID: 1, Name: "o", Team: 1, X: 500, Y: 500, Alive: true, HP: 20}
	other := &Player{ID: 2, Name: "e", Team: 2, X: 1500, Y: 1500, Alive: true, HP: 100}
	m.players[1], m.players[2] = owner, other
	m.explode(&Grenade{X: 505, Y: 500, owner: owner})
	if owner.Alive || owner.kills != 0 {
		t.Fatalf("own grenade: alive %v kills %d", owner.Alive, owner.kills)
	}
}
