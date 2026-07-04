# GSBS Client — Data and logging

This document describes where the client stores its data and log file on **Windows** and **Linux**, so you can inspect config, manifest cache, and logs when troubleshooting.

## Where client data is stored

The client uses a single directory for all persistent data. Paths are derived from the OS “user config” directory.

| Platform | Data directory | Typical path |
|----------|----------------|--------------|
| **Windows** | `%APPDATA%\gsbs` | `C:\Users\<user>\AppData\Roaming\gsbs` |
| **Linux**   | `~/.config/gsbs` | `/home/<user>/.config/gsbs` |

Inside that directory you will find:

| File | Description |
|------|-------------|
| **config.json** | Server URL, token, client name, sync interval, optional launcher paths, `encryption_passphrase` (local E2E key), and `watch_paths`. |
| **manifest.json** | Cached game save locations from the server. Clients call `GET /api/manifest/v2` first (ETag/304, rich per-game metadata), then fall back to v1 (`GET /api/manifest`). The cache stores flat path entries plus optional v2 game metadata and a `source` field (`"v2"` or `"v1"`). |
| **discovery.json** | Last game discovery scan (installed games matched to manifest). |
| **outbox/** | Pending uploads when the server was unreachable. |
| **conflicts.json** | Detected sync conflicts (local vs server both modified). |
| **tray_state.json** | Cached per-game last sync times and titles for the tray menu. |
| **gsbs.log** | Client log file. All client activity (login, sync, pull, push, watcher, tray actions, errors) is written here so you can see what is happening. |
| **client.lock** | Single-instance lock while the tray app is running (Unix flock / Windows mutex). |

## CLI commands

Besides the tray app, `gsbs-client` offers subcommands: `login`, `list [--dry-run-pull]`, `debug-sync <game_id> [--dry-run]`, and `export [--game ID] [--out DIR]` — the latter downloads your saves (decrypting end-to-end-encrypted ones locally) into a zip archive with a manifest that any GSBS server can re-import (WebUI → My Games → Import archive).

## System tray (Windows and Linux)

When started without `--console`, the client runs in the system tray.

### Menu

- **Status** — last sync time, sync-in-progress, pause state, or last error.
- **Sync now / Pause** — manual sync and pause/resume.
- **Synced games** — up to 12 recently synced games with status (✓ ok, ⚠ conflict, ↑ uploaded, ⏳ pending). Click a game to open version history in the browser.
- **Discovered games** — installed games matched to the manifest. Each entry shows its sync readiness: `✓` ready (auto-syncing), `⚠ <reason>` not ready (e.g. *save folder not found*, *not in server manifest*), `⊘` disabled. Click to enable/disable sync. **Add a game manually…** opens a local browser page to search the manifest by name or add a save folder by path. **Rescan installed games** re-runs discovery.
- **Issues** — conflict count, pending offline uploads, last error (with link to log).
- **Account & Setup** (submenu) — server URL, sync interval, login, **Detect launcher paths**, **Refresh manifest**.
- **Advanced** (submenu) — edit config, view log, open data folder, run at startup, version, check/install updates.
  - **Local status page** — opens a local browser page (`http://127.0.0.1:41234/dashboard`) with live sync status, watched games, pending uploads, and conflicts. The page uses the same professional dark design system as the server WebUI (compiled CSS, DM Sans font, indigo accent — fully offline, no CDN).

Ready + enabled discovered games sync automatically; there is no separate "add to sync" step for games the manifest covers and whose save folder exists. For games the manifest doesn't cover (or with a non-standard folder), use **Add a game manually…**.

### Notifications

Toasts (via the OS notification system) for sync complete/failed, new saves uploaded (debounced per game), conflicts, first-run discovery summary, and setup required.

### First-run setup wizard

If not logged in, the tray opens a local browser setup page (`http://127.0.0.1:41234`). On Windows, tray **Login...** now opens this browser page by default; the native Walk dialog is available as a fallback via a direct sub-item. After login, the page polls `/status` for discovery progress and lists matched games.

The local browser UI (`client/webui/`) is fully embedded in the client binary — it shares the server's compiled CSS, vendored fonts (DM Sans, JetBrains Mono), and component classes, so both UIs look like the same product. All pages (Setup, Dashboard, Games, Quick Actions, Help, About) are served from `127.0.0.1` only and use the same dark theme with indigo accent.

### Autostart

- **Windows**: tray checkbox **Run at startup** (Registry Run key, launches with `--minimized`).
- **Linux**: same checkbox creates `~/.config/autostart/gsbs-client.desktop`.

Only one tray instance runs at a time; a second launch shows a notification and exits.

### Linux requirements

The tray uses [fyne.io/systray](https://github.com/fyne-io/systray), a pure-Go StatusNotifierItem (AppIndicator) implementation over D-Bus — no GTK or appindicator libraries are required. `xdg-utils` is used for opening files/URLs.

If the icon does not appear, confirm a status notifier / AppIndicator host is running: GNOME needs the *AppIndicator and KStatusNotifierItem Support* extension; KDE, Xfce, and Cinnamon work out of the box.

### Auto-update

The client checks [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) on startup (after 30s) and every 24 hours. When a newer version is available:

- A notification appears; the tray shows **Install update X.Y.Z…**
- **Check for updates…** runs a manual check
- **Version: x.y.z** shows the installed build

Update checks return a typed result with one of the following statuses: `available`, `up_to_date`, `disabled`, `metered_skip`, `network_error`, `api_error`, `manifest_mismatch`, or `unsupported_arch`. Manual checks via **Check for updates…** always show the explicit outcome — there is no silent "you're up to date" on network or API failures; the tray displays the actual reason and the check is also logged under the `update:` prefix in `gsbs.log`. The tray shows an in-progress state while the check is running.

Updates download the platform binary (`gsbs-client-windows-amd64.exe` or `gsbs-client-linux-amd64`), verify SHA256 from `latest-client.json`, replace the running binary, and restart with `--minimized`. Installers (`.exe` setup, `.deb`, AppImage) are for first install; in-app updates use the raw binary.

Config options:

| Field | Default | Description |
|-------|---------|-------------|
| `update_check_enabled` | `true` | Set `false` to disable checks |
| `update_repo` | official repo | GitHub `owner/repo` override |

On Windows, update checks are skipped when `skip_sync_when_metered` is true and the connection is metered.

Install via package manager or installer: see [INSTALL.md](INSTALL.md).

## Manifest (v2-first)

On each fetch the client:

1. Calls **`GET /api/manifest/v2?platform=<os>`** with `If-None-Match` when a cached ETag exists → **304** skips download.
2. Falls back to **`GET /api/manifest`** (v1 flat entries) if v2 is unavailable or returns no entries.

v2 improves discovery matching (launcher IDs, titles, taxonomy). Servers before v1.0.17 serve v1 only; clients work with either. Tray **Refresh manifest** forces a re-fetch. Delete `manifest.json` only when debugging a corrupt cache.

Optional `manifest_include`: `"saves"`, `"config"`, or `"both"` (default).

## Auto-discovery

On startup and every `discovery_interval` (default 4h), the client scans Steam, Epic, GOG, Ubisoft, EA App, Heroic, Lutris, Bottles, and Prism Launcher for installed games. Games are matched to the server manifest using launcher external IDs (Steam App ID, GOG ID, etc.) from the PCGW sync job, with title fallback and optional Steam→PCGW lookup cached in `discovery.json`.

### Auto-watch modes

- **`auto_watch_mode: "discovered"`** (default for new installs): only watch save paths for installed games matched to the manifest. Per-game opt-out via tray **Discovered games** (stored in `discovery.json` as `disabled_game_ids`).
- **`auto_watch_mode: "legacy"`** (default when omitted in existing configs): watch any manifest path whose save directory already exists on disk.

When no watch paths are built at startup or after a manifest refresh, the client logs a diagnostic summary with skip counts:

- **discovered** — manifest entries skipped because the game is not in the discovered/installed set
- **platform** — entry is for another OS (e.g. Windows path on Linux)
- **missing_dir** — save directory template resolved but the folder does not exist locally yet
- **malformed** — entry has no usable save rules or path template

Example log line: `sync: no watch paths — skipped discovered=12 platform=40 missing_dir=3 malformed=0`

Optional config: `conflict_policy` (`last_write_wins`, `keep_local`, `keep_server`), launcher folder overrides (`heroic_folder`, `lutris_folder`, `bottles_folder`, `prism_folder`, `flatpak_steam_folder`, `ea_app_folder`), `steam_library_folders` (extra Steam library roots when not in `libraryfolders.vdf`), `game_install_paths` (per-game install folder override for `<game-install-folder>` resolution), `launcher_user_id` (auto-detected from Steam when empty; tray **Detect launcher paths** merges it into config), `discovery_interval`, `game_aware_sync` (default `true`: sync for a game is deferred while it runs and flushed on exit; unavailable under Flatpak), `game_scan_interval` (process-scan interval, default `15s`), `conflict_policy_overrides` (per-game policy map, e.g. `{"12345": "keep_local"}`), `crypto_v2` (`true`/`false` pins the save-encryption format; unset follows the server's fleet-readiness signal).

### Cross-OS sync (Windows ↔ Linux / Steam Deck)

GSBS 2.0 supports syncing saves between Windows and Linux for games tracked by PCGamingWiki.

### How it works

Each PCGW-sourced save rule carries a `slot_label` — an OS-neutral integer index assigned during server PCGW ingest. The client derives `path_key` from `(game_id, slot_label, is_config)` rather than the raw path, so:

- A Windows client watching `%APPDATA%\Elden Ring\<user-id>\` and a Linux client watching `~/.local/share/EldenRing/` both produce the **same `path_key`** for the same logical save slot.
- The server stores one record per `(user, game_id, path_key)`. When either machine syncs, it gets the other's save and writes it to its local OS-native path.

### Proton / Steam Deck

For Windows-native games running via Steam/Proton on Linux, the client resolves the Proton `compatdata` path:

```
<SteamLibrary-folder>/steamapps/compatdata/<SteamAppID>/pfx/users/steamuser/AppData/Roaming/<Game>/
```

This is synthesized automatically from the Steam library and app ID. No manual path configuration is needed for PCGW-tracked games.

### User-defined `watch_paths` are OS-specific

Entries in `watch_paths` in `config.json` are manually configured and use a per-OS `path_key` (hash of the full rule). They do **not** cross-sync between Windows and Linux — each machine only pushes and pulls its own copy. This is intentional: manual paths are machine-specific.

### One-time re-sync after upgrading to 2.0

When upgrading from a pre-2.0 client, the client detects that its push hash cache was built under the old `path_key` scheme and clears it automatically on first run. This triggers a one-time re-sync check (the client re-evaluates each save against the server). No data is lost — the server still holds all saves; the client simply re-confirms which are already up to date.

## Manual watch paths

`watch_paths` in `config.json` can override or supplement manifest paths. Each entry supports:

| Field | Description |
|-------|-------------|
| `game_id` | Server game ID |
| `path_key` | Stable key for this save slot (required for legacy config entries) |
| `path_templates` | Legacy list of OS-specific path templates |
| `directory` | Save root directory template (preferred with manifest-style rules) |
| `include_patterns` | Glob patterns relative to `directory` (e.g. `["*.sav", "profile.dat"]`); omit with `sync_all: true` to upload every file |
| `sync_all` | When true, watch and push all files under `directory` |
| `recursive` | When true, watch subdirectories recursively |

Config `watch_paths` are merged first; manifest-derived paths with the same `(game_id, rule_key)` are not duplicated.

## Offline outbox

If a push fails after retries, the file is queued in `outbox/` with exponential backoff (2m–30m cap). Entries older than 7 days are dropped. Auth/quota errors are not retried.

### Auth-failure containment (`ErrUnauthorized`)

When the server returns HTTP 401 (token revoked, expired, or invalid), the outbox stops all retry attempts immediately — it will not keep hammering the server. The client sets an internal `auth_failed` flag that is surfaced in:

- **Local dashboard** (`http://127.0.0.1:41234/status`) — shows `auth_failed: true` and the last check time.
- **Tray tooltip** — indicates authentication failure next to the sync state.

To resume syncing, re-login via tray **Login…** or run `gsbs-client login`. A new valid token clears the flag and the outbox resumes.

## Optional E2E encryption

Enable **End-to-end encryption** in WebUI **Settings**. Set `encryption_passphrase` in local `config.json` on each client (never sent to the server). New pushes are encrypted with AES-GCM; pulls decrypt locally. Existing plaintext saves remain until naturally re-uploaded.

## Sync behavior

- **Upload**: File watcher debounces changes (2s) and pushes with SHA256 hash metadata. Duplicate content (same hash as the last successful push for that slot) is skipped locally; the server may also respond with `{"status":"unchanged"}`. A supervisor restarts the watcher if fsnotify fails.
- **Startup reconciliation**: On startup (after the initial pull), the client scans all local save files under watched directories and uploads any that are not yet on the server. This ensures saves reach the server on first install or when the server has no record — not just when files change. Files already matching the server hash are skipped.
- **Download**: Uses summary + hash comparison to fetch only changed saves; listens for SSE `save-updated` events from other machines. Transient network errors use exponential backoff (`pkg/retry`).
- **Conflicts**: Default policy is `last_write_wins` (`conflict_policy`). When both local and server changed, conflicts are recorded in `conflicts.json` with hashes and timestamps. Resolve from the tray (keep all local / use all server) or the WebUI dashboard. Legacy `skip_overwrite_when_local_newer: true` maps to `keep_local`. Per-game overrides via `conflict_policy_overrides`. Since 4.0.0 a new device's FIRST push of an existing save always surfaces a conflict instead of overwriting, and near-simultaneous changes (within a 2-minute clock-skew window) conflict rather than letting the faster clock win.
- **Versions**: Server keeps last N versions per save (`GSBS_SAVE_VERSION_RETENTION`). Restore from WebUI **Versions** link or tray **Open dashboard**.

## Log file (gsbs.log)

- **Path**: same directory as config, e.g.  
  - Windows: `%APPDATA%\gsbs\gsbs.log`  
  - Linux: `~/.config/gsbs/gsbs.log`
- **Content**: Structured text log (slog) with date/time. Level from `GSBS_LOG_LEVEL` (`debug`, `info`, `warn`, `error`). Includes:
  - Startup: log file path, sync start, server URL
  - Login: success/failure (CLI, tray, or browser setup)
  - Manifest: fetch from server, cache save, refresh (SSE or manual)
  - Sync: initial pull, periodic pull, “sync now”, watch paths count, zero-watch diagnostics
  - Manifest refresh: watch path diff (+added -removed) after SSE, discovery, or manual refresh
  - Watcher: directory add errors, push after file change, push errors; `watcher_event_received`, `watcher_event_unmapped`, `watcher_push_paused`, `watcher_file_lock_retry`, `reconcile_upload`, `reconcile_skip_unchanged`, `push_cache_load_error`, `push_http_error`
  - Pull: decode/mkdir/write errors, successful writes per game/path
  - Tray: sync started, config reloaded, menu actions, per-game status updates
  - SSE: connection/disconnect, received events
- **Rotation**: When `gsbs.log` reaches 5 MiB, the client renames it to `gsbs.log.old` and starts a fresh `gsbs.log`. Only one backup (`.old`) is kept.
- **Verbose**: Set `"verbose_log": true` in config to get extra detail (per-file pull/push, resolved paths). Restart the client after changing.

When running with a console (e.g. `gsbs-client` on Linux, or `gsbs-client --console` on Windows), the same log lines are also printed to stderr. When running as a Windows tray app without a console, **only the log file** receives output, so check `%APPDATA%\gsbs\gsbs.log` to see what the client is doing.

## Quick reference

- **See what the client is doing**: open `gsbs.log` in the data directory above.
- **Change server or credentials**: edit `config.json` or use `gsbs-client login` (or the tray “Login / Setup…” flow).
- **Manifest cache**: `manifest.json`; delete it to force a full re-fetch from the server on next run (normally not needed).
