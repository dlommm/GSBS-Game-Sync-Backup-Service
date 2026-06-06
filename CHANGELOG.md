# Changelog

All notable changes to GSBS are documented here. Format based on [Keep a Changelog](https://keepachangelog.com/).

## [1.5.0] - 2026-06-06

### Added

- **Cross-OS save sync**: `path_key` is now OS-independent for PCGW-sourced games — Windows and Linux/Steam Deck saves for the same game converge to a single server slot.
- **Proton/compatdata path resolution**: client synthesizes `compatdata/<appid>/pfx/...` paths for Windows-template games running under Proton/Steam on Linux; multi-library and multi-account detection with ModTime-based fallback.
- **Versioned DB migrations**: `PRAGMA user_version`-based transactional migrations with a 3-second backup warning on destructive steps; `GSBS_DRY_RUN_MIGRATION=1` dry-run mode.
- **Migration step 16**: merge-aware collapse of per-OS save slots into OS-neutral keys; loser preserved in version history.
- **Optimistic-concurrency push**: `X-GSBS-If-Hash` request header enables conflict detection; server returns HTTP 409 with the current hash and version on mismatch.
- **SSE reliability**: 30-second heartbeat keeps connections alive; per-user connection cap of 5 prevents ghost accumulation; 64 KB line buffer with appropriately sized `bufio.Scanner`.
- **Manifest `schema_version`** field in manifest v2 responses for forward-compatibility signaling.
- `xdgcachehome` placeholder resolves to `$XDG_CACHE_HOME` / `~/.cache` on Linux.
- Client: **per-game sync-readiness diagnostics** — each discovered game is classified (`ready`, `no_manifest_entry`, `wrong_platform`, `save_dir_missing`, `malformed_rules`, `disabled`); shown inline in the tray, tooltips, `debug-sync` output, and `game_sync_readiness` structured logs.
- Client tray: **"Add a game manually…"** item opens a local browser page to search the manifest or add a save folder by path; writes a `watch_paths` entry and restarts sync.
- Client tray: grouped items into **Account & Setup** and **Advanced** submenus for a cleaner top-level menu.
- Client: structured sync logging via `client/logx` (`GSBS_LOG_LEVEL=debug|info|warn|error`) with `game_id`, `path_key`, `relative_path`, and error fields on watcher, outbox, and push paths.
- Client CLI: **`gsbs-client debug-sync <game_id> [--dry-run]`** — inspect resolved watch paths and optionally force-push saves for a single game.
- Client: persisted push-dedup cache (`push_hash_cache.json`) survives restarts; bounded to 4 concurrent pushes; graceful shutdown flushes the watcher debounce and drains the outbox.
- Client tray: push-failure and auth-error toasts (`OnPushError`, `OnAuthError`).
- Server manifest v2: `deleted_game_ids` on delta responses when games are removed from the PCGW catalog.
- `pkg/saverule`: `ValidateRule` / `FilterValidRules` for save-rules sanity checks.

### Changed

- **`GSBS_SESSION_SECRET` is required**: server exits with a clear error on startup if the env var is unset.
- `backup_on_pull` defaults to `true` for new installs.
- Docker release pipeline: platform builds must all succeed before the image is pushed to Docker Hub.
- golangci-lint upgraded to v2.12.2; explicit `.golangci.yml` config committed.
- Manifest ETag is now content-derived (stable across identical responses; eliminates spurious re-downloads).
- Redundant token re-validation on push eliminated; `clientID` is passed via request context.
- `docker-entrypoint.sh` volume chown is conditional (top-level only, not recursive) to avoid delays on large mounts.
- `-trimpath -s -w` added to all release builds.
- Client outbox: stores `relative_path` and file references (re-read at send time) instead of base64 blobs; dedupes pending entries per `(game_id, path_key)`; mutex prevents concurrent drains.
- Client watcher: exclude patterns support relative-path globs (e.g. `cache/*`); debounced pushes are cancelled when their watch root is removed.
- Client manifest v2: a successful empty v2 response is now authoritative (no spurious v1 fallback); delta merge keys include save-rule identity.
- Client `resolveSavePath`: uses discovered/config install roots from `BuildInstallRootsByGame` (Proton/Steam libs/launchers).
- Client push: reloads token from config once on HTTP 401 before surfacing an auth error.
- Discovery: Steam/PCGW lookups use shared `pkg/pcgw` rate limiting; lookup failures are logged.

### Fixed

- Pulled saves, `.gsbs.bak` backups, and `conflicts.json` are now written atomically (tmp + rename), eliminating torn-file corruption on crash or power loss.
- Shutdown flush uses a fresh context so saves in flight at SIGTERM are not dropped.
- Manifest cache slice was mutated and shared across goroutines; fixed with a copy-before-filter.
- `handleRestoreSaveVersion` now respects read-only mode.
- `UpsertSaveWithMeta` is wrapped in a transaction, eliminating version conflicts under concurrent pushes.
- `migrateTokenHashes` and `migrateBlobsToFS` deadlock on upgrade fixed (collect all rows, then update).
- `DeleteUser` is now fully transactional.
- `Client.token` / `authRetried` and `discoveryState` data races eliminated.
- Pull→push echo suppressed via `markPushed` after applying a pulled save.
- Outbox: backoff reset per entry; lock released across network I/O; stale entries are re-pushed instead of dropped.
- PCGW client retries on 5xx and transient network errors; MediaWiki `error`-field-on-200 responses are detected and surfaced.
- `<br>`-separated multi-paths in PCGW wikitext templates are now split correctly.
- Registry templates (`{{p|hkcu}}` etc.) excluded from the client manifest projection.
- Path templates containing `..` traversal are rejected at ingest.
- `ExtractAllTemplates` recovers from malformed `{{` instead of halting.
- Auto-update refuses to apply a binary when the SHA256 checksum is absent from `latest-client.json`.
- `GSBS_METRICS_TOKEN` is required when metrics are enabled; comparison uses constant-time equality.
- SQLite `_busy_timeout=5000` added; `MaxOpenConns` raised from 1 to 5 for WAL-mode concurrency.
- PCGW background job is drained on graceful shutdown; `last_seen` update moved inline.
- Client watcher: `<game-install-folder>` save templates now resolve correctly (install roots were used for path building but not attached to the watcher).
- Outbox replay no longer drops `X-Relative-Path` for multi-file save slots (was routing retries to the wrong server slot).
- `MergeManifestDelta` collision when multiple save_rules shared the same directory.

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

[Unreleased]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.3...v2.0.0
[1.2.3]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.2...v1.2.3
[1.5.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.1...v1.5.0
[1.2.1]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.17...v1.1.0
[1.0.17]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.16...v1.0.17
[1.0.16]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.15...v1.0.16
[1.0.15]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.0.14...v1.0.15
[1.0.14]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.14
[1.0.13]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/tag/v1.0.13
