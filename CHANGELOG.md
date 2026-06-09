# Changelog

All notable changes to GSBS are documented here. Format based on [Keep a Changelog](https://keepachangelog.com/).

## [2.1.5] - 2026-06-09

### Fixed

- **PCGW sync (critical)**: Phase 2 ingest was discarding the partial result when `IngestPage` returned an error. This caused failed pages to accumulate retries without ever being written to `pcgw_games`, eventually becoming `dead_letter=1`. Because both the "missing" and "failed/partial" queues exclude dead-lettered pages, every subsequent Phase 2 run (Auto Catch-Up, Parse Missing Only, Retry Failed Pages) saw an empty queue and exited immediately — leaving Local count frozen at 3,945 indefinitely. Fixed by persisting the stub `parse_status="failed"` row to `pcgw_games` before returning the error, so failed pages move to the retryable failed-partial queue instead of silent limbo.

### Added

- **Server/WebUI**: `ResetPCGWDeadLetter` store method + `POST /admin/pcgw/reset-dead-letter` handler — clears `dead_letter=1` and `retry_count` on all blocked catalog entries so they re-enter Phase 2 queues on the next run.
- **Server/WebUI**: "Reset Dead Letter" button in Advanced Maintenance (always visible). When blocked pages exist, a prominent warning banner with count appears above the maintenance actions.
- **Server/WebUI**: Success/error flash messages for the reset action on the Activity page.

## [2.1.4] - 2026-06-09

### Added

- **Server/WebUI**: Jobs panel shows a **backlog progress bar and ETA** when idle — ingested %, remaining pages, estimated total time, runs needed, and time per run (based on historical rate from last 3 successful runs).
- **Server/WebUI**: Admin logs **Export CSV** button — downloads current filtered log entries as a `.csv` file (time, app, level, event, summary, context, raw).
- **Client/WebUI**: Same Export CSV button on the client `/logs` page.

### Fixed

- **Server/WebUI**: Changing any log filter (Application, Level, search text, limit, Show routine HTTP) now **immediately refreshes the table** — the JS `change` handler now calls `refreshLogs()` for all controls except the auto-refresh interval selector.
- **Server/WebUI**: SSE log events no longer have a leading dot (e.g. `sse.client.subscribed` instead of `.client.subscribed`).
- **Server/WebUI**: APP column in the log table no longer wraps ("ss e", "ht tp") — added `white-space: nowrap` and removed `word-break: break-all` override for component badges.
- **Server/WebUI**: Context column truncates with ellipsis at 14rem and shows full text on hover (`title` attribute).
- **Server/WebUI**: Log table hides Context column on screens ≤900px and Event column on screens ≤640px for better mobile readability; table remains horizontally scrollable.

## [2.1.3] - 2026-06-09

### Added

- **Server/WebUI**: Admin logs **App** column and application filter (pcgw, job, sse, http, store, cron, etc.); **Hide routine HTTP** by default (health checks, static assets, log/jobs partial polling, SSE dashboard).
- **Client/WebUI**: Same application filter and App column on `/logs` (sync, auth, tray, setup, client).
- **pkg/logview**: Component-aware filtering, HTTP noise detection, enriched PCGW/job summaries (run_id, queue, ok/partial/failed), and stable event ids (`pcgw.sync`, `job.lifecycle`, etc.).

### Fixed

- **PCGW sync**: Incremental sync no longer reports success with 0 pages when the catalog is incomplete — Phase 1 rescan runs when local catalog count is below remote total; resume stats load from the resumed run id.
- **PCGW sync**: Cancel now marks the DB run and job as canceled immediately and aborts in-flight HTTP/rate-limit waits via context cancellation.
- **PCGW sync**: Refresh-new uses force-full ingest; canceled runs are no longer picked up as resumable.

### Changed

- **pkg/pcgw**: All PCGW HTTP and rate-limit sleeps honor `context.Context` so job cancel stops network activity promptly.
- **Server**: HTTP request logs include `component=http` for filtering.

## [2.1.2] - 2026-06-09

### Added

- **Server/WebUI**: Admin logs page now parses structured zerolog JSON into Event, Summary, Context, and expandable raw Details columns; level badges, richer search (method, path, status, user_id, request_id), and improved HTTP request messages.
- **Client/WebUI**: New `/logs` page on the local setup server with the same filter/search/auto-refresh table UX as server admin logs; linked from topbar, Help, and Quick actions.
- **pkg/logview**: Shared log tail/filter/parse helpers for server zerolog JSON and client slog/plain text lines.

### Changed

- **Server**: Migrated SSE hub, PCGW jobs, store migrations/reconcile, and PCGW cron from stdlib `log` to `logx` so all operational logs appear in the unified service log file the WebUI reads.

## [2.1.1] - 2026-06-09

### Fixed

- **Server/WebUI**: PCGW sync controls (Quick Actions, Advanced Maintenance, Import/Export, Destructive Actions) removed from the PCGW page — they now appear exclusively on Activity & Jobs where they belong.
- **Server/WebUI**: Fixed runtime template error on Activity & Jobs page caused by `admin_pcgw_actions.html` referencing `.ResumableSyncRun`, a field that was missing from `jobsViewData`; field is now populated from `GetResumablePCGWSyncRun`.
- **Client/WebUI**: Setup page now shows the real GSBS icon (`gsbs-icon.png`) embedded in the binary instead of the SVG fallback; `handleClientLogo` falls back to the embedded logo.png.
- **Client/WebUI**: Setup page wizard step pills and discovery panel replaced all inline styles with proper CSS classes (`.wizard-steps`, `.discovery-*`).

### Changed

- **Server/WebUI**: Sync status now shows a human-readable phase label ("Phase 1: Listing game catalog" / "Phase 2: Parsing game data"), a progress bar with ARIA attributes, elapsed run time, and estimated time remaining computed from current throughput and historical average of the last 3 successful runs.
- **Build**: `script/build-webui.sh` now copies `docs/images/gsbs-icon.png` (not the server logo) to `client/webui/static/logo.png` on each CSS rebuild.

## [2.1.0] - 2026-06-09

### Added

- **Unified WebUI design system**: Server and client browser UIs now share one compiled dark theme (indigo accent, DM Sans / JetBrains Mono), vendored woff2 fonts (no CDN), and semantic component classes (`.panel`, `.stat-card`, `.btn-primary`, `.topbar`, etc.).
- **Client WebUI package** (`client/webui/`): Embedded Go templates replace inline HTML in `setup_server.go`; pages for Setup, Dashboard, Games, Quick Actions, Help, About, and Open Log; `/static/` served from embedded assets (works offline).
- **Shared toast notifications**: `toast.js` on server and client for success/error/info/warn feedback; server wires `audit-updated` SSE events to toasts.
- **PCGW admin polish**: Six-card stats summary (total games, save locations, last sync, status), breadcrumb, improved action cards with `aria-describedby`, idle job state, and table caption for accessibility.
- **Client WebUI tests**: `client/webui/template_names_test.go` — parse and render tests for all client pages.

### Changed

- **Build**: `script/build-webui.sh` compiles Tailwind once and syncs `app.css`, fonts, favicon, and logo to both `server/webui/static/` and `client/webui/static/`.
- **Server WebUI**: Removed Google Fonts CDN; self-hosted fonts; `scope="col"` on table headers; improved empty states, ARIA labels, and mobile responsive rules for admin timeline and PCGW controls.
- **Client local UI**: Dropped Tailwind CDN; unified topbar nav (About included); form validation uses toasts instead of `alert()`.
- **Documentation**: `docs/ARCHITECTURE.md` and `docs/CLIENT.md` updated for shared WebUI architecture.

### Fixed

- **Server/job**: PCGW incremental sync no-op gate now accounts for missing backlog entries so Phase 2 ingest is not skipped when pages remain unprocessed.

## [2.0.6] - 2026-06-08

### Fixed

- CI/tests: normalized Windows path separator handling in log source tests so cross-platform test assertions are stable on `windows-latest`.

## [2.0.5] - 2026-06-08

### Fixed

- Admin/WebUI logs: `/admin/logs` now resolves file sources in a robust order (`GSBS_SERVICE_LOG_PATH`, then legacy `GSBS_LOG_FILE`, then Windows default path) and shows clearer guidance when no readable log file exists yet.
- Server logging init: console mode now honors `GSBS_SERVICE_LOG_PATH` / `GSBS_LOG_FILE` for file-backed logging when configured, with safe fallback to stdout if file initialization fails.

## [2.0.4] - 2026-06-08

### Added

- Admin/WebUI: new `/admin/logs` page with level filtering, text search, line limit control, and optional auto-refresh polling.
- Admin/WebUI: new `Auto Catch-Up Missing Backlog` action that repeatedly runs budgeted Phase 2 ingest cycles until backlog clears (with cancel support).
- Server/job: explicit `MaxPagesPerRunWithSource()` parsing to report effective Phase 2 cap source/value in admin UI.

### Changed

- Admin/WebUI: PCGW sync action labels now explicitly describe Phase 1 vs Phase 2 behavior (IDs refresh vs parse/store backlog) to reduce operator confusion.
- Admin/WebUI: jobs/status messaging now clearly distinguishes catalog scan completion from budgeted ingest progress and cap-reached resume behavior.

### Fixed

- Server/job: resume ingest no longer reuses stale queue cursor indexes against rebuilt queues, preventing skipped or stalled backlog progress after interrupted runs.
- Admin/WebUI: destructive PCGW wipe flow now uses clean confirmation prompts (removed typed `WIPE PCGW` modal and stale loading state).

## [2.0.3] - 2026-06-08

### Added

- Admin/WebUI: new `Sync Missing Local` action on Activity & Jobs to explicitly process remote catalog entries that are missing locally.
- Admin/WebUI: action-specific flash feedback for `Retry Failed Items` and `Sync Missing Local`, plus tests for the new messaging paths.

### Changed

- Admin/WebUI: moved PCGW sync/import/export/maintenance/destructive controls from the PCGW page to Activity & Jobs, with cleaner card-based formatting and a dedicated wipe confirmation modal.

### Fixed

- Server/store: `limit=0` in PCGW catalog list queries now means unbounded (instead of silently capping to 500), which prevented large missing backlogs from being enqueued for ingest.
- Admin/WebUI: `Retry Failed Items` now reports accurate start failures (`job_already_running` vs generic start failure) instead of appearing as a no-op.

## [2.0.2] - 2026-06-07

### Added

- Windows server: native Service Control Manager support in `gsbs-server` (`--service`, `--install-service`, `--uninstall-service`, `--start-service`, `--stop-service`) with shared startup/shutdown lifecycle for console and service modes.
- Windows server: `--env-file` support and default ProgramData env loading, so service installs can reliably boot with installer-generated configuration.
- Windows server: service-mode file logging via `GSBS_SERVICE_LOG_PATH` (default `C:\ProgramData\GSBS\logs\server.log`).
- Release: Windows server installer artifact `gsbs-server-setup-X.Y.Z-windows-amd64.exe` added to release workflow and checksums.

### Changed

- Windows installer: server deployment is now service-first (recommended) instead of scheduled task startup.
- Windows installer: generated config now includes ProgramData-based service log path defaults and service management shortcuts.
- Documentation: installation/server configuration/release docs now cover Windows service deployment and log locations.

## [2.0.1] - 2026-06-07

### Added

- Admin/WebUI: first-run onboarding guidance in login/register screens and a "Getting Started" panel on Admin Overview when the instance is empty.

### Changed

- Admin/WebUI: consolidated PCGW controls into clearer sections (status, jobs, import/export, maintenance, destructive actions) with improved layout and helper text.
- Client tray: status icons now use tinted GSBS logo variants and add a distinct "recovering watcher" icon when watcher health is degraded.
- Build: Windows server release binary now builds with `CGO_ENABLED=1` to support sqlite in release artifacts, while client remains `CGO_ENABLED=0`.

## [2.0.0] - 2026-06-07

### Fixed

- Server: `GET /api/saves?summaries=1` 500 errors — enriched error logs (user_id, limit, offset, request_id, error_class), 503 returned for db_locked errors, quota checks now fail-closed (storage byte errors return 503 instead of silently bypassing quota).
- Client: updater silently broken due to missing `json:"tag_name"` struct tag on `ghRelease` — version comparison never ran in production.
- Client: manual "Check for updates" showed "latest" on all failures including network errors, API errors, and metered skips.
- Windows: fsnotify overflow events silently dropped watched file changes — now triggers a directory rescan to catch missed events.
- Windows: locked files after push retries were silently dropped — now enqueued to the persistent outbox.

### Added

- Server: panic recovery middleware with structured log and request-id correlation.
- Server: HTTP security headers baseline (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP, HSTS) via `securityHeaders` middleware.
- Server: dashboard partial error states with inline HTMX retry notice on `StoreError`.
- Server: disabled-user session cutoff — `requireSession` checks `IsUserDisabled` and revokes the session immediately.
- Server: `RevokeAllClientTokens` — password change and 2FA disable now revoke all active client tokens.
- Client: typed `UpdateCheckResult` with explicit statuses: `available`, `up_to_date`, `disabled`, `metered_skip`, `network_error`, `api_error`, `manifest_mismatch`, `unsupported_arch`.
- Client: in-progress tray state during update checks; distinct messages per check outcome (no more silent "latest" on failure).
- Client: `ErrUnauthorized` sentinel — outbox stops hammering on 401, surfaces re-login message in local dashboard and tray tooltip.
- Client: local dashboard `/status` exposes updater last-check status and `auth_failed` state.
- CI: Windows test job (`windows-latest`), `govulncheck` step, `latest-client.json` completeness guard in `release-assets.sh`.

### Removed

- Server: `PCGWSyncLegacy`, `PCGWSyncFull` unused wrapper functions; `GetPCGWGameByPageName`, `UpdatePCGWGameSyncState` unused store methods.
- Server: orphan template partials (`stat_card.html`, `quota_bar.html`, `chart_svg.html`); stale `/admin/pcgw/sync/resume` form action.
- Client: unused dead functions `minimizedMode()`, `parseDurationFlex()`, `RecordConflictSimple()`.

## [1.6.0] - 2026-06-07

### Added

- **Startup reconciliation upload**: Client scans local save files at startup and uploads any that are missing on the server, independent of file-change events. Ensures saves are seeded even on first run or after server resets. Logs `reconcile_upload` / `reconcile_skip_unchanged` per file.
- **Local status dashboard**: New local web page at `http://127.0.0.1:41234/dashboard` (also available via tray **Advanced → Local status page**) shows live sync status, watched games, pending uploads, conflicts, and last sync result. Auto-refreshes every 5 seconds.
- **Tray "Local status page"** item in **Advanced** submenu opens the local dashboard in the system browser.
- **Sync Now** button on local dashboard triggers an immediate sync.

### Changed

- **Tray "Login..." now opens the browser-based setup page by default** on Windows. The Walk native dialog is retained as a fallback only when the local setup server fails to bind a port. This provides a modern, consistent login experience matching the server WebUI.
- **Windows watcher path matching** is now case-insensitive — avoids missed uploads when fsnotify returns a differently-cased path than the registered watch directory.
- **Watcher file-lock retry**: debounce push retries stat/read up to 3 times (300ms apart) when a Windows sharing-violation or file-lock error is detected, reducing missed uploads caused by games holding exclusive write locks during save.
- **Modernized setup/login HTML**: Setup and add-game pages now use Tailwind CSS (loaded via CDN), dark-mode support, and a clean card layout matching the server WebUI style.

### Fixed

- **Push diagnostics**: `watcher_event_unmapped` log op identifies fsnotify events that arrive for a path not registered in the watcher (useful for diagnosing watch root mismatches on Windows).
- Push hash cache I/O errors are now surfaced as `push_cache_load_error` / `push_cache_write_error` structured log ops instead of being silently swallowed.
- Non-specific push HTTP errors now log the first 512 bytes of the response body as `push_http_error` for easier server-side triage.

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
[2.0.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.6.0...v2.0.0
[1.6.0]: https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/compare/v1.5.0...v1.6.0
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
