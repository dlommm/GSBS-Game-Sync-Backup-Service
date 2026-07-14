# Changelog

> All notable changes to GSBS, newest first. Format based on [Keep a Changelog](https://keepachangelog.com/).

For the complete machine-readable changelog, see [CHANGELOG.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/CHANGELOG.md) in the repository. This page summarizes the highlights of each release.

---

## [5.3.0] — 2026-07-14

Full-project fix & polish release.

### Highlights
- **Clients no longer sync GSBS's own `.gsbs.bak` backup files** (they were uploaded as bogus save slots on default settings — delete any existing `*.gsbs.bak` slots from the game page).
- **`keep_local` keeps both versions**: a newer server copy surfaces as a conflict instead of overwriting your local file; resolving "keep local" no longer loops on cross-machine conflicts.
- **Pull integrity**: downloads are verified against the server-recorded content hash before anything touches disk; the path-escape guard runs before any write and covers install-folder saves.
- **linux/arm64 clients can finally auto-update**; Windows update apply retries + rolls back; macOS apply failures notify instead of dying silently.
- Client resilience: no more overlapping sync loops after settings changes; SSE listener detects dead connections and handles token rotation/revocation.
- WebUI: Manifest/PCGW/Analytics tables use the shared pager; import errors surface; audit toasts scoped to you; `?error=` text is never reflected; Encryption Center flags stale legacy-only devices.

---

## [5.2.4] — 2026-07-07

### Added

- **HTTPS without a proxy**: set `GSBS_TLS_CERT`/`GSBS_TLS_KEY` and GSBS serves TLS itself — handy on a LAN. And Admin → Settings gained an HTTPS section that shows your current status and both setup recipes (the Setup Health "Fix" link now actually leads somewhere).

---

## [5.2.3] — 2026-07-07

### Fixed

- Client restarts (including after self-updates) no longer look alarming: a cache-labeling bug forced a full manifest re-download on every start, and the save-path safety guard logged ~120 warnings per rebuild. The cache now heals itself and the guard logs one summary line instead.

---

## [5.2.2] — 2026-07-07

### Changed

- The WebUI's green gradient border is gone — the app now fills the window edge-to-edge (server and client). The gradient look remains only on the login and setup screens.

---

## [5.2.1] — 2026-07-06

Accessibility polish and one convenience: updates now install straight from the client's Status page.

### Added

- **"Install now"** on the client update card — same safe download-verify-restart flow as the tray.

### Fixed

- Muted text everywhere now meets WCAG AA contrast in both themes, and every page has a proper screen-reader heading — a full axe-core audit passes with zero violations.

---

## [5.2.0] — 2026-07-06

Phase 3 of the v5 overhaul: the connected features. Conflicts, notifications, encryption state, and play sessions all become visible where you are — not just on the machine where they happened.

### Added

- **Conflict Center** — see and resolve sync conflicts from any browser; the tray flow still works, and a successful push clears things automatically.
- **Notification bell** — every event lands in an in-app inbox with deep links, webhooks optional.
- **Encryption Center** — know exactly which device is keeping your fleet on legacy crypto, and how much of each game is encrypted.
- **Play-session markers** — the version timeline shows which saves came from which play session.
- **Per-game conflict policies** — now editable in the client's Settings.
- **PCGW guided status** — the admin sync page tells you the one thing worth doing next.

Migrations 30–32 run automatically on first start (back up first, as always).

---

## [5.1.0] — 2026-07-06

Phase 2 of the v5 overhaul: the redesigned surfaces come alive with real-time and self-care features.

### Added

- **Live Sync Pulse** — watch saves arrive in real time on the dashboard rail; device rows pulse while syncing.
- **Device Health** — app versions, stale-device callouts, and an honest token-expiry countdown; clients now auto-rotate their tokens monthly so they never silently expire.
- **Setup Health** — the admin overview checklist is now live: HTTPS, backups, first device/sync, notifications, and 2FA are each checked against real state with fix links.
- **Restore confidence** — Insights shows everyone whether server backups and integrity checks are passing.
- **Storage breakdown** — see each game's version-history footprint and prune it to policy with one audited click.

---

## [5.0.0] — 2026-07-06

A complete visual redesign of every GSBS interface — the v5 look. Same features, new design
language, plus two small bug fixes the redesign surfaced.

### Changed

- **New design language everywhere.** Dark charcoal app shell floating on a mint→teal gradient,
  one disciplined teal accent, and green/amber/red reserved for status meaning — across the
  server WebUI, the admin area, the setup wizard, and the client's local pages. The light theme
  derives from the same palette and keeps the signature gradient backdrop.
- **Sidebar navigation.** User pages get a left sidebar with icon pills — and **Devices** joins
  the primary navigation. The top bar becomes a rounded search field (opens the command palette),
  theme toggle, and account controls; on phones the sidebar is a slide-in drawer.
- **Dashboard redesign.** A "Recent games" card grid (cover art, sync-status glyph, health badge,
  "View Versions" button) with your devices in a side rail.
- **Accessibility.** All filled teal elements use dark text (WCAG AA contrast), and the UI now
  honors your system's reduced-motion preference.

### Fixed

- The client's logs page "Export CSV" button now works (the link previously went nowhere).
- The client dashboard's connection/sync/watcher status dots now actually render in their cards
  (a CSS collision had hidden them since 4.1.0).
- `/api/openapi.json` reports the running server's real version.

---

## [4.3.0] — 2026-07-05

Login fix for admin-created accounts, a WebUI table overhaul, security dependency bumps, and saner sync-schedule defaults.

### Fixed

- **Users created from Admin → Users can now log in from the desktop client.** The admin create-user form skipped the 8-character password minimum that login enforced, so admin-created accounts with short passwords could never sign in — it looked like only admins could use the client. Creation now enforces 8–72 characters, login no longer applies a minimum (existing short-password accounts work immediately, no reset needed), the client stops trimming whitespace off passwords, and login errors show the real reason instead of a bare HTTP status.

### Added

- **Paginated, filterable tables everywhere.** Activity & Jobs (all job types + job/status filters), audit log (action filter, search, CSV export), Server Logs (Newer/Older paging with match counts), and dashboard Recent Activity (Load more) — plus a per-table "Table ⚙" menu with column show/hide, compact density, and zebra stripes, remembered per browser.

### Changed

- **Default sync schedules are now weekly Monday 03:00** (bundle fetch and API sync), timed after the public manifest publisher's Sunday run; the client's default pull interval is now 6 hours (file changes still push immediately). Existing settings are untouched.

### Security

- **CVE-2026-39821 (9.6 critical) and five medium CVEs fixed** via `golang.org/x/net` v0.56.0; the Docker image moves to Alpine 3.24.1, clearing the sqlite (CVE-2026-11822/11824) and busybox (CVE-2025-60876) advisories still unpatched on Alpine 3.23.

---

## [4.2.0] — 2026-07-05

macOS correctness release: the Mac client now understands it's a Mac, and can update itself.

### Fixed

- **The macOS client no longer treats itself as Linux.** It now matches macOS save rules from the catalog (previously it applied Linux rules), resolves paths under `~/Library`, and detects installed Steam/Epic/Heroic games in their Mac locations. This fixes a serious bug where a mislabeled catalog entry synced hundreds of unrelated `~/Library/Preferences` `.plist` files to the server as "12 Orbits" saves — if you ran the 4.0–4.1 Mac client, delete that game's saves on your server.
- **The watch-safety guard now covers macOS**: `~/Library` and its key subfolders can never be watched wholesale, no matter what the catalog says.
- **Catalog ingest keeps filenames.** Single-file save locations no longer widen into whole-directory sync rules; run a full PCGW resync after upgrading the server.
- **Fixed a client deadlock** that froze the tray and stuck the local dashboard on "Connecting to the local client." after the first sync of a newly-seen game.

### Added

- **macOS client auto-update.** The Mac tray's "Install update" now downloads the checksummed binary, updates `GSBS.app` in place, and relaunches — same as Windows/Linux. When that's not possible (older releases, or a failed update), the tray opens the GitHub releases page instead.

---

## [4.1.1] — 2026-07-04

Critical macOS fix plus tray polish.

### Fixed

- **macOS menu-bar icon now appears.** The 4.1.0 macOS app started the tray only on Windows/Linux, so it launched invisibly with no way to log in. macOS now runs the same menu-bar tray as the other platforms. **macOS users on 4.1.0 should update.**
- A tray data race that could open the wrong game's save history is fixed.

### Changed

- Cleaner tray menu: conflict controls collapse into a single submenu shown only when conflicts exist; pending-uploads/error rows stay hidden until relevant.
- The macOS DMG is now ad-hoc code-signed, replacing the "GSBS is damaged" dead-end with the softer "Open Anyway" flow (first launch still needs a one-time approval; see [Installation](Installation)).

---

## [4.1.0] — 2026-07-04

WebUI re-polish across server and client, an analytics deep-dive, 2FA recovery codes, and macOS DMG packaging.

### Added

- **Analytics deep-dive**: the Insights page gained a 7/30/90-day window, data-synced and play-rhythm charts, per-device attribution, most-active games, version-history depth, and a "Protection at a glance" row. Admin analytics gained a **Fleet Activity** tab (fleet-wide sync/data/active-user/audit charts), adoption panels (client versions, OS split, 2FA/encryption uptake, signups, job reliability), and a 30/90/365-day trends selector.
- **Client "Sync insights" page**: persisted sync-cycle history, success rate, per-game state with "Reveal folder", full conflict details, and the offline upload queue.
- **2FA recovery codes**: 10 one-time codes issued on enable (shown once, hashed at rest), accepted at login, regenerable from Settings.
- **Client 2FA logins**: `gsbs-client login`, the tray, and the setup page now complete the TOTP step (previously impossible).
- Branded error pages, keyboard shortcuts (`g` chords + `?` help), password strength/show-hide, sortable admin tables, a real multi-step setup wizard, and a `script/ui-smoke.sh` release gate.

### Changed

- **Client WebUI rebuilt to server quality**: shared layout, light/dark theme, sync-status hero with re-login button, honest "Sync now" completion feedback, next-sync countdown, persisted log filters, real quick actions — and the loopback UI now ships the same strict Content-Security-Policy as the server.
- **Fixed in the server WebUI**: the admin "Verify now" / "Backup now" / "Send test notification" buttons (broken by a CSRF field mismatch in 4.0.0), unstyled admin buttons/radios, toast positioning, GB-denominated quotas, readable PCGW game detail, consistent empty/loading states, SVG icons everywhere, and a fully self-contained CSP (Google Fonts CDN removed).
- **macOS client now ships as a drag-to-Applications `.dmg`** (unsigned — clear quarantine once with `xattr -cr /Applications/GSBS.app`; see [Installation](Installation)). macOS devices now report `darwin` and "open log/folder" uses the native `open`.

---

## [4.0.0] — 2026-07-04

Major release: security & reliability audit fixes plus new flagship features.

### Added

- **Zero-config first run + web setup wizard**: no required environment variables — the server auto-generates its session secret and opens a browser wizard to create the admin account (first user = admin) and pick options. Settings resolve `env > database > default`. See [Installation](Installation).
- **Game-aware sync**: the client defers a game's pushes and pulls while it is running and flushes immediately on exit (on by default; not available under Flatpak).
- **Built-in server backups + restore**: scheduled tar.zst archives (DB snapshot + keys + saves), local retention, optional S3 upload, `gsbs-server restore` command, docs/RESTORE.md runbook.
- **Notifications**: webhook/Discord/ntfy alerts for conflicts, quota, new devices/logins, backup results, and stale devices — server-wide and per-user.
- **Save export & import**: real save bytes as zip archives (per game or whole account, optional version history), importable into any GSBS server; `gsbs-client export` downloads and decrypts locally.
- **Per-game controls**: version-retention overrides (server) and conflict-policy overrides (client); Devices page shows app versions.
- **Localization framework**: English source-of-truth catalog with per-key fallback and a Settings language picker; drop-in JSON translations (English-only this release).
- Weekly **data-integrity verification** job with admin overview findings + "Verify now" (encrypted saves are skipped by design).
- **History pruning** (audit log 180d, manifest fetches 30d, stats snapshots 730d; `GSBS_*_RETENTION_DAYS`, `0` = forever) and optional age-based save-version pruning (`GSBS_SAVE_VERSION_MAX_AGE_DAYS`, keeps newest 3 per file).
- **Server log rotation** (20 MiB × 3 by default; `GSBS_LOG_MAX_BYTES` / `GSBS_LOG_MAX_BACKUPS`).
- **Disk-full protection**: pushes get HTTP 507 before any bytes land when the volume is nearly full; clients retry from their outbox.

### Security

- **2FA secrets encrypted at rest** (key file `gsbs-keys/totp.key` beside the database — back it up together with the DB; see [Upgrading](Upgrading)).
- **Save encryption upgrades to Argon2id automatically** once every device on an account runs ≥ 4.0.0 (mixed fleets keep the legacy format; `crypto_v2` config pins either way).
- **Password changes log out all other devices and browsers**; enabling 2FA revokes client tokens; TOTP codes are single-use.
- HTTP timeouts + write deadlines (Slowloris protection), server-verified content hashes, capped gzip bodies, optional strict overwrite protection for pre-4.0 clients.
- Supply chain: signed build-provenance attestations + SPDX SBOM on releases, CodeQL, Dependabot, `errcheck`/`gosec` linting, Go 1.26.
- **Storage quotas are now real limits**: enforced atomically inside the write transaction and counting version history. Over-quota users are grandfathered (shrink/replace allowed, growth blocked). Dashboards show the new usage figure with 80%/over warnings. See [Upgrading](Upgrading).
- WebUI two-factor (TOTP) verification and registration are now rate-limited like password login.
- `GSBS_SESSION_SECRET` is now **optional** — auto-generated into `gsbs-keys/` if unset; a *set* value must still be 32+ characters and not a placeholder. See [Upgrading](Upgrading).
- The auto-generated `/metrics` token is no longer logged in cleartext; compose files gained `no-new-privileges` and resource limits.

### Fixed

- **Multi-device correctness:** a new device's first sync can no longer overwrite another machine's save (safety precondition always on); clock differences within 2 minutes surface conflicts instead of silently picking a winner; startup reconciliation refuses to upload blindly when the server can't be reached; slow in-place game writes are re-checked before pushing so torn snapshots never upload.
- A failed database transaction can no longer destroy a save file in filesystem storage mode (content is staged and only promoted after commit).
- Client save writes (pulled saves, outbox, conflict records) are now fsynced before the atomic rename — durable across power loss.
- Locked-file detection on Windows uses the OS error code, so it works on localized (non-English) Windows.
- Pulls for legacy server rows without a content hash now respect the conflict policy instead of overwriting the local file.

### Changed

- The `.deb` no longer depends on `libayatana-appindicator3` (the tray is pure-Go D-Bus). GNOME needs the AppIndicator extension for the tray icon.
- CI: Windows race-detector tests, a Flatpak-parity client build check, pinned release tooling.

## [3.2.3] — 2026-06-26

- Cover art resolves for games whose Steam App ID lives only in the PCGW infobox (e.g. *The Witcher 3*).
- "No art" results are no longer cached forever — the negative cache expires after 7 days and self-heals.

## [3.2.2] — 2026-06-26

- **Game cover art on My Games**: real Steam covers fetched server-side from Steam's CDN, cached on disk (`GSBS_COVER_ROOT`), served locally at `/covers/{game_id}`. Browsers never call Steam directly; games without art keep the generated icon tile.

## [3.2.1] — 2026-06-25

- Dashboard Recent Activity is now tabbed (All / Saves / Devices / Security); Admin overview opens with a branded About card; S3 sync history.

## [3.2.0] — 2026-06-25

- **My Games page** — grid/list browser with per-game health, search, filters, CSV/JSON export, and bulk delete.
- **Game detail page** — metric cards, save-file explorer with inline text preview, insights sidebar.
- **Insights page** — per-day sync-volume chart, top games by storage, device backup-health alerts.
- **Devices page** — live online/offline status, rename, revoke. **Command palette** (`Ctrl`/`⌘`+`K`).
- Schema migration (v21): save versions record the writing device and per-version byte change.

## [3.1.x] — 2026-06-24

- **3.1.7** — Linux/Proton: saves now actually upload (watch paths store the resolved `compatdata` path instead of the raw Windows template).
- **3.1.6** — nested save files upload again; accurate Proton-aware readiness diagnostics.
- **3.1.5** — Steam Windows games appear and resolve on Linux (server fills `steam_app_ids` from the PCGW infobox at serve time).
- **3.1.4** — games that save directly in the home folder sync as specific named files, non-recursively; reconcile honors non-recursive rules.
- **3.1.3** — **critical:** the client refuses to watch the home directory or top-level system roots; per-game "Delete all" added to purge accidental uploads.
- **3.1.2** — Flatpak tray icon renders in the sandbox (pure-Go StatusNotifierItem tray); Flatpak moved to the Freedesktop 24.08 runtime.
- **3.1.1** — Dashboard "Synced Saves" is a collapsible Game → category → files tree with real file names.

## [3.0.x] — 2026-06-18

- **Manifest bundle sync (GitHub mode)** — pre-built PCGW bundles with ETag skip, smart merge, deltas, and admin toggle; fresh installs default to `github`.
- **Encrypted saves now dedup** — change detection keys off the plaintext hash, so unchanged encrypted saves are skipped instead of re-uploaded every cycle.
- **Crash-safe server saves** — disk-backed writes use temp file + fsync + atomic rename.
- **First-push overwrite guard** — fresh clients send `X-GSBS-If-Absent`; the server 409s instead of clobbering another machine's save (for `keep_local`/`keep_server` policies).
- Client manifest v2 pagination fixes (full catalog downloads, truncated-cache recovery).

---

## [2.1.7] — 2026-06-10

### Fixed

- **PCGW sync:** **Parse Missing Only** now skips Phase 1 and processes only catalog IDs not yet stored locally.
- **PCGW sync:** **Retry Failed Pages** now skips Phase 1 as documented.
- **WebUI:** Advanced Maintenance help text for Rebuild Save Locations and Full Reparse.

### Added

- `SkipCatalogPhase` / `MissingOnly` sync options and `RunPCGWSyncMissingLocal` runner.

## [2.1.6] — 2026-06-10

- Fast incremental Phase 1 (catalog probe + tail scan); deferred rev-check; dynamic job ETA in WebUI.

## [2.1.5] — 2026-06-09

- Phase 2 failed-page stub persistence; **Reset Dead Letter** admin action.

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
