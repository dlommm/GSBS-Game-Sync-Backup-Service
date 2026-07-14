# Upgrading

> The single canonical reference for all GSBS upgrade procedures — server, client, Docker/Compose, rollback, and version-specific migration notes. All other pages link here instead of duplicating these steps.

---

## Before you upgrade

**Always back up your data before upgrading the server.**

- **Server:** Copy the SQLite database. See [Backup procedure](#backup-procedure) below.
- **Client:** No server-side migration is needed for client-only updates. Local `config.json` is preserved across upgrades.
- **Check the [version notes](#version-notes) table** to see if the target version has schema migrations or breaking changes.

---

## Backup procedure

> **Warning:** The server runs SQLite in WAL mode. A plain `cp gsbs.db` while running may produce an inconsistent snapshot. Use one of these safe methods.

**Option A — stop the container first (simplest):**

```bash
docker stop gsbs
cp /path/to/data/gsbs.db /path/to/backup/gsbs.db
cp /path/to/data/gsbs.db-wal /path/to/backup/ 2>/dev/null || true
docker start gsbs
```

**Option B — live backup (no downtime):**

```bash
sqlite3 /path/to/data/gsbs.db "VACUUM INTO '/path/to/backup/gsbs-backup.db'"
```

If `GSBS_SAVE_ROOT` is set, also back up that directory — it holds save file bytes outside SQLite.

---

## Server upgrade

### Docker Compose (recommended)

```bash
cd /path/to/compose
docker compose pull
docker compose up -d
```

The server runs any pending schema migrations automatically on startup.

**Pin to a specific version in production** (avoids surprise migrations from `:latest`):

```yaml
# In docker-compose.yml
image: dendlomm/gsbs-server:2.0.0
```

Bump the tag deliberately and restart:

```bash
# Edit docker-compose.yml to update the tag, then:
docker compose up -d
```

### Pre-built Docker image (manual)

```bash
docker pull dendlomm/gsbs-server:X.Y.Z
docker stop gsbs
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="..." \
  -e GSBS_DB="/app/data/gsbs.db" \
  -v gsbs-data:/app/data \
  dendlomm/gsbs-server:X.Y.Z
```

### Binary

1. Stop the running server.
2. Download the new `gsbs-server-linux-amd64` or `gsbs-server-windows-amd64.exe` from [GitHub Releases](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/releases/latest).
3. Replace the binary.
4. Start with the same environment variables and `GSBS_DB` path.

---

## Dry-run migration preview

For releases that include schema migrations, preview changes before running them:

```bash
GSBS_DRY_RUN_MIGRATION=1 docker run --rm \
  -v gsbs-data:/app/data \
  -e GSBS_DB=/app/data/gsbs.db \
  dendlomm/gsbs-server:X.Y.Z
```

Check the logs for row counts and warnings. **No data is written** in dry-run mode. Remove `GSBS_DRY_RUN_MIGRATION` to run the real upgrade.

---

## Client upgrade

### In-app auto-update (recommended)

The tray client checks GitHub Releases daily and shows **Install update X.Y.Z…** when a newer version is available:

1. Open the tray menu → **Install update X.Y.Z…**
2. The client downloads the raw binary, verifies the SHA256 checksum from `latest-client.json`, replaces the running binary, and restarts with `--minimized`.

Manual check: tray **Advanced → Check for updates…**

### Windows installer

```
Run gsbs-client-setup-X.Y.Z-windows-amd64.exe over the existing install.
```

The installer preserves `config.json` and local data.

### Linux .deb package

```bash
sudo dpkg -i gsbs-client_X.Y.Z_amd64.deb
```

Config stays in `~/.config/gsbs`.

### Linux AppImage

Replace the AppImage file. Config stays in `~/.config/gsbs`.

### Disable auto-update

```json
{
  "update_check_enabled": false
}
```

---

## After a server upgrade

After upgrading the server:

1. **Check the startup log** for migration status messages.
2. **Clients re-sync automatically** — for schema migrations that change `path_key` derivation, clients detect the old hash cache was built under a different scheme and clear it on first run. This triggers a one-time re-sync check (no data loss; the server still holds all saves).
3. **Token behavior:** Password changes and 2FA disable now revoke all active client tokens. If a credential change occurred during the upgrade window, clients must re-login.

---

## Upgrade ordering (server and client together)

For any upgrade:

1. **Upgrade the server first.**
2. **Then upgrade clients** (one at a time or all at once via auto-update).

The server is always backward-compatible with older clients. Newer server features (manifest v2, richer discovery) are used automatically once clients are also upgraded.

---

## Rollback

> **Warning:** Schema migrations are one-way. Rolling back the server binary after a migration has run requires also restoring a pre-migration backup.

**Server rollback:**

1. Restore the DB backup taken before upgrading:
   ```bash
   docker stop gsbs
   cp /path/to/backup/gsbs.db /path/to/data/gsbs.db
   docker start gsbs
   ```
2. Roll back the image tag or binary to the previous version.

**Client rollback:**

- Reinstall the previous release binary or installer from [GitHub Releases](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/releases).
- Or disable auto-update (`"update_check_enabled": false`) until the issue is resolved.

---

## Version notes

| Version | Key changes | Action required |
|---|---|---|
| **4.0.0** | Session-secret strength is enforced at startup; TOTP/register rate limiting; client fsync durability; `.deb` drops the appindicator dependency; quotas count version history (enforced atomically); history tables pruned by default; integrity job, log rotation, disk-full protection added. | `GSBS_SESSION_SECRET` is now **optional** (auto-generated into `gsbs-keys/`); a *set* value still must be 32+ chars or the server won't start — replace weak ones. The setup wizard only appears on fresh servers, so upgrades go straight to login. |
| **3.2.0** | My Games / Insights / Devices pages; per-version device + byte-change tracking. | **Back up DB first.** Schema migration (v21) runs on startup. |
| **3.0.0** | Manifest bundle sync (GitHub mode) for fresh installs; encrypted-save dedup via plaintext hashes; crash-safe disk-backed saves; first-push overwrite guard. | Encrypted saves re-upload once after upgrade, then converge. No action needed. |
| **2.0.0** | Security headers; panic recovery; fail-closed quota; disabled-user session cutoff. `RevokeAllClientTokens` on password change / 2FA disable — re-login required after credential changes. Updater JSON tag fix (version checks were silently broken in production). Windows fsnotify overflow rescan + locked-file outbox enqueue. `ErrUnauthorized` sentinel stops outbox on 401. | **Back up DB first.** Schema migration runs on startup. Clients re-sync saves under new `path_key` scheme (one-time, no data loss). |
| **1.6.0** | Startup reconciliation upload; local status dashboard (`http://127.0.0.1:41234`); tray browser login. | No migration needed. |
| **1.5.0** | Cross-OS save sync (`path_key` now OS-neutral for PCGW-sourced games); Proton/compatdata resolution; versioned DB migrations (`PRAGMA user_version`); optimistic-concurrency push (409 on conflict). | **Back up DB.** Migration 16 merges per-OS save slots into OS-neutral keys. |
| **1.2.3** | Auto-detect Steam user ID; per-game install path overrides; admin analytics HTMX PCGW search. | No migration needed. |
| **1.1.0** | Manifest v2 (ETag/304, `deleted_game_ids`); discovery v2; `POST /api/clients/revoke`; session GC. | Upgrade server before clients for best discovery matching. |
| **1.0.17** | Full PCGW mirror (`GET /api/manifest/v2`); admin PCGW UI; `cmd/pcgw-sync`, `cmd/pcgw-fetch`. | Upgrade server before clients for manifest v2. |
| **1.0.16** | WebUI template fixes for Docker/production embeds. | No migration needed. |
| **1.0.15** | WebUI embed fix for Docker (all top-level templates). | No migration needed. |
| **1.0.14** | Client auto-update, Linux packages, Windows installer. | First release with packaged clients. |

---

## `watch_paths` behavior across upgrades

Existing client configs without `auto_watch_mode` keep **legacy** watch behavior (any manifest path whose directory exists). New installs default to **discovered** mode. See [Client Setup & Usage → Auto-watch modes](Client-Setup-and-Usage#auto-watch-modes).

---

## Related pages

- [Installation](Installation)
- [Server Configuration](Server-Configuration)
- [Troubleshooting](Troubleshooting)
- [Changelog](Changelog)
- [FAQ](FAQ)
