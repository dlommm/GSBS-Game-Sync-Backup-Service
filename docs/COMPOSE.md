# Docker Compose deployment

Run GSBS server with persistent SQLite storage and Caddy TLS reverse proxy.

## Quick start

1. Copy the env template and set a session secret:

```bash
cp .env.example .env
# Edit .env — GSBS_SESSION_SECRET must be a long random string
```

2. For **local HTTP testing**, either:
   - Use [docker-compose.dev.yml](../docker-compose.dev.yml) (`docker compose -f docker-compose.dev.yml up --build`) — server on `http://localhost:8080`, or
   - Uncomment the `:80` block in [Caddyfile](Caddyfile) and use `docker compose up -d`.

3. For **production with TLS**, edit [Caddyfile](Caddyfile) with your domain, ensure DNS points to the host, then:

```bash
docker compose up -d
```

## Services

| Service | Purpose |
|---------|---------|
| `gsbs-server` | GSBS API + WebUI (internal port 8080) |
| `caddy` | TLS termination and reverse proxy (ports 80/443) |

The production compose file does **not** expose port 8080 on the host; traffic goes through Caddy. Use `docker-compose.dev.yml` for direct access during development.

## Health checks

`gsbs-server` exposes `/api/health`. Caddy starts only after the server is healthy (`depends_on` with `condition: service_healthy`).

## Volumes

- `gsbs-data` — SQLite database at `/app/data/gsbs.db`
- `caddy-data` / `caddy-config` — Caddy certificates and config state

## TLS

Edit [Caddyfile](Caddyfile) with your domain. Caddy obtains certificates automatically via Let's Encrypt.

For local HTTP without TLS, uncomment the `:80` server block in the Caddyfile.

## Reverse proxy alternatives

Copy-paste examples in [docs/examples/](examples/):

| File | Proxy |
|------|-------|
| [compose-caddy.yml](examples/compose-caddy.yml) | Caddy (same as root compose) |
| [compose-nginx.yml](examples/compose-nginx.yml) | nginx |
| [compose-traefik.yml](examples/compose-traefik.yml) | Traefik v3 + Let's Encrypt |
| [compose-unraid.yml](examples/compose-unraid.yml) | Unraid — port 8080, no proxy ([UNRAID.md](examples/UNRAID.md)) |

## Unraid

No reverse proxy: use [examples/compose-unraid.yml](examples/compose-unraid.yml) (all settings inline, no `.env`). See [examples/UNRAID.md](examples/UNRAID.md).

## Environment

See [DOCKER.md](DOCKER.md) for the full server environment variable reference. Required for production compose: `GSBS_SESSION_SECRET` in `.env`.

## Client connection

Point `gsbs-client` at your public URL (via Caddy), e.g. `https://gsbs.example.com`. See [INSTALL.md](INSTALL.md) for client setup.
