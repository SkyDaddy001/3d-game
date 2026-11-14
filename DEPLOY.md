# Putting Arena Royale online

The server is one static binary with the pages and game files embedded. It needs a Linux machine
with a public address, a domain name, HTTPS, and an SMTP account for sign-in emails.

## What you need first

1. A server (any small Linux VM; Oracle Cloud's free tier is enough to start).
2. A domain or subdomain with an A record pointing at the server.
3. SMTP details from an email provider, for the six-digit sign-in codes.
4. Ports 80 and 443 open in the cloud firewall **and** the server's own firewall.

## Option A: binary + systemd + Caddy

```sh
# On your own machine: build for the server (use GOARCH=arm64 for ARM servers such as Ampere).
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o arena-server .
scp arena-server deploy/arena.service deploy/arena.env.example deploy/Caddyfile you@server:

# On the server:
sudo useradd --system --home /var/lib/arena --create-home arena
sudo install -D arena-server /opt/arena/arena-server
sudo install -m 600 arena.env.example /etc/arena.env      # then edit it
sudo cp arena.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now arena

# HTTPS: install Caddy (https://caddyserver.com/docs/install), then
sudo cp Caddyfile /etc/caddy/Caddyfile                     # edit the domain first
sudo systemctl reload caddy
```

Check it: `systemctl status arena`, `journalctl -u arena -f`, then open `https://your-domain`.

## Option B: Docker

```sh
docker build -t arena-royale .
docker run -d --name arena --restart unless-stopped \
  -p 127.0.0.1:8080:8080 -v arena-data:/data --env-file /etc/arena.env arena-royale
```

Put Caddy (or any HTTPS reverse proxy) in front, as in option A. With Docker, set
`ARENA_ADDR=:8080` and `ARENA_DATA=/data/arena-data.json` in the env file.

## Settings

| Variable | Meaning |
| --- | --- |
| `ARENA_ADDR` | Listen address. Default `:8080`. Use `127.0.0.1:8080` behind a proxy. |
| `ARENA_DATA` | Path of the accounts file. Back this file up. |
| `ARENA_SECURE_COOKIE` | Set to `1` when the site is on HTTPS. |
| `ARENA_BOT_FILL` | Room size bots fill to. `0` turns bots off. |
| `ARENA_SMTP_*` | Email server for sign-in codes (see `deploy/arena.env.example`). |
| `ARENA_DEV_MAIL` | `1` prints codes to the log. Development only; never on a public server. |

## Before inviting players

- Fill in the contact email and city in `templates/terms.html` and `templates/privacy.html`, and
  have a lawyer review both.
- Back up the accounts file on a schedule; it is the only copy of every profile.
- All accounts and matches live in one process. Restarting the server ends running matches.
  One process on one machine is the limit until accounts move to a database.
- `go.sum` was created without the checksum database in the build sandbox. Run
  `go mod verify` once on a normal connection to confirm the dependency.
