# Changelog

> All notable changes to GSBS, newest first. Format based on [Keep a Changelog](https://keepachangelog.com/).

For the complete machine-readable changelog, see [CHANGELOG.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/CHANGELOG.md) in the repository.

---

## [2.0.0] — 2026-06-07

### Fixed

- Server: `GET /api/saves?summaries=1` 500 errors — enriched error logs (`user_id`, `limit`, `offset`, `request_id`, `error_class`); 503 returned for `db_locked` errors; quota checks now fail-closed (storage byte errors return 503 instead of silently bypassing quota).
- Client: updater silently broken due to missing `json:"tag_name"` struct tag on `ghRelease` — version comparison never ran in production.
- Client: manual "Check for updates" showed "latest" on all failures including network errors, API errors, and metered skips.
- Windows: fsnotify overflow events silently dropped watched file changes — now triggers a directory rescan.
- Windows: locked files after push retries were silently dropped — now enqueued to the persistent outbox.

### Added

- Server: panic recovery middleware with structured log and request-ID correlation.
- Server: HTTP security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP, HSTS).
- Server: dashboard partial error states with inline HTMX retry notice on `StoreError`.
- Server: disabled-user session cutoff — `requireSession` checks `IsUserDisabled` and revokes the session immediately.
- Server: `RevokeAllClientTokens` — password change and 2FA disable now revoke all active client tokens.
- Client: typed `UpdateCheckResult` with explicit statuses: `available`, `up_to_date`, `disabled`, `metered_skip`, `network_error`, `api_error`, `manifest_mismatch`, `unsupported_arch`.
- Client: in-progress tray state during update checks; distinct messages per check outcome.
- Client: `ErrUnauthorized` sentinel — outbox stops hammering on 401; re-login message surfaced in local dashboard and tray tooltip.
- Client: local dashboard `/status` exposes updater last-check status and `auth_failed` state.
- CI: Windows test job, `govulncheck`, `latest-client.json` completeness guard in `release-assets.sh`.

### Removed

- Server: `PCGWSyncLegacy`, `PCGWSyncFull` unused wrapper functions; unused store methods; orphan template partials; stale form action.
- Client: unused dead functions `minimizedMode()`, `parseDurationFlex()`, `RecordConflictSimple()`.

---

## [1.6.0] — 2026-06-07

### Added

- **Startup reconciliation upload:** Client scans local save files at startup and uploads any missing on the server, independent of file-change events.
- **Local status dashboard:** `http://127.0.0.1:41234/dashboard` — live sync status, watched games, pending uploads, conflicts, last sync result. Auto-refreshes every 5 seconds.
- **Tray "Local status page"** in **Advanced** submenu.
- **Sync Now** button on local dashboard.

### Changed

- **Tray "Login…"** now opens the browser-based setup page by default on Windows. Walk native dialog is a fallback.
- **Windows watcher path matching** is now case-insensitive.
- **Watcher file-lock retry:** debounce push retries stat/read up to 3 times (300ms apart) on Windows sharing-violation errors.
- **Modernized setup/login HTML:** Tailwind CSS, dark-mode support, card layout.

### Fixed

- Push diagnostics: `watcher_event_unmapped` log op for paths not registered in the watcher.
- Push hash cache I/O errors surfaced as structured log ops instead of silently swallowed.
- Non-specific push HTTP errors log the first 512 bytes of the response body.

---

## [1.5.0] — 2026-06-06

### Added

- **Cross-OS save sync:** `path_key` is now OS-independent for PCGW-sourced games.
- **Proton/compatdata path resolution:** synthesizes `compatdata/<appid>/pfx/…` paths for Windows games under Steam/Proton on Linux.
- **Versioned DB migrations:** `PRAGMA user_version`-based transactional migrations; `GSBS_DRY_RUN_MIGRATION=1` dry-run mode.
- **Optimistic-concurrency push:** `X-GSBS-If-Hash` header; server returns 409 on mismatch.
- **SSE reliability:** 30s heartbeat; per-user cap of 5 connections; 64 KB line buffer.
- **Manifest `schema_version`** field in v2 responses.
- Client: per-game sync-readiness diagnostics (`ready`, `no_manifest_entry`, `wrong_platform`, `save_dir_missing`, `malformed_rules`, `disabled`).
- Client tray: **"Add a game manually…"** item.
- Client tray: grouped **Account & Setup** and **Advanced** submenus.
- Client: structured sync logging via `client/logx`.
- Client CLI: `gsbs-client debug-sync <game_id> [--dry-run]`.
- Client: persisted push-dedup cache; bounded concurrent pushes; graceful shutdown.

### Changed

- `GSBS_SESSION_SECRET` is now required; server exits on startup if unset.
- `backup_on_pull` defaults to `true` for new installs.
- Manifest ETag is content-derived (stable across identical responses).
- golangci-lint upgraded to v2.12.2.

### Fixed

- Pulled saves and backups written atomically (tmp + rename), eliminating torn-file corruption.
- Many concurrency and correctness fixes (data races, deadlocks, lock releases).

---

## [1.2.3] — 2026-05-30

- Client: auto-detect Steam user ID from `loginusers.vdf`.
- Client config: `steam_library_folders`, `game_install_paths` overrides.
- Admin analytics: expanded Overview/PCGW/Sync tabs, HTMX PCGW catalog search.

---

## [1.2.2] — 2026-05-30

- `pkg/ico`: shared Windows `.ico` encoder.
- Admin analytics: PCGW sync run history tab, parse-failure count.

---

## [1.2.1] — 2026-05-25

- Admin PCGW page: fix "Template error" while sync is running.
- Unraid compose example updated.

---

## [1.2.0] — 2026-05-25

- Docker Scout remediation; non-root server container (`gsbs` UID 1000); Dockerfile `HEALTHCHECK`.
- File-backed save storage (`GSBS_SAVE_ROOT`).
- Admin analytics and settings UI; PCGW job filters and configurable cron.

---

## [1.1.0] — 2026-05-25

- Manifest v2 (ETag/304, `deleted_game_ids`, OS `platform` filter).
- Discovery v2 index with launcher IDs and tray toggle.
- `POST /api/clients/revoke`; session GC on startup.
- Added `docs/TROUBLESHOOTING.md`, `docs/UPGRADE.md`.

---

## [1.0.16 / 1.0.17] — 2026-05-24

- Full PCGamingWiki mirror; `GET /api/manifest/v2`; admin PCGW UI.
- CLI tools: `cmd/pcgw-sync`, `cmd/pcgw-fetch`.
- WebUI template bug fixes for Docker production embeds.

---

## [1.0.14 / 1.0.15] — 2026-05-24

- Windows Inno Setup installer; Linux `.deb` and AppImage packages.
- Client auto-update from GitHub Releases (`latest-client.json`, SHA256 verification).
- GitHub Actions release workflow (tag → build → GitHub Release + Docker Hub).
- WebUI embed fix for Docker.

---

For full details on any release, see [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases) and the [repository CHANGELOG](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/CHANGELOG.md).

---

## Related pages

- [Upgrading](Upgrading)
- [Home](Home)
- [FAQ](FAQ)
