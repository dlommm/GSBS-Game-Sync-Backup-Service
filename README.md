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
- `docs/` — Design and API notes.

## Quick start

**Build and run (from repo root):**
```bash
go mod tidy && go build -o gsbs-server ./server && go build -o gsbs-client ./client
./gsbs-server &
./gsbs-client login   # then run ./gsbs-client
```

1. **Server**  
   ```bash
   cd server && go build -o gsbs-server . && ./gsbs-server
   ```
   Optional env: `GSBS_ADDR=:8080` `GSBS_DB=gsbs.db` (see `server/README.md`).

2. **Client**  
   ```bash
   cd client && go build -o gsbs-client . && ./gsbs-client login
   ```
   Then add `watch_paths` in config and run `./gsbs-client`.

3. **Game data**  
   Save locations come from PCGamingWiki. The client (or a one-off job) can use the PCGW API and/or parsed game pages to populate a local cache of paths per game and OS.

## PCGamingWiki

- **API**: [PCGamingWiki:API](https://www.pcgamingwiki.com/wiki/PCGamingWiki:API) — MediaWiki API + Cargo (e.g. `Infobox_game`, `Cloud`).
- **Redirects**: `https://www.pcgamingwiki.com/api/appid.php?appid=STEAM_APPID` for Steam → wiki page.
- **Save locations**: Typically in the “Game data” section of each game page (e.g. “Save game data location”, “Configuration file(s) location”). Not always in Cargo; we use the API to get page IDs/titles and optionally parse wikitext or cache parsed results in the DB.

## License

MIT (or your choice).
