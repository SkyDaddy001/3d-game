package main

import "math"

// Heights. The map is flat, but shots, cover, and bodies have height: a shot is a line that starts
// at the shooter's eyes and rises or falls with their aim. Low cover stops low shots only, and a
// crouching player is a smaller target who can hide behind a small crate.

const (
	standTop     = 36.0 // top of a standing player's head
	crouchTop    = 24.0
	downedTop    = 10.0 // a downed player lies on the ground
	standEye     = 30.0 // where a standing player's shots start
	crouchEye    = 16.0
	headBand     = 10.0 // the top of the body that counts as the head when standing
	crouchBand   = 8.0
	jumpSpeed    = 78.0  // upward speed at take-off, in units per second
	gravity      = 220.0 // a jump peaks about 14 units up and lasts about 0.7 seconds
	crouchSpeed  = 0.55  // movement speed factor while crouching
	crouchAim    = 0.6   // spread factor while crouching: steadier aim
	maxPitch     = 1.2   // how far up or down a player can aim, in radians
	tallTop      = 200.0 // buildings and trees: nothing shoots over them
	containerTop = 38.0
	crateExtra   = 8.0 // how much taller than wide a crate is
)

// top is how high an obstacle stands. A crate is a little taller than it is wide, so the small
// one (26) fully hides a crouching player (24) and the medium one (38) a standing player (36).
func (r Rect) top() float64 {
	switch r.K {
	case "crate":
		return r.W + crateExtra
	case "container":
		return containerTop
	}
	return tallTop
}

// bodyTop is the height of the top of the player's body above their feet.
func (p *Player) bodyTop() float64 {
	switch {
	case p.swim:
		return swimTop
	case p.downed:
		return downedTop
	case p.crouch:
		return crouchTop
	}
	return standTop
}

// eye is the height above the ground that the player's shots start from.
func (p *Player) eye() float64 {
	if p.swim {
		return p.Z + swimEye
	}
	if p.crouch {
		return p.Z + crouchEye
	}
	return p.Z + standEye
}

// headFrom is the height above the feet where the head begins. A downed player has no head zone.
func (p *Player) headFrom() float64 {
	switch {
	case p.downed, p.swim:
		return math.Inf(1)
	case p.crouch:
		return crouchTop - crouchBand
	}
	return standTop - headBand
}

// rayRectSpan returns where a ray enters and leaves a rectangle on the ground plane.
func rayRectSpan(ox, oy, dx, dy float64, r Rect) (enter, leave float64, ok bool) {
	tmin, tmax := 0.0, math.Inf(1)
	axes := [2][4]float64{
		{ox, dx, r.X, r.X + r.W},
		{oy, dy, r.Y, r.Y + r.H},
	}
	for _, ax := range axes {
		o, d, lo, hi := ax[0], ax[1], ax[2], ax[3]
		if math.Abs(d) < 1e-9 {
			if o < lo || o > hi {
				return 0, 0, false
			}
			continue
		}
		t1, t2 := (lo-o)/d, (hi-o)/d
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		tmin = math.Max(tmin, t1)
		tmax = math.Min(tmax, t2)
		if tmin > tmax {
			return 0, 0, false
		}
	}
	return tmin, tmax, true
}

// rayStopped reports where a rising or falling ray is stopped by an obstacle, if it is. The ray
// starts at height h0 and gains slope units of height per unit travelled. A ray that stays above
// the obstacle's top the whole way across passes over it.
func rayStopped(ox, oy, dx, dy, h0, slope float64, r Rect) (float64, bool) {
	enter, leave, ok := rayRectSpan(ox, oy, dx, dy, r)
	if !ok {
		return 0, false
	}
	top := r.top()
	if h0+slope*enter < top {
		return enter, true // hits the side
	}
	if !math.IsInf(leave, 1) && h0+slope*leave < top {
		return (top - h0) / slope, true // comes down onto the top
	}
	return 0, false
}

// canSee reports whether p has a clear line from their eyes to the middle of q's body.
func (m *Match) canSee(p, q *Player) bool {
	dx, dy := q.X-p.X, q.Y-p.Y
	dist := math.Hypot(dx, dy)
	if dist == 0 {
		return true
	}
	dx, dy = dx/dist, dy/dist
	h0 := p.eye()
	slope := (q.Z + q.bodyTop()/2 - h0) / dist
	for _, r := range m.obstacles {
		if t, ok := rayStopped(p.X, p.Y, dx, dy, h0, slope, r); ok && t < dist {
			return false
		}
	}
	return true
}

// pitchTo is the up or down aim that points p's shots at the middle of q's body.
func pitchTo(p, q *Player) float64 {
	dist := math.Hypot(q.X-p.X, q.Y-p.Y)
	return math.Atan2(q.Z+q.bodyTop()/2-p.eye(), math.Max(dist, 1))
}

// updateStance applies crouching and jumping for one tick.
func (p *Player) updateStance(dt float64) {
	if p.wantJump && p.Z == 0 && !p.crouch {
		p.vz = jumpSpeed
	}
	p.wantJump = false
	if p.Z > 0 || p.vz > 0 {
		p.Z += p.vz * dt
		p.vz -= gravity * dt
		if p.Z <= 0 {
			p.Z, p.vz = 0, 0
		}
	}
}
