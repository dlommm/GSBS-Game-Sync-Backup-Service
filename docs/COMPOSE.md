# Docker Compose deployment

Run GSBS server with persistent SQLite storage and optional Caddy TLS reverse proxy.

## Quick start

1. Copy env template and set a session secret:

```bash
echo 'GSBS_SESSION_SECRET=change-me-to-a-long-random-string' > .env
```

2. Start services:

```bash
docker compose up -d
```

3. Open `http://localhost:8080` (direct) or your Caddy hostname when configured.

## Services

| Service | Purpose |
|---------|---------|
| `gsbs-server` | GSBS API + WebUI on port 8080 |
| `caddy` | TLS termination and reverse proxy (ports 80/443) |

## Volumes

- `gsbs-data` — SQLite database at `/app/data/gsbs.db`
- `caddy-data` / `caddy-config` — Caddy certificates and config state

## TLS

Edit [Caddyfile](Caddyfile) with your domain. Caddy obtains certificates automatically via Let's Encrypt.

## Environment

See [DOCKER.md](DOCKER.md) for full server environment variable reference.

## Client connection

Point `gsbs-client` at your public URL (via Caddy), e.g. `https://gsbs.example.com`.
