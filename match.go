package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"sync"
	"time"
)

const (
	tickRate     = 30
	worldSize    = 2000.0
	center       = worldSize / 2
	playerRadius = 12.0
	moveSpeed    = 180.0
	aoiRadius    = 700.0 // interest management: only send entities this close
	shotRange    = 500.0
	shotDamage   = 25.0
	shotCooldown = 0.4
	zoneDPS      = 5.0
	pickupRange  = 25.0
	healAmount   = 30.0
	maxPlayers   = 100
	shrinkEvery  = 30.0
)

type Player struct {
	ID        int
	Name      string
	X, Y, Aim float64
	HP        float64
	Alive     bool
	DX, DY    float64
	wantShoot bool
	cooldown  float64
	gone      bool
	send      chan []byte
}

type Loot struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

type PlayerView struct {
	ID    int     `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	HP    float64 `json:"hp"`
	Alive bool    `json:"alive"`
}

type Zone struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	R float64 `json:"r"`
}

type Snapshot struct {
	T       string       `json:"t"`
	Started bool         `json:"started"`
	Over    bool         `json:"over"`
	Winner  string       `json:"winner"`
	Alive   int          `json:"alive"`
	Zone    Zone         `json:"zone"`
	Me      PlayerView   `json:"me"`
	Players []PlayerView `json:"players"`
	Loot    []Loot       `json:"loot"`
}

type Match struct {
	mu       sync.Mutex
	ID       string
	players  map[int]*Player
	loot     map[int]*Loot
	nextID   int
	started  bool
	over     bool
	winner   string
	zoneR    float64
	zoneLeft float64
}

func NewMatch(id string) *Match {
	m := &Match{
		ID:       id,
		players:  map[int]*Player{},
		loot:     map[int]*Loot{},
		zoneR:    worldSize * 0.75,
		zoneLeft: shrinkEvery,
	}
	for i := 0; i < 80; i++ {
		m.addLoot()
	}
	go m.run()
	return m
}

func (m *Match) newID() int {
	m.nextID++
	return m.nextID
}

// addLoot must be called with m.mu held (or before the match is shared).
func (m *Match) addLoot() {
	id := m.newID()
	m.loot[id] = &Loot{ID: id, X: rand.Float64() * worldSize, Y: rand.Float64() * worldSize}
}

func (m *Match) Join(name string) (*Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.over || len(m.players) >= maxPlayers {
		return nil, false
	}
	a := rand.Float64() * 2 * math.Pi
	r := math.Sqrt(rand.Float64()) * m.zoneR * 0.9
	id := m.newID()
	p := &Player{
		ID: id, Name: name, HP: 100, Alive: true,
		X: center + r*math.Cos(a), Y: center + r*math.Sin(a),
		send: make(chan []byte, 16),
	}
	m.players[id] = p
	return p, true
}

func (m *Match) Leave(p *Player) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.gone = true
	p.Alive = false
	if !m.started {
		delete(m.players, p.ID)
	}
}

func (m *Match) Input(p *Player, dx, dy, aim float64, shoot bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.DX, p.DY, p.Aim = dx, dy, aim
	if shoot {
		p.wantShoot = true
	}
}

func (m *Match) Start() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || len(m.players) < 2 {
		return false
	}
	m.started = true
	return true
}

func (m *Match) Started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *Match) Summary() (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.players), m.started
}

func (m *Match) run() {
	t := time.NewTicker(time.Second / tickRate)
	defer t.Stop()
	for range t.C {
		if m.tick(1.0 / tickRate) {
			return
		}
	}
}

// tick advances the match (once started) and broadcasts snapshots.
// It returns true when the match is finished.
func (m *Match) tick(dt float64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started && !m.over {
		m.step(dt)
	}
	m.broadcast()
	return m.over
}

func (m *Match) step(dt float64) {
	m.zoneLeft -= dt
	if m.zoneLeft <= 0 {
		m.zoneR = math.Max(m.zoneR*0.7, 60)
		m.zoneLeft = shrinkEvery
	}

	for _, p := range m.players {
		if !p.Alive {
			continue
		}
		p.X = clamp(p.X+p.DX*moveSpeed*dt, 0, worldSize)
		p.Y = clamp(p.Y+p.DY*moveSpeed*dt, 0, worldSize)
		p.cooldown -= dt

		if p.wantShoot && p.cooldown <= 0 {
			m.fire(p)
			p.cooldown = shotCooldown
		}
		p.wantShoot = false

		if math.Hypot(p.X-center, p.Y-center) > m.zoneR {
			p.HP -= zoneDPS * dt
		}
		m.tryPickup(p)

		if p.HP <= 0 {
			p.HP = 0
			p.Alive = false
		}
	}

	alive, last := 0, (*Player)(nil)
	for _, p := range m.players {
		if p.Alive {
			alive++
			last = p
		}
	}
	if alive <= 1 {
		m.over = true
		m.winner = "nobody"
		if last != nil {
			m.winner = last.Name
		}
	}
}

// fire is a hitscan ray from the shooter along its aim, hitting the nearest player in line.
func (m *Match) fire(p *Player) {
	dx, dy := math.Cos(p.Aim), math.Sin(p.Aim)
	var target *Player
	best := shotRange
	for _, q := range m.players {
		if q == p || !q.Alive {
			continue
		}
		vx, vy := q.X-p.X, q.Y-p.Y
		t := vx*dx + vy*dy
		if t < 0 || t > best {
			continue
		}
		if math.Abs(vx*dy-vy*dx) <= playerRadius {
			target, best = q, t
		}
	}
	if target != nil {
		target.HP -= shotDamage
		if target.HP <= 0 {
			target.HP = 0
			target.Alive = false
		}
	}
}

func (m *Match) tryPickup(p *Player) {
	if p.HP >= 100 {
		return
	}
	for id, l := range m.loot {
		if math.Hypot(p.X-l.X, p.Y-l.Y) < pickupRange {
			p.HP = math.Min(100, p.HP+healAmount)
			delete(m.loot, id)
			m.addLoot()
			return
		}
	}
}

func (p *Player) view() PlayerView {
	return PlayerView{ID: p.ID, X: p.X, Y: p.Y, HP: p.HP, Alive: p.Alive}
}

func near(p *Player, x, y float64) bool {
	return math.Hypot(p.X-x, p.Y-y) <= aoiRadius
}

// broadcast sends each connected player only the entities inside their area of interest.
func (m *Match) broadcast() {
	alive := 0
	for _, p := range m.players {
		if p.Alive {
			alive++
		}
	}
	z := Zone{X: center, Y: center, R: m.zoneR}

	for _, me := range m.players {
		if me.gone {
			continue
		}
		snap := Snapshot{
			T: "state", Started: m.started, Over: m.over, Winner: m.winner,
			Alive: alive, Zone: z, Me: me.view(),
			Players: []PlayerView{}, Loot: []Loot{},
		}
		for _, q := range m.players {
			if q != me && q.Alive && near(me, q.X, q.Y) {
				snap.Players = append(snap.Players, q.view())
			}
		}
		for _, l := range m.loot {
			if math.Hypot(me.X-l.X, me.Y-l.Y) <= aoiRadius {
				snap.Loot = append(snap.Loot, *l)
			}
		}
		msg, _ := json.Marshal(snap)
		select {
		case me.send <- msg:
		default: // slow client: drop this snapshot, the next one supersedes it
		}
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
