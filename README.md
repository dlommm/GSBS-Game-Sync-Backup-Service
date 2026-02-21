# GSBS — Game Sync & Backup Service

![GSBS logo](docs/images/gsbs-logo.png)

A server-based game save syncing system. **Windows** and **Linux** clients sync game saves to a central server with multi-user support. Multiple clients per user stay in sync; new clients pull all saves and only write where the game is installed (folder exists).

## Features

- **Multi-user**: Many users, each with multiple clients (e.g. desktop + laptop).
- **Auto-upload**: Clients watch save locations and upload changed files to the server.
- **Pull on new client**: New client pulls all saves for the user; only applies saves when the target folder exists (game installed).
- **OS-aware paths**: Resolves Windows vs Linux paths (e.g. `%USERPROFILE%`, `<SteamLibrary-folder>`, Proton `compatdata`).
- **Game save locations**: Uses [PCGamingWiki](https://www.pcgamingwiki.com/wiki/) (API + page data) for save/config paths.

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              GSBS Server                 │
                    │  • Auth (users, API tokens)             │
                    │  • Per-user save storage (by game/key)  │
                    │  • REST/API: push save, list, pull      │
                    └─────────────────────────────────────────┘
                                      ▲
                    ┌─────────────────┼─────────────────┐
                    │                 │                 │
              ┌─────┴─────┐     ┌─────┴─────┐     ┌─────┴─────┐
              │  Client 1 │     │  Client 2 │     │  Client N │
              │ (Windows) │     │  (Linux)  │     │ (any OS)  │
              └───────────┘     └───────────┘     └───────────┘
                    │                 │                 │
              Watch local       Watch local       Watch local
              save paths        save paths        save paths
              → upload          → upload          → upload
              ← pull (if dir    ← pull (if dir    ← pull (if dir
                 exists)          exists)           exists)
```

- **Server**: Stores saves per user and per “game slot” (game id + path key). Clients push by path key and pull all for user; server does not need to know OS.
- **Client**: Discovers OS (Windows vs Linux), loads game save locations (from PCGW or local DB), resolves placeholders (`%USERPROFILE%`, `<SteamLibrary-folder>`, etc.), watches only existing directories. On change → upload; on first run or manual “sync” → pull and write only where directory exists.

## Repository layout

- `server/` — GSBS server (API, auth, save storage).
- `client/` — Windows and Linux client (watch, upload, pull, path resolution).
- `pkg/` — Shared code: path resolution, protocol types, PCGW client.
- `cmd/` — Standalone tools: `pcgw-fetch` (fetch save locations for one game), `pcgw-sync` (one-off PCGW sync into server DB), `write-ico`, `resize-icon`.
- `docs/` — Design and API notes.

## Prerequisites

- **Go 1.24+** (see `go.mod`). Build the server with CGO enabled for SQLite (`CGO_ENABLED=1`).

## Quick start

**Build and run (from repo root):**
```bash
go mod tidy && go build -o gsbs-server ./server && go build -o gsbs-client ./client
./gsbs-server &
./gsbs-client login   # then run ./gsbs-client
```

**Docker:** See [docs/DOCKER.md](docs/DOCKER.md) for building and running the server image.

1. **Server**  
   ```bash
   cd server && go build -o gsbs-server . && ./gsbs-server
   ```
   **Server environment variables** (all optional):
   - `GSBS_ADDR` — Listen address (default `:8080`).
   - `GSBS_DB` — SQLite database path (default `gsbs.db`).
   - `GSBS_SESSION_SECRET` — Secret for signing WebUI session cookies; **set a strong value in production** (otherwise a warning is logged).
   - `GSBS_ALLOW_REGISTER` — Set to `false` or `0` to disable public registration.
   - `GSBS_ADMIN_USERNAME` — If set, only this user can access the `/admin` page (stats, revoke client tokens).
   - `GSBS_MAX_STORAGE_BYTES` — Global storage limit in bytes (0 or unset = unlimited). Rejects push when total storage would exceed this.
   - `GSBS_READ_ONLY` — Set to `true` or `1` to disable push and delete (pull and WebUI read still work).
   - `GSBS_PCGW_CRON` — Cron expression for the weekly PCGW manifest sync (default `0 3 * * 0` = Sunday 03:00). Example: `0 0 * * *` for daily at midnight.
   - `GSBS_RATE_LIMIT_MANIFEST` — Optional rate limit for manifest fetches (e.g. `60,1m` = 60 per minute by IP or user).

   See `server/README.md` for API details.

2. **Client**  
   ```bash
   cd client && go build -o gsbs-client . && ./gsbs-client login
   ```
   Then add `watch_paths` in config and run `./gsbs-client`.  
   **Client data and logs**: Config, manifest cache, and log file live in one directory per OS — see [docs/CLIENT.md](docs/CLIENT.md) for paths (e.g. Windows `%APPDATA%\gsbs\gsbs.log`, Linux `~/.config/gsbs/gsbs.log`).

3. **Game data**  
   Save locations come from PCGamingWiki. The server runs a weekly PCGW sync job (or use `go run ./cmd/pcgw-sync` with `GSBS_DB` set). To fetch save-location templates for a single game by Steam App ID: `go run ./cmd/pcgw-fetch 311560` (or build `pcgw-fetch` and run `./pcgw-fetch 311560`). See [docs/EXAMPLE_CONFIG.md](docs/EXAMPLE_CONFIG.md).

## PCGamingWiki

- **API**: [PCGamingWiki:API](https://www.pcgamingwiki.com/wiki/PCGamingWiki:API) — MediaWiki API + Cargo (e.g. `Infobox_game`, `Cloud`).
- **Redirects**: `https://www.pcgamingwiki.com/api/appid.php?appid=STEAM_APPID` for Steam → wiki page.
- **Save locations**: Typically in the “Game data” section of each game page (e.g. “Save game data location”, “Configuration file(s) location”). Not always in Cargo; we use the API to get page IDs/titles and optionally parse wikitext or cache parsed results in the DB.

## License

MIT (or your choice).


Testing
