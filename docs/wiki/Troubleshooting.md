# Troubleshooting

> Common problems, their causes, and how to fix them. For install steps see [Installation](Installation); for client data paths see [Client Setup & Usage](Client-Setup-and-Usage).

---

## Where to find logs

| Component | Location |
|---|---|
| Client (Windows) | `%APPDATA%\gsbs\gsbs.log` |
| Client (Linux) | `~/.config/gsbs/gsbs.log` |
| Server (Docker) | `docker compose logs gsbs` |
| Server (binary) | stderr / `GSBS_LOG_LEVEL=debug` |

Set `GSBS_LOG_LEVEL=debug` on the server or client for structured diagnostics. Add `"verbose_log": true` to client `config.json` for per-file push/pull detail.

---

## Server problems

### WebUI shows blank pages or template errors

Usually a stale Docker image or missing embedded templates from a source build.

- Pull the latest image: `docker compose pull && docker compose up -d`
- Or rebuild from source: run `./script/build-webui.sh` before `go build ./server`

### Cannot log in / session expires immediately

- Set `GSBS_SESSION_SECRET` to a long random value (`openssl rand -hex 32`) and restart the container.
- Behind a reverse proxy: ensure the proxy forwards `Host`, `X-Forwarded-Proto`, and cookies. Set `GSBS_TRUST_PROXY=1` on the server.

### PCGW manifest is empty or outdated

- Admin → **PCGW**: check the last sync run status and any errors.
- Manually trigger a sync: Admin → PCGW → **Incremental Sync** (routine) or **Auto Catch-Up** (large backlog).
- Check `GSBS_PCGW_CRON` is not set to `""` (disabled).

### PCGW Local count stuck / Missing not decreasing

- Check **Dead-letter** on the PCGW status card. If non-zero, run **Reset Dead Letter** (Advanced Maintenance), then **Auto Catch-Up**.
- Use **Parse Missing Only** to ingest never-fetched catalog IDs without a catalog refresh.
- Use **Retry Failed Pages** for `failed`/`partial` rows only.
- If the page budget is exhausted mid-run (`GSBS_PCGW_MAX_PAGES_PER_RUN`, default 5000), run **Incremental Sync** again or **Auto Catch-Up** to continue.
- **Full Reparse** (Advanced Maintenance) forces a full catalog rescan and re-processes backlog plus wiki-changed pages — rarely needed.

### Docker container is unhealthy

```bash
docker compose logs gsbs
docker compose ps
```

Common causes:
- `GSBS_DB` points to a path not on a mounted volume (container can't write it).
- Incorrect volume permissions. The entrypoint fixes `/app/data` ownership on startup.
- `GSBS_SESSION_SECRET` is unset (server will not start without it in newer builds).

### Dashboard shows a 500 or "store error" notice

- Check server logs for `error_class` (e.g. `db_locked`, `schema_error`).
- If `db_locked`: SQLite is under contention. Reduce concurrency or check for another process holding the DB.
- If `schema_error`: the DB migration may be incomplete. Review the startup log for migration status.
- Dashboard partials show an inline HTMX retry notice on store errors — the page remains usable while the underlying issue is resolved.

### API rate limit errors (429)

Clients receive 429 when they exceed per-user or per-IP rate limits. Defaults are generous (120 pushes/min per user). If you hit them legitimately, adjust `GSBS_RATE_LIMIT_PUSH` or similar.

---

## Client problems

### Tray icon missing (Linux)

The tray is pure-Go D-Bus (StatusNotifierItem) — no appindicator libraries are needed, but your desktop must run an SNI host:

- **GNOME:** Install the *AppIndicator and KStatusNotifierItem Support* extension (GNOME has no built-in tray).
- **KDE / Xfce / Cinnamon:** Usually works out of the box.
- `xdg-utils` is used for opening links (`sudo apt install xdg-utils` / `sudo dnf install xdg-utils`).

Run `gsbs-client --console` to verify the client starts correctly without a tray.

### Flatpak: games on another drive aren't discovered or synced

The sandbox grants home, the default Steam/Heroic/Lutris/Bottles data dirs, and `/run/media` (SD cards). A Steam library on another internal drive (e.g. `/mnt/games`) is invisible until you grant it:

```bash
flatpak override --user io.github.dlommm.GSBS --filesystem=/mnt/games
```

or add the path in **Flatseal → GSBS → Filesystem**, then restart the client. GSBS shows a one-time "limited access" notification when it detects a blocked save folder.

### Flatpak: login token isn't remembered

The client stores tokens in the Secret Service keyring. On systems without one, fall back to an on-disk token:

```bash
flatpak override --user io.github.dlommm.GSBS --env=GSBS_TOKEN_STORE=file
```

### Flatpak: no update option in the tray

By design — sandboxed installs update via `flatpak update` or your software center, not the in-app updater.

### "Not logged in" / 401 errors

Token expired or revoked. To resume:

1. Open tray **Login…** or run `gsbs-client login`.
2. If the token was revoked: create a new API token in WebUI **Settings → API tokens**.

> **Version note:** Password changes and 2FA disable now revoke all active client tokens automatically. After a credential change, you must log in again on every client.

### Outbox keeps retrying / 401 hammering

When the server returns HTTP 401, the outbox stops retries immediately via the `ErrUnauthorized` sentinel. If the local status page shows `auth_failed: true`, simply re-login to resume.

If you see repeated 401 errors after re-logging in, verify the token has not been revoked: WebUI **Settings → API tokens**.

### No games discovered

- Confirm games are installed via a supported launcher (Steam, Epic, GOG, Ubisoft Connect, Heroic, Lutris, Bottles, Prism Launcher, or Flatpak Steam).
- Set launcher folder overrides in `config.json` if auto-detect fails (e.g. `heroic_folder`, `steam_library_folders`).
- Tray → **Rescan installed games** to re-run discovery immediately.
- Check `discovery.json` and `gsbs.log` (look for launcher detection logs).

### Saves not syncing

Check the tray **Discovered games** list for the per-game readiness reason:

| Tray label | Cause | Fix |
|---|---|---|
| `⚠ not in server manifest` | `no_manifest_entry` | Run PCGW sync on server, or use **Add a game manually…** |
| `⚠ no saves for this OS` | `wrong_platform` | Use **Add a game manually…** with your local folder |
| `⚠ save folder not found` | `save_dir_missing` | Launch the game once so its folder exists, or set `game_install_paths` in config |
| `⚠ no valid save rule` | `malformed_rules` | Fix the manifest entry on the server |
| `⊘ <game>` | `disabled` | Click to re-enable, then **Refresh manifest** |

**Advanced diagnostics:**

```bash
gsbs-client debug-sync <game_id> --dry-run
```

Prints readiness reason, resolved watch dirs, `path_key`, and files that would upload — without sending any data.

Also check:
- `sync_paused` is not `true` in `config.json`.
- The outbox (`outbox/` directory) does not contain stale undeliverable entries.
- `GSBS_LOG_LEVEL=debug` shows detailed sync logs with `game_id`, `path_key`, and `relative_path`.

### Saves not uploading on Windows after burst writes

Two Windows-specific edge cases:

- **fsnotify overflow:** If the OS event queue overflows during many rapid file changes, the client now triggers a directory rescan to catch missed events (look for `watcher_overflow_rescan` in the log).
- **Locked files:** Files held exclusively by the game during push retries are now enqueued to the outbox instead of dropped (look for `push_locked_file_enqueued` in the log).

If saves still don't upload: check `outbox/` in the client data directory and `gsbs.log` for either log op above.

### Manifest fetch fails

Clients try `GET /api/manifest/v2` first, then fall back to v1. If manifest fetch consistently fails:

- Confirm the server URL is correct in `config.json`.
- Confirm the client token is valid (`gsbs-client login` if in doubt).
- Delete `manifest.json` in the client data directory to force a full re-fetch on next run (rarely needed).
- Check server logs for manifest handler errors.

### "Check for updates" does nothing

- Look in `gsbs.log` for lines prefixed with `update:` (e.g. `update: check result status=network_error`).
- Possible statuses: `available`, `up_to_date`, `disabled`, `metered_skip`, `network_error`, `api_error`, `manifest_mismatch`, `unsupported_arch`.
- `metered_skip` on Windows: connection is metered and `skip_sync_when_metered` is true — switch to an unmetered connection or disable the setting.
- `api_error` or `network_error`: check GitHub reachability and `update_repo` value in config.

---

## Upgrade problems

All upgrade troubleshooting is covered in [Upgrading](Upgrading). See:

- [Upgrading → Before you upgrade](Upgrading#before-you-upgrade)
- [Upgrading → Rollback](Upgrading#rollback)

---

## Diagnostic commands

```bash
# Client readiness for a specific game
gsbs-client debug-sync <game_id> --dry-run

# Force-push all matched files for a game (requires login)
gsbs-client debug-sync <game_id>

# Show what the client is syncing
gsbs-client list

# Check server health
curl https://your-server/api/health
curl https://your-server/api/health?ready=1
```

---

## Still stuck?

1. Reproduce with `GSBS_LOG_LEVEL=debug` enabled on both server and client.
2. Search [GitHub Issues](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/issues).
3. For security concerns, see [Contributing → Security](Contributing#security).

---

## Related pages

- [Installation](Installation)
- [Client Setup & Usage](Client-Setup-and-Usage)
- [Server Configuration](Server-Configuration)
- [Upgrading](Upgrading)
- [FAQ](FAQ)

## 2FA fails after restoring a server backup

Since 4.0.0, 2FA secrets are encrypted with a key file kept next to the database (`gsbs-keys/totp.key`). Restoring the DB **without** that file makes TOTP fail closed. Restore `gsbs-keys/` from the same backup, or disable 2FA for the user directly in SQLite and re-enroll:

```bash
sqlite3 /path/to/gsbs.db "UPDATE users SET totp_enabled = 0, totp_secret = '' WHERE username = 'NAME';"
```
