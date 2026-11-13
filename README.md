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

## Accounts, sign-in, and friends

- Each browser gets a device account on its first visit. The secret cookie is the login,
  so returning to the same browser restores the same profile. Only a hash of the cookie is saved.
- A player picks a **usertag** (3–16 letters, digits, or underscores). It is unique, ignoring case,
  and it is how friends find each other.
- Playing needs a **verified email**. A six-digit code is emailed; it expires after 10 minutes,
  locks after five wrong tries, and can be requested again after 30 seconds. The address is kept
  only as a hash, so one mailbox can link to only one account.
- **Friends** are added by usertag and are mutual.
- Accounts are saved to `arena-data.json` (set `ARENA_DATA` to change the path). Writes are atomic.

Email is configured through environment variables:

| Variable | Purpose |
| --- | --- |
| `ARENA_SMTP_HOST`, `ARENA_SMTP_PORT` (default 587) | SMTP server for sending codes |
| `ARENA_SMTP_USER`, `ARENA_SMTP_PASS` | SMTP login |
| `ARENA_SMTP_FROM` | Sender address |
| `ARENA_DEV_MAIL=1` | Development only: print codes to the server log instead of sending |

With no email settings, sign-in codes are not sent and verification is unavailable.

Production notes: serve over HTTPS and add `Secure` to the cookie, and do not use
`ARENA_DEV_MAIL` on a public server, because the log then holds live codes.

## Health, downing, and revives

- Health recovers at 10 HP per second after five seconds without taking damage.
- When a player's health runs out from a bullet, they are **downed** instead of eliminated. A
  downed player cannot move or shoot.
- A living teammate who holds **E** within 60 units for three seconds revives them with 30 HP.
- A downed player who is not revived within 20 seconds bleeds out. A second lethal hit while downed
  eliminates them, and so does the zone at any time.
- Medkit loot still restores 30 HP. Touch players hold the REVIVE button (bottom right) to revive.

