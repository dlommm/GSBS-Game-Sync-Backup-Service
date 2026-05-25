<p align="center">
  <a href="https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-">
    <img src="https://raw.githubusercontent.com/dlommm/GSBS--Game-Sync---Backup-Service-/main/docs/images/gsbs-logo-sm.png" alt="GSBS — Game Sync & Backup Service" width="640" />
  </a>
</p>

# GSBS Server

[GSBS](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-) (Game Sync & Backup Service) keeps game saves in sync across your PCs. This image runs the **central server** only. Install the **Windows/Linux client** from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) on each gaming machine.

Clients watch save folders, upload on change, and pull existing saves when a game is installed. The server stores one current save per user per game path, serves a PCGamingWiki-based location manifest, and provides a WebUI for registration, tokens, and admin.

## What's in this image

- REST API and embedded WebUI
- SQLite persistence (users, saves, manifest, job history)
- Scheduled PCGamingWiki sync for save-location data
- Health endpoint: `GET /api/health` (optional `?ready=1` for DB readiness)
- Runs as non-root user `gsbs` (UID 1000); entrypoint fixes `/app/data` volume ownership on startup

**Platforms:** `linux/amd64`, `linux/arm64`

**Tags:** `latest` and semver without `v` (example `1.0.17`). Match [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases).

## Quick start

```bash
docker pull dendlomm/gsbs-server:latest

docker run -d \
  --name gsbs \
  --restart unless-stopped \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="$(openssl rand -hex 32)" \
  -e GSBS_DB=/app/data/gsbs.db \
  -v gsbs-data:/app/data \
  dendlomm/gsbs-server:latest
```

## Server

Open `http://localhost:8080`, register, create an API token, and point the client at your server URL.

**Production:** Put TLS in front (Caddy, Nginx, or Traefik). The repo includes [docker-compose.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docker-compose.yml) (server + Caddy). See [docs/COMPOSE.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/COMPOSE.md).

## Persistence

Mount a volume at `/app/data` and set `GSBS_DB=/app/data/gsbs.db`. All state (users, saves, manifest) lives in that SQLite file and survives container recreation.

## Important environment variables

| Variable | Notes |
| --- | --- |
| `GSBS_SESSION_SECRET` | **Required in production.** Signs WebUI session cookies. |
| `GSBS_DB` | Database path. Use `/app/data/gsbs.db` with a volume. |
| `GSBS_ADDR` | Listen address (default `:8080`). |
| `GSBS_ADMIN_USERNAME` | Restrict `/admin` to one user. |
| `GSBS_READ_ONLY` | Set to `true` to disable push and delete. |
| `GSBS_MAX_STORAGE_BYTES` | Optional global storage cap. |

Full list: [docs/DOCKER.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/DOCKER.md)

## Architecture

```
        +---------------------------+
        |   gsbs-server (this)      |
        |  API + WebUI + SQLite     |
        +-------------+-------------+
                      |
        +-------------+-------------+
        v             v             v
   gsbs-client   gsbs-client       ...
    (Windows)      (Linux)
```

## Documentation

- [Install guide](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/INSTALL.md)
- [Docker deployment](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/DOCKER.md)
- [Client behavior](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/CLIENT.md)
- [Architecture and security](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/ARCHITECTURE.md)

## License

MIT — [LICENSE](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/LICENSE)
