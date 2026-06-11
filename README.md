# GSBS — Game Sync & Backup Service

[![CI](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/actions/workflows/ci.yml/badge.svg)](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/actions/workflows/ci.yml)
[![Release](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/actions/workflows/release.yml/badge.svg)](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker Hub](https://img.shields.io/badge/Docker-dendlomm%2Fgsbs--server-blue)](https://hub.docker.com/r/dendlomm/gsbs-server)
[![Latest release](https://img.shields.io/github/v/release/dlommm/GSBS--Game-Sync---Backup-Service-)](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest)

![GSBS logo](docs/images/gsbs-logo-sm.png)

**Sync game saves across Windows and Linux.** Run a central server, install clients on each PC, and GSBS keeps saves in sync — only writing pulled files where the game is actually installed.

| Dashboard | System tray |
|-----------|-------------|
| ![Dashboard](docs/images/screenshots/dashboard.png) | ![Tray menu](docs/images/screenshots/tray-menu.png) |

## Quick install

### 1. Server (Docker Compose — recommended)

```bash
git clone https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-.git
cd GSBS--Game-Sync---Backup-Service-   # folder name matches the GitHub repo name
cp .env.example .env
# Edit .env — set GSBS_SESSION_SECRET (openssl rand -hex 32)
docker compose up -d
```

Open `https://your-domain` (via Caddy) or use [docker-compose.dev.yml](docker-compose.dev.yml) for local HTTP on port 8080. See [docs/COMPOSE.md](docs/COMPOSE.md) and [docs/INSTALL.md](docs/INSTALL.md).

Windows hosts can also use `gsbs-server-setup-X.Y.Z-windows-amd64.exe` from Releases. The wizard writes `C:\ProgramData\GSBS\server.env`, installs/runs a Windows Service by default, and keeps ProgramData data on uninstall by default.

### 2. Client

Download the latest release for your platform from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest):

| Platform | Install |
|----------|---------|
| **Windows** | Run `gsbs-client-setup-X.Y.Z-windows-amd64.exe` |
| **Linux (Debian/Ubuntu)** | `sudo dpkg -i gsbs-client_X.Y.Z_amd64.deb` |
| **Linux (portable)** | `chmod +x gsbs-client-X.Y.Z-x86_64.AppImage && ./gsbs-client-*.AppImage` |

Sign in via the tray menu (**Login…**), point at your server URL, and syncing starts automatically. Clients check for updates daily from GitHub Releases.

### 3. First sync

Register on the server WebUI, create an API token, and log in from the client. GSBS discovers installed games, watches save folders, and uploads changes. New machines pull existing saves when the target folder exists.

## Features

- **Multi-user** — many users, each with multiple clients (desktop + laptop).
- **Auto-upload** — watches save locations and uploads on change.
- **Auto-discovery** — scans Steam, Epic, GOG, Ubisoft, Heroic, Lutris, and more against the PCGW manifest.
- **Offline queue** — failed uploads persist and retry automatically.
- **Pull on new client** — only writes saves where the game folder exists.
- **OS-aware paths** — `%USERPROFILE%`, Steam libraries, Proton `compatdata`, etc.
- **WebUI + admin** — dashboard, save versions, activity, admin overview.
- **Client auto-update** — checks GitHub Releases; install from the tray menu.

### What's new in 2.0

- **Stability hardening**: panic recovery middleware, security headers (CSP, HSTS, X-Frame-Options), disabled-user session cutoff, and fail-closed quota enforcement on the server.
- **Token revocation on credential change**: password change and 2FA disable now revoke all active client tokens.
- **Updater fix**: missing JSON tag that silently broke the updater in production is fixed; manual update checks now show explicit status (available, up-to-date, network error, metered skip, etc.).
- **Windows sync reliability**: fsnotify overflow now triggers a directory rescan; locked files are enqueued to the persistent outbox instead of silently dropped.
- **Auth-failure containment**: `ErrUnauthorized` sentinel stops the outbox from hammering a 401; re-login prompt surfaced in local dashboard and tray tooltip.
- **CI**: Windows test job, `govulncheck`, and release-assets completeness guard added.

## Documentation

**The canonical user documentation is the [GSBS GitHub Wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki).** The wiki is automatically synced from `docs/wiki/` in this repository on every push to `main` and on each release tag.

| Wiki page | What it covers |
|---|---|
| [Installation](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Installation) | Server and client install on all platforms |
| [Client Setup & Usage](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Client-Setup-and-Usage) | Tray, auto-discovery, sync behavior, E2E encryption, logs |
| [Server Configuration](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Server-Configuration) | All environment variables, Docker, TLS, admin |
| [How It Works](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/How-It-Works) | Architecture, data model, path keys, PCGW, cross-OS sync |
| [Troubleshooting](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Troubleshooting) | Common problems and fixes |
| [Upgrading](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Upgrading) | All upgrade procedures, version notes, rollback |
| [API Reference](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/API-Reference) | Full REST API reference |
| [FAQ](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/FAQ) | Frequently asked questions |
| [Contributing](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Contributing) | Build from source, tests, conventions |

**In-repo reference docs** (source of truth for the wiki, kept in `docs/`):

| File | Description |
|---|---|
| [docs/COMPOSE.md](docs/COMPOSE.md) | Docker Compose + TLS (Caddy) detail |
| [docs/EXAMPLE_CONFIG.md](docs/EXAMPLE_CONFIG.md) | Client config JSON examples |
| [docs/RELEASE.md](docs/RELEASE.md) | Maintainer release workflow |
| [SECURITY.md](SECURITY.md) | Security policy |

## Build from source

Requires **Go 1.25+** and **Node.js** (for WebUI CSS).

```bash
go mod tidy
./script/build-webui.sh
go build -o gsbs-server ./server
go build -o gsbs-client ./client
```

Run tests: `go test ./server/... ./pkg/... ./client/...`

See [CONTRIBUTING.md](CONTRIBUTING.md) for lint, coverage, and conventions.

## Troubleshooting

| Symptom | Where to look |
|---------|---------------|
| WebUI blank or login fails | [Troubleshooting wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Troubleshooting#server-problems) |
| Client 401 / not syncing | Re-login from tray; check `gsbs.log` — [Troubleshooting wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Troubleshooting#client-problems) |
| No tray icon (Linux) | AppIndicator packages — [Client Setup & Usage wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Client-Setup-and-Usage#linux-requirements) |
| Upgrading server or client | [Upgrading wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki/Upgrading) |

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              GSBS Server                 │
                    │  Auth · WebUI · Save storage · PCGW job │
                    └─────────────────────────────────────────┘
                                      ▲
              ┌───────────────────────┼───────────────────────┐
              │                       │                       │
        ┌─────┴─────┐           ┌─────┴─────┐           ┌─────┴─────┐
        │  Client   │           │  Client   │           │  Client   │
        │ (Windows) │           │  (Linux)  │           │    …      │
        └───────────┘           └───────────┘           └───────────┘
```

## Repository layout

- `server/` — API, auth, WebUI, PCGW cron job
- `client/` — Windows/Linux tray client (watch, sync, auto-update)
- `pkg/` — shared types, path resolution, PCGW client
- `cmd/` — `pcgw-sync`, `pcgw-fetch`, icon tools
- `script/` — build, release, packaging (Inno Setup, `.deb`, AppImage)
- `docs/` — guides and examples

## Server configuration

PCGW data: fresh installs default to **GitHub manifest bundle** sync (`pcgw_sync_source=github`) — the server fetches a pre-built bundle from [gsbs-manifest](https://github.com/dlommm/gsbs-manifest) on a schedule (ETag-aware). Existing installs with PCGW data stay on **API sync** until changed in **Admin → Settings**. See [docs/MANIFEST_BUNDLE.md](docs/MANIFEST_BUNDLE.md).

API sync schedule: set `GSBS_PCGW_CRON` in Docker/compose (default `0 3 * * 0`, weekly Sunday 03:00; use `""` to disable). Bundle fetch cron defaults to daily 04:00 (`GSBS_PCGW_BUNDLE_CRON`). When env vars are **not** set, admins configure schedules in the WebUI under **Admin → Settings**.

Two-phase PCGW API sync: Phase 1 enumerates all PCGW game IDs into `pcgw_catalog`; Phase 2 fetches only missing, failed/partial, and changed pages. Set `GSBS_PCGW_MAX_PAGES_PER_RUN` to cap the Phase 2 ingest budget per run (default 5000). Interrupted runs save a checkpoint and resume automatically on the next sync.

Save storage: set `GSBS_SAVE_ROOT` (e.g. `/app/data/gamesaves` on the same Docker volume as `GSBS_DB`) to store save files on disk instead of SQLite BLOBs. Clients must send `X-Relative-Path` on push when filesystem storage is enabled. See [docs/DOCKER.md](docs/DOCKER.md) for all environment variables.

## License

[MIT](LICENSE)
