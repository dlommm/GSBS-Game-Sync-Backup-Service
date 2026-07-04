# Changelog

All notable changes to GSBS are documented here. Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [4.0.0] - Unreleased

Major release: full-project security & reliability audit fixes plus new flagship features. One startup behavior change (see **Upgrade note** below).

### Added

- **Game-aware sync.** The client now detects running games (a lightweight process scan matched against known install folders, every 15s) and defers that game's pushes *and* pulls until it exits — no more half-written saves uploading mid-session or the server overwriting a save the game has open. On exit, pending saves flush immediately and a pull runs. The tray shows "In game — sync deferred". On by default (`game_aware_sync: false` disables; `game_scan_interval` tunes). Not available under Flatpak (the sandbox hides host processes) — sync simply behaves as before there.
- **Built-in server backups with one-command restore.** A scheduled job (Admin → Settings → Backups, or `GSBS_BACKUP_DIR`) snapshots the database (`VACUUM INTO`), the encryption key files, and all stored saves into a `tar.zst` archive, keeps the newest N locally, and optionally uploads to any S3-compatible bucket (`GSBS_BACKUP_S3_*`, credentials env-only). Restore with `gsbs-server restore <archive>` — full runbook in the new docs/RESTORE.md. "Backup now" button included.
- **Notifications.** Webhook (JSON), Discord, and ntfy alerts — server-wide (admin settings, with per-event toggles and a test button) and per-user (own Settings page): sync conflicts, quota at 80%/exceeded, new device connections, new web logins, backup results, and devices that stop syncing for N days.
- **Save archive export & import.** Download real save files as a zip — per game (with optional version history) from the game page, or the whole account from My Games — each with a manifest that lets any GSBS server re-import it (validated exactly like client pushes: path checks, size caps, quota). New `gsbs-client export` command downloads and locally **decrypts** your saves into the same importable format.
- **Per-game controls.** Per-game version-retention overrides on the server (admin settings) and per-game conflict-policy overrides on the client (`conflict_policy_overrides` in the config).
- The Devices page now shows each device's app version.
- **Weekly data-integrity verification.** A new `integrity_check` job re-hashes every stored save against its recorded checksum and reports problems (hash mismatch, missing file, unreadable file) on the admin overview, with a "Verify now" button. Client-encrypted saves are skipped — the server cannot read them by design. Findings clear automatically once a slot verifies clean again.
- **History pruning.** The audit log, manifest-fetch log, and stats snapshots are now pruned on a daily schedule (defaults: 180 / 30 / 730 days; `GSBS_AUDIT_RETENTION_DAYS`, `GSBS_MANIFEST_FETCH_RETENTION_DAYS`, `GSBS_STATS_RETENTION_DAYS`, `0` = keep forever). Optional age-based save-version pruning via `GSBS_SAVE_VERSION_MAX_AGE_DAYS` (off by default; always keeps the newest 3 versions per file).
- **Server log rotation.** File logging (`GSBS_LOG_FILE`/`GSBS_SERVICE_LOG_PATH`) now rotates by size — 20 MiB × 3 backups by default, tunable via `GSBS_LOG_MAX_BYTES` / `GSBS_LOG_MAX_BACKUPS` (`GSBS_LOG_MAX_BYTES=0` restores unbounded append).
- **Disk-full protection.** Pushes are refused with HTTP 507 *before* any bytes are written when the storage volume is nearly full (free space < 2× payload + 256 MiB). Clients queue the save in their offline outbox and retry automatically.
- Dashboard: the storage gauge now reflects total stored bytes **including version history** (what the quota actually enforces), with a warning banner at 80% and when over quota.

### Security

- **Two-factor secrets are now encrypted at rest.** TOTP seeds are sealed with AES-256-GCM under a key file stored *outside* the database (`gsbs-keys/totp.key`, auto-created beside it; `GSBS_TOTP_KEY_FILE` overrides). Existing secrets are re-encrypted on upgrade. **Back up the `gsbs-keys/` directory together with your database** — a restore without it fails closed (recovery runbook in TROUBLESHOOTING).
- **Save encryption upgrades to Argon2id automatically.** A new `gsbs2:` format replaces PBKDF2 with Argon2id (64 MiB, t=3). Clients report their version to the server, and an account switches to writing the new format only once **every device seen in the last 30 days** can read it — mixed fleets keep working with zero coordination. `crypto_v2: true/false` in the client config forces or pins the format; all clients read both formats forever.
- **Changing your password now logs out everything else.** The API endpoint revokes all other devices' tokens and all browser sessions (it previously revoked nothing); the WebUI now also drops other browser sessions. Enabling 2FA revokes existing client tokens so every device proves the second factor. TOTP codes are single-use — replaying a just-used code within its 30-second window is rejected.
- **HTTP hardening:** the server now sets read/header/idle timeouts and per-request write deadlines (Slowloris/slow-reader protection) with rolling deadlines that keep SSE streams alive; the gzip push path caps the raw compressed body; the server verifies the client-declared content hash on unencrypted pushes (mismatch → 400) instead of trusting it.
- **Optional strict protection for old clients:** a new admin setting (*Settings → Sync Safety*) rejects precondition-less overwrites from pre-4.0 clients when the save was last written by a different device. Off by default.
- **Storage quotas are now real limits.** Enforcement moved inside the database write transaction (concurrent pushes can no longer race past the limit) and usage now counts version history — previously only current saves counted, so real disk use could reach ~8× the configured quota. Users already over the new accounting are grandfathered: their saves keep syncing as long as usage does not grow; only growth is blocked.
- **Supply chain:** releases now ship a signed build-provenance attestation (verify with `gh attestation verify`) and an SPDX SBOM; CodeQL and Dependabot run on the repository; the linter gained `errcheck` and `gosec`; builds moved to Go 1.26.
- **WebUI two-factor (TOTP) verification and registration are now rate-limited** with the same per-IP limiter as password login. Previously an attacker who knew the password could brute-force the 6-digit code without throttling, and open registration could be spammed.
- **`GSBS_SESSION_SECRET` strength is now enforced at startup**: the server refuses to start with a secret shorter than 32 characters or a known placeholder value. For local development only, set `GSBS_INSECURE_DEV_SECRET=1` to bypass (the dev compose file does this automatically).
- The auto-generated `/metrics` bearer token is no longer written to the log in cleartext — only a SHA-256 fingerprint prefix is logged. Set `GSBS_METRICS_TOKEN` explicitly to scrape metrics.
- Version-download filenames are fully sanitized before being placed in the `Content-Disposition` header.
- `docker-compose.yml` now sets `no-new-privileges`, a memory limit, and a PID limit on the server container.

### Fixed

- **A new device's first sync can no longer overwrite another machine's save.** The first-push safety precondition is now always on (previously it was disabled under the default `last_write_wins` policy): if the server already holds a different save for a file this device has never pushed, the push surfaces a conflict in the tray instead of silently clobbering. `last_write_wins` still governs all subsequent pushes and pulls.
- **Clock differences between machines can no longer decide sync winners.** The client estimates the server's clock offset from response headers and treats timestamps within a 2-minute window as simultaneous: under `last_write_wins` that now surfaces a conflict instead of letting whichever machine's clock runs fast silently win; `keep_local`/`keep_server` resolve the window per their policy.
- **Startup reconciliation no longer uploads blindly after a network hiccup.** If the server's save list cannot be fetched, reconcile now skips entirely (previously it pushed *every* local file unguarded, which could overwrite newer server saves) — the watcher keeps syncing real changes safely and reconcile retries on the next start.
- **Slow in-place save writes are detected before pushing.** The client re-checks the file after reading; if the game is still writing (size/mtime moved) the push is re-queued instead of uploading a torn snapshot, with a cap so hot files still sync.
- **A failed database transaction can no longer destroy a save file** (filesystem storage mode). New content is staged beside the canonical file and only renamed into place after the transaction commits; previously the old save was overwritten first and then deleted on rollback, losing the last good copy. Orphaned staging files are swept at startup.
- **Client save writes are now crash/power-loss durable**: pulled saves, conflict records, the offline outbox, and the push hash cache are fsynced before the atomic rename. Previously a power cut at the wrong moment could leave a truncated or empty file.
- **Locked-file detection on Windows now checks the OS error code** (`ERROR_SHARING_VIOLATION`/`ERROR_LOCK_VIOLATION`) instead of matching English error text, so saves locked by a running game are correctly queued to the outbox on localized Windows installs.
- Pulls for legacy server rows that lack a content hash now respect the configured conflict policy instead of silently overwriting the local file.

### Changed

- **The `.deb` package no longer depends on `libayatana-appindicator3`** — the tray has been pure-Go D-Bus (StatusNotifierItem) since the systray upgrade, so the library was never linked. On GNOME, install the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/) to see the tray icon (this was already required; it is now documented).
- CI: Windows tests now run with the race detector; a `CGO_ENABLED=0` client build check guards the Flatpak (pure-Go tray) configuration on every push; release tooling versions are pinned.
- Docs: Flatpak install and troubleshooting coverage added to README/wiki; wiki changelog now has a freshness gate; SECURITY.md gained a deployment-hardening section.

### Upgrade note

- Deployments using a `GSBS_SESSION_SECRET` shorter than 32 characters will not start after upgrading until the secret is replaced (`openssl rand -base64 32`). Rotating the secret logs out active WebUI sessions; API clients are unaffected.
- **Storage usage will appear higher after upgrading** — quotas now count version history (up to `GSBS_SAVE_VERSION_RETENTION` ≈ 8 copies per file), which better reflects real disk use. Nobody loses data and existing over-quota users keep syncing (shrink/replace allowed, growth blocked); raise quotas or lower version retention if users hit limits unexpectedly.
- History tables are pruned by default from this release (audit 180 days, manifest fetches 30, stats snapshots 730). Set the corresponding `GSBS_*_RETENTION_DAYS` variable to `0` to keep records forever.
- **Back up the new `gsbs-keys/` directory together with your database.** It holds the at-rest encryption key for 2FA secrets; a database restored without it has 2FA fail closed (see the TROUBLESHOOTING runbook to recover).
- **Changing a password now logs out other devices and browsers** on both the API and WebUI — this is the intended behavior; affected clients show their normal re-login prompt.
- Encrypted saves migrate to the stronger Argon2id format automatically once every device on the account runs ≥ 4.0.0 — no action needed. Keep any device you still use updated, or pin `crypto_v2: false` in its config if it must stay on 3.x.

## [3.2.3] - 2026-06-26

### Fixed

- **Cover art now resolves for games whose Steam App ID lives only in the PCGW infobox** (e.g. *The Witcher 3*). The cover proxy read only the Cargo `steam_appids` column, which PCGW frequently leaves empty even for Steam games; it now falls back to the infobox-derived ID the same way the save-path manifest does.
- Cover "no art" results are no longer cached permanently — the negative-cache marker now expires after 7 days and is removed on a successful fetch, so covers self-heal once a manifest sync supplies a missing Steam App ID. (To refresh immediately, use **Settings → Cover Art Cache → Clear cover cache**.)

## [3.2.2] - 2026-06-26

### Added

- **Game cover art on My Games.** Game cards and the game detail header now show real Steam cover art. The server fetches a game's `library_600x900.jpg` from Steam's public CDN on demand (using the Steam App ID already in the manifest), caches it to disk, and serves it locally at `/covers/{game_id}` — so browsers never call Steam directly. Games without Steam art fall back to the existing generated icon tile.
  - Cache directory is `GSBS_COVER_ROOT` (default `/app/data/covers`). Covers are cached effectively indefinitely; an admin **Settings → Cover Art Cache → Clear cover cache** control forces a re-fetch. Missing/absent covers are negatively cached so the server doesn't re-poll Steam on every view.

### Notes

- No new dependencies, no database schema changes, and no external image pipeline — covers are sourced directly from Steam's CDN per game on demand.

## [3.2.1] - 2026-06-25

### Changed

- **Dashboard:** the Recent Activity feed is now a tabbed section (All / Saves / Devices / Security), and the redundant Synced Saves panel at the bottom was removed — synced saves now live on the dedicated My Games page.
- **Admin overview** now opens with a branded About card (logo, version, and quick links).

### Fixed

- **Admin Users:** the row "Actions" dropdown was being painted underneath the next row's sticky cell, making it hard to read. The open row's cell is now raised above its siblings.
- **S3 sync history:** S3 bundle syncs are now recorded in the PCGW Sync History (mode `bundle`) with success/failure status and the per-sync change count, and the "Latest sync run" timestamp reflects them. Previously the Sync History was empty when using the S3 bundle source.
- Steam App IDs extracted from a PCGW infobox are now returned in a stable order (main edition before the "side" alternate edition) instead of relying on map iteration order.

## [3.2.0] - 2026-06-25

### Added

- **Dedicated "My Games" page** (`/dashboard/games`) — a grid/list browser of your synced games with generated cover-tile icons, per-game health status (healthy/stale), search, status filtering, and sorting (recent/name/size/files). Includes CSV/JSON export of save metadata and bulk-delete of multiple games from the list view.
- **Game detail page** (`/dashboard/games/{id}`) — sticky header, metric cards (files, total size, last sync, encryption), a per-category save-file explorer with inline text preview, and an insights sidebar that highlights the largest recorded change and the device that made it.
- **Insights page** (`/dashboard/analytics`) — a per-day sync-volume chart, top games by storage, connected-device list, and backup-health alerts (e.g. "device X hasn't synced in N days").
- **Devices page** (`/dashboard/clients`) — manage connected devices with live online/offline status, rename, and revoke.
- **Command palette** (`Ctrl`/`⌘`+`K`) — global search across pages and games with full keyboard navigation.
- **Admin per-user drill-down** — a read-only view of any user's insights, linked from the Users page.
- **Dashboard refresh** — a storage-usage gauge, quick-access tiles, and an activity feed with status-coloured events.

### Changed

- Refreshed the admin sidebar with icons and collapsible sections (persisted per browser).
- Save version history now records and displays the per-version byte change and the device that wrote each version.

### Backend

- Schema migration (v21): `save_versions` now stores `client_id` and `change_bytes` per version.
- Added per-user sync-volume and largest-change aggregation queries, a per-user device rename, and a `client-activity` SSE event broadcast on push.

## [3.1.7] - 2026-06-25

### Fixed

- **Linux/Proton: saves now actually upload.** Discovered Steam games resolved their Proton `compatdata` save path for the readiness check, but the watch path then stored the raw *Windows* template (e.g. `%LOCALAPPDATA%\\...`) instead of the resolved path. The watcher and reconcile re-resolve that template with a non-Proton resolver — on Linux `%LOCALAPPDATA%` maps to `~/.local/share/...`, which doesn't exist — so the watcher registered `0` paths and nothing synced ("3 active watch paths" but `watcher_paths count=0`). The watch path now stores the resolved compatdata path for Proton candidates, so both the watcher and reconcile use the real save folder.

## [3.1.6] - 2026-06-24

### Fixed

- **Nested save files now upload (reconcile regression from 3.1.4).** Making the initial scan honor non-recursive rules was too broad: plain game-folder rules default to non-recursive, so the scan stopped descending into subfolders and nested saves weren't pushed to the server. The scan now recurses for normal game folders and only restricts to the top level for named-file rules anchored at a broad root (the home-folder safety case).
- **Accurate Linux/Proton readiness.** The tray "Discovered games" status (and `debug-sync`) didn't account for Proton candidates, so Steam Windows games on Linux always showed "no saves for this OS" even when they resolve via the compatdata prefix. `DiagnoseGameSync` now mirrors the real watch logic (Proton-aware) and reports the true reason (ready to sync / save folder not found).

## [3.1.5] - 2026-06-24

### Fixed

- **Linux/Proton: Steam games now show up and resolve.** On Linux, Windows-platform PCGW games (e.g. *Ori and the Will of the Wisps*, *The Witcher 3*) didn't appear in the add-game search and showed "no saves for this OS" in the tray. Root cause: the manifest entries served to the client had empty `steam_app_ids` (PCGW's Cargo field is often empty and bundle-imported catalogs store null), so the client never treated them as Proton candidates. The server now fills `steam_app_ids` from the game's PCGW infobox **at serve time for the v1 manifest too** (v2 already did) — robust against manifest-bundle re-imports — and the client's add-game search now includes Proton-eligible Windows games on Linux (resolving to the `compatdata` save path).

## [3.1.4] - 2026-06-24

### Added

- **Sync games that save directly in the home/user folder.** A top-level root (home, `%USERPROFILE%`, `~/.config`, etc.) may now be watched for **specific named files, non-recursively** — so a game that drops a known save file straight in `$HOME` syncs cleanly, while broad/wildcard/recursive rules on such roots stay blocked. Applies to manifest entries and manual adds (which are watched non-recursively when anchored at a root).

### Fixed

- **Reconcile honors non-recursive rules.** The initial scan previously walked the *entire* directory tree regardless of a rule's recursive flag (only filtering which files to upload). It now skips subdirectories for non-recursive rules, so a named-file-in-home rule no longer traverses all of home.
- The home/root safety guard is confirmed to cover **Windows and macOS** as well as Linux (it's derived from the resolver's `%USERPROFILE%`/`%APPDATA%`/`%LOCALAPPDATA%`/`Documents` etc.), exercised by the cross-platform test on the Windows CI runner.

## [3.1.3] - 2026-06-24

### Fixed

- **Critical — never watch your whole home folder.** A save rule that resolved to the home directory or a top-level root (e.g. a game whose Linux save path resolved to `$HOME` with no game-specific subfolder) made the client recursively watch and upload *everything* under it — dotfiles, shell history, caches, other apps' data. The client now refuses any watch directory that is the home dir or a top-level XDG/system root (`~/.config`, `~/.local/share`, `~/.cache`, `~/.var/app`, `~/Documents`, `%APPDATA%`, etc.). This applies to discovered games, manually-added folders, and any previously-saved watch path. Only game-specific subfolders are watched.

### Added

- **Dashboard: "Delete all" per game.** Removes every synced save (and its versions) for a game in one action — useful for purging files uploaded before the fix above. Per-file Delete is unchanged.

## [3.1.2] - 2026-06-24

### Changed

- **Flatpak tray icon now renders in the sandbox**: switched the system tray from `getlantern/systray` to `fyne.io/systray`, whose Linux backend is a pure-Go StatusNotifierItem implementation that sends the icon as a D-Bus pixmap. The old library wrote the icon to the sandbox's `/tmp` and asked the host panel to load it by that path — invisible outside the sandbox, so the Flatpak tray showed no icon. As a result the client is now pure Go on Linux/Windows (no CGO), and the Flatpak no longer builds the libayatana-appindicator/libdbusmenu stack.
- **Flatpak runtime**: moved from the end-of-life GNOME 47 runtime to the Freedesktop 24.08 runtime (GTK is no longer needed). Clears the software center's "stopped receiving core updates" warning.

### Fixed

- **Flatpak store listing**: app-stream screenshots pointed at old-branding placeholder images; they now reference real client screenshots.

## [3.1.1] - 2026-06-24

### Added

- **Dashboard "Synced Saves" redesign**: the flat one-row-per-file table is now a collapsible **Game → category (Saves/Config) → files** tree. Files show their real name and folder (from the stored relative path) instead of a path hash, with per-group counts, total size, last-synced time, and an encrypted badge. Games with many files collapse to a single row; search matches filenames and expands matches.

### Fixed

- **Steam/Proton save paths on Linux**: Steam App IDs are now read from the PCGW infobox (`steam appid`) when the Cargo `Steam_AppID` field is empty (e.g. *Ori and the Will of the Wisps*). Without the App ID, Linux clients could not translate a Windows save template (e.g. `%LOCALAPPDATA%\…`) into the Proton `steamapps/compatdata/<appid>/pfx/drive_c/…` location, so only the raw Windows path was shown. Applied at manifest serve-time (fixes existing/bundle-imported data with no re-sync) and at projection-time (correct at rest for future syncs).

## [3.0.3] - 2026-06-18

### Fixed

- **Dashboard stats crash**: save summary queries used `LENGTH(content)` for `size_bytes`; filesystem-backed saves (`GSBS_SAVE_ROOT`) store NULL `content` and size in `content_size`, causing `sql: Scan error … converting NULL to int64`. Queries now use `COALESCE(content_size, LENGTH(content), 0)`.
- **Client revoke in WebUI**: explicit revoke (admin, user dashboard, `POST /api/clients/revoke`) only rotated the API token and left the `clients` row, so revoked devices still appeared in the UI. Revoke now deletes the client registration; bulk token rotation on password/2FA change is unchanged.

## [3.0.2] - 2026-06-18

### Fixed

- **Server manifest v2 catalog (critical)**: `GET /api/manifest/v2` listed games from `pcgw_games` (~581 Windows titles on typical installs) instead of the full `game_save_locations` catalog shown in Admin. v2 now pages distinct games from `game_save_locations`, matching v1 completeness for save paths while keeping grouped metadata.
- **Client manifest cache completeness**: on-disk cache tracks a `complete` flag after a full paginated v2 download. Incomplete caches no longer accept **304 Not Modified** (which could freeze a partial catalog forever). Manual refresh forces a full re-download; delta `since=` is only used when the cache is complete.

## [3.0.1] - 2026-06-18

### Fixed

- **Client manifest v2 pagination (critical)**: clients previously downloaded only the first page of `GET /api/manifest/v2` (server default 10,000 games), so titles late in the alphabet — e.g. *The Witcher 3* — were missing from search, discovery, and the on-disk cache. Clients now fetch the catalog in 5,000-game chunks until complete, auto-detect and re-download truncated caches from older clients, and store `games_total` locally. Server v2 responses now include `games_total` for reliable pagination.

## [3.0.0] - 2026-06-18

### Added

- **Manifest bundle sync (GitHub mode)**: Servers can fetch pre-built PCGW manifest bundles from [gsbs-manifest](https://github.com/dlommm/gsbs-manifest) instead of live API sync. ETag 304 skip, smart merge import (`merge_skip_unchanged` / `delta`), delta bundles for seeded servers, admin sync-source toggle, bundle cron, and **Fetch bundle now** on Admin → PCGW. Fresh installs default to `github`; existing installs with PCGW data stay on `api`.
- **Bundle schema v2**: Catalog export, lite profile (omit heavy wikitext), `deleted_game_ids` for deltas, manifest version bump only when rows change.
- **CLI**: `cmd/pcgw-bundle-export` (`--full`, `--delta`, `--since`, `--lite`) for optional CLI export.
- **PCGW sync logging**: Structured `phase1_reason`, rev-check decision/progress, catalog scan progress, and Phase 2 skip/progress events so slow runs show why Phase 1 ran or pages were not updated immediately.
- **Docs**: [docs/MANIFEST_BUNDLE.md](docs/MANIFEST_BUNDLE.md).
- **First-push overwrite guard (M2)**: when a client has no last-pushed hash for a slot (fresh client or cleared cache) it sends `X-GSBS-If-Absent: 1`; the server rejects with 409 if a *different* save already exists, surfacing a conflict instead of silently clobbering another machine's save. Identical content is allowed. Enabled for `keep_local`/`keep_server` policies; `last_write_wins` keeps blind overwrite by design. Backward compatible with old servers and clients.
- **Paginated full-pull fallback**: the full-pull path (used when the summaries-first sync is unavailable) now fetches and applies saves in bounded pages so neither the server nor the client holds an entire library in memory at once.

### Fixed

- **Encrypted saves now dedup (sync correctness)**: change-detection keyed off the encrypted wire bytes, but AES-GCM uses a fresh random salt+nonce per encryption, so identical content hashed differently every cycle. Encrypted users re-uploaded the full save and minted a new server version on every sync. Change-detection (push-skip cache, `X-Content-Hash`, optimistic-concurrency, watcher echo-suppression, reconcile) now keys off the **plaintext** content hash, so unchanged encrypted saves are detected and skipped. Unencrypted behavior is unchanged (wire == plaintext). One-time: existing encrypted saves re-upload once after upgrade, then converge.
- **Crash-safe server saves**: disk-backed canonical writes (`GSBS_SAVE_ROOT`) now use temp-file + `fsync` + atomic rename + parent-dir `fsync` instead of a direct `os.WriteFile`. A crash or power loss can no longer leave a truncated/torn canonical save.

### Changed

- **Cron**: `pcgw_sync_source=github` schedules `pcgw_bundle_fetch`; `api` keeps incremental PCGW sync cron. First start runs bundle fetch in GitHub mode.
- **CI**: `govulncheck` is now blocking (fails on a reachable/"called" vulnerability) and pinned to `v1.1.4` for reproducible runs. Keep the Go toolchain on its latest patch (CI tracks the latest `1.25.x`) so stdlib advisories stay resolved.

## [2.1.7] - 2026-06-10

### Fixed

- **PCGW sync (critical)**: **Parse Missing Only** previously invoked the same incremental sync as the main Sync button (Phase 1 probe + full backlog queue). It now skips Phase 1 (`catalog_scan_mode=skipped`) and processes only catalog IDs not yet in `pcgw_games`.
- **PCGW sync**: **Retry Failed Pages** now skips Phase 1 as documented — Phase 2 only for `failed`/`partial` rows.
- **WebUI**: Advanced Maintenance help text corrected for **Rebuild Save Locations** (manifest version bump only) and **Full Reparse** (full catalog rescan + backlog/changed pages, not every unchanged OK page).

### Added

- **PCGW sync**: `SkipCatalogPhase` and `MissingOnly` options on `PCGWSyncOptions`; `RunPCGWSyncMissingLocal` runner entry point.
- **Tests**: `TestTargetedModes_SkipCatalogPhase` verifies missing-only and retry-failed skip catalog API calls and queue filtering.

## [2.1.6] - 2026-06-10

### Fixed

- **PCGW sync (critical)**: Routine incremental sync no longer runs a full Phase 1 catalog scan (~4 hours for ~40k games). Incremental sync uses a single-call catalog probe and tail scan when new IDs exist; full scan only on first run, incomplete catalog, Refresh New Games, Full Reparse, or periodic interval (`GSBS_PCGW_FULL_CATALOG_DAYS`, default 7).
- **PCGW sync**: `buildChangedQueue` (per-page rev-check) is deferred when the probe finds no new IDs and the rev-check interval has not elapsed (default 7 days), so no-op incremental runs exit in seconds.
- **WebUI**: PCGW job status now shows a dynamic ETA during Phase 1 and Phase 2 via `formatETASec` (e.g. `~4h 12m`), using live runner throughput instead of minute-only estimates hidden when total was unknown.

### Added

- **PCGW sync**: `ProbeCatalogGrowth` and `ScanCatalogTail` in the catalog job; `runCatalogPhase` gates full vs fast Phase 1.
- **PCGW sync**: `catalog_scan_mode` on sync runs (`full`, `fast_probe`, `tail`, `skipped`, `resumed`) surfaced in analytics.
- **Server**: `GSBS_PCGW_FULL_CRON` now schedules periodic full catalog rescans; `GSBS_PCGW_FULL_CATALOG_DAYS` env var documented.
- **Store**: Migration step 18 (`catalog_scan_mode`, `last_rev_check_at`); `GetLastSuccessfulPhase1Stats`, `SetLastRevCheckAt`, `UpdateLastFullSyncAt`.
- **Runner**: `ProgressTotal()` and `ProgressETASec()` getters; EMA throughput resets on phase change.

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
