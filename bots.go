package main

import (
	"math"
	"math/rand"
)

// Bots fill a room when a match starts, so a match is playable with any number of real players.
// A bot is an ordinary Player driven by thinkBots instead of a network connection; it follows
// every rule a human does (same weapons, spread, reloads, zone damage, and line of sight).

const (
	botSight      = 480.0 // how far a bot notices enemies
	botAimError   = 0.09  // radians of aim wobble (standard deviation)
	botReact      = 0.6   // seconds an enemy must stay visible before a bot opens fire
	botReviveSeek = 320.0 // how far a bot will go to revive a teammate
	botGrace      = 5.0   // seconds after the start before bots open fire, so nobody dies on arrival
	botLootPhase  = 35.0  // opening seconds spent gathering cash and buying rather than hunting
	botEarlySight = 170.0 // during the loot phase a bot only fights what comes this close
	botCashSeek   = 650.0 // how far a bot will walk for a coin
)

// Bot names contain a space, which usertags cannot, so a bot never shares a name with a player.
var botNames = []string{
	"Bot Rex", "Bot Luna", "Bot Pixel", "Bot Mango", "Bot Nova", "Bot Ziggy", "Bot Echo", "Bot Maple",
	"Bot Comet", "Bot Tango", "Bot Pebble", "Bot Fizz", "Bot Sunny", "Bot Dash", "Bot Koda", "Bot Juno",
}

// botState is a bot's short-term memory.
type botState struct {
	seen       float64 // seconds the current target has been in view
	trigger    float64 // cycles the trigger so semi-automatic weapons keep firing
	strafeDir  float64 // +1 or -1: side-step direction in a fight
	strafeLeft float64
	wanderA    float64 // heading while roaming
	wanderLeft float64
	detourLeft float64 // seconds left walking around something that blocked the way
	lastX      float64
	lastY      float64
	stuck      float64
}

// addBotsLocked adds bots until the room holds size players. The caller must hold m.mu.
func (m *Match) addBotsLocked(size int) {
	used := map[string]bool{}
	for _, p := range m.players {
		used[p.Name] = true
	}
	for i := 0; len(m.players) < size && len(m.players) < maxPlayers && i < len(botNames); i++ {
		name := botNames[(i+int(m.nextID))%len(botNames)]
		if used[name] {
			continue
		}
		used[name] = true
		x, y := m.spreadSpawn()
		id := m.newID()
		m.players[id] = &Player{
			ID: id, Name: name, HP: 100, Alive: true, X: x, Y: y,
			look: Look{Custom: true, Hair: rand.Intn(len(hairOptions)), Skin: rand.Intn(len(skinOptions)), Shirt: rand.Intn(5), Hat: rand.Intn(2)}.Code(),
			bot:  &botState{strafeDir: 1, wanderA: rand.Float64() * 2 * math.Pi, lastX: x, lastY: y},
		}
	}
}

// clearLine reports whether no obstacle stands between two points.
func (m *Match) clearLine(x1, y1, x2, y2 float64) bool {
	dx, dy := x2-x1, y2-y1
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		return true
	}
	dx, dy = dx/dist, dy/dist
	for _, r := range m.obstacles {
		if t, ok := rayRect(x1, y1, dx, dy, r); ok && t < dist {
			return false
		}
	}
	return true
}

// thinkBots sets each bot's controls for this tick. It runs before movement and shooting.
func (m *Match) thinkBots(dt float64) {
	for _, p := range m.players {
		b := p.bot
		if b == nil || !p.Alive || p.downed {
			continue
		}
		p.ensureAmmo()

		// Notice when something blocks the way, and walk around it for a moment.
		moved := math.Hypot(p.X-b.lastX, p.Y-b.lastY)
		b.lastX, b.lastY = p.X, p.Y
		if (p.DX != 0 || p.DY != 0) && moved < moveSpeed*dt*0.3 {
			b.stuck += dt
		} else {
			b.stuck = 0
		}
		if b.stuck > 0.25 {
			b.stuck = 0
			b.detourLeft = 0.5 + rand.Float64()*0.6
			b.wanderA = rand.Float64() * 2 * math.Pi
		}
		b.detourLeft -= dt

		// Nearest visible enemy. Early on, bots keep to themselves unless someone gets close or
		// shoots them, so a match opens with looting and buying instead of an instant brawl.
		var target *Player
		best := botSight
		if m.round == nil && m.elapsed < botLootPhase && p.hurtCool == 0 {
			best = botEarlySight
		}
		for _, q := range m.players {
			if q == p || !q.Alive || (p.Team != 0 && q.Team == p.Team) {
				continue
			}
			d := math.Hypot(q.X-p.X, q.Y-p.Y)
			if d < best && m.clearLine(p.X, p.Y, q.X, q.Y) {
				target, best = q, d
			}
		}

		var mx, my float64 // movement wish
		toZone := math.Hypot(m.zoneX-p.X, m.zoneY-p.Y)

		if target != nil {
			aim := math.Atan2(target.Y-p.Y, target.X-p.X)
			p.Aim = aim + rand.NormFloat64()*botAimError
			b.seen += dt
			if b.seen >= botReact && m.elapsed >= botGrace {
				// Tap the trigger: pressed for part of each cycle, so every weapon type fires.
				b.trigger += dt
				if math.Mod(b.trigger, 0.3) < 0.15 || weapons[p.weapon].Auto {
					p.wantShoot = true
				}
			}
			p.switchTo = botWeaponFor(p, best)

			// Side-step, and keep a middle distance.
			b.strafeLeft -= dt
			if b.strafeLeft <= 0 {
				b.strafeDir = -b.strafeDir
				b.strafeLeft = 0.5 + rand.Float64()
			}
			mx, my = -math.Sin(aim)*b.strafeDir, math.Cos(aim)*b.strafeDir
			switch {
			case best > 260:
				mx, my = mx+math.Cos(aim), my+math.Sin(aim)
			case best < 110:
				mx, my = mx-math.Cos(aim), my-math.Sin(aim)
			}
		} else {
			b.seen = 0
			b.trigger = 0
			if p.mags[p.weapon] < weapons[p.weapon].Mag/2 {
				p.wantReload = true
			}
			p.wantBuy = botPurchase(p)
			gx, gy, has := m.botGoal(p)
			if !has {
				// Roam, changing heading every few seconds.
				b.wanderLeft -= dt
				if b.wanderLeft <= 0 {
					b.wanderA = rand.Float64() * 2 * math.Pi
					b.wanderLeft = 2 + rand.Float64()*3
				}
				gx, gy = p.X+math.Cos(b.wanderA)*100, p.Y+math.Sin(b.wanderA)*100
			}
			a := math.Atan2(gy-p.Y, gx-p.X)
			p.Aim = a
			if math.Hypot(gx-p.X, gy-p.Y) > 8 {
				mx, my = math.Cos(a), math.Sin(a)
			}
		}

		// Staying inside the zone comes first.
		if toZone > m.zoneR*0.85 {
			a := math.Atan2(m.zoneY-p.Y, m.zoneX-p.X)
			mx, my = math.Cos(a), math.Sin(a)
		}
		if b.detourLeft > 0 {
			mx, my = math.Cos(b.wanderA), math.Sin(b.wanderA)
		}
		if l := math.Hypot(mx, my); l > 1 {
			mx, my = mx/l, my/l
		}
		p.DX, p.DY = mx, my
	}
}

// botGoal picks where a bot with no enemy in view should go: a downed teammate to revive, or a
// medkit when hurt. It also holds revive when close enough.
func (m *Match) botGoal(p *Player) (x, y float64, ok bool) {
	if p.Team != 0 {
		for _, q := range m.players {
			if q == p || q.Team != p.Team || !q.Alive || !q.downed {
				continue
			}
			d := math.Hypot(q.X-p.X, q.Y-p.Y)
			if d > botReviveSeek {
				continue
			}
			if d <= reviveRange*0.8 {
				p.wantRevive = true
				return p.X, p.Y, true // stand still and revive
			}
			return q.X, q.Y, true
		}
	}
	if p.HP < 70 {
		bestD := 400.0
		for _, l := range m.loot {
			if d := math.Hypot(l.X-p.X, l.Y-p.Y); l.K != "cash" && d < bestD {
				bestD, x, y, ok = d, l.X, l.Y, true
			}
		}
		if ok {
			return x, y, true
		}
	}
	// Still saving up for a rifle: collect coins, staying inside the zone.
	if !p.owned[2] {
		bestD := botCashSeek
		for _, l := range m.loot {
			if l.K != "cash" || math.Hypot(l.X-m.zoneX, l.Y-m.zoneY) > m.zoneR*0.8 {
				continue
			}
			if d := math.Hypot(l.X-p.X, l.Y-p.Y); d < bestD && m.clearLine(p.X, p.Y, l.X, l.Y) {
				bestD, x, y, ok = d, l.X, l.Y, true
			}
		}
	}
	return x, y, ok
}

// botWeaponFor returns the 1-based weapon slot a bot wants at this distance, skipping empty ones.
func botWeaponFor(p *Player, dist float64) int {
	order := []int{2, 1, 0} // rifle, SMG, pistol
	if dist < 130 {
		order = []int{3, 1, 0} // shotgun up close
	} else if dist < 280 {
		order = []int{1, 2, 0}
	}
	for _, w := range order {
		if p.owned[w] && (p.mags[w] > 0 || p.reserves[w] > 0) {
			return w + 1
		}
	}
	return 1
}

// botPurchase picks what a bot buys when it has a quiet moment: the best weapon it can afford,
// or ammo once a weapon it owns has run dry. Zero means nothing.
func botPurchase(p *Player) int {
	for _, w := range []int{2, 1, 3} { // rifle, then SMG, then shotgun
		if !p.owned[w] && p.credits >= weapons[w].Price {
			return w + 1
		}
	}
	for w := range weapons {
		if p.owned[w] && p.mags[w] == 0 && p.reserves[w] == 0 && p.credits >= ammoPrice {
			return w + 1
		}
	}
	return 0
}

// spreadSpawn picks the candidate spawn point farthest from everyone already in the room, so
// players do not start on top of each other.
func (m *Match) spreadSpawn() (float64, float64) {
	bestX, bestY, bestD := 0.0, 0.0, -1.0
	for try := 0; try < 14; try++ {
		x, y := m.spawnPoint()
		nearest := math.Inf(1)
		for _, q := range m.players {
			nearest = math.Min(nearest, math.Hypot(q.X-x, q.Y-y))
		}
		if nearest > bestD {
			bestX, bestY, bestD = x, y, nearest
		}
	}
	return bestX, bestY
}
