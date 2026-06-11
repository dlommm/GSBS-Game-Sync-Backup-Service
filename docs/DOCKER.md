# Deploying GSBS Server with Docker

This guide covers building and running the GSBS server in Docker, plus optional production setup (volumes, env, reverse proxy).

## Quick start

### Build the image

From the repository root (where `Dockerfile` and `go.mod` are):

```bash
docker build -t gsbs-server:latest .
```

### Run the server

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret-at-least-32-chars" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

- **Port**: The server listens on `:8080` inside the container. `-p 8080:8080` exposes it on the host.
- **Database**: Set the DB path to a volume mount so data persists. The server uses `GSBS_DB` (default `gsbs.db`), so use a path inside the container that is backed by a volume, e.g. `/app/data/gsbs.db`.
- **Session secret**: Set `GSBS_SESSION_SECRET` in production so session cookies are signed. Omit or leave empty only for local testing.

### Run with database in a volume (recommended)

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret-at-least-32-chars" \
  -e GSBS_DB="/app/data/gsbs.db" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

Create the named volume if needed (Docker creates it on first use). The SQLite file will be stored in `gsbs-data` and persist across container restarts.

### Data persistence: DB and manifest

All server state lives in a single SQLite database:

- **Users and clients** — accounts, tokens, last-seen
- **Saves** — uploaded save data (per user/game/path_key). With `GSBS_SAVE_ROOT` set, file bytes live under that directory; otherwise they are stored as BLOBs in SQLite.
- **Manifest (game save locations)** — table `game_save_locations`, filled by the PCGW sync job or manual import; served at `GET /api/manifest`
- **Job runs and manifest fetch log** — for the admin UI

There is no separate “manifest file” on disk: the manifest is stored in the DB. As long as `GSBS_DB` points to a path **on a mounted volume** (e.g. `GSBS_DB=/app/data/gsbs.db` with `-v gsbs-data:/app/data`), everything persists across container restarts and recreates. You do **not** need to re-download or re-sync the manifest when you recreate the container; clients will get the same manifest from the same DB.

**Summary:** Use one volume for `/app/data`, set `GSBS_DB=/app/data/gsbs.db`, and (recommended) `GSBS_SAVE_ROOT=/app/data/gamesaves`. User data, saves, and manifest stay intact when you replace the container.

---

## Pushing to Docker Hub

1. **Log in** (use your Docker Hub username and password or a [Personal Access Token](https://hub.docker.com/settings/security)):
   ```bash
   docker login
   ```

2. **Build and tag** the image with your Docker Hub username and repository name:
   ```bash
   docker build -t YOUR_DOCKERHUB_USERNAME/gsbs-server:latest .
   docker tag YOUR_DOCKERHUB_USERNAME/gsbs-server:latest YOUR_DOCKERHUB_USERNAME/gsbs-server:2.0.0   # optional version tag
   ```
   Replace `YOUR_DOCKERHUB_USERNAME` with your actual Docker Hub username.

3. **Push** the image:
   ```bash
   docker push YOUR_DOCKERHUB_USERNAME/gsbs-server:latest
   docker push YOUR_DOCKERHUB_USERNAME/gsbs-server:2.0.0   # if you tagged a version
   ```

**Releases:** Push a git tag `vX.Y.Z` to trigger [.github/workflows/release.yml](../.github/workflows/release.yml) (binaries, installer, `.deb`, AppImage, GitHub Release, Docker Hub). CI on `main` does not build Docker. Local fallback: `./script/release.sh [VERSION]`. See [docs/RELEASE.md](RELEASE.md).

**Pre-built image:** The official image is published at [dendlomm/gsbs-server](https://hub.docker.com/r/dendlomm/gsbs-server) on Docker Hub. Releases are built for **linux/amd64** and **linux/arm64**. Anyone can run:
```bash
docker pull dendlomm/gsbs-server:latest
docker run -d -p 8080:8080 -e GSBS_SESSION_SECRET=xxx -v gsbs-data:/app/data -e GSBS_DB=/app/data/gsbs.db dendlomm/gsbs-server:latest
```
If you see *no matching manifest for linux/amd64* (e.g. an older image was pushed only for one arch), build locally: `docker build -t gsbs-server:latest .` and use `gsbs-server:latest` in your compose or run command.

---

## Environment variables

| Variable | Default | Description |
|---------|---------|-------------|
| `GSBS_ADDR` | `:8080` | Listen address inside the container (e.g. `:8080` or `0.0.0.0:8080`). |
| `GSBS_DB` | `gsbs.db` | Path to the SQLite database file. Use a path under a mounted volume for persistence. |
| `GSBS_SAVE_ROOT` | (unset) | When set, save file bytes are stored on disk under this directory (metadata stays in SQLite). Recommended: `/app/data/gamesaves` on the same volume as `GSBS_DB`. When unset, saves are stored as BLOBs in the database (legacy behavior). |
| `GSBS_MIGRATE_BLOBS_TO_FS` | (unset) | Set to `1` on startup to export existing BLOB saves to files under `GSBS_SAVE_ROOT` (requires `GSBS_SAVE_ROOT`). |
| `GSBS_DRY_RUN_MIGRATION` | (unset) | Set to `1` to preview the 2.0 schema migration (logs row counts and warnings) without writing any changes. Remove after previewing. |
| `GSBS_SESSION_SECRET` | (insecure default) | Secret used to sign WebUI session cookies. **Set in production.** Expired browser sessions are purged automatically on startup and daily (no extra env var). |
| `GSBS_ADMIN_USERNAME` | (empty) | If set, only this user can access the `/admin` page (stats and revoke client tokens). |
| `GSBS_MAX_STORAGE_BYTES` | (unlimited) | Global storage limit in bytes; 0 or unset = unlimited. |
| `GSBS_READ_ONLY` | `false` | Set to `true` or `1` to disable push and delete (pull and read still work). |
| `GSBS_SAVE_VERSION_RETENTION` | `8` | Save versions kept per slot (5–10). |
| `GSBS_LOG_LEVEL` | `info` | Structured log level: `debug`, `info`, `warn`, `error`. |
| `GSBS_RATE_LIMIT_AUTH` | `20,1m` | Login/register/TOTP rate limit per IP. |
| `GSBS_RATE_LIMIT_PUSH` | `120,1m` | Push rate limit per user. |
| `GSBS_RATE_LIMIT_PULL` | `60,1m` | Pull rate limit per user. |
| `GSBS_RATE_LIMIT_MANIFEST` | `60,1m` | Manifest rate limit. |
| `GSBS_RATE_LIMIT_GENERAL` | `300,1m` | General API rate limit per user. |
| `GSBS_TRUST_PROXY` | (unset) | When set, trust `X-Forwarded-For` / `X-Real-IP` for client IP. |
| `GSBS_TOKEN_MAX_AGE` | `2160h` | Max client token age (90 days). |
| `GSBS_METRICS_TOKEN` | (unset) | Bearer token required for `/metrics` when set. |
| `GSBS_PCGW_CRON` | `0 3 * * 0` | Cron expression for PCGW incremental sync. **Overrides** admin Settings when set (including `""` to disable). When unset, schedule comes from admin Settings (`pcgw_cron`, default weekly Sunday 03:00). |
| `GSBS_PCGW_FULL_CRON` | (unset) | Cron expression for a scheduled full PCGW catalog rescan (now implemented). Schedules `RunPCGWSyncFull` in addition to the normal incremental cron. |
| `GSBS_PCGW_FULL_CATALOG_DAYS` | `7` | Days between forced full catalog scans during incremental sync. Set to `0` to disable. |
| `GSBS_PCGW_RATE_LIMIT` | `2s` | Delay between PCGW HTTP requests. |
| `GSBS_PCGW_USER_AGENT` | `GSBS/<version> (+https://github.com/…)` | User-Agent sent to PCGamingWiki. |
| `GSBS_PCGW_STORE_FULL_WIKITEXT` | `true` | When `false`, skip storing zstd full-page wikitext (section text still stored). |
| `GSBS_PCGW_MAX_PAGES_PER_RUN` | `5000` | Maximum number of pages to ingest in Phase 2 per sync run. Interrupted runs resume from checkpoint on next run. |

Example with all options:

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_ADDR=":8080" \
  -e GSBS_DB="/app/data/gsbs.db" \
  -e GSBS_SAVE_ROOT="/app/data/gamesaves" \
  -e GSBS_SESSION_SECRET="your-long-random-secret" \
  -e GSBS_ADMIN_USERNAME="admin" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

---

## Docker Compose

The repository includes production and development compose files:

| File | Use case |
|------|----------|
| [docker-compose.yml](../docker-compose.yml) | Production: prebuilt image + Caddy TLS |
| [docker-compose.dev.yml](../docker-compose.dev.yml) | Local dev: build from source, port 8080 |

**Production:**

```bash
cp .env.example .env   # set GSBS_SESSION_SECRET
docker compose up -d
```

Edit [docs/Caddyfile](../docs/Caddyfile) with your domain. See [COMPOSE.md](COMPOSE.md) for TLS, health checks, and nginx/Traefik examples.

**Local development:**

```bash
docker compose -f docker-compose.dev.yml up --build
```

Open `http://localhost:8080`.

---

## Image security

The runtime image is based on **`alpine:3.23.4`** with `apk upgrade` applied at build time. Rebuild and push a new tag after Dockerfile changes so Docker Scout reflects fixes.

- **Non-root:** The server runs as user `gsbs` (UID/GID 1000). The entrypoint (`script/docker-entrypoint.sh`) ensures `/app/data` exists and is owned by `gsbs` before dropping privileges — this fixes existing Docker volumes created when the container ran as root.
- **Go dependencies:** Server binary is built with current `golang.org/x/crypto` and `golang.org/x/sys` (see `go.mod`). Rebuild the image after `go.mod` changes so Scout does not flag stale module versions embedded in the binary.
- **Health checks:** Use BusyBox `wget` (included in the base image; no separate `wget` package). The Dockerfile includes a `HEALTHCHECK` on `GET /api/health`.
- **Docker Scout baseline:** After a fresh build with current `go.mod` and Dockerfile, expect **0 Critical / 0 High** from Go modules. One **Medium** BusyBox CVE ([CVE-2025-60876](https://scout.docker.com/v/CVE-2025-60876)) may remain until Alpine ships a fixed `busybox` package — health checks use localhost-only `wget`, so practical exposure is low. Monitor [Alpine security tracker](https://security.alpinelinux.org/).
- **Published image:** `dendlomm/gsbs-server:latest` on Docker Hub is updated only on release (git tag push). Local Dockerfile changes alone do not update Hub; cut a release to publish fixes.

## Production tips

### TLS / HTTPS

The server does not terminate TLS. In production, run it behind a reverse proxy (Caddy, Nginx, Traefik) that handles HTTPS and forwards to the container.

Example with Caddy (add to your Caddyfile):

```text
your-domain.com {
  reverse_proxy localhost:8080
}
```

Then bind the server to a local port (e.g. `-p 127.0.0.1:8080:8080`) so only the proxy can reach it.

### Restart policy

Use `--restart unless-stopped` (or Compose `restart: unless-stopped`) so the container restarts after a crash or host reboot.

### Health check (optional)

The image includes BusyBox `wget` for health checks. You can add a health check so the orchestrator knows the server is up (use the no-auth `/api/health` endpoint). For Kubernetes, use `GET /api/health` for liveness and `GET /api/health?ready=1` for readiness (checks DB with a 2s timeout; returns 503 if the store is down or slow). The health response includes `version` when the server is built with version ldflags.

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "-O", "-", "http://localhost:8080/api/health"]
  interval: 30s
  timeout: 5s
  retries: 3
  start_period: 15s
```

### Data backup

The SQLite database is stored in the volume (e.g. `gsbs-data`). GSBS runs SQLite in **WAL mode**, so a simple `cp gsbs.db` while the server is running may miss in-flight transactions or produce an inconsistent snapshot. Use one of the following safe methods:

**Option A — stop the container first (simplest):**
```bash
docker stop gsbs
cp /path/to/gsbs-data/gsbs.db /path/to/backup/gsbs.db
cp /path/to/gsbs-data/gsbs.db-wal /path/to/backup/gsbs.db-wal 2>/dev/null || true
cp /path/to/gsbs-data/gsbs.db-shm /path/to/backup/gsbs.db-shm 2>/dev/null || true
docker start gsbs
```

**Option B — live backup with SQLite (no downtime):**
```bash
# Using sqlite3 (requires sqlite3 on the host or inside the container)
sqlite3 /path/to/gsbs-data/gsbs.db ".backup /path/to/backup/gsbs-backup.db"

# Or use VACUUM INTO (SQLite 3.27+) — writes a clean, WAL-checkpointed copy
sqlite3 /path/to/gsbs-data/gsbs.db "VACUUM INTO '/path/to/backup/gsbs-backup.db'"
```

Both options produce a consistent single-file backup with no in-flight transaction risk. If `GSBS_SAVE_ROOT` is set, also back up that directory (it holds save file bytes outside SQLite).

### Upgrading

> **Back up your data volume before upgrading.** The 2.0 release runs a one-way schema migration on startup. It cannot be automatically rolled back.

**Preview migration changes before upgrading (dry run):**
```bash
GSBS_DRY_RUN_MIGRATION=1 docker run --rm \
  -v gsbs-data:/app/data \
  -e GSBS_DB=/app/data/gsbs.db \
  dendlomm/gsbs-server:X.Y.Z
```
Check the logs for row counts and any warnings; no data is written.

**Upgrade:**
```bash
docker compose pull   # or docker pull dendlomm/gsbs-server:X.Y.Z
docker compose up -d  # restarts container; migration runs on startup
```

After the migration, clients perform a one-time re-sync check on their next run (no data loss — see `docs/CLIENT.md` for details).

### Pin to a specific version tag in production

Avoid using `:latest` in production compose files — it makes upgrade timing unpredictable. Instead, pin to a specific version tag and bump it deliberately:

```yaml
image: dendlomm/gsbs-server:2.0.0
```

This lets you control when migrations run and makes rollbacks straightforward (restore backup → roll back image tag).

---

## Summary

1. **Build**: `docker build -t gsbs-server:latest .`
2. **Run**: Expose port `8080`, set `GSBS_SESSION_SECRET` and `GSBS_DB` to a path in a mounted volume.
3. **Persist**: Use a volume for `/app/data` (or wherever `GSBS_DB` points).
4. **Production**: Put the server behind HTTPS (reverse proxy), set a strong session secret, and optionally set `GSBS_ADMIN_USERNAME` for the admin UI.
