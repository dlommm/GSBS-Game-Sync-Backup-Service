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
image: dendlomm/gsbs-server:1.0.17
```

After upgrading to **v1.0.17+**, the PCGW mirror schema expands. The server migrates SQLite on startup; allow extra time on first boot. Trigger an initial PCGW sync from **Admin → PCGW** if the manifest is empty.

### Binary

1. Stop the running server.
2. Replace the binary from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases).
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
| **1.0.17** | Manifest v2 (`GET /api/manifest/v2`); clients prefer v2. Full PCGW mirror and admin UI. Upgrade server before clients for best discovery. |
| **1.0.16** | WebUI template fixes for Docker/production embeds. |
| **1.0.15** | WebUI embed fix for Docker (all top-level templates). |
| **1.0.14** | Client auto-update, Linux packages, Windows installer. |

Existing client configs without `auto_watch_mode` keep **legacy** watch behavior (any manifest path whose directory exists). New installs default to **discovered** mode. See [CLIENT.md](CLIENT.md) and [EXAMPLE_CONFIG.md](EXAMPLE_CONFIG.md).

## Rollback

- **Server**: restore the DB backup and run the previous Docker tag or binary.
- **Client**: reinstall the previous release binary or disable auto-update until resolved.

If issues persist after upgrade, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
