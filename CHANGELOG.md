# Changelog

All notable changes to GSBS are documented here. Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [1.0.17] - 2026-05-24

### Added

- Full PCGamingWiki mirror: structured SQLite schema for games, sections, system requirements, metadata, sync runs, and parse failures.
- `GET /api/manifest/v2` with ETag/304; clients try v2 first and fall back to v1.
- Admin WebUI at `/admin/pcgw`: search, filters, sync controls, per-game detail, JSON export.
- CLI tools: `cmd/pcgw-sync`, `cmd/pcgw-fetch`.
- Path resolver: `%PUBLIC%` placeholder support.

### Changed

- PCGW sync: incremental updates via `last_rev_id` and content hash; section-level partial writes.
- `pkg/pcgw`: full page ingest, wikitext parsers, placeholder tokens, zstd compression, rate limiting and 429 retry.

## [1.0.15] - 2026-05-24

### Fixed

- WebUI login and all top-level pages broken in Docker: embed now includes `templates/*.html` (not only partials).
- Unraid compose example: inline config, no `.env` required ([compose-unraid.yml](docs/examples/compose-unraid.yml)).

### Added

- [docs/examples/UNRAID.md](docs/examples/UNRAID.md) — Unraid deployment guide.

## [1.0.14] - 2026-05-24

### Added

- Windows Inno Setup installer (`gsbs-client-setup-X.Y.Z-windows-amd64.exe`).
- Linux `.deb` and AppImage packages.
- Client auto-update from GitHub Releases (`latest-client.json`, SHA256 verification, tray UI).
- GitHub Actions release workflow (tag → build → GitHub Release + Docker Hub).
- Shared build scripts: `script/build.sh`, `script/release-assets.sh`.
- Docker Compose health checks, `docker-compose.dev.yml`, `.env.example`.
- Reverse proxy examples: Caddy, nginx, Traefik in `docs/examples/`.
- Documentation: [INSTALL.md](docs/INSTALL.md), [RELEASE.md](docs/RELEASE.md), [SECURITY.md](SECURITY.md).

### Changed

- README restructured for end-user install first; badges and screenshot strip.
- Production `docker-compose.yml` exposes server only to Caddy (not host port 8080).

## [1.0.13] — previous release

See [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases) for earlier history.

[Unreleased]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.17...HEAD
[1.0.17]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.16...v1.0.17
[1.0.15]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.14...v1.0.15
[1.0.14]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.14
[1.0.13]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.13
