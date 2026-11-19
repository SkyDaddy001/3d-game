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
	treeN        = 70
	startCredits = 1000
	killReward   = 300
	cashAmount   = 250
	ammoPrice    = 200  // refills the reserve of a weapon the player already owns
	cashShare    = 0.35 // fraction of pickups that are cash rather than medkits
	treeSize     = 10.0 // side of a tree trunk's collision square
	crateN       = 54
	crateSize    = 18.0 // the small crate; see crateKinds for the bigger ones
	crateHP      = 60.0
	containerN   = 14
	containerLen = 84.0 // shipping containers: long, unbreakable cover
	containerWid = 32.0
	maxEvents    = 5
	headRadius   = 5.0 // a shot this close to the center counts as a headshot
	headDamage   = 100.0
	bloomPerShot = 0.06 // spread (radians) added per shot
	bloomMax     = 0.3
	bloomDecay   = 1.2 // spread (radians per second) recovered while not firing
	bountyTime   = 8.0 // seconds a killer is revealed to everyone
	pingTime     = 5.0 // seconds a team ping stays visible
	pingCooldown = 2.0
	downedTime   = 20.0 // seconds a downed player can be revived before bleeding out
	reviveTime   = 3.0  // seconds a teammate must hold revive near a downed player
	reviveRange  = 60.0
	regenDelay   = 5.0  // seconds without damage before health starts to recover
	regenRate    = 10.0 // HP per second while recovering
	revivedHP    = 30.0
	minDuos      = 4 // teams of two only form with at least this many players
)

// Weapon describes one weapon type. Players carry one magazine and reserve per weapon.
type Weapon struct {
	Price        int // credits to buy; the pistol is free and owned from the start
	Name         string
	Mag          int
	Reserve      int
	Interval     float64 // seconds between shots
	ReloadTime   float64 // seconds to reload
	Damage       float64 // body damage per pellet
	HeadDamage   float64 // headshot damage per pellet
	Pellets      int     // rays per shot (shotguns fire several)
	Spread       float64 // base spread in radians
	BloomPerShot float64
	BloomMax     float64
	Range        float64
	Auto         bool // holding fire keeps shooting; otherwise each press fires once
}

var weapons = []Weapon{
	{Name: "Pistol", Price: 0, Mag: 12, Reserve: 60, Interval: 0.25, ReloadTime: 1.4, Damage: 25, HeadDamage: 100, Pellets: 1, Spread: 0.01, BloomPerShot: 0.04, BloomMax: 0.15, Range: 500},
	{Name: "SMG", Price: 900, Mag: 30, Reserve: 150, Interval: 0.075, ReloadTime: 1.8, Damage: 18, HeadDamage: 54, Pellets: 1, Spread: 0.02, BloomPerShot: 0.02, BloomMax: 0.22, Range: 400, Auto: true},
	{Name: "Rifle", Price: 1800, Mag: 30, Reserve: 120, Interval: 0.1, ReloadTime: 2.2, Damage: 28, HeadDamage: 84, Pellets: 1, Spread: 0.01, BloomPerShot: 0.03, BloomMax: 0.2, Range: 600, Auto: true},
	{Name: "Shotgun", Price: 1100, Mag: 6, Reserve: 30, Interval: 0.9, ReloadTime: 2.4, Damage: 14, HeadDamage: 40, Pellets: 8, Spread: 0.12, Range: 300},
	{Name: "Sniper", Price: 2600, Mag: 5, Reserve: 20, Interval: 1.2, ReloadTime: 2.8, Damage: 80, HeadDamage: 100, Pellets: 1, Spread: 0, Range: 2000},
}

type Player struct {
	ID          int
	Name        string
	Team        int
	X, Y, Aim   float64
	HP          float64
	Alive       bool
	Place       int // final placement, set when the player is eliminated
	DX, DY      float64
	wantShoot   bool
	chatAt      time.Time // when this player last sent a quick chat message
	wantPing    bool
	pingAt      Point
	cooldown    float64
	pingCool    float64
	bloom       float64 // current spread in radians; grows while firing, recovers over time
	revealLeft  float64 // seconds left during which this player is shown to everyone
	weapon      int     // index into weapons
	mags        [5]int  // rounds in the magazine, per weapon
	reserves    [5]int  // spare rounds, per weapon
	armed       bool    // ammo is initialized on first use
	reloadLeft  float64 // seconds left in a reload; zero when not reloading
	prevShoot   bool    // whether fire was held last tick, for semi-auto weapons
	wantReload  bool
	switchTo    int    // 1-based slot requested this tick, zero for none
	hitSeq      int    // increments on every hit this player lands, for hit markers
	hitKind     string // "body" or "head" for the most recent hit
	gone        bool
	downed      bool    // on the ground: can be revived by a teammate, bleeds out otherwise
	bleed       float64 // seconds left before a downed player bleeds out
	reviveProg  float64 // seconds of revive progress from a nearby teammate
	hurtCool    float64 // seconds since last damage; health recovers when it reaches zero
	wantRevive  bool
	bot         *botState // set for computer-controlled players; nil for people
	owned       [5]bool   // weapons the player has; the pistol from the start, others bought
	credits     int
	look        string // avatar look code shown to other players
	wantBuy     int
	lootSig     uint64  // signature of the pickup list last delivered to this player
	obstSig     uint64  // signature of the obstacle list last delivered
	echo        float64 // client clock value from the latest input, echoed back for ping
	Pitch       float64 // up or down aim in radians; positive is up
	Z           float64 // height of the feet above the ground while jumping
	vz          float64
	crouch      bool
	wantJump    bool
	armor       float64 // absorbs part of body damage until used up
	grenades    int
	wantThrow   bool
	throwCool   float64
	wantSide    int     // rounds: the side picked before the start (1 Alpha, 2 Bravo)
	swim        bool    // in a lake: slow, low in the water, and unable to shoot
	quiet       float64 // seconds since the player last fired
	sentWorld   bool    // the fixed terrain has been delivered to this player
	kills       int     // eliminations credited to this player
	damage      float64 // total damage dealt to enemies
	send        chan []byte
	voice       chan []byte // voice packets for this player to hear
	voiceWindow time.Time
	voiceCount  int
}

// Controls is one client's input for a tick.
type Controls struct {
	DX, DY, Aim float64
	Shoot       bool
	Ping        bool
	PX, PY      float64 // world position to ping
	Reload      bool
	Switch      int // 1-based weapon slot, zero for none
	Revive      bool
	Buy         int // 1-based weapon slot to buy, or to refill if already owned; zero for none
	Echo        float64
	Pitch       float64
	Crouch      bool
	Jump        bool
	Throw       bool
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
	K  string  `json:"k,omitempty"` // kind: empty for a medkit, "cash" for credits
}

// Rect is a static obstacle that blocks movement and shots.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
	K string  `json:"k,omitempty"` // kind: empty for a building, "tree" for a tree trunk, "crate" for a breakable crate
	// HP is a crate's remaining strength. Buildings and trees cannot be damaged.
	HP float64 `json:"-"`
}

type AmmoView struct {
	Weapon     string  `json:"weapon"`
	Slot       int     `json:"slot"`
	Mag        int     `json:"mag"`
	Reserve    int     `json:"reserve"`
	Reloading  bool    `json:"reloading"`
	ReloadFrac float64 `json:"reloadFrac"`
	Auto       bool    `json:"auto"`
	Range      float64 `json:"range"`
	Credits    int     `json:"credits"`
	Owned      []bool  `json:"owned"`  // one entry per weapon slot
	Prices     []int   `json:"prices"` // weapon prices, by slot
	AmmoPrice  int     `json:"ammoPrice"`
	Armor      float64 `json:"armor"`
	Grenades   int     `json:"grenades"`
	GearPrices []int   `json:"gearPrices"` // armour, then grenade
}

type PlayerView struct {
	Weapon string  `json:"weapon"`
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Team   int     `json:"team"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	HP     float64 `json:"hp"`
	Alive  bool    `json:"alive"`
	Place  int     `json:"place"`
	Bloom  float64 `json:"bloom"`
	Downed bool    `json:"downed"`
	Aim    float64 `json:"aim"`
	Kills  int     `json:"kills"`
	Look   string  `json:"look,omitempty"` // avatar look code; empty for the default look
	Crouch bool    `json:"crouch,omitempty"`
	Swim   bool    `json:"swim,omitempty"`
	Hidden bool    `json:"hidden,omitempty"` // in a bush and not seen from a distance
	Z      float64 `json:"z,omitempty"`      // height above the ground while jumping
	Bleed  float64 `json:"bleed"`
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
	// Loot and Obstacles are sent only when the set near the player changes; a snapshot without
	// them means "same as before". This keeps snapshots small.
	Loot      *[]Loot      `json:"loot,omitempty"`
	Obstacles *[]Rect      `json:"obstacles,omitempty"`
	Echo      float64      `json:"echo,omitempty"` // the client clock value from its latest input, for measuring ping
	Events    []string     `json:"events"`
	Revealed  []PlayerView `json:"revealed"`
	Pings     []Ping       `json:"pings"`
	HitSeq    int          `json:"hitSeq"`
	HitKind   string       `json:"hitKind"`
	Ammo      AmmoView     `json:"ammo"`
	XP        int          `json:"xp"`       // experience earned this match; set once the match is over
	Trophies  int          `json:"trophies"` // trophy change this match; set once a ranked match is over
	Ranked    bool         `json:"ranked"`
	Round     *RoundView   `json:"round,omitempty"`    // set in rounds mode
	Grenades  []*Grenade   `json:"grenades,omitempty"` // grenades in the air near the player
	World     *WorldView   `json:"world,omitempty"`    // lakes and bushes; sent once
	Lobby     *LobbyView   `json:"lobby,omitempty"`    // set while waiting for the match to start
	Shots     []ShotView   `json:"shots,omitempty"`    // other players' shots within earshot since the last snapshot
	Chat      []ChatView   `json:"chat,omitempty"`     // quick chat this player hears since the last snapshot
}

type Match struct {
	zones         []captureZone // capture mode: zones A, B and C
	captureFrac   [2]float64    // fractional points per team, so scoring is per second, not per step
	captureScores [2]int        // points per team
	captureWinner int           // 1 or 2 once a team reaches the score limit
	bomb          *bombState    // plant and defuse rounds; nil in other rounds
	rng           *rand.Rand    // the match's own random source: bots and the map use it, so tests can fix it
	mu            sync.Mutex
	ID            string
	players       map[int]*Player
	loot          map[int]*Loot
	obstacles     []Rect
	events        []string
	pings         []Ping
	nextID        int
	started       bool
	over          bool
	winner        string
	zoneX         float64
	zoneY         float64
	zoneR         float64
	zoneLeft      float64
	idleLimit     time.Duration
	idleAt        time.Time // when the match last had zero connected players; zero if it has some
	closed        bool      // set when the match stops; the server removes closed matches
	// OnEnd, if set, is called once with every player's result when the match is decided.
	OnEnd func([]PlayerResult)
	// FillTo is the room size: when the match starts, bots join until it holds this many players.
	FillTo int
	// Room settings, chosen by whoever created the room.
	Max        int         // most players allowed; zero means the server limit
	Mode       string      // "solo", "duo", or empty: duos once there are four or more players
	Private    bool        // hidden from the room list and from quick play; joined by number or invite
	Host       string      // usertag of the creator
	Ranked     bool        // results change trophies; set for public rooms
	elapsed    float64     // seconds of play since the match started
	Practice   bool        // a practice room: bots move about but never shoot
	round      *roundState // set in rounds mode
	rec        *recorder   // set while the match is being recorded for a replay
	nav        *navGrid    // walkable cells for bot routes; rebuilt when obstacles change
	shots      []ShotView  // shots fired since the last broadcast
	chats      []chatMsg   // quick chat sent since the last broadcast
	grenades   []*Grenade
	lakes      []Circle
	bushes     []Circle
	resultSize int // what placements were measured against when the match finished
}

// PlayerResult is one player's outcome, reported when the match ends.
type PlayerResult struct {
	Name    string
	Kills   int
	Place   int
	Damage  int
	Won     bool
	Players int // how many players, bots included, the match had
}

// NewMatch creates a match and starts its clock.
func NewMatch(id string) *Match {
	m := newMatchState(id)
	go m.run()
	return m
}

// newMatchState builds a match with its map, but does not start the clock.
func newMatchState(id string) *Match {
	m := &Match{
		ID:        id,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
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
	// Trees are small solid obstacles. One that would stand inside a building is skipped.
	for i := 0; i < treeN; i++ {
		t := Rect{X: rand.Float64() * (worldSize - treeSize), Y: rand.Float64() * (worldSize - treeSize), W: treeSize, H: treeSize, K: "tree"}
		if !m.blocked(t.X+treeSize/2, t.Y+treeSize/2) {
			m.obstacles = append(m.obstacles, t)
		}
	}
	// Crates are solid cover that can be shot apart; each one drops a pickup when it breaks.
	// Containers are long steel boxes that cannot be destroyed: cover a whole team can hold.
	for i := 0; i < containerN; i++ {
		w, h := containerLen, containerWid
		if rand.Intn(2) == 0 {
			w, h = h, w
		}
		c := Rect{X: rand.Float64() * (worldSize - w), Y: rand.Float64() * (worldSize - h), W: w, H: h, K: "container"}
		if m.rectFree(c) {
			m.obstacles = append(m.obstacles, c)
		}
	}
	for i := 0; i < crateN; i++ {
		kind := crateKinds[i%len(crateKinds)]
		c := Rect{X: rand.Float64() * (worldSize - kind.size), Y: rand.Float64() * (worldSize - kind.size), W: kind.size, H: kind.size, K: "crate", HP: kind.hp}
		if m.rectFree(c) {
			m.obstacles = append(m.obstacles, c)
		}
	}
	m.addTerrain()
	for i := 0; i < 80; i++ {
		m.addLoot()
	}
	return m
}

func (m *Match) newID() int {
	m.nextID++
	return m.nextID
}

// addLoot must be called with m.mu held (or before the match is shared).
func (m *Match) addLoot() {
	id := m.newID()
	l := &Loot{ID: id, X: rand.Float64() * worldSize, Y: rand.Float64() * worldSize}
	if rand.Float64() < cashShare {
		l.K = "cash"
	}
	m.loot[id] = l
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
	p.downed = false
	how := ""
	if headshot {
		how = " (headshot)"
	}
	if m.round != nil { // rounds have no placements: players come back next round
		m.logEvent(fmt.Sprintf("%s eliminated by %s%s", p.Name, by, how))
	} else {
		p.Place = m.countAlive() + 1
		m.logEvent(fmt.Sprintf("%s eliminated by %s%s (#%d)", p.Name, by, how, p.Place))
	}
	if killer != nil {
		killer.revealLeft = bountyTime
		killer.kills++
		killer.ensureAmmo()
		killer.credits += killReward
	}
}

func (m *Match) Join(name string) (*Player, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.over || len(m.players) >= m.capacity() {
		return nil, false
	}
	x, y := m.spawnPoint()
	id := m.newID()
	p := &Player{
		ID: id, Name: name, HP: 100, Alive: true,
		X: x, Y: y,
		send:  make(chan []byte, 16),
		voice: make(chan []byte, voiceQueueDepth),
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
	switch m.Mode {
	case "solo":
		duos = false
	case "duo":
		duos = len(ids) >= 3 // two players in one team would end the match at once
	}
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
		if !m.blocked(x, y) && !m.inWater(x, y) {
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
	if c.Reload {
		p.wantReload = true
	}
	if c.Revive {
		p.wantRevive = true
	}
	if c.Switch > 0 {
		p.switchTo = c.Switch
	}
	if c.Buy > 0 {
		p.wantBuy = c.Buy
	}
	p.echo = c.Echo
	p.Pitch = clamp(c.Pitch, -maxPitch, maxPitch)
	p.crouch = c.Crouch
	if c.Jump {
		p.wantJump = true
	}
	if c.Throw {
		p.wantThrow = true
	}
}

func (m *Match) Start() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || len(m.players) == 0 {
		return false
	}
	// Trophies need a real opponent: a room with fewer than two people is unranked, so nobody can
	// climb the leaderboard by beating bots.
	people := 0
	for _, p := range m.players {
		if p.bot == nil && !p.gone {
			people++
		}
	}
	if people < 2 {
		m.Ranked = false
	}
	m.addBotsLocked(m.FillTo)
	if len(m.players) < 2 {
		return false
	}
	if m.roundsMode() {
		if m.Mode == "bomb" {
			m.bomb = newBomb()
		}
		m.startRounds()
	} else {
		if m.Mode == "capture" {
			m.zones = newCaptureZones()
		}
		m.assignTeams()
		m.groupTeams()
	}
	m.started = true
	return true
}

func (m *Match) Started() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// IsRanked reports whether this match changes trophies.
func (m *Match) IsRanked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Ranked
}

// SetLook sets the avatar look other players see for p.
func (m *Match) SetLook(p *Player, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.look = code
}

// capacity is the most players the room takes. The caller must hold m.mu.
func (m *Match) capacity() int {
	if m.Max > 0 && m.Max < maxPlayers {
		return m.Max
	}
	return maxPlayers
}

// RoomInfo describes a room for the lobby list.
type RoomInfo struct {
	ID      string
	Host    string
	Players int
	Max     int
	Mode    string
	Bots    bool
	Private bool
	Started bool
}

// Info returns the room's settings and how full it is.
func (m *Match) Info() RoomInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	people := 0
	for _, p := range m.players {
		if p.bot == nil && !p.gone {
			people++
		}
	}
	return RoomInfo{
		ID: m.ID, Host: m.Host, Players: people, Max: m.capacity(), Mode: m.Mode,
		Bots: m.FillTo > 0, Private: m.Private, Started: m.started,
	}
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
		if !p.gone && p.bot == nil {
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
	frozen := m.round != nil && m.stepRound(dt)
	if !frozen {
		m.zoneLeft -= dt
		if m.zoneLeft <= 0 {
			m.shrinkZone()
			m.zoneLeft = shrinkEvery
			if m.round != nil {
				m.zoneLeft = roundShrinkEvery
			}
		}
	}
	m.elapsed += dt
	m.thinkBots(dt)
	m.stepCapture(dt)
	m.stepBomb(dt)
	if frozen {
		// Buy phase and the pause between rounds: shopping works, moving and shooting do not.
		for _, p := range m.players {
			p.DX, p.DY, p.wantShoot, p.wantRevive, p.wantJump = 0, 0, false, false, false
			p.wantThrow = false
		}
	}

	for _, p := range m.players {
		if !p.Alive {
			continue
		}
		if p.downed {
			p.crouch, p.Z, p.vz, p.wantJump = false, 0, 0, false
			p.DX, p.DY = 0, 0
			p.wantShoot, p.wantPing, p.wantReload = false, false, false
			p.switchTo = 0
			p.bleed -= dt
			if math.Hypot(p.X-m.zoneX, p.Y-m.zoneY) > m.zoneR {
				m.eliminate(p, "the zone", nil, false)
			} else if p.bleed <= 0 {
				m.eliminate(p, "bleeding out", nil, false)
			} else if !m.canBeRevived(p) {
				m.eliminate(p, "a team wipe", nil, false)
			}
			continue
		}
		speed := moveSpeed
		p.swim = m.inWater(p.X, p.Y)
		p.quiet = math.Min(p.quiet+dt, 10)
		if p.swim { // both hands are busy: no shooting, throwing, crouching, or jumping
			speed *= swimSpeed
			p.wantShoot, p.wantThrow, p.wantJump, p.crouch = false, false, false, false
		} else if p.crouch {
			speed *= crouchSpeed
		}
		p.updateStance(dt)
		nx := clamp(p.X+p.DX*speed*dt, 0, worldSize)
		ny := clamp(p.Y+p.DY*speed*dt, 0, worldSize)
		// Slide along obstacles: try each axis independently.
		if !m.blocked(nx, p.Y) {
			p.X = nx
		}
		if !m.blocked(p.X, ny) {
			p.Y = ny
		}
		p.cooldown -= dt
		p.pingCool -= dt
		m.updateWeapon(p, dt)
		p.throwCool = math.Max(0, p.throwCool-dt)
		if p.wantThrow {
			m.throwGrenade(p)
			p.wantThrow = false
		}

		if p.wantPing && p.pingCool <= 0 {
			m.pings = append(m.pings, Ping{X: p.pingAt.X, Y: p.pingAt.Y, Name: p.Name, Left: pingTime, team: p.Team})
			p.pingCool = pingCooldown
		}
		p.wantPing = false

		if math.Hypot(p.X-m.zoneX, p.Y-m.zoneY) > m.zoneR {
			p.HP -= zoneDPS * dt
			p.hurtCool = regenDelay
		}
		m.tryPickup(p)
		p.hurtCool = math.Max(0, p.hurtCool-dt)
		if p.hurtCool == 0 && p.HP > 0 && p.HP < 100 {
			p.HP = math.Min(100, p.HP+regenRate*dt)
		}

		if p.HP <= 0 {
			m.eliminate(p, "the zone", nil, false)
		}
	}

	m.updateRevives(dt)
	m.stepGrenades(dt)

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

	if m.round != nil {
		m.checkRound()
	} else {
		m.checkWinner()
	}
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
	m.finish(m.winner, len(m.players))
}

// finish ends the match, announces the winner, and reports each real player's result. size is
// what placements are measured against: the player count, or 2 for a two-team match.
func (m *Match) finish(winner string, size int) {
	m.over = true
	m.winner = winner
	if m.winner != "nobody" {
		m.logEvent(fmt.Sprintf("%s wins the match", m.winner))
	}
	m.resultSize = size
	if m.OnEnd != nil {
		results := make([]PlayerResult, 0, len(m.players))
		for _, p := range m.players {
			if p.bot != nil {
				continue
			}
			results = append(results, PlayerResult{
				Name: p.Name, Kills: p.kills, Place: p.Place, Damage: int(p.damage), Won: p.Place == 1, Players: size,
			})
		}
		go m.OnEnd(results)
	}
}

// groupTeams moves each team's members next to their first member, so teammates start together.
func (m *Match) groupTeams() {
	ids := make([]int, 0, len(m.players))
	for id := range m.players {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	anchor := map[int]*Player{}
	for _, id := range ids {
		p := m.players[id]
		a, ok := anchor[p.Team]
		if !ok || p.Team == 0 {
			anchor[p.Team] = p
			continue
		}
		for try := 0; try < 40; try++ {
			ang, d := rand.Float64()*2*math.Pi, 45+rand.Float64()*45
			x := clamp(a.X+math.Cos(ang)*d, playerRadius, worldSize-playerRadius)
			y := clamp(a.Y+math.Sin(ang)*d, playerRadius, worldSize-playerRadius)
			if !m.blocked(x, y) {
				p.X, p.Y = x, y
				break
			}
		}
		if p.bot != nil {
			p.bot.lastX, p.bot.lastY = p.X, p.Y
		}
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
	enter, _, ok := rayRectSpan(ox, oy, dx, dy, r)
	return enter, ok
}

// ensureAmmo fills a player's magazines and reserves the first time they are needed.
func (p *Player) ensureAmmo() {
	if p.armed {
		return
	}
	p.armed = true
	p.credits = startCredits
	// Everyone starts with the pistol. A player created already holding another weapon owns it too.
	for _, i := range []int{0, p.weapon} {
		p.owned[i] = true
		p.mags[i] = weapons[i].Mag
		p.reserves[i] = weapons[i].Reserve
	}
}

// buy handles a purchase for weapon slot (1-based). An unowned weapon is bought, loaded, and
// drawn. An owned one has its reserve ammo refilled for ammoPrice. It reports whether credits
// were spent.
func (p *Player) buy(slot int) bool {
	p.ensureAmmo()
	if slot == armorSlot || slot == grenadeSlot {
		return p.buyGear(slot)
	}
	i := slot - 1
	if i < 0 || i >= len(weapons) {
		return false
	}
	w := weapons[i]
	if p.owned[i] {
		if p.reserves[i] >= w.Reserve || p.credits < ammoPrice {
			return false
		}
		p.credits -= ammoPrice
		p.reserves[i] = w.Reserve
		return true
	}
	if p.credits < w.Price {
		return false
	}
	p.credits -= w.Price
	p.owned[i] = true
	p.mags[i] = w.Mag
	p.reserves[i] = w.Reserve
	p.weapon = i
	p.reloadLeft = 0
	return true
}

// weaponPrices lists each weapon's price by slot, for the shop menu.
var weaponPrices = func() []int {
	out := make([]int, len(weapons))
	for i, w := range weapons {
		out[i] = w.Price
	}
	return out
}()

func (p *Player) ammoView() AmmoView {
	p.ensureAmmo()
	w := weapons[p.weapon]
	frac := 0.0
	if p.reloadLeft > 0 {
		frac = 1 - p.reloadLeft/w.ReloadTime
	}
	return AmmoView{
		Weapon: w.Name, Slot: p.weapon + 1, Mag: p.mags[p.weapon], Reserve: p.reserves[p.weapon],
		Reloading: p.reloadLeft > 0, ReloadFrac: frac, Auto: w.Auto, Range: w.Range,
		Credits: p.credits, Owned: p.owned[:], Prices: weaponPrices, AmmoPrice: ammoPrice,
		Armor: math.Round(p.armor), Grenades: p.grenades, GearPrices: gearPrices,
	}
}

// updateWeapon handles switching, reloading, and firing for one player and one tick.
func (m *Match) updateWeapon(p *Player, dt float64) {
	p.ensureAmmo()
	if p.wantBuy > 0 {
		p.buy(p.wantBuy)
		p.wantBuy = 0
	}
	if p.switchTo > 0 && p.switchTo <= len(weapons) && p.switchTo-1 != p.weapon && p.owned[p.switchTo-1] {
		p.weapon = p.switchTo - 1
		p.reloadLeft = 0
	}
	p.switchTo = 0

	w := weapons[p.weapon]
	if p.reloadLeft > 0 {
		p.reloadLeft -= dt
		if p.reloadLeft <= 0 {
			p.reloadLeft = 0
			take := min(w.Mag-p.mags[p.weapon], p.reserves[p.weapon])
			p.mags[p.weapon] += take
			p.reserves[p.weapon] -= take
		}
	}
	if p.wantReload && p.reloadLeft == 0 && p.mags[p.weapon] < w.Mag && p.reserves[p.weapon] > 0 {
		p.reloadLeft = w.ReloadTime
	}
	p.wantReload = false

	trigger := p.wantShoot && (w.Auto || !p.prevShoot)
	if trigger && p.cooldown <= 0 && p.reloadLeft == 0 {
		if p.mags[p.weapon] > 0 {
			m.fire(p)
			p.cooldown = w.Interval
		} else if p.reserves[p.weapon] > 0 {
			p.reloadLeft = w.ReloadTime
		}
	}
	p.prevShoot = p.wantShoot
	p.wantShoot = false
}

// fire spends one round and shoots the current weapon's pellets with spread and bloom.
func (m *Match) fire(p *Player) {
	p.ensureAmmo()
	w := weapons[p.weapon]
	if p.mags[p.weapon] <= 0 {
		return
	}
	p.mags[p.weapon]--
	p.quiet = 0
	m.recordShot(p, w.Range)

	hitAny, hitHead := false, false
	for i := 0; i < w.Pellets; i++ {
		wobble := w.Spread + p.bloom
		if p.crouch {
			wobble *= crouchAim
		}
		spread := (rand.Float64()*2 - 1) * wobble
		rise := (rand.Float64()*2 - 1) * wobble * 0.5
		hit, head := m.shootRay(p, p.Aim+spread, p.Pitch+rise, w.Range, w.Damage, w.HeadDamage)
		hitAny = hitAny || hit
		hitHead = hitHead || head
	}
	p.bloom = math.Min(w.BloomMax, p.bloom+w.BloomPerShot)
	if hitAny {
		p.hitSeq++
		p.hitKind = "body"
		if hitHead {
			p.hitKind = "head"
		}
	}
}

// shootRay traces one ray. Obstacles stop it, teammates are passed through, and the nearest
// enemy in line takes damage. It reports whether a player was hit and whether it was a headshot.
func (m *Match) shootRay(p *Player, angle, pitch, reach, dmg, headDmg float64) (bool, bool) {
	dx, dy := math.Cos(angle), math.Sin(angle)
	h0, slope := p.eye(), math.Tan(pitch)
	if slope < 0 { // aiming down: the shot ends where it meets the ground
		reach = math.Min(reach, h0/-slope)
	}
	wall, wallAt := reach, -1
	for i, r := range m.obstacles {
		if t, ok := rayStopped(p.X, p.Y, dx, dy, h0, slope, r); ok && t < wall {
			wall, wallAt = t, i
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
		// Where the shot is, measured up from the target's feet, when it reaches them.
		at := h0 + slope*t - q.Z
		if perp <= playerRadius && at >= 0 && at <= q.bodyTop() {
			target, best = q, t
			// A headshot is centred and lands in the head. Bots never score one: an instant
			// kill from a computer feels unfair.
			targetHead = perp <= headRadius && at >= q.headFrom() && p.bot == nil
		}
	}
	if target == nil {
		// Nothing alive in the way: the shot lands on whatever stopped it. A crate takes damage.
		if wallAt >= 0 && m.obstacles[wallAt].K == "crate" {
			m.damageCrate(wallAt, dmg)
			return true, false
		}
		return false, false
	}
	d := dmg
	if targetHead {
		d = headDmg
	}
	m.hurt(target, p, d, targetHead)
	return true, targetHead
}

// crateKinds are the crate sizes, cycled through as the map is built. A small crate is low cover;
// the medium and large ones are tall enough to stand behind, and take more shots to break.
var crateKinds = []struct{ size, hp float64 }{
	{crateSize, crateHP}, {30, 110}, {42, 170}, {30, 110}, {42, 170}, {crateSize, crateHP},
}

// rectFree reports whether a new obstacle would sit clear of every existing one.
func (m *Match) rectFree(c Rect) bool {
	const gap = 6.0
	for _, o := range m.obstacles {
		if c.X < o.X+o.W+gap && c.X+c.W+gap > o.X && c.Y < o.Y+o.H+gap && c.Y+c.H+gap > o.Y {
			return false
		}
	}
	return true
}

// damageCrate wears a crate down. When it breaks it is removed and leaves a pickup behind.
func (m *Match) damageCrate(i int, dmg float64) {
	c := &m.obstacles[i]
	c.HP -= dmg
	if c.HP > 0 {
		return
	}
	id := m.newID()
	l := &Loot{ID: id, X: c.X + c.W/2, Y: c.Y + c.H/2}
	if rand.Float64() < 0.6 {
		l.K = "cash"
	}
	m.loot[id] = l
	m.obstacles = append(m.obstacles[:i], m.obstacles[i+1:]...)
}

// knockDown drops a player when their HP runs out. A teammate can revive them within downedTime.
// A second lethal hit while downed eliminates them.
func (m *Match) knockDown(target, killer *Player, headshot bool) {
	if target.downed || !m.canBeRevived(target) {
		m.eliminate(target, killer.Name, killer, headshot)
		return
	}
	target.HP = 0
	target.downed = true
	target.bleed = downedTime
	target.reviveProg = 0
	m.logEvent(fmt.Sprintf("%s downed by %s", target.Name, killer.Name))
}

// canBeRevived reports whether a player has a teammate still standing. Without one, being downed
// would only delay the elimination, so the player is eliminated at once.
func (m *Match) canBeRevived(p *Player) bool {
	if p.Team == 0 {
		return false
	}
	for _, q := range m.players {
		if q != p && q.Team == p.Team && q.Alive && !q.downed {
			return true
		}
	}
	return false
}

// updateRevives advances revives. A living teammate who is holding revive within reviveRange for
// reviveTime brings a downed player back with revivedHP.
func (m *Match) updateRevives(dt float64) {
	for _, d := range m.players {
		if !d.downed || !d.Alive {
			d.reviveProg = 0
			continue
		}
		reviving := false
		for _, r := range m.players {
			if r == d || !r.Alive || r.downed || r.Team == 0 || r.Team != d.Team || !r.wantRevive {
				continue
			}
			if math.Hypot(r.X-d.X, r.Y-d.Y) <= reviveRange {
				reviving = true
				break
			}
		}
		if !reviving {
			d.reviveProg = 0
			continue
		}
		d.reviveProg += dt
		if d.reviveProg >= reviveTime {
			d.downed = false
			d.HP = revivedHP
			d.bleed = 0
			d.reviveProg = 0
			d.hurtCool = regenDelay
			m.logEvent(d.Name + " was revived")
		}
	}
	for _, p := range m.players {
		p.wantRevive = false
	}
}

// tryPickup collects loot the player is standing on: cash always, a medkit only when hurt.
func (m *Match) tryPickup(p *Player) {
	for id, l := range m.loot {
		if math.Hypot(p.X-l.X, p.Y-l.Y) >= pickupRange {
			continue
		}
		if l.K == "cash" {
			p.ensureAmmo()
			p.credits += cashAmount
		} else if p.HP < 100 {
			p.HP = math.Min(100, p.HP+healAmount)
		} else {
			continue
		}
		delete(m.loot, id)
		m.addLoot()
		return
	}
}

func (p *Player) view() PlayerView {
	return PlayerView{
		Weapon: weapons[p.weapon].Name,
		ID:     p.ID, Name: p.Name, Team: p.Team, X: p.X, Y: p.Y,
		HP: p.HP, Alive: p.Alive, Place: p.Place, Bloom: p.bloom,
		Downed: p.downed, Bleed: p.bleed, Aim: p.Aim, Kills: p.kills, Look: p.look, Crouch: p.crouch && !p.downed, Swim: p.swim, Z: math.Round(p.Z*10) / 10,
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
		if me.gone || me.bot != nil {
			continue
		}
		snap := Snapshot{
			T: "state", Started: m.started, Over: m.over, Winner: m.winner,
			Alive: alive, Zone: z, Me: me.view(),
			Players: []PlayerView{}, Echo: me.echo,
			Events: append([]string{}, m.events...), Revealed: []PlayerView{},
			Pings: []Ping{}, HitSeq: me.hitSeq, HitKind: me.hitKind, Ammo: me.ammoView(),
			Ranked: m.Ranked,
		}
		for _, sh := range m.shots {
			if sh.by != me && math.Hypot(sh.X-me.X, sh.Y-me.Y) <= hearRange {
				snap.Shots = append(snap.Shots, sh)
			}
		}
		if !m.started {
			lv := &LobbyView{Mode: m.Mode, Side: me.wantSide}
			for _, q := range m.players {
				if q.gone {
					continue
				}
				switch q.wantSide {
				case 1:
					lv.Alpha++
				case 2:
					lv.Bravo++
				}
			}
			snap.Lobby = lv
		}
		snap.Chat = m.chatsFor(me)
		snap.Me.Hidden = me.Alive && me.quiet >= bushQuiet && !me.swim && m.inBush(me.X, me.Y)
		if m.round != nil {
			snap.Round = m.roundView(me)
		}
		for _, g := range m.grenades {
			if near(me, g.X, g.Y) {
				snap.Grenades = append(snap.Grenades, g)
			}
		}
		if m.over {
			snap.XP = ComputeXP(me.kills, me.Place, int(me.damage))
			if m.Ranked {
				snap.Trophies = ComputeTrophies(me.Place, me.kills, m.resultSize)
			}
		}
		for _, q := range m.players {
			if q == me || !q.Alive {
				continue
			}
			if q.Team == me.Team && me.Team != 0 || near(me, q.X, q.Y) && !m.concealed(me, q) {
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
		loot, lootSig := []Loot{}, uint64(1)
		for _, l := range m.loot {
			if math.Hypot(me.X-l.X, me.Y-l.Y) <= aoiRadius {
				loot = append(loot, *l)
				lootSig += uint64(l.ID)*2654435761 + 1
			}
		}
		obstacles, obstSig := []Rect{}, uint64(1)
		for _, r := range m.obstacles {
			if math.Hypot(me.X-(r.X+r.W/2), me.Y-(r.Y+r.H/2)) <= aoiRadius+200 {
				obstacles = append(obstacles, r)
				obstSig += uint64(int64(r.X*8))*73856093 ^ uint64(int64(r.Y*8))*19349663 + 1
			}
		}
		if lootSig != me.lootSig {
			snap.Loot = &loot
		}
		if obstSig != me.obstSig {
			snap.Obstacles = &obstacles
		}
		if !me.sentWorld {
			snap.World = m.worldView()
		}
		msg, _ := json.Marshal(snap)
		select {
		case me.send <- msg:
			// Only now does the client have these lists; a dropped snapshot must be sent again.
			me.lootSig, me.obstSig = lootSig, obstSig
			me.sentWorld = true
		default: // slow client: drop this snapshot, the next one supersedes it
		}
	}
	m.shots = m.shots[:0]
	m.chats = m.chats[:0]
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
