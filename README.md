# Arena Royale

A browser battle royale: a Go server runs the match authoritatively, a WebSocket
carries state and inputs, htmx renders the lobby, and a canvas client draws the arena.

## Features

- Lobby with create/join over htmx (match list refreshes every 2 seconds)
- Server-authoritative simulation at 30 Hz
- Shrinking zone that deals damage outside it; each shrink moves the safe center
- Hitscan shooting with a cooldown
- Loot (health pickups) that respawns
- Obstacles that block movement (with sliding along walls) and stop shots
- Kill feed, placement on elimination, and an end-of-match result screen
- Interest management: each player only receives nearby players and loot
- Canvas client with interpolation between server snapshots

## Requirements

- Go 1.24 or newer
- A browser with WebSocket and canvas support

## Run locally

```sh
go run .
```

Open http://localhost:8080, enter a name, and create a match. Open more tabs
(or another browser) to join, then press **Start** once two or more players are in.

Desktop controls: move with WASD or the arrow keys, aim with the mouse, and hold the
mouse button or Space to shoot.

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
| `templates/` | Lobby, match list fragment, match page |
| `static/game.js` | Canvas renderer, input, interpolation |
| `.github/workflows/ci.yml` | Format check, vet, race tests, build |

## Known limitations

- Matches with no connected players for 60 seconds are closed and removed.
- Gameplay is minimal: one weapon, no vehicles, and obstacles are randomly placed rectangles.
- The htmx script loads from a CDN.
