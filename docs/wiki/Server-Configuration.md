# Server Configuration

> All environment variables, Docker and Compose setup, TLS/reverse proxy, admin access, storage options, and production hardening for the GSBS server.

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `GSBS_ADDR` | `:8080` | Listen address (e.g. `0.0.0.0:8080`) |
| `GSBS_DB` | `gsbs.db` | Path to the SQLite database. Use a path in a mounted volume for persistence. |
| `GSBS_SESSION_SECRET` | (insecure) | **Required in production.** Signs WebUI session cookies. Generate: `openssl rand -hex 32` |
| `GSBS_ADMIN_USERNAME` | (empty) | If set, only this username can access the `/admin` pages |
| `GSBS_ALLOW_REGISTER` | `true` | Set `false` to block new user registrations in production |
| `GSBS_SAVE_ROOT` | (unset) | When set, saves are stored as files on disk instead of SQLite BLOBs. Recommended: `/app/data/gamesaves` |
| `GSBS_MIGRATE_BLOBS_TO_FS` | (unset) | Set `1` on startup to export existing BLOB saves to `GSBS_SAVE_ROOT` |
| `GSBS_DRY_RUN_MIGRATION` | (unset) | Set `1` to preview schema migrations without writing. Remove after preview. |
| `GSBS_MAX_STORAGE_BYTES` | (unlimited) | Global storage limit in bytes; 0 = unlimited |
| `GSBS_READ_ONLY` | `false` | Set `true` to disable push and delete (pull and read still work) |
| `GSBS_SAVE_VERSION_RETENTION` | `8` | Save versions kept per slot (recommended 5–10) |
| `GSBS_LOG_LEVEL` | `info` | Structured log level: `debug`, `info`, `warn`, `error` |
| `GSBS_TOKEN_MAX_AGE` | `2160h` | Max client token lifetime (default 90 days) |
| `GSBS_TRUST_PROXY` | (unset) | Trust `X-Forwarded-For` / `X-Real-IP` from reverse proxy |
| `GSBS_METRICS_TOKEN` | (unset) | Bearer token required to access `/metrics` |

### Rate limiting

| Variable | Default | Scope |
|---|---|---|
| `GSBS_RATE_LIMIT_AUTH` | `20,1m` | Login/register/TOTP per IP |
| `GSBS_RATE_LIMIT_PUSH` | `120,1m` | Push per user |
| `GSBS_RATE_LIMIT_PULL` | `60,1m` | Pull per user |
| `GSBS_RATE_LIMIT_MANIFEST` | `60,1m` | Manifest fetches |
| `GSBS_RATE_LIMIT_GENERAL` | `300,1m` | General API per user |

### PCGW sync (PCGamingWiki)

| Variable | Default | Description |
|---|---|---|
| `GSBS_PCGW_CRON` | `0 3 * * 0` | Cron expression for incremental PCGW sync (weekly Sunday 03:00). **Overrides** admin Settings when set; use `""` to disable via env. When unset, schedule is configurable from admin Settings. |
| `GSBS_PCGW_FULL_CRON` | (unset) | Optional cron for a full PCGW resync |
| `GSBS_PCGW_RATE_LIMIT` | `2s` | Delay between PCGW HTTP requests |
| `GSBS_PCGW_MAX_PAGES_PER_RUN` | `5000` | Max pages to ingest per sync run. Interrupted runs resume from checkpoint. |
| `GSBS_PCGW_USER_AGENT` | `GSBS/<version> (+https://...)` | User-Agent for PCGamingWiki requests |
| `GSBS_PCGW_STORE_FULL_WIKITEXT` | `true` | Set `false` to skip storing full-page zstd wikitext |

---

## Docker Compose (recommended)

The repository includes ready-to-use Compose files:

| File | Use case |
|---|---|
| `docker-compose.yml` | Production — prebuilt image + Caddy TLS |
| `docker-compose.dev.yml` | Local dev — build from source, port 8080 |

**Production startup:**

```bash
cp .env.example .env   # set GSBS_SESSION_SECRET
docker compose up -d
```

**Local development:**

```bash
docker compose -f docker-compose.dev.yml up --build
```

Open `http://localhost:8080`.

See the [compose-caddy.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/compose-caddy.yml), [compose-nginx.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/compose-nginx.yml), and [compose-traefik.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/compose-traefik.yml) examples for reverse proxy alternatives.

---

## TLS and reverse proxy

The server does not terminate TLS. Run it behind a reverse proxy that handles HTTPS.

**Caddy (recommended — automatic HTTPS):**

```text
your-domain.com {
  reverse_proxy localhost:8080
}
```

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name your-domain.com;
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

**Traefik:** See [compose-traefik.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/compose-traefik.yml).

Set `GSBS_TRUST_PROXY=1` when behind a proxy so the server trusts `X-Forwarded-For` for rate limiting.

---

## Data persistence

All server state lives in a single SQLite database (`GSBS_DB`). Mount it to a Docker volume so data persists across container restarts:

```yaml
volumes:
  - gsbs-data:/app/data
environment:
  GSBS_DB: /app/data/gsbs.db
  GSBS_SAVE_ROOT: /app/data/gamesaves  # recommended
```

With `GSBS_SAVE_ROOT` set, file bytes are stored on disk under that directory; SQLite holds metadata only. Without it, saves are stored as inline BLOBs (legacy, works but grows the DB file).

---

## Storage backup

> **Warning:** The server uses SQLite WAL mode. A plain `cp gsbs.db` while running may produce an inconsistent backup. Use one of these safe methods.

**Option A — stop the container first:**

```bash
docker stop gsbs
cp /path/to/data/gsbs.db /path/to/backup/
cp /path/to/data/gsbs.db-wal /path/to/backup/ 2>/dev/null || true
docker start gsbs
```

**Option B — live backup (no downtime):**

```bash
sqlite3 /path/to/data/gsbs.db "VACUUM INTO '/path/to/backup/gsbs-backup.db'"
```

Also back up `GSBS_SAVE_ROOT` if set (it holds save file bytes outside SQLite).

---

## Health checks

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /api/health` | None | Liveness — returns `{"status":"ok"}` |
| `GET /api/health?ready=1` | None | Readiness — runs a 2s DB check; 503 if DB is slow/down |

Docker Compose health check:

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "-O", "-", "http://localhost:8080/api/health"]
  interval: 30s
  timeout: 5s
  retries: 3
  start_period: 15s
```

---

## Admin WebUI

The admin interface is available at `/admin` (session required + must match `GSBS_ADMIN_USERNAME` if set).

| Route | Description |
|---|---|
| `/admin` | Overview — SVG charts, global stats, server config |
| `/admin/users` | User list, client-count bars, revoke client tokens |
| `/admin/manifest` | Server manifest search and pagination |
| `/admin/activity` | Jobs, manifest fetches, audit log, stats snapshots |
| `/admin/settings` | PCGW cron, filters, first-start auto sync |
| `/admin/analytics` | Storage, active clients, sync volume, PCGW coverage |
| `/admin/pcgw` | PCGW catalog search, sync controls, per-game detail, export/import |

![Admin overview](https://raw.githubusercontent.com/dlommm/GSBS--Game-Sync---Backup-Service-/main/docs/images/screenshots/admin-overview.png)

**PCGW sync (admin):**

| Action | How |
|---|---|
| Run incremental sync | Admin → PCGW → **Sync** |
| Force full resync | Admin → PCGW → **Full sync** |
| Rebuild manifest only | Admin → PCGW → **Rebuild manifest** |
| Export manifest bundle | `GET /admin/pcgw/export/manifest.json.gz` |
| Import bundle | Admin → PCGW → **Import** |
| Push manifest to clients | Admin → **Push manifest** (sends SSE event) |

---

## Production hardening checklist

- [ ] Set `GSBS_SESSION_SECRET` to 32+ random bytes.
- [ ] Set `GSBS_ALLOW_REGISTER=false` after creating your accounts.
- [ ] Set `GSBS_ADMIN_USERNAME` to restrict admin access.
- [ ] Run behind HTTPS (Caddy, nginx, or Traefik).
- [ ] Set `GSBS_TRUST_PROXY=1` if behind a reverse proxy.
- [ ] Mount `GSBS_DB` and `GSBS_SAVE_ROOT` to a persistent volume.
- [ ] Pin the server to a specific Docker tag in production (not `:latest`).
- [ ] Set up a backup schedule for the SQLite database.
- [ ] Configure `GSBS_PCGW_CRON` or leave unset to use admin Settings.

---

## Image security

- Runtime image: **Alpine 3.23.4** with `apk upgrade` at build time.
- Server runs as non-root user `gsbs` (UID/GID 1000).
- The Docker entrypoint ensures `/app/data` is owned by `gsbs` before dropping privileges.
- Go dependencies: built with current `golang.org/x/crypto` and `golang.org/x/sys`.
- After a fresh build with current `go.mod`: expect **0 Critical / 0 High** from Go modules.

---

## Related pages

- [Installation](Installation)
- [How It Works](How-It-Works)
- [Upgrading](Upgrading)
- [API Reference](API-Reference)
- [Troubleshooting](Troubleshooting)
