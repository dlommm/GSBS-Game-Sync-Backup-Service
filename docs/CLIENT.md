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
| **config.json** | Server URL, token, client name, sync interval, optional Ubisoft/launcher paths, and `watch_paths`. |
| **manifest.json** | Cached list of game save locations from the server (from `GET /api/manifest`). Refreshed on sync and when the server pushes an update. |
| **gsbs.log** | Client log file. All client activity (login, sync, pull, push, watcher, tray actions, errors) is written here so you can see what is happening. |

## Log file (gsbs.log)

- **Path**: same directory as config, e.g.  
  - Windows: `%APPDATA%\gsbs\gsbs.log`  
  - Linux: `~/.config/gsbs/gsbs.log`
- **Content**: One line per log message, with date and time. Includes:
  - Startup: log file path, sync start, server URL
  - Login: success/failure (CLI, tray, or browser setup)
  - Manifest: fetch from server, cache save, refresh (SSE or manual)
  - Sync: initial pull, periodic pull, “sync now”, watch paths count
  - Watcher: directory add errors, push after file change, push errors
  - Pull: decode/mkdir/write errors, successful writes per game/path
  - Tray (Windows): sync started, config reloaded, “Sync now” / “Refresh manifest” triggered
  - SSE: connection/disconnect, received events
- **Rotation**: When `gsbs.log` reaches 5 MiB, the client renames it to `gsbs.log.old` and starts a fresh `gsbs.log`. Only one backup (`.old`) is kept.
- **Verbose**: Set `"verbose_log": true` in config to get extra detail (per-file pull/push, resolved paths). Restart the client after changing.

When running with a console (e.g. `gsbs-client` on Linux, or `gsbs-client --console` on Windows), the same log lines are also printed to stderr. When running as a Windows tray app without a console, **only the log file** receives output, so check `%APPDATA%\gsbs\gsbs.log` to see what the client is doing.

## Quick reference

- **See what the client is doing**: open `gsbs.log` in the data directory above.
- **Change server or credentials**: edit `config.json` or use `gsbs-client login` (or the tray “Login / Setup…” flow).
- **Manifest cache**: `manifest.json`; delete it to force a full re-fetch from the server on next run (normally not needed).
