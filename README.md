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
- The 3D client is checked in headless Chrome with software rendering only. Frame rate on real phones and laptops is unmeasured.
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

## Look and feel

- Stylized animated look: warm sun with a cool sky fill, soft shadows, a gradient sky dome with
  drifting clouds, a gentle bloom, and painterly grass with flowers and bushes.
- Buildings are pastel blocks with glass windows and colored roofs. Trees are solid: they block
  movement and shots.
- Characters have a big head, blinking eyes, hair and skin tones picked from the usertag, a run
  cycle, breathing, and a lying-down pose when downed. They face where the player aims.
- First-person hands and weapons bob with each step, kick on recoil, and dip on reload and switch.
- The HUD is drawn at device resolution, with a health bar and an ammo panel.
- Phones (coarse pointers) skip the bloom pass and use smaller shadows.

## Levels

When a match ends, each player's kills, placement, and damage are turned into XP and saved to
their profile. The lobby shows level, XP progress, matches, wins, and kills. Levels are cosmetic.

## Bots

- When a match starts, bots join until the room holds 8 players, so one real player can play
  alone. Set `ARENA_BOT_FILL` to change the room size, or to `0` to turn bots off.
- Bots use the same weapons, spread, reloads, line of sight, and zone damage as people, with two
  handicaps: they hold fire for the first five seconds of a match, and they never score headshots. They roam, fight what they can see, pick up medkits when hurt, stay in the zone,
  and revive a downed teammate nearby.
- Bot names contain a space ("Bot Luna"), which usertags cannot, so they never clash.
- Bots earn no XP and are not saved. A room with only bots left closes.
- A player with no teammate standing is eliminated on a lethal hit instead of being downed.

Playing still needs the server: there is no offline mode.

## Shop

- Everyone starts with the pistol and 1,000 credits. An elimination pays 300; gold coins on the
  map pay 250.
- Press **B** (or the SHOP button on touch) to open the armory anywhere, then **1–5** or a tap to
  buy: SMG 900, Shotgun 1,100, Rifle 1,800, Sniper 2,600. Buying an owned weapon refills its
  reserve ammo for 200.
- Only owned weapons can be selected. Bots buy weapons as they can afford them.

## Custom rooms

- The lobby's Custom room form sets the room size (2, 4, 8, 12, or 16), the team mode (auto,
  solo, or duos), whether bots fill empty spots, and whether the room is private.
- Private rooms are left out of the room list and quick play. Share the six-digit room number, or
  invite a friend.
- Play (random team) joins the first open public room, or creates one.

## HUD and map

- Sci-fi HUD: compass strip, segmented health, ammo panel with weapon slots and credits, and a
  kill feed.
- The radar turns with the player and shows nearby buildings, trees, pickups, teammates, pings,
  and the zone edge. **Tab** or **N** (or a tap on the radar) opens the full map of everything
  seen so far; on touch, tap the map to ping.

## Progress and identity

- **Play again:** once you are out or the match ends, one button joins the next public room.
- **Trophies:** public rooms are ranked. A win is +30, second +18, third +10, the top half +4,
  anything lower -8, leaving early -10, plus 2 per elimination. Trophies never drop below zero.
  Private rooms are unranked, and so is any room that starts with fewer than two real players,
  so beating bots alone does not move the leaderboard.
- **Leaderboard:** the lobby lists the ten players with the most trophies.
- **Daily reward:** 150 XP once per UTC day.
- **Looks:** choose hair, skin, shirt colour and a hat in the lobby. Some items unlock by level
  (gold shirt 3, midnight 5, neon 8; cap 2, headband 4, crown 6, halo 10). Other players see your
  look in matches.

## Crates

Wooden crates are solid cover with 60 health. Shooting one breaks it and leaves a coin or a
medkit. Buildings and trees cannot be damaged.

## Deploying

See [DEPLOY.md](DEPLOY.md) for running the server on a Linux machine with HTTPS, with systemd or Docker.

## Rounds mode

Choose **Rounds** as the team setting of a custom room for a team-versus-team match.

- The room splits into two teams, Alpha and Bravo, and bots fill empty spots.
- Each round starts with a 12-second buy phase: the armory opens, and nobody can move or shoot.
- A round ends when one team is wiped out, or after 100 seconds, when the team with more players
  standing (then more total health) takes it. The arena is small and its zone shrinks every 25
  seconds.
- Everyone starts with 1,000 credits. A round win pays 1,400, a loss 900, and an elimination 300.
  Credits carry over. Survivors keep their weapons; anyone who died keeps only the pistol.
- First team to 5 rounds wins the match. If a whole team leaves, the other team wins.

## Replays of bot matches

- **Watch a bot match** in the lobby makes the server play a complete match between bots at full
  speed, record it, and open the recording. The form fields `bots` (2 to 16) and `mode`
  (`solo`, `duo`, `rounds`, or empty) are accepted by `POST /replays/bots`.
- The replay page follows one player from behind. The camera moves to another player when the
  one it follows is eliminated, and to wherever shooting breaks out after a few quiet seconds.
- Controls: the buttons in the status bar, or Space to pause, Left and Right to change player,
  A to hand the camera back to the director, + and - for speed, R to restart.
- Recordings hold the world ten times a second (about 1.5 MB for a short match). The server keeps
  the latest 12 in memory; they are gone after a restart.
- Adding `?capture` to a replay address stops the page from animating by itself, so a script can
  render it frame by frame with `window.__replay.step(ms, speed)` and build a video.

## Cover and teammates

- Crates come in three sizes. Small ones (60 health) are low cover; medium (110) and large (170)
  ones are tall enough to stand behind. All of them break and drop a pickup.
- Shipping containers are long steel boxes that cannot be destroyed.
- Teammates start next to each other. The radar shows each teammate as an arrow with their name,
  pinned to the rim when they are out of range; a squad list under the radar shows their health
  and distance; and a green pointer floats over them in the world, visible through walls.

## Accounts on several devices, limits, and safety

- **Another device:** on a new device choose "Already have an account?", enter the verified email,
  and type the code. Up to five devices can be signed in. "Sign out other devices" and "Sign out
  here" are in the lobby. An account without a verified email cannot sign out of its only device,
  because nothing could bring it back.
- **Rate limits:** sign-in codes, code guesses, usertag attempts, room creation, bot-match
  recordings, friend requests, invites, and reports are capped per device and per address.
- **Backups:** the accounts file is copied on a schedule; see DEPLOY.md.
- **Usertags:** names that pose as staff or bots, or contain blocked words, are refused, including
  simple disguises such as digits for letters.
- **Block and report:** blocking ends a friendship and stops friend requests and invites both
  ways. Reports are saved to a file for the operator; nothing acts on them automatically.
- **Ping:** the radar shows the round trip to the server.
- **Smaller snapshots:** building and pickup lists are sent only when they change.

## Movement prediction

Your own movement is applied on your device at once, using the server's movement rules, and is
corrected from each server snapshot: the client starts from the server's position and replays the
inputs the server has not answered yet. The server still decides every position.

Add `?lag=150` to a match address to delay everything sent and received by that many
milliseconds and feel a slow connection.


## Height: aiming up and down, crouching, jumping

- The mouse (or a touch drag) now aims up and down as well as left and right.
- A shot is a line from the shooter's eyes. Where it lands on the body decides the damage: a
  centred shot in the head is a headshot; too high goes over, too low hits the ground.
- **Crouch** (hold C or Ctrl; CROUCH button on touch): a smaller target, slower movement, and
  steadier aim. **Jump** (Space; JUMP button on touch). Fire moved from Space to the mouse or F.
- Cover has height. A small crate hides a crouching player but not a standing one's head; medium
  and large crates and containers hide a standing player; buildings and trees block everything.
- A downed player lies flat, so you must aim down to finish them.
- Bots aim up and down, and cannot see over cover that is taller than their eyes.

## Lakes, bushes, and grass

- **Lakes:** three per map, away from the middle. In water you swim at half speed, sit low (only
  head and shoulders show), and cannot shoot, throw, crouch, or jump. Nobody spawns in a lake.
- **Bushes:** large enough to stand in. A player in a bush is hidden from enemies more than 70
  units away, until they fire. Bushes do not stop movement or bullets. Bots are fooled too.
- **Grass and flowers** are decoration and are left out of the water.

## Armour, grenades, and the sniper scope

- **Armour** (shop slot 6, 700): 50 points that take half of body damage until used up.
  Headshots ignore it.
- **Grenades** (slot 7, 400 each, carry 2): throw with G or the NADE button. Aim up to throw
  further. They stop at walls and explode after 1.6 seconds, hurting enemies and the thrower, but
  not teammates or anyone behind cover tall enough to hide them. They also break crates.
- **Scope:** with the sniper, hold the right mouse button, press Z, or tap SCOPE.
- Bots buy armour once they own a rifle. They do not throw grenades.
