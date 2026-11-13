# Arena Royale

A browser battle royale in first person: a Go server runs the match authoritatively, a
WebSocket carries state and inputs, htmx renders the lobby, and three.js draws the arena.

## Features

- Lobby with create/join over htmx (match list refreshes every 2 seconds)
- Server-authoritative simulation at 30 Hz
- Shrinking zone that deals damage outside it; each shrink moves the safe center
- Hitscan shooting with a cooldown
- Loot (health pickups) that respawns
- Obstacles that block movement (with sliding along walls) and stop shots
- Kill feed, placement on elimination, and an end-of-match result screen
- Five weapons with different damage, fire rate, spread, magazine size, and range:
  Pistol (semi-auto), SMG and Rifle (automatic), Shotgun (8 pellets), and Sniper
  (one-shot headshots)
- R to reload, 1–5 or the mouse wheel to switch weapons, and on touch the R and SWAP buttons
- Headshots (100 damage, instant kill) and body shots (25 damage) with hit markers
- Weapon spread that grows while firing and recovers when you stop
- Teams: matches with four or more players start as duos. Teammates see each other,
  share pings, and cannot damage each other.
- Team pings: press Q or tap the minimap to mark a point for your team for 5 seconds.
- Sound effects for shots, hits, headshots, kills, pickups, pings, and zone shrinks,
  synthesized in the browser. Press M to mute.
- Bounty reveal: a player who gets a kill shows on every survivor's minimap and in the
  world for 8 seconds, so kills make you a target
- Interest management: each player only receives nearby players and loot
- First-person 3D client (three.js) with interpolation between server snapshots

## Requirements

- Go 1.24 or newer
- A browser with WebSocket and canvas support

## Run locally

```sh
go run .
```

Open http://localhost:8080, enter a name, and create a match. Open more tabs
(or another browser) to join, then press **Start** once two or more players are in.

Desktop controls: move with WASD or the arrow keys, aim with the mouse, hold the
mouse button or Space to shoot, press Q to ping, and M to mute.

Touch controls: drag the joystick at bottom-left to move, touch and drag elsewhere on
the screen to aim, and hold FIRE at bottom-right to shoot.

## Test

```sh
go test -race ./...
```

The integration tests start the full HTTP and WebSocket server in-process and
drive real clients through a match.

## Layout

| Path | Purpose |
| --- | --- |
| `main.go` | Routes, htmx handlers, WebSocket endpoint |
| `match.go` | Match simulation: movement, zone, combat, loot, snapshots |
| `server_test.go` | End-to-end tests over HTTP and WebSocket |
| `templates/` | Lobby, match list fragment, match page (loads three.js) |
| `static/game3d.js` | three.js first-person renderer, weapon models, overlay HUD, input, sounds |
| `static/vendor/` | Vendored three.js r160, RoomEnvironment, and htmx 2.0.4 (MIT licensed) |
| `.github/workflows/ci.yml` | Format check, vet, race tests, build |

## Known limitations

- Matches with no connected players for 60 seconds are closed and removed.
- Gameplay is minimal: one weapon, no vehicles, and obstacles are randomly placed rectangles.
- The 3D client has not been checked in a real browser yet; it passes a syntax check only.
- Textures and models are generated in code. No photoreal art assets are included.
