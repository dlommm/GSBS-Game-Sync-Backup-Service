# FAQ

> Frequently asked questions about GSBS. If your question isn't here, check [Troubleshooting](Troubleshooting) or open an issue on [GitHub](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/issues).

---

## General

### What is GSBS and how is it different from Steam Cloud?

GSBS is a self-hosted sync service — you run the server yourself, on your own hardware or a VPS. Your saves never touch a third-party cloud provider. Unlike Steam Cloud (which only works for games that opt in), GSBS works for any game whose save location is in [PCGamingWiki](https://www.pcgamingwiki.com/), or any folder you point it at manually.

### What operating systems are supported?

| Component | Supported |
|---|---|
| Server | Linux (amd64, arm64 via Docker) |
| Client | Windows (amd64), Linux (amd64) |
| macOS | Not officially supported |

### Does GSBS support multiple users?

Yes. The server supports many independent users, each with their own saves and multiple clients (e.g. desktop + laptop).

### Can I sync between Windows and Linux for the same game?

Yes, for games tracked by PCGamingWiki. GSBS derives a platform-neutral `path_key` so Windows and Linux saves for the same game converge to a single server slot. Proton/Steam Deck paths are resolved automatically. See [How It Works → Cross-OS sync](How-It-Works#cross-os-sync-windows--linux).

### Does GSBS work with Steam Deck?

Yes. The client runs on Linux, and Proton `compatdata` paths are resolved automatically for Windows games running under Steam/Proton. You do not need to configure paths manually for PCGW-tracked games.

---

## Installation & setup

### Which server install method should I use?

Docker Compose with Caddy (the default `docker-compose.yml`) is recommended. It handles HTTPS automatically and is the most battle-tested configuration. See [Installation → Option A](Installation#option-a-docker-compose-recommended).

### What ports does the server need to be open?

Port 443 (HTTPS) for client-to-server communication. If you run without TLS (local-only), port 8080. No inbound ports are needed on client machines.

### Can I run the server without Docker?

Yes. Download `gsbs-server-linux-amd64` (or the Windows binary) from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest), set `GSBS_SESSION_SECRET` and `GSBS_DB`, and run the binary. See [Installation → Option C](Installation#option-c-binary-advanced).

### Should I use `:latest` Docker tag in production?

No. Pin to a specific version tag (e.g. `dendlomm/gsbs-server:2.0.0`) so you control when schema migrations run and can roll back if needed. See [Server Configuration](Server-Configuration#pin-to-a-specific-version-tag-in-production).

### How do I register users after disabling registration?

1. Temporarily set `GSBS_ALLOW_REGISTER=true` and restart the container.
2. Register the account via the WebUI.
3. Set `GSBS_ALLOW_REGISTER=false` again and restart.

Alternatively, create accounts via direct DB access (advanced).

---

## Sync & discovery

### A game isn't being discovered. What should I do?

1. Confirm the game is installed via a [supported launcher](Client-Setup-and-Usage#auto-discovery).
2. If the launcher is installed in a non-standard location, add the folder override to `config.json` (e.g. `heroic_folder`, `steam_library_folders`).
3. Tray → **Rescan installed games**.
4. Check `discovery.json` and `gsbs.log` for launcher detection output.

### A game is discovered but not syncing. Why?

The tray shows the reason next to each game. Common causes and fixes:

| Reason | Fix |
|---|---|
| `no_manifest_entry` | Run PCGW sync on the server |
| `save_dir_missing` | Launch the game once so its save folder is created |
| `wrong_platform` | Use **Add a game manually…** |
| `disabled` | Click the game in the tray to re-enable |

Run `gsbs-client debug-sync <game_id> --dry-run` for detailed diagnostics.

### What is the "folder-exists rule"?

GSBS never creates save directories. When pulling a save, if the target folder doesn't exist (i.e. the game isn't installed on that machine), the save is skipped. This prevents writing orphan files to uninstalled game locations.

### Can I add a game that isn't in PCGamingWiki?

Yes. Tray → **Discovered games** → **Add a game manually…** — search the manifest or paste an absolute save folder path. This writes a `watch_paths` entry to `config.json`.

### How often does the server sync with PCGamingWiki?

Weekly by default (Sunday 03:00). Admins can change the schedule in **Admin → Settings** (`GSBS_PCGW_CRON` env overrides the DB setting). Trigger a manual sync any time from **Admin → PCGW → Sync**.

### My saves aren't uploading after rapid file changes on Windows. Why?

Two scenarios:
- **fsnotify overflow:** Many rapid changes can overflow the OS event queue. GSBS now triggers a directory rescan to catch missed events (`watcher_overflow_rescan` in the log).
- **Locked files:** Games that hold exclusive write locks during save will cause push failures. The file is now enqueued to the outbox (`push_locked_file_enqueued` in the log) and retried once released.

---

## Conflicts & versions

### What happens when two machines both change the same save?

The default conflict policy is `last_write_wins` — the most recently pushed save wins. Both machines' changes are recorded in `conflicts.json`. You can resolve conflicts from the tray or the WebUI dashboard (keep all local / use all server).

Change the policy in `config.json`:
```json
{ "conflict_policy": "keep_local" }
```

### How many save versions does the server keep?

8 by default. Configurable via `GSBS_SAVE_VERSION_RETENTION`. Restore any version from WebUI **Versions** or the tray **Open dashboard**.

---

## Encryption & security

### Is my data encrypted on the server?

Saves are stored in plaintext by default. To enable encryption:
1. Turn on **End-to-end encryption** in WebUI **Settings**.
2. Set `"encryption_passphrase"` in each client's `config.json`.

With E2E encryption enabled, saves are encrypted locally with AES-GCM before being sent. The server never sees the passphrase or the plaintext. See [Client Setup & Usage → E2E encryption](Client-Setup-and-Usage#e2e-encryption).

### What happens if I lose my encryption passphrase?

Encrypted saves cannot be recovered without the passphrase. There is no server-side recovery. Back up your passphrase securely.

### Is communication between the client and server encrypted?

Yes, when you run the server behind HTTPS (recommended). The built-in Docker Compose setup uses Caddy for automatic TLS. Without HTTPS, communication is unencrypted — acceptable only on a trusted local network.

### The Windows installer isn't code-signed. Is it safe?

Yes — download only from the official [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) page. Verify the SHA256 checksum in `SHA256SUMS` if in doubt. Windows SmartScreen warning is expected for unsigned executables; choose **More info → Run anyway**.

---

## Updates

### How does client auto-update work?

The client checks GitHub Releases on startup (after 30s) and every 24 hours. When an update is available, it appears in the tray as **Install update X.Y.Z…**. The client downloads the binary, verifies the SHA256 checksum, replaces itself, and restarts. No re-running the installer needed.

### "Check for updates" shows nothing / always says I'm up to date even on failures.

Look in `gsbs.log` for lines prefixed with `update:` to see the actual result. Common statuses: `network_error`, `api_error`, `metered_skip`, `manifest_mismatch`. On Windows, `metered_skip` means the connection is flagged as metered — switch to an unmetered connection or disable the setting.

### Can I point the updater at a fork or private repo?

Yes. Set in `config.json`:
```json
{ "update_repo": "your-org/your-fork" }
```

---

## Server management

### How do I back up the server?

See [Upgrading → Backup procedure](Upgrading#backup-procedure). The SQLite database is the single source of truth. With `GSBS_SAVE_ROOT` set, also back up that directory.

### A user changed their password and can no longer sync. Why?

Password changes (and 2FA disable) revoke all active client tokens. Every client must re-login via tray **Login…** or `gsbs-client login` and create a new token.

### Can I run the server in read-only mode?

Yes. Set `GSBS_READ_ONLY=true`. Push and delete are disabled; pull and read still work.

### Does GSBS have metrics/monitoring?

Yes, when `GSBS_METRICS=1`. `GET /metrics` returns Prometheus text format (request counts, storage, users, clients, saves). Guard with `GSBS_METRICS_TOKEN` in production. See [API Reference → Metrics](API-Reference#metrics-optional).

---

## NAS / Unraid

### Can I run the server on a NAS?

Yes. Any device that can run Docker (Synology DSM, Unraid, TrueNAS SCALE, etc.) can host the server. See the [Unraid guide](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/UNRAID.md).

### Do I need the server and clients to be on the same network?

No. The server only needs to be reachable by the client at the configured URL. Clients can connect from anywhere over the internet (with HTTPS and your server URL).

---

## Related pages

- [Troubleshooting](Troubleshooting)
- [Client Setup & Usage](Client-Setup-and-Usage)
- [Server Configuration](Server-Configuration)
- [Upgrading](Upgrading)
- [How It Works](How-It-Works)
