package main

import "testing"

func TestPlayersStartWithPistolAndCredits(t *testing.T) {
	p := &Player{}
	p.ensureAmmo()
	if !p.owned[0] || p.owned[1] || p.owned[2] || p.owned[3] || p.owned[4] {
		t.Fatalf("starting weapons: %v, want only the pistol", p.owned)
	}
	if p.credits != startCredits {
		t.Fatalf("starting credits %d, want %d", p.credits, startCredits)
	}
}

func TestBuyWeapon(t *testing.T) {
	p := &Player{}
	p.ensureAmmo()
	if p.buy(3) { // rifle costs more than the starting credits
		t.Fatal("bought a rifle without enough credits")
	}
	if !p.buy(2) {
		t.Fatal("could not buy an SMG with starting credits")
	}
	if !p.owned[1] || p.weapon != 1 || p.mags[1] != weapons[1].Mag {
		t.Fatalf("after buying: owned %v weapon %d mag %d", p.owned, p.weapon, p.mags[1])
	}
	if p.credits != startCredits-weapons[1].Price {
		t.Fatalf("credits after buying: %d", p.credits)
	}
}

func TestBuyAmmoForOwnedWeapon(t *testing.T) {
	p := &Player{}
	p.ensureAmmo()
	if p.buy(1) {
		t.Fatal("a full reserve should not be refilled or charged for")
	}
	p.reserves[0] = 0
	if !p.buy(1) || p.reserves[0] != weapons[0].Reserve || p.credits != startCredits-ammoPrice {
		t.Fatalf("ammo refill: reserve %d credits %d", p.reserves[0], p.credits)
	}
}

func TestCannotSwitchToUnownedWeapon(t *testing.T) {
	m := newArena()
	p := &Player{ID: 1, Name: "p", Team: 1, Alive: true, HP: 100, switchTo: 5}
	m.players[1] = p
	m.updateWeapon(p, 0.03)
	if p.weapon != 0 {
		t.Fatalf("switched to an unowned weapon: slot %d", p.weapon+1)
	}
	p.wantBuy = 2
	m.updateWeapon(p, 0.03)
	if p.weapon != 1 {
		t.Fatal("buying through controls should equip the new weapon")
	}
}

func TestKillPaysCredits(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 25}
	m.players[1], m.players[2] = shooter, target
	m.fire(shooter)
	if shooter.credits != startCredits+killReward {
		t.Fatalf("credits after a kill: %d, want %d", shooter.credits, startCredits+killReward)
	}
}

func TestCashPickup(t *testing.T) {
	m := newArena()
	p := &Player{ID: 1, Name: "p", Team: 1, X: 500, Y: 500, Alive: true, HP: 100}
	m.players[1] = p
	m.loot[7] = &Loot{ID: 7, X: 505, Y: 500, K: "cash"}
	m.loot[8] = &Loot{ID: 8, X: 495, Y: 500} // a medkit: left alone at full health
	m.tryPickup(p)
	if p.credits != startCredits+cashAmount {
		t.Fatalf("credits after cash pickup: %d", p.credits)
	}
	if _, still := m.loot[8]; !still {
		t.Fatal("a medkit should not be used at full health")
	}
}

func TestBotBuysWhatItCanAfford(t *testing.T) {
	p := &Player{}
	p.ensureAmmo()
	if got := botPurchase(p); got != 2 {
		t.Fatalf("with starting credits a bot should buy the SMG (slot 2), got %d", got)
	}
	p.credits = 5000
	if got := botPurchase(p); got != 3 {
		t.Fatalf("with plenty of credits a bot should buy the rifle (slot 3), got %d", got)
	}
}
