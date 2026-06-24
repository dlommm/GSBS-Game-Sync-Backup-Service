<p align="center">
  <a href="https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-">
    <img src="https://raw.githubusercontent.com/dlommm/GSBS--Game-Sync---Backup-Service-/main/assets/images/dockerhub-banner.png" alt="GSBS — Game Sync & Backup Service" width="640" />
  </a>
</p>

<p align="center">
  <table align="center" cellpadding="0" cellspacing="0" role="presentation">
    <tr>
      <td align="center" bgcolor="#4338ca" style="padding: 14px 28px; border-radius: 8px;">
        <strong style="color: #ffffff; font-size: 1.1em;">🚀 GSBS v3 — Major release</strong><br>
        <span style="color: #e0e7ff;">S3 manifest bundle sync · full PCGW catalog in minutes, not days · far fewer API calls · sync reliability improvements</span>
      </td>
    </tr>
  </table>
</p>

# GSBS Server

[GSBS](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-) (Game Sync & Backup Service) keeps game saves in sync across your PCs. This image runs the **central server** only. Install the **Windows/Linux client** from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) on each gaming machine.

Clients watch save folders, upload on change, and pull existing saves when a game is installed. The server stores one current save per user per game path, serves a PCGamingWiki-based location manifest, and provides a WebUI for registration, tokens, and admin.

## What's new in v3

**Manifest ready in minutes, not days.** Fresh Docker installs default to **S3 manifest bundle sync** — a pre-built PCGamingWiki snapshot hosted on public object storage (Cloudflare R2, S3-compatible). On first start the server downloads the full catalog (~40k+ games) in one fetch instead of crawling the PCGW API page-by-page, which previously took **days** on a new server.

**Far fewer PCGW API calls.** Routine updates fetch a small `index.json` version pointer first. When nothing changed, ETag/`304 Not Modified` exits in seconds with **zero** bundle download and **no** PCGW traffic. When a new version is published, the server applies only what changed via smart merge — reducing load on both your server and PCGamingWiki.

**Other v3 highlights:**
- **Encrypted save dedup** — unchanged encrypted saves no longer re-upload every sync cycle
- **First-push overwrite guard** — prevents a fresh client from silently clobbering another machine's save
- **Crash-safe canonical saves** — atomic disk writes when using `GSBS_SAVE_ROOT`
- **Client manifest pagination (3.0.1)** — full catalog download for large game libraries

Switch sync mode anytime in **Admin → Settings** (`s3` bundle vs direct PCGW API). See [docs/MANIFEST_BUNDLE.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/MANIFEST_BUNDLE.md).

## What's in this image

- REST API and embedded WebUI
- SQLite persistence (users, saves, manifest, job history)
- **S3 manifest bundle sync** (default) or scheduled direct PCGW sync
- Versioned bundle index with ETag-aware fetch and smart merge import
- Health endpoint: `GET /api/health` (optional `?ready=1` for DB readiness)
- Runs as non-root user `gsbs` (UID 1000); entrypoint fixes `/app/data` volume ownership on startup

**Platforms:** `linux/amd64`, `linux/arm64`

**Tags:** `latest` and semver without `v` (example `3.0.1`). Match [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases).

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

On first boot the server fetches the PCGW manifest bundle automatically — clients can search and sync games within minutes.

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
| `GSBS_PCGW_SYNC_SOURCE` | `s3` (default, manifest bundle) or `api` (direct PCGW crawl). |
| `GSBS_PCGW_BUNDLE_URL` | Full bundle URL (default: official CDN). |
| `GSBS_PCGW_BUNDLE_INDEX_URL` | Version index URL (auto-derived from bundle URL if unset). |
| `GSBS_PCGW_BUNDLE_CRON` | Bundle fetch schedule when source is `s3` (default daily 04:00). |
| `GSBS_ADMIN_USERNAME` | Restrict `/admin` to one user. |
| `GSBS_READ_ONLY` | Set to `true` to disable push and delete. |
| `GSBS_MAX_STORAGE_BYTES` | Optional global storage cap. |

Full list: [docs/DOCKER.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/DOCKER.md)

## Architecture

```
        +---------------------------+
        |   gsbs-server (this)      |
        |  API + WebUI + SQLite     |
        |  ← S3 manifest bundle     |
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
- [Manifest bundle sync (S3)](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/MANIFEST_BUNDLE.md)
- [Client behavior](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/CLIENT.md)
- [Architecture and security](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/ARCHITECTURE.md)

## License

MIT — [LICENSE](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/LICENSE)
