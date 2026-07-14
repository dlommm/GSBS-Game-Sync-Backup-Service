# Upgrading GSBS

How to upgrade server and client with minimal downtime. Back up data before any upgrade.

## Before you upgrade

- **Server**: copy the SQLite database (Docker volume or `GSBS_DB` path).
- **Client**: no server-side migration needed for client-only updates; local `config.json` is preserved across upgrades.

## Server

### Docker Compose (recommended)

```bash
cd /path/to/compose
docker compose pull
docker compose up -d
```

Pin a version instead of `:latest` in production:

```yaml
image: dendlomm/gsbs-server:2.0.0
```

After upgrading to **v2.0.0**, the server runs schema migrations on startup; back up your DB first (see [DOCKER.md](DOCKER.md#data-backup)). Clients with tokens created before 2.0 continue to work unless a password change or 2FA disable occurred (those now revoke all tokens — re-login required).

### Binary

1. Stop the running server.
2. Replace the binary from [GitHub Releases](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/releases).
3. Start with the same env vars and `GSBS_DB` path.

### Release tags

Pushing tag `vX.Y.Z` publishes GitHub Release assets and Docker Hub `dendlomm/gsbs-server:X.Y.Z` and `:latest`. See [RELEASE.md](RELEASE.md).

## Client

### In-app auto-update (recommended)

The tray client checks GitHub Releases daily. When an update is available:

1. Open the tray menu → **Install update X.Y.Z…**
2. The client downloads the raw binary, verifies SHA256, replaces itself, and restarts.

Disable checks with `"update_check_enabled": false` in `config.json`.

### Installer / package

- **Windows**: run the new `gsbs-client-setup-*.exe` over the existing install.
- **Linux `.deb`**: `sudo dpkg -i gsbs-client_X.Y.Z_amd64.deb`
- **AppImage**: replace the file; config stays in `~/.config/gsbs`.

Installers are for first install; ongoing updates use the raw binary via auto-update.

## Notable version changes

| Version | Change |
|---------|--------|
| **4.0.0** | **`GSBS_SESSION_SECRET` is now optional** — if unset the server generates one into `gsbs-keys/session.secret` (back it up with the DB). A *set* value must still be 32+ chars and not a placeholder or the server won't start; replace weak ones (`openssl rand -base64 32`; rotating logs out WebUI sessions). The web **setup wizard** activates only on servers with no users, so upgrades are unaffected. The `.deb` no longer depends on `libayatana-appindicator3`. **Storage quotas now count version history** and are enforced atomically — dashboard usage will appear higher (grandfathered: over-quota users can still shrink/replace). History tables are pruned by default (`GSBS_*_RETENTION_DAYS`, 0 = keep forever). |
| **3.2.0** | Schema migration (v21): `save_versions` gains `client_id` and `change_bytes`. Back up DB first. |
| **3.0.0** | Fresh installs default to manifest **bundle sync** (GitHub mode); existing installs keep API sync. Encrypted saves re-upload **once** after upgrade (change detection moved to plaintext hashes), then converge. Disk-backed saves now fsync (crash-safe). |
| **2.0.0** | Security headers, panic recovery, fail-closed quota, disabled-user session cutoff. `RevokeAllClientTokens` on password change / 2FA disable — re-login required after credential changes. Updater JSON tag fix (version checks were silently broken). Windows fsnotify overflow rescan + locked-file outbox enqueue. `ErrUnauthorized` sentinel stops outbox on 401 — re-login to resume. |
| **1.6.0** | Startup reconciliation upload; local status dashboard; tray browser login. |
| **1.5.0** | Cross-OS save sync (`path_key` OS-neutral); Proton/compatdata resolution; versioned DB migrations; optimistic-concurrency push (409 on conflict). |
| **1.2.3** | Auto-detect Steam user ID; per-game install path overrides; admin analytics HTMX PCGW search. |
| **1.1.0** | Manifest v2 (ETag/304, `deleted_game_ids`); discovery v2 index; `POST /api/clients/revoke`; session GC. |
| **1.0.17** | Full PCGW mirror (`GET /api/manifest/v2`); admin PCGW UI; `cmd/pcgw-sync`, `cmd/pcgw-fetch`. Upgrade server before clients for best discovery. |
| **1.0.16** | WebUI template fixes for Docker/production embeds. |
| **1.0.15** | WebUI embed fix for Docker (all top-level templates). |
| **1.0.14** | Client auto-update, Linux packages, Windows installer. |

Existing client configs without `auto_watch_mode` keep **legacy** watch behavior (any manifest path whose directory exists). New installs default to **discovered** mode. See [CLIENT.md](CLIENT.md) and [EXAMPLE_CONFIG.md](EXAMPLE_CONFIG.md).

## Rollback

- **Server**: restore the DB backup and run the previous Docker tag or binary.
- **Client**: reinstall the previous release binary or disable auto-update until resolved.

If issues persist after upgrade, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
