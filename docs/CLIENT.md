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
| **manifest.json** | Cached list of game save locations from the server (from `GET /api/manifest`). Supports delta fetch via `?since=`. |
| **discovery.json** | Last game discovery scan (installed games matched to manifest). |
| **outbox/** | Pending uploads when the server was unreachable. |
| **conflicts.json** | Detected sync conflicts (local vs server both modified). |
| **tray_state.json** | Cached per-game last sync times and titles for the tray menu. |
| **gsbs.log** | Client log file. All client activity (login, sync, pull, push, watcher, tray actions, errors) is written here so you can see what is happening. |
| **client.lock** | Single-instance lock while the tray app is running (Unix flock / Windows mutex). |

## System tray (Windows and Linux)

When started without `--console`, the client runs in the system tray.

### Menu

- **Status** — last sync time, sync-in-progress, pause state, or last error.
- **Sync now / Pause** — manual sync and pause/resume.
- **Synced games** — up to 12 recently synced games with status (✓ ok, ⚠ conflict, ↑ uploaded, ⏳ pending). Click a game to open version history in the browser.
- **Discovered games** — installed games matched to the manifest; **Rescan installed games** re-runs discovery.
- **Issues** — conflict count, pending offline uploads, last error (with link to log).
- **Settings** — login, detect launcher paths, refresh manifest, edit config, run at startup, open data folder.

### Notifications

Toasts (via the OS notification system) for sync complete/failed, new saves uploaded (debounced per game), conflicts, first-run discovery summary, and setup required.

### First-run setup wizard

If not logged in, the tray opens a local browser setup page (`http://127.0.0.1:41234`). After login, the page polls `/status` for discovery progress and lists matched games.

### Autostart

- **Windows**: tray checkbox **Run at startup** (Registry Run key, launches with `--minimized`).
- **Linux**: same checkbox creates `~/.config/autostart/gsbs-client.desktop`.

Only one tray instance runs at a time; a second launch shows a notification and exits.

### Linux requirements

The tray uses [systray](https://github.com/getlantern/systray) (AppIndicator/status notifier). Install your desktop’s tray support, e.g.:

- **Debian/Ubuntu**: `libappindicator3-1` or `libayatana-appindicator3-1`, plus `xdg-utils` for opening files/URLs.
- **Fedora**: `libappindicator-gtk3`, `xdg-utils`.

If the icon does not appear, confirm a status notifier / AppIndicator host is running (GNOME may need an extension; KDE and most Xfce setups work out of the box).

### Auto-update

The client checks [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) on startup (after 30s) and every 24 hours. When a newer version is available:

- A notification appears; the tray shows **Install update X.Y.Z…**
- **Check for updates…** runs a manual check
- **Version: x.y.z** shows the installed build

Updates download the platform binary (`gsbs-client-windows-amd64.exe` or `gsbs-client-linux-amd64`), verify SHA256 from `latest-client.json`, replace the running binary, and restart with `--minimized`. Installers (`.exe` setup, `.deb`, AppImage) are for first install; in-app updates use the raw binary.

Config options:

| Field | Default | Description |
|-------|---------|-------------|
| `update_check_enabled` | `true` | Set `false` to disable checks |
| `update_repo` | official repo | GitHub `owner/repo` override |

On Windows, update checks are skipped when `skip_sync_when_metered` is true and the connection is metered.

Install via package manager or installer: see [INSTALL.md](INSTALL.md).

## Auto-discovery

On startup and every `discovery_interval` (default 4h), the client scans Steam, Epic, GOG, Ubisoft, EA App, Heroic, Lutris, Bottles, and Prism Launcher for installed games. Games are matched to the server manifest using launcher external IDs (Steam App ID, GOG ID, etc.) from the PCGW sync job, with title fallback and optional Steam→PCGW lookup cached in `discovery.json`.

### Auto-watch modes

- **`auto_watch_mode: "discovered"`** (default for new installs): only watch save paths for installed games matched to the manifest.
- **`auto_watch_mode: "legacy"`** (default when omitted in existing configs): watch any manifest path whose save directory already exists on disk.

Optional config: `conflict_policy` (`last_write_wins`, `keep_local`, `keep_server`), `ea_app_folder`, `discovery_interval`.

## Offline outbox

If a push fails after retries, the file is queued in `outbox/` with exponential backoff (2m–30m cap). Entries older than 7 days are dropped. Auth/quota errors are not retried.

## Optional E2E encryption

Enable **End-to-end encryption** in WebUI **Settings**. Set `encryption_passphrase` in local `config.json` on each client (never sent to the server). New pushes are encrypted with AES-GCM; pulls decrypt locally. Existing plaintext saves remain until naturally re-uploaded.

## Sync behavior

- **Upload**: File watcher debounces changes (2s) and pushes with SHA256 hash metadata. A supervisor restarts the watcher if fsnotify fails.
- **Download**: Uses summary + hash comparison to fetch only changed saves; listens for SSE `save-updated` events from other machines. Transient network errors use exponential backoff (`pkg/retry`).
- **Conflicts**: Default policy is `last_write_wins` (`conflict_policy`). When both local and server changed, conflicts are recorded in `conflicts.json` with hashes and timestamps. Resolve from the tray (keep all local / use all server) or the WebUI dashboard. Legacy `skip_overwrite_when_local_newer: true` maps to `keep_local`.
- **Versions**: Server keeps last N versions per save (`GSBS_SAVE_VERSION_RETENTION`). Restore from WebUI **Versions** link or tray **Open dashboard**.

## Log file (gsbs.log)

- **Path**: same directory as config, e.g.  
  - Windows: `%APPDATA%\gsbs\gsbs.log`  
  - Linux: `~/.config/gsbs/gsbs.log`
- **Content**: Structured text log (slog) with date/time. Level from `GSBS_LOG_LEVEL` (`debug`, `info`, `warn`, `error`). Includes:
  - Startup: log file path, sync start, server URL
  - Login: success/failure (CLI, tray, or browser setup)
  - Manifest: fetch from server, cache save, refresh (SSE or manual)
  - Sync: initial pull, periodic pull, “sync now”, watch paths count
  - Watcher: directory add errors, push after file change, push errors
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
