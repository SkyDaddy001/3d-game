package main

import (
	"math"
	"testing"
)

// Starting a match fills the room with bots, so one real player is enough to play.
func TestBotsFillTheRoomAtStart(t *testing.T) {
	m := NewMatch("bots01")
	m.mu.Lock()
	m.FillTo = 8
	m.mu.Unlock()
	human, ok := m.Join("solo_player")
	if !ok {
		t.Fatal("join failed")
	}
	if !m.Start() {
		t.Fatal("a single player should be able to start a bot-filled match")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bots, names := 0, map[string]bool{}
	for _, p := range m.players {
		if names[p.Name] {
			t.Fatalf("duplicate name %q", p.Name)
		}
		names[p.Name] = true
		if p.bot != nil {
			bots++
			if nameRE.MatchString(p.Name) {
				t.Fatalf("bot name %q could be mistaken for a usertag", p.Name)
			}
			if p.Team == 0 {
				t.Fatalf("bot %q has no team", p.Name)
			}
		}
	}
	if len(m.players) != 8 || bots != 7 {
		t.Fatalf("room: %d players, %d bots; want 8 and 7", len(m.players), bots)
	}
	if human.bot != nil {
		t.Fatal("the human must not be a bot")
	}
}

// Without bot fill, one player cannot start a match alone.
func TestNoBotsMeansTwoPlayersNeeded(t *testing.T) {
	m := NewMatch("bots02")
	m.Join("alone")
	if m.Start() {
		t.Fatal("one player with no bots should not start")
	}
}

// A bot that sees an enemy aims at them and, after its reaction time, hurts them.
func TestBotFightsVisibleEnemy(t *testing.T) {
	m := newArena()
	m.started = true
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	enemy := &Player{ID: 2, Name: "e", Team: 2, X: 650, Y: 500, Alive: true, HP: 100}
	m.players[1], m.players[2] = bot, enemy

	for i := 0; i < 30*4; i++ {
		m.step(1.0 / 30)
	}
	if enemy.HP != 100 {
		t.Fatal("bots should hold fire during the opening grace period")
	}
	for i := 0; i < 30*8 && enemy.Alive && enemy.HP == 100; i++ {
		m.step(1.0 / 30)
	}
	if enemy.HP == 100 {
		t.Fatal("the bot never hit a visible enemy after the grace period")
	}
}

// A wall between them hides the enemy, so the bot holds fire.
func TestBotDoesNotShootThroughWalls(t *testing.T) {
	m := newArena()
	m.started = true
	m.obstacles = []Rect{{X: 560, Y: 300, W: 30, H: 400}}
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	enemy := &Player{ID: 2, Name: "e", Team: 2, X: 650, Y: 500, Alive: true, HP: 100}
	m.players[1], m.players[2] = bot, enemy
	bot.ensureAmmo()
	full := bot.mags

	m.thinkBots(1.0 / 30)
	m.thinkBots(1.0)
	if bot.wantShoot || bot.mags != full {
		t.Fatal("the bot should not fire at an enemy behind a wall")
	}
}

// A bot outside the safe zone walks back toward it.
func TestBotReturnsToZone(t *testing.T) {
	m := newArena()
	m.started = true
	m.zoneR = 300
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 1500, Y: 1000, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	m.players[1] = bot
	before := math.Hypot(bot.X-m.zoneX, bot.Y-m.zoneY)
	for i := 0; i < 30; i++ {
		m.step(1.0 / 30)
	}
	after := math.Hypot(bot.X-m.zoneX, bot.Y-m.zoneY)
	if after >= before-100 {
		t.Fatalf("bot should close on the zone: distance %v -> %v", before, after)
	}
}

// A bot revives a downed teammate standing next to it.
func TestBotRevivesTeammate(t *testing.T) {
	m := newArena()
	m.started = true
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	mate := &Player{ID: 2, Name: "mate", Team: 1, X: 620, Y: 500, Alive: true, downed: true, bleed: downedTime}
	m.players[1], m.players[2] = bot, mate
	for i := 0; i < 30*8 && mate.downed; i++ {
		m.step(1.0 / 30)
	}
	if mate.downed || !mate.Alive {
		t.Fatal("the bot should walk over and revive its downed teammate")
	}
}

// With a teammate standing, a lethal hit downs the player; a second one eliminates.
func TestDownedOnlyWithATeammateStanding(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	shooter := &Player{ID: 1, Name: "a", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 25}
	mate := &Player{ID: 3, Name: "c", Team: 2, X: 900, Y: 900, Alive: true, HP: 100}
	m.players[1], m.players[2], m.players[3] = shooter, target, mate

	m.fire(shooter)
	if !target.downed || !target.Alive {
		t.Fatal("with a teammate standing, the first lethal hit should down the target")
	}
	m.fire(shooter)
	if !target.Alive {
		t.Fatal("a level shot should pass over a player lying on the ground")
	}
	shooter.Pitch = pitchTo(shooter, target) // aim down at them
	shooter.bloom = 0
	m.fire(shooter)
	if target.Alive {
		t.Fatal("a second lethal hit should eliminate the downed target")
	}
}

// Bots are not counted as connected players, so a room with only bots closes.
func TestBotsDoNotKeepAMatchOpen(t *testing.T) {
	m := newArena()
	m.players[1] = &Player{ID: 1, Name: "Bot A", Team: 1, Alive: true, HP: 100, bot: &botState{}}
	m.idleLimit = 0
	m.trackIdle()
	m.trackIdle()
	if !m.closed {
		t.Fatal("a match with only bots should close")
	}
}

// A room of bots plays to a finish: the zone and the fighting leave one team standing.
func TestBotMatchReachesAWinner(t *testing.T) {
	m := newArena()
	m.zoneR = 1500
	m.zoneLeft = shrinkEvery
	m.obstacles = []Rect{{X: 900, Y: 900, W: 120, H: 80}, {X: 600, Y: 1200, W: 80, H: 140}, {X: 1300, Y: 700, W: 10, H: 10, K: "tree"}}
	m.addBotsLocked(8)
	m.assignTeams()
	m.started = true
	const dt = 1.0 / 30
	for i := 0; i < 30*60*15 && !m.over; i++ {
		m.step(dt)
		m.checkWinner()
	}
	if !m.over {
		t.Fatalf("bot match still running after 15 simulated minutes, %d alive", m.countAlive())
	}
}

// A bot's shot through the head counts as a body shot, never an instant kill.
func TestBotsCannotHeadshot(t *testing.T) {
	m := &Match{players: map[int]*Player{}}
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 0, Y: 50, Alive: true, HP: 100, Aim: 0, bot: &botState{}}
	target := &Player{ID: 2, Name: "b", Team: 2, X: 100, Y: 50, Alive: true, HP: 100}
	m.players[1], m.players[2] = bot, target
	m.fire(bot)
	if target.HP != 100-weapons[0].Damage {
		t.Fatalf("bot shot dead centre: HP %v, want a body shot", target.HP)
	}
}

// A room with one real player is unranked; two real players keep it ranked.
func TestRankedNeedsTwoRealPlayers(t *testing.T) {
	solo := NewMatch("rank01")
	solo.mu.Lock()
	solo.FillTo, solo.Ranked = 8, true
	solo.mu.Unlock()
	solo.Join("only_one")
	solo.Start()
	if solo.IsRanked() {
		t.Fatal("one person against bots should not be ranked")
	}

	duo := NewMatch("rank02")
	duo.mu.Lock()
	duo.FillTo, duo.Ranked = 8, true
	duo.mu.Unlock()
	duo.Join("first_p")
	duo.Join("second_p")
	duo.Start()
	if !duo.IsRanked() {
		t.Fatal("two people in a public room should be ranked")
	}
}

// In the opening phase a bot ignores a distant enemy and goes for coins instead.
func TestBotsLootBeforeHunting(t *testing.T) {
	m := newArena()
	m.started = true
	m.elapsed = botGrace + 1
	bot := &Player{ID: 1, Name: "Bot A", Team: 1, X: 500, Y: 500, Alive: true, HP: 100, bot: &botState{strafeDir: 1}}
	enemy := &Player{ID: 2, Name: "e", Team: 2, X: 800, Y: 500, Alive: true, HP: 100}
	m.players[1], m.players[2] = bot, enemy
	m.loot[9] = &Loot{ID: 9, X: 500, Y: 700, K: "cash"}
	// Stop as soon as the coin is taken: the next coin appears somewhere random.
	for i := 0; i < 30*3 && m.loot[9] != nil; i++ {
		m.step(1.0 / 30)
	}
	if enemy.HP != 100 {
		t.Fatal("a bot attacked a distant enemy during the loot phase")
	}
	if _, still := m.loot[9]; still {
		t.Fatalf("the bot did not collect the coin 200 units away in three seconds (at %.0f, %.0f)", bot.X, bot.Y)
	}

	// Once the phase is over, the same enemy is fair game.
	m.elapsed = botLootPhase + 1
	bot.X, bot.Y = 600, 500 // coin hunting may have led it away; put it back in view
	for i := 0; i < 30*8 && enemy.HP == 100; i++ {
		m.step(1.0 / 30)
	}
	if enemy.HP == 100 {
		t.Fatal("after the loot phase the bot should fight a visible enemy")
	}
}

// Bots start well apart from each other.
func TestBotsSpawnSpreadOut(t *testing.T) {
	m := newArena()
	m.zoneR = 1500
	m.addBotsLocked(8)
	closest := math.Inf(1)
	for _, a := range m.players {
		for _, b := range m.players {
			if a != b {
				closest = math.Min(closest, math.Hypot(a.X-b.X, a.Y-b.Y))
			}
		}
	}
	if closest < 200 {
		t.Fatalf("two bots spawned only %.0f apart", closest)
	}
}

// Teammates start next to each other, even though teams start far apart.
func TestTeammatesStartTogether(t *testing.T) {
	m := NewMatch("group1")
	m.mu.Lock()
	m.FillTo, m.Mode = 8, "duo"
	m.mu.Unlock()
	m.Join("lone_human")
	m.Start()
	m.mu.Lock()
	defer m.mu.Unlock()
	byTeam := map[int][]*Player{}
	for _, p := range m.players {
		byTeam[p.Team] = append(byTeam[p.Team], p)
	}
	for team, ps := range byTeam {
		if len(ps) != 2 {
			t.Fatalf("team %d has %d players", team, len(ps))
		}
		if d := math.Hypot(ps[0].X-ps[1].X, ps[0].Y-ps[1].Y); d > 100 {
			t.Fatalf("team %d starts %.0f apart", team, d)
		}
	}
}
