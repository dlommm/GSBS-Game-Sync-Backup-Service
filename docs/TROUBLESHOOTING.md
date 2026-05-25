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

Install AppIndicator support and `xdg-utils`. GNOME may need a tray extension; KDE and Xfce usually work out of the box. Details in [CLIENT.md](CLIENT.md).

### “Not logged in” or 401 errors

Token expired or revoked. Open tray **Login…** or run `gsbs-client login`. Create a new API token in WebUI **Settings** if needed.

### No games discovered

- Confirm games are installed via a supported launcher (Steam, Epic, GOG, Heroic, Lutris, Bottles, Prism, etc.).
- Set launcher folder overrides in `config.json` if auto-detect fails — see [EXAMPLE_CONFIG.md](EXAMPLE_CONFIG.md).
- Tray → **Rescan installed games** or wait for `discovery_interval` (default 4h).
- Check `discovery.json` and `gsbs.log` in the client data directory.

### Saves not syncing

- **Discovered mode** (`auto_watch_mode: "discovered"`): only installed, matched games sync. Check tray **Discovered games** — unchecked entries are disabled.
- **Folder-exists rule**: the client never creates save directories; the game must have run at least once so the folder exists.
- Verify `sync_paused` is false and Windows metered-connection skip is not blocking sync.
- Inspect `gsbs.log` for pull/push errors and `outbox/` for queued uploads.

### Manifest fetch fails

Clients try `GET /api/manifest/v2` first, then fall back to v1. Upgrade the server to v1.0.17+ for v2. Delete `manifest.json` in the client data dir to force a full re-fetch (rarely needed).

## Logs and config

| Component | Location |
|-----------|----------|
| Client log | `%APPDATA%\gsbs\gsbs.log` (Windows) or `~/.config/gsbs/gsbs.log` (Linux) |
| Client config | Same directory, `config.json` |
| Server (Docker) | `docker compose logs` |

Set `GSBS_LOG_LEVEL=debug` on the server or `"verbose_log": true` in client config for more detail.

## Still stuck?

1. Reproduce with verbose logging enabled.
2. Check [GitHub Issues](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/issues).
3. For security concerns see [SECURITY.md](../SECURITY.md).
