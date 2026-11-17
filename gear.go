package main

import "math"

// Gear bought in the shop besides weapons: armour and grenades.

const (
	armorSlot     = 6 // shop slots after the five weapons
	grenadeSlot   = 7
	armorMax      = 50.0
	armorPrice    = 700
	armorAbsorb   = 0.5 // share of body damage that armour takes while it lasts
	grenadePrice  = 400
	grenadeMax    = 2
	grenadeSpeed  = 300.0
	grenadeFuse   = 1.6
	blastRadius   = 95.0
	blastDamage   = 110.0 // at the centre, falling to nothing at the edge
	throwCooldown = 1.0
)

var gearPrices = []int{armorPrice, grenadePrice}

// Grenade is a thrown grenade on its way to exploding.
type Grenade struct {
	ID     int     `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"` // height, for drawing the arc
	vx, vy float64
	left   float64
	owner  *Player
}

// buyGear handles the armour and grenade shop slots.
func (p *Player) buyGear(slot int) bool {
	switch slot {
	case armorSlot:
		if p.armor >= armorMax || p.credits < armorPrice {
			return false
		}
		p.credits -= armorPrice
		p.armor = armorMax
		return true
	case grenadeSlot:
		if p.grenades >= grenadeMax || p.credits < grenadePrice {
			return false
		}
		p.credits -= grenadePrice
		p.grenades++
		return true
	}
	return false
}

// hurt applies damage from by to target and credits it. Armour takes part of body damage;
// headshots go straight through. A player whose health runs out is downed or eliminated.
func (m *Match) hurt(target, by *Player, d float64, head bool) {
	if !head && target.armor > 0 {
		soak := math.Min(target.armor, d*armorAbsorb)
		target.armor -= soak
		d -= soak
	}
	if by != target {
		by.damage += math.Min(d, math.Max(target.HP, 0))
	}
	target.HP -= d
	target.hurtCool = regenDelay
	if target.HP > 0 {
		return
	}
	if by == target {
		m.eliminate(target, "their own grenade", nil, false)
		return
	}
	m.knockDown(target, by, head)
}

// throwGrenade launches one of p's grenades the way they are aiming.
func (m *Match) throwGrenade(p *Player) {
	if p.grenades <= 0 || p.throwCool > 0 {
		return
	}
	p.grenades--
	p.throwCool = throwCooldown
	// Aiming up throws further; aiming at the ground drops it short.
	speed := grenadeSpeed * clamp(0.75+p.Pitch, 0.25, 1.3)
	m.grenades = append(m.grenades, &Grenade{
		ID: m.newID(), X: p.X, Y: p.Y, Z: p.eye(),
		vx: math.Cos(p.Aim) * speed, vy: math.Sin(p.Aim) * speed, left: grenadeFuse, owner: p,
	})
}

// stepGrenades moves grenades, stops them at walls, and explodes the ones whose fuse has run out.
func (m *Match) stepGrenades(dt float64) {
	live := m.grenades[:0]
	for _, g := range m.grenades {
		g.left -= dt
		nx, ny := clamp(g.X+g.vx*dt, 0, worldSize), clamp(g.Y+g.vy*dt, 0, worldSize)
		if m.solidAt(nx, ny) {
			g.vx, g.vy = 0, 0 // it drops where it hit
		} else {
			g.X, g.Y = nx, ny
		}
		slow := math.Max(0, 1-1.3*dt)
		g.vx, g.vy = g.vx*slow, g.vy*slow
		g.Z = 4 + 30*math.Max(0, g.left/grenadeFuse)*math.Sin(math.Pi*math.Max(0, g.left/grenadeFuse))
		if g.left <= 0 {
			m.explode(g)
			continue
		}
		live = append(live, g)
	}
	m.grenades = live
}

// solidAt reports whether a point is inside an obstacle.
func (m *Match) solidAt(x, y float64) bool {
	for _, r := range m.obstacles {
		if x > r.X && x < r.X+r.W && y > r.Y && y < r.Y+r.H {
			return true
		}
	}
	return false
}

// explode damages everyone in the blast who is not behind cover. It spares the thrower's
// teammates, like bullets do, but not the thrower.
func (m *Match) explode(g *Grenade) {
	for _, q := range m.players {
		if !q.Alive || (q != g.owner && q.Team == g.owner.Team && q.Team != 0) {
			continue
		}
		dist := math.Hypot(q.X-g.X, q.Y-g.Y)
		if dist >= blastRadius || !m.blastReaches(g.X, g.Y, q) {
			continue
		}
		m.hurt(q, g.owner, blastDamage*(1-dist/blastRadius), false)
		if q != g.owner {
			g.owner.hitSeq++
			g.owner.hitKind = "body"
		}
	}
	for i := len(m.obstacles) - 1; i >= 0; i-- {
		o := m.obstacles[i]
		if o.K != "crate" {
			continue
		}
		if d := math.Hypot(o.X+o.W/2-g.X, o.Y+o.H/2-g.Y); d < blastRadius {
			m.damageCrate(i, blastDamage*(1-d/blastRadius))
		}
	}
}

// blastReaches reports whether nothing solid stands between a blast and a player. Cover lower
// than the player's body does not count unless they are crouched or down behind it.
func (m *Match) blastReaches(x, y float64, q *Player) bool {
	dx, dy := q.X-x, q.Y-y
	dist := math.Hypot(dx, dy)
	if dist < 1 {
		return true
	}
	dx, dy = dx/dist, dy/dist
	for _, r := range m.obstacles {
		if t, ok := rayRect(x, y, dx, dy, r); ok && t < dist && r.top() >= q.Z+q.bodyTop() {
			return false
		}
	}
	return true
}
