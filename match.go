package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// defaultIdleTimeout is how long a match may have no connected players before it closes.
const defaultIdleTimeout = 60 * time.Second

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
	obstacleN    = 40
	maxEvents    = 5
	headRadius   = 5.0 // a shot this close to the center counts as a headshot
	headDamage   = 100.0
	bloomPerShot = 0.06 // spread (radians) added per shot
	bloomMax     = 0.3
	bloomDecay   = 1.2 // spread (radians per second) recovered while not firing
	bountyTime   = 8.0 // seconds a killer is revealed to everyone
	pingTime     = 5.0 // seconds a team ping stays visible
	pingCooldown = 2.0
	minDuos      = 4 // teams of two only form with at least this many players
)

type Player struct {
	ID         int
	Name       string
	Team       int
	X, Y, Aim  float64
	HP         float64
	Alive      bool
	Place      int // final placement, set when the player is eliminated
	DX, DY     float64
	wantShoot  bool
	wantPing   bool
	pingAt     Point
	cooldown   float64
	pingCool   float64
	bloom      float64 // current spread in radians; grows while firing, recovers over time
	revealLeft float64 // seconds left during which this player is shown to everyone
	hitSeq     int     // increments on every hit this player lands, for hit markers
	hitKind    string  // "body" or "head" for the most recent hit
	gone       bool
	send       chan []byte
}

// Controls is one client's input for a tick.
type Controls struct {
	DX, DY, Aim float64
	Shoot       bool
	Ping        bool
	PX, PY      float64 // world position to ping
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Ping struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Name string  `json:"name"`
	Left float64 `json:"left"`
	team int
}

type Loot struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

// Rect is a static obstacle that blocks movement and shots.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type PlayerView struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Team  int     `json:"team"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	HP    float64 `json:"hp"`
	Alive bool    `json:"alive"`
	Place int     `json:"place"`
	Bloom float64 `json:"bloom"`
}

type Zone struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	R float64 `json:"r"`
}

type Snapshot struct {
	T         string       `json:"t"`
	Started   bool         `json:"started"`
	Over      bool         `json:"over"`
	Winner    string       `json:"winner"`
	Alive     int          `json:"alive"`
	Zone      Zone         `json:"zone"`
	Me        PlayerView   `json:"me"`
	Players   []PlayerView `json:"players"`
	Loot      []Loot       `json:"loot"`
	Obstacles []Rect       `json:"obstacles"`
	Events    []string     `json:"events"`
	Revealed  []PlayerView `json:"revealed"`
	Pings     []Ping       `json:"pings"`
	HitSeq    int          `json:"hitSeq"`
	HitKind   string       `json:"hitKind"`
}

type Match struct {
	mu        sync.Mutex
	ID        string
	players   map[int]*Player
	loot      map[int]*Loot
	obstacles []Rect
	events    []string
	pings     []Ping
	nextID    int
	started   bool
	over      bool
	winner    string
	zoneX     float64
	zoneY     float64
	zoneR     float64
	zoneLeft  float64
	idleLimit time.Duration
	idleAt    time.Time // when the match last had zero connected players; zero if it has some
	closed    bool      // set when the match stops; the server removes closed matches
}

func NewMatch(id string) *Match {
	m := &Match{
		ID:        id,
		players:   map[int]*Player{},
		loot:      map[int]*Loot{},
		idleLimit: defaultIdleTimeout,
		zoneX:     center,
		zoneY:     center,
		zoneR:     worldSize * 0.75,
		zoneLeft:  shrinkEvery,
	}
	for i := 0; i < obstacleN; i++ {
		m.obstacles = append(m.obstacles, Rect{
			X: rand.Float64() * (worldSize - 160),
			Y: rand.Float64() * (worldSize - 160),
			W: 40 + rand.Float64()*100,
			H: 40 + rand.Float64()*100,
		})
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

// blocked reports whether a player centered at (x, y) would overlap an obstacle.
func (m *Match) blocked(x, y float64) bool {
	for _, r := range m.obstacles {
		if x > r.X-playerRadius && x < r.X+r.W+playerRadius &&
			y > r.Y-playerRadius && y < r.Y+r.H+playerRadius {
			return true
		}
	}
	return false
}

func (m *Match) logEvent(s string) {
	m.events = append(m.events, s)
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

func (m *Match) countAlive() int {
	n := 0
	for _, p := range m.players {
		if p.Alive {
			n++
		}
	}
	return n
}

// eliminate removes a player from the match, records their placement, and logs the kill.
// killer is nil for zone deaths; a player kill reveals the killer to everyone.
func (m *Match) eliminate(p *Player, by string, killer *Player, headshot bool) {
	p.HP = 0
	p.Alive = false
	p.Place = m.countAlive() + 1
	how := ""
	if headshot {
		how = " (headshot)"
	}
	m.logEvent(fmt.Sprintf("%s eliminated by %s%s (#%d)", p.Name, by, how, p.Place))
	if killer != nil {
		killer.revealLeft = bountyTime
	}
}

func (m *Match) Join(name string) (*Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.over || len(m.players) >= maxPlayers {
		return nil, false
	}
	x, y := m.spawnPoint()
	id := m.newID()
	p := &Player{
		ID: id, Name: name, HP: 100, Alive: true,
		X: x, Y: y,
		send: make(chan []byte, 16),
	}
	m.players[id] = p
	return p, true
}

// assignTeams groups players into duos when there are enough of them, otherwise each player
// is on their own team. Teams are fixed at match start.
func (m *Match) assignTeams() {
	ids := make([]int, 0, len(m.players))
	for id := range m.players {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	duos := len(ids) >= minDuos
	for i, id := range ids {
		if duos {
			m.players[id].Team = i/2 + 1
		} else {
			m.players[id].Team = i + 1
		}
	}
}

// spawnPoint picks a random spot inside the zone that is not inside an obstacle.
func (m *Match) spawnPoint() (float64, float64) {
	var x, y float64
	for try := 0; try < 50; try++ {
		a := rand.Float64() * 2 * math.Pi
		r := math.Sqrt(rand.Float64()) * m.zoneR * 0.9
		x = clamp(m.zoneX+r*math.Cos(a), playerRadius, worldSize-playerRadius)
		y = clamp(m.zoneY+r*math.Sin(a), playerRadius, worldSize-playerRadius)
		if !m.blocked(x, y) {
			break
		}
	}
	return x, y
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

func (m *Match) Input(p *Player, c Controls) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.DX, p.DY, p.Aim = c.DX, c.DY, c.Aim
	if c.Shoot {
		p.wantShoot = true
	}
	if c.Ping {
		p.wantPing = true
		p.pingAt = Point{X: c.PX, Y: c.PY}
	}
}

func (m *Match) Start() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || len(m.players) < 2 {
		return false
	}
	m.assignTeams()
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
// It returns true when the match has stopped.
func (m *Match) tick(dt float64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started && !m.over {
		m.step(dt)
	}
	m.trackIdle()
	if m.closed {
		return true
	}
	m.broadcast()
	return m.over
}

// trackIdle closes the match once nobody has been connected for idleLimit.
func (m *Match) trackIdle() {
	connected := 0
	for _, p := range m.players {
		if !p.gone {
			connected++
		}
	}
	if connected > 0 {
		m.idleAt = time.Time{}
		return
	}
	if m.idleAt.IsZero() {
		m.idleAt = time.Now()
		return
	}
	if time.Since(m.idleAt) >= m.idleLimit {
		m.closed = true
	}
}

// Closed reports whether the match has stopped and can be removed.
func (m *Match) Closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *Match) step(dt float64) {
	m.zoneLeft -= dt
	if m.zoneLeft <= 0 {
		m.shrinkZone()
		m.zoneLeft = shrinkEvery
	}

	for _, p := range m.players {
		if !p.Alive {
			continue
		}
		nx := clamp(p.X+p.DX*moveSpeed*dt, 0, worldSize)
		ny := clamp(p.Y+p.DY*moveSpeed*dt, 0, worldSize)
		// Slide along obstacles: try each axis independently.
		if !m.blocked(nx, p.Y) {
			p.X = nx
		}
		if !m.blocked(p.X, ny) {
			p.Y = ny
		}
		p.cooldown -= dt
		p.pingCool -= dt

		if p.wantShoot && p.cooldown <= 0 {
			m.fire(p)
			p.cooldown = shotCooldown
		}
		p.wantShoot = false

		if p.wantPing && p.pingCool <= 0 {
			m.pings = append(m.pings, Ping{X: p.pingAt.X, Y: p.pingAt.Y, Name: p.Name, Left: pingTime, team: p.Team})
			p.pingCool = pingCooldown
		}
		p.wantPing = false

		if math.Hypot(p.X-m.zoneX, p.Y-m.zoneY) > m.zoneR {
			p.HP -= zoneDPS * dt
		}
		m.tryPickup(p)

		if p.HP <= 0 {
			m.eliminate(p, "the zone", nil, false)
		}
	}

	// Spread recovers, bounties and pings count down for everyone.
	for _, p := range m.players {
		p.bloom = math.Max(0, p.bloom-bloomDecay*dt)
		p.revealLeft = math.Max(0, p.revealLeft-dt)
	}
	live := m.pings[:0]
	for _, pg := range m.pings {
		pg.Left -= dt
		if pg.Left > 0 {
			live = append(live, pg)
		}
	}
	m.pings = live

	m.checkWinner()
}

// checkWinner ends the match once at most one team has living players.
func (m *Match) checkWinner() {
	if m.over {
		return
	}
	aliveTeams := map[int]bool{}
	for _, p := range m.players {
		if p.Alive {
			aliveTeams[p.Team] = true
		}
	}
	if len(aliveTeams) > 1 {
		return
	}
	m.over = true
	m.winner = "nobody"
	for _, p := range m.players {
		if !p.Alive {
			continue
		}
		p.Place = 1
		m.winner = p.Name
		if len(m.teamMembers(p.Team)) > 1 {
			m.winner = fmt.Sprintf("Team %d", p.Team)
		}
	}
	if m.winner != "nobody" {
		m.logEvent(fmt.Sprintf("%s wins the match", m.winner))
	}
}

func (m *Match) teamMembers(team int) []*Player {
	var out []*Player
	for _, p := range m.players {
		if p.Team == team && !p.gone {
			out = append(out, p)
		}
	}
	return out
}

// shrinkZone moves to a smaller circle whose center lies inside the old one,
// so the safe area drifts the way a battle-royale zone does.
func (m *Match) shrinkZone() {
	newR := math.Max(m.zoneR*0.7, 60)
	slack := m.zoneR - newR
	a := rand.Float64() * 2 * math.Pi
	d := math.Sqrt(rand.Float64()) * slack
	m.zoneX = clamp(m.zoneX+d*math.Cos(a), newR, worldSize-newR)
	m.zoneY = clamp(m.zoneY+d*math.Sin(a), newR, worldSize-newR)
	m.zoneR = newR
}

// rayRect returns the distance along the ray at which it first enters r.
func rayRect(ox, oy, dx, dy float64, r Rect) (float64, bool) {
	tmin, tmax := 0.0, math.Inf(1)
	axes := [2][4]float64{
		{ox, dx, r.X, r.X + r.W},
		{oy, dy, r.Y, r.Y + r.H},
	}
	for _, ax := range axes {
		o, d, lo, hi := ax[0], ax[1], ax[2], ax[3]
		if math.Abs(d) < 1e-9 {
			if o < lo || o > hi {
				return 0, false
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
			return 0, false
		}
	}
	return tmin, true
}

// fire is a hitscan ray from the shooter along its aim, with weapon spread. Obstacles stop
// it, teammates are not hit, and the nearest enemy in line takes damage.
func (m *Match) fire(p *Player) {
	spread := (rand.Float64()*2 - 1) * p.bloom
	dx, dy := math.Cos(p.Aim+spread), math.Sin(p.Aim+spread)
	p.bloom = math.Min(bloomMax, p.bloom+bloomPerShot)

	wall := shotRange
	for _, r := range m.obstacles {
		if t, ok := rayRect(p.X, p.Y, dx, dy, r); ok && t < wall {
			wall = t
		}
	}

	var target *Player
	var targetHead bool
	best := wall
	for _, q := range m.players {
		if q == p || !q.Alive || q.Team == p.Team {
			continue
		}
		vx, vy := q.X-p.X, q.Y-p.Y
		t := vx*dx + vy*dy
		if t < 0 || t > best {
			continue
		}
		perp := math.Abs(vx*dy - vy*dx)
		if perp <= playerRadius {
			target, best = q, t
			targetHead = perp <= headRadius
		}
	}
	if target == nil {
		return
	}
	dmg, kind := shotDamage, "body"
	if targetHead {
		dmg, kind = headDamage, "head"
	}
	target.HP -= dmg
	p.hitSeq++
	p.hitKind = kind
	if target.HP <= 0 {
		m.eliminate(target, p.Name, p, targetHead)
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
	return PlayerView{
		ID: p.ID, Name: p.Name, Team: p.Team, X: p.X, Y: p.Y,
		HP: p.HP, Alive: p.Alive, Place: p.Place, Bloom: p.bloom,
	}
}

func near(p *Player, x, y float64) bool {
	return math.Hypot(p.X-x, p.Y-y) <= aoiRadius
}

// broadcast sends each connected player their view of the match. Teammates are always visible,
// other players only inside the area of interest.
func (m *Match) broadcast() {
	alive := m.countAlive()
	z := Zone{X: m.zoneX, Y: m.zoneY, R: m.zoneR}

	for _, me := range m.players {
		if me.gone {
			continue
		}
		snap := Snapshot{
			T: "state", Started: m.started, Over: m.over, Winner: m.winner,
			Alive: alive, Zone: z, Me: me.view(),
			Players: []PlayerView{}, Loot: []Loot{}, Obstacles: []Rect{},
			Events: append([]string{}, m.events...), Revealed: []PlayerView{},
			Pings: []Ping{}, HitSeq: me.hitSeq, HitKind: me.hitKind,
		}
		for _, q := range m.players {
			if q == me || !q.Alive {
				continue
			}
			if q.Team == me.Team && me.Team != 0 || near(me, q.X, q.Y) {
				snap.Players = append(snap.Players, q.view())
			}
			if q.revealLeft > 0 && !q.gone {
				snap.Revealed = append(snap.Revealed, q.view())
			}
		}
		for _, pg := range m.pings {
			if pg.team == me.Team && me.Team != 0 {
				snap.Pings = append(snap.Pings, pg)
			}
		}
		for _, l := range m.loot {
			if math.Hypot(me.X-l.X, me.Y-l.Y) <= aoiRadius {
				snap.Loot = append(snap.Loot, *l)
			}
		}
		for _, r := range m.obstacles {
			if math.Hypot(me.X-(r.X+r.W/2), me.Y-(r.Y+r.H/2)) <= aoiRadius+200 {
				snap.Obstacles = append(snap.Obstacles, r)
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
