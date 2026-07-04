# Troubleshooting GSBS

Common problems and where to look. For install steps see [INSTALL.md](INSTALL.md); for client data paths see [CLIENT.md](CLIENT.md).

## Server

### WebUI shows blank pages or template errors

Usually a stale Docker image or missing embedded templates. Pull the latest image or rebuild from source (run `./script/build-webui.sh` before `go build`). See [UPGRADE.md](UPGRADE.md).

### Cannot log in / session expires immediately

- Set `GSBS_SESSION_SECRET` to a long random value (`openssl rand -hex 32`) and restart the container.
- Behind a reverse proxy, ensure the proxy forwards `Host`, `X-Forwarded-Proto`, and cookies. See [COMPOSE.md](COMPOSE.md).

### PCGW manifest empty or outdated

- Admin → **PCGW** (v1.0.17+): check last sync run and errors.
- Manually trigger sync: `docker exec <container> /app/gsbs-server pcgw-sync` or run `cmd/pcgw-sync` against the server DB (advanced).

### Docker container unhealthy

```bash
docker compose logs gsbs
docker compose ps
```

Confirm `GSBS_DB` points to a writable volume path (e.g. `/app/data/gsbs.db`).

## Client

### Tray icon missing (Linux)

The tray is pure-Go D-Bus (StatusNotifierItem) — no libraries to install, but the desktop must host an SNI tray. GNOME needs the *AppIndicator and KStatusNotifierItem Support* extension; KDE, Xfce, and Cinnamon usually work out of the box. `xdg-utils` is needed for opening links. Details in [CLIENT.md](CLIENT.md).

### Flatpak: games on another drive aren't discovered or synced

The sandbox grants home, the default Steam/Heroic/Lutris/Bottles data dirs, and `/run/media` (SD cards). A Steam library on another internal drive (e.g. `/mnt/games`) is invisible until you grant it:

```bash
flatpak override --user io.github.dlommm.GSBS --filesystem=/mnt/games
```

or add the path in **Flatseal → GSBS → Filesystem**, then restart the client. GSBS shows a one-time "limited access" notification when it detects a blocked save folder. See [FLATPAK.md](FLATPAK.md).

### Flatpak: login token isn't remembered

The client stores tokens in the Secret Service keyring (`org.freedesktop.secrets`). On systems without one (some minimal/immutable setups), set `GSBS_TOKEN_STORE=file` to fall back to an on-disk token:

```bash
flatpak override --user io.github.dlommm.GSBS --env=GSBS_TOKEN_STORE=file
```

### Flatpak: no update option in the tray

By design — sandboxed installs update via `flatpak update` or your software center, not the in-app updater.

### “Not logged in” or 401 errors

Token expired or revoked. Open tray **Login…** or run `gsbs-client login`. Create a new API token in WebUI **Settings** if needed.

### No games discovered

- Confirm games are installed via a supported launcher (Steam, Epic, GOG, Heroic, Lutris, Bottles, Prism, etc.).
- Set launcher folder overrides in `config.json` if auto-detect fails — see [EXAMPLE_CONFIG.md](EXAMPLE_CONFIG.md).
- Tray → **Rescan installed games** or wait for `discovery_interval` (default 4h).
- Check `discovery.json` and `gsbs.log` in the client data directory.

### Saves not syncing

A game can appear under tray **Discovered games** but still not sync. The tray now shows the reason inline next to each game, and `debug-sync` prints it. Reasons:

| Tray label | Reason | Fix |
|---|---|---|
| `✓ <game>` | Ready — will sync | Nothing to do |
| `⚠ <game> — not in server manifest` | `no_manifest_entry`: the game's save locations aren't in the server manifest | Run the PCGW sync on the server, or use **Add a game manually…** to point at the save folder |
| `⚠ <game> — no saves for this OS` | `wrong_platform`: manifest only has save paths for another OS | Use **Add a game manually…** with the local folder |
| `⚠ <game> — save folder not found` | `save_dir_missing`: path resolves but the folder doesn't exist | Launch the game once, or set `game_install_paths` in config |
| `⚠ <game> — no valid save rule` | `malformed_rules`: server save rules failed validation | Fix the manifest entry on the server |
| `⊘ <game>` | `disabled`: you turned sync off | Click the entry to re-enable, then **Refresh manifest** |

Other checks:

- **Discovered mode** (`auto_watch_mode: "discovered"`): only installed, matched, enabled games sync. Ready+enabled games auto-sync — no extra step needed.
- **Folder-exists rule**: the client never creates save directories; the game must have run at least once so the folder exists.
- **Add a game manually**: tray → **Discovered games** → **Add a game manually…** opens a local browser page. Search the manifest by name (e.g. "Witcher 3") and click *Use this*, or paste an absolute save-folder path. It writes a `watch_paths` entry and restarts sync.
- Verify `sync_paused` is false and Windows metered-connection skip is not blocking sync.
- Inspect `gsbs.log` for pull/push errors and `outbox/` for queued uploads. When zero watch paths are built, the log includes a `game_sync_readiness` line per game with the reason.
- Run **`gsbs-client debug-sync <game_id> --dry-run`** to print the readiness reason, resolved watch dirs, `path_key`, and `relative_path` for each file that would upload (no network write). Omit `--dry-run` to force-push all matched files (requires login).
- Set **`GSBS_LOG_LEVEL=debug`** (environment variable) for structured sync logs with `game_id`, `path_key`, and `relative_path` fields in `gsbs.log`.

### Manifest fetch fails

Clients try `GET /api/manifest/v2` first, then fall back to v1. Upgrade the server to v1.0.17+ for v2. Delete `manifest.json` in the client data dir to force a full re-fetch (rarely needed).

## Logs and config

| Component | Location |
|-----------|----------|
| Client log | `%APPDATA%\gsbs\gsbs.log` (Windows) or `~/.config/gsbs/gsbs.log` (Linux) |
| Client config | Same directory, `config.json` |
| Server (Docker) | `docker compose logs` |

Set `GSBS_LOG_LEVEL=debug` on the server or client (environment variable) for structured sync diagnostics. Use `"verbose_log": true` in client config for extra per-file size lines during push/pull.

## Still stuck?

1. Reproduce with verbose logging enabled.
2. Check [GitHub Issues](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/issues).
3. For security concerns see [SECURITY.md](../SECURITY.md).

## 2.0 behavior changes

### "Check for updates" does nothing / always shows latest

In 2.0 the updater was fixed: a missing JSON struct tag that silently broke version comparisons in production has been corrected. Manual **Check for updates…** now shows the explicit check outcome instead of silently showing "latest". If the check still appears to do nothing:

- Look for `update:` prefixed lines in `gsbs.log` (e.g. `update: check result status=network_error`).
- Possible statuses: `available`, `up_to_date`, `disabled`, `metered_skip`, `network_error`, `api_error`, `manifest_mismatch`, `unsupported_arch`.
- On Windows, `metered_skip` means the connection is metered and `skip_sync_when_metered` is true — switch to an unmetered connection or disable the setting.

### Saves not uploading on Windows after burst writes

In 2.0, two Windows-specific issues are fixed:

- **fsnotify overflow**: if the OS event queue overflows (many rapid file changes), the client now triggers a full directory rescan to catch any events that were dropped. Previously those changes were silently missed.
- **Locked files**: if a file is still held exclusively by the game during a push retry, the push is now enqueued to the persistent outbox instead of being silently dropped. It will retry automatically once the file is released.

If saves are still not uploading, check `outbox/` in the client data directory and look for `watcher_overflow_rescan` or `push_locked_file_enqueued` log ops in `gsbs.log`.

### Client keeps retrying after server error / 401 hammering

In 2.0, when the server returns HTTP 401 (token revoked or expired), the outbox stops all retries immediately via the `ErrUnauthorized` sentinel. The client will not keep hammering the server. The `auth_failed` state is visible in:

- **Local dashboard** (`http://127.0.0.1:41234/status` or tray **Advanced → Local status page**).
- **Tray tooltip** — shows an auth-failure indicator next to the sync state.

Re-login via tray **Login…** or `gsbs-client login` to resume. If you see repeated 401 errors on the server despite re-logging in, verify that your client token has not been revoked (WebUI **Settings → API tokens**). Note: in 2.0, password changes and 2FA disable automatically revoke all active client tokens — you will need to create a new token and log in again.

## 2FA fails after restoring a server backup ("invalid code" for correct codes)

Since 4.0.0, two-factor secrets are encrypted with a key file stored **outside** the database (`gsbs-keys/totp.key`, next to `gsbs.db`; `GSBS_TOTP_KEY_FILE` overrides). If you restore a database without that key file, TOTP validation fails **closed** — correct codes are rejected and affected users cannot complete login.

**Fix (preferred):** restore the `gsbs-keys/` directory from the same backup and restart the server.

**Recovery (key file lost):** disable 2FA for the affected user directly in the database, then re-enroll from Settings:

```bash
sqlite3 /path/to/gsbs.db "UPDATE users SET totp_enabled = 0, totp_secret = '' WHERE username = 'NAME';"
```

Always back up `gsbs-keys/` together with the database (the built-in backup job includes it automatically).

## "Conflict" notification on a brand-new machine's first sync

Expected since 4.0.0: when a fresh device (or one after a reinstall) first pushes a save that already exists on the server with different content, GSBS records a **conflict** instead of silently overwriting the other machine's copy. Resolve it from the tray (**Conflicts → Keep local / Use server**) or the WebUI. Subsequent syncs behave per your conflict policy as usual.
