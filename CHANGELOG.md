# Changelog

All notable changes to GSBS are documented here. Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [1.2.3] - 2026-05-30

### Added

- Client: auto-detect Steam user ID from `loginusers.vdf` (saved to `launcher_user_id` when empty).
- Client config: `steam_library_folders` for extra Steam library roots; `game_install_paths` per-game install folder overrides for `<game-install-folder>`.
- Client discovery: record Steam install path from `appmanifest` `installdir` and merge with PCGW hints when resolving save paths.
- PCGW placeholder map: `%USERPROFILE%`, `%APPDATA%`, `%LOCALAPPDATA%`, `%PROGRAMFILES(x86)%`, Saved Games, Documents, launcher/XDG paths; `{{p|game}}` → `<game-install-folder>`.
- Admin analytics: expanded Overview/PCGW/Sync tabs, HTMX PCGW catalog search, richer breakdowns and partial table.
- Tests: PCGW path splitting, placeholder normalization, Steam loginusers parsing, analytics store queries.

### Changed

- Client path resolution: split save rules on `|` only outside `{{...}}` templates; `ResolveAllForGame` for install-folder placeholders.
- Admin Settings and Users pages: form layout, dark-theme inputs, compact action menus (fixed dropdown clipping).
- Docs: `CLIENT.md` and `EXAMPLE_CONFIG.md` document new path override options.

### Fixed

- PCGW manifest paths corrupted by splitting inside `{{p|key}}` placeholders (e.g. `{{p`, `steam}}/userdata/...`).
- Admin WebUI Settings (PCGW sync schedule/filters) and Users (create dialog, actions menu) broken styling/layout.

## [1.2.2] - 2026-05-30

### Added

- `pkg/ico`: shared Windows `.ico` encoder (multi-size, XOR + AND mask); used by client tray icons and `cmd/write-ico`.
- Admin analytics: PCGW sync run history tab and parse-failure count; store `ListPCGWSyncRuns` and `CountPCGWParseFailures`.

### Changed

- Client tray: embed `client/icon.ico` (16×16 + 32×32); state icons generated via `pkg/ico`.
- Admin analytics and users pages: layout and styling polish.

### Fixed

- CI lint job: install Linux systray build deps so `client/` typechecks on Ubuntu runners.

## [1.2.1] - 2026-05-25

### Fixed

- Admin PCGW page: fix "Template error" while a sync is running (missing job progress fields on full page render).
- Unraid compose example: add `GSBS_SAVE_ROOT`, optional BLOB-to-filesystem migration env, and updated docs.

## [1.2.0] - 2026-05-25

### Added

- Docker Scout remediation: upgrade `golang.org/x/crypto` and `golang.org/x/sys`; non-root server container (`gsbs` UID 1000) with entrypoint volume ownership fix; Dockerfile `HEALTHCHECK`.
- File-backed save storage (`GSBS_SAVE_ROOT`), save-path safety rules (`pkg/savepath`, `pkg/saverule`), and sync path hardening.
- Admin analytics and settings UI; PCGW job filters, status, and configurable cron via DB/env.
- PCGW bundle export/import; admin settings persistence.
- Docs: [DOCKERHUB.md](docs/DOCKERHUB.md), [SYNC_REFACTOR.md](docs/SYNC_REFACTOR.md).

### Changed

- Client sync: improved watcher, manifest matching, and pull/push path resolution.
- PCGW sync runner: progress ETA, filters, and admin job status badges.
- Docker runtime: Alpine 3.23.4 base with `apk upgrade`; expanded `.dockerignore`.

### Fixed

- CI lint: errcheck on row close, gofmt, errorlint, and related staticcheck issues.

## [1.1.0] - 2026-05-25

### Added

- Client manifest v2 cache: ETag/`If-None-Match`, `deleted_game_ids`, persisted v2 game metadata, OS `platform` filter.
- Discovery v2 index: `other_ids`, match reasons, tray toggle to enable/disable discovered games.
- Config keys: `bottles_folder`, `prism_folder`, `flatpak_steam_folder`.
- `POST /api/clients/revoke` for programmatic client token revocation.
- Session GC on startup and periodic purge of expired web sessions.
- Docs: [TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md), [UPGRADE.md](docs/UPGRADE.md).
- Tests: manifest v1/v2 API, client manifest fetch, store versions/clients, watcher debounce, launchers detect.

### Changed

- Client `discovered` watch mode watches nothing until games are matched (except explicit `watch_paths`).
- Tray: richer discovered game rows, manifest age and watcher health in tooltip, quota errors surfaced on push.
- WebUI: admin PCGW polish, loading skeletons, fixed admin overview SSE hooks, zerolog in critical handlers.
- CI: `-race` and coverage artifact upload; lint job on release workflow.

## [1.0.16] - 2026-05-24

### Fixed

- WebUI template naming and layout block collisions: admin pages, dashboard, and settings render correctly when templates are embedded in production builds.

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

[Unreleased]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.1...HEAD
[1.2.1]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.17...v1.1.0
[1.0.17]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.16...v1.0.17
[1.0.16]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.15...v1.0.16
[1.0.15]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.14...v1.0.15
[1.0.14]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.14
[1.0.13]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.13
