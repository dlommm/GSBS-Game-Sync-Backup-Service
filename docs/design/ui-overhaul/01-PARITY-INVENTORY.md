# Feature Parity Inventory — GSBS UI/UX Overhaul

**Baseline:** v4.3.0 (`e671fe3`, 2026-07-05). Verified against the live code (route dispatch in
`server/webui/router.go` + `server/webui/handlers_admin_pcgw.go:routeAdminPCGW`, client routes in
`client/setup_server.go`, tray menu in `client/tray_menu.go`) and a green `script/ui-smoke.sh`
sweep (41 checks).

**How to use this file:** every row is a capability that MUST exist in the redesigned UI. A screen
spec (03/04) that doesn't cite inventory rows, or a row with no home in the new design, is a defect
in the overhaul plan. During Stage B (Phase 1 implementation), tick the checkbox when the capability
is verified working on the redesigned surface. `FIX-*` rows are defects found during Phase 0 that
Phase 1 must resolve.

**Status legend:**
- `preserved` — same capability, same location, re-skinned
- `enhanced` — same capability, improved presentation/affordance in the new design (no behavior loss)
- `relocated` — capability moves (old entry point redirects or is also kept); nothing is ever `dropped`

---

## API — frozen compatibility surface (17 endpoints)

These are the client contract (`server/api/handler.go:195-213`, `server/api/openapi.json`). The
redesign may ADD endpoints (Stage D) but these must keep working unchanged.

| # | Endpoint | Methods | Auth | Status |
|---|----------|---------|------|--------|
| API-1 | `/api/health` (`?ready=1` pings DB) | GET | none | preserved |
| API-2 | `/api/register` | POST | none | preserved |
| API-3 | `/api/login` (returns token or `totp_required`+`totp_token`) | POST | none | preserved |
| API-4 | `/api/login/totp` | POST | none | preserved |
| API-5 | `/api/saves` (push incl. `X-Encrypted`/`X-GSBS-If-Hash`/`X-GSBS-If-Absent`; pull; delete) | GET/POST/DELETE | bearer | preserved |
| API-6 | `/api/manifest` | GET | none | preserved |
| API-7 | `/api/manifest/v2` | GET | none | preserved |
| API-8 | `/api/clients` | GET | bearer | preserved |
| API-9 | `/api/clients/revoke` | POST | bearer | preserved |
| API-10 | `/api/saves/versions` | GET | bearer | preserved |
| API-11 | `/api/saves/versions/download` | GET | bearer | preserved |
| API-12 | `/api/saves/versions/restore` | POST | bearer | preserved |
| API-13 | `/api/events` (SSE: manifest-updated, save-updated, audit-updated, client-activity, job-progress, job-finished, server-shutting-down) | GET | bearer | preserved |
| API-14 | `/api/change-password` (revokes other tokens + sessions) | POST | bearer | preserved |
| API-15 | `/api/token/refresh` (rotates token, `expires_in` = 90 days) | POST | bearer | preserved (client gains proactive use in Stage C — see FIX-2) |
| API-16 | `/api/account` (incl. `crypto_v2_ready`) | GET | bearer | preserved |
| API-17 | `/api/openapi.json` | GET | none | preserved (version string fix: FIX-3) |

---

## WU — Web user surface

### Auth & session

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-1 | [ ] Password login (standalone branded page, getting-started help, register link when enabled) | `login.html` | `GET/POST /login`, `/` | enhanced |
| WU-2 | [ ] TOTP second-factor login (6-digit) with collapsible **recovery-code** form | `login_totp.html` | `GET/POST /login/totp` | enhanced |
| WU-3 | [ ] Self-registration gated by `allow_register` (strength meter, confirm-match) | `register.html` | `GET/POST /register` | preserved |
| WU-4 | [ ] Logout (CSRF-checked POST) | topbar | `POST /logout` | preserved |
| WU-5 | [ ] Cookie sessions (7-day), TOTP-step cookie 5 min, enroll cookie 10 min | `session.go` | — | preserved |
| WU-6 | [ ] New-web-login notification event fires on login | `handlers_auth.go:102,196` | — | preserved |

### Dashboard

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-7 | [ ] Stat cards, SSE-refreshed on `save-updated` | `dashboard.html` | `GET /dashboard`, `/dashboard/partial/stats` | enhanced (new layout per mockup) |
| WU-8 | [ ] Storage gauge incl. version history, 80% / over-quota banners | stats partial | same | enhanced |
| WU-9 | [ ] Devices panel with manage link | `partials/dashboard_clients.html` | `/dashboard/partial/clients` | enhanced |
| WU-10 | [ ] Recent Activity feed: tabs All/Saves/Devices/Security, "Load more" offset paging | `partials/dashboard_activity.html` | `/dashboard/partial/activity?offset=N` | enhanced (feeds Live Sync Pulse in Stage C) |
| WU-11 | [ ] Quick-access tiles (Games/Devices/Settings) | `dashboard.html` | — | relocated (sidebar IA absorbs; tiles may remain as shortcuts) |
| WU-12 | [ ] First-login onboarding tour | `app.js` `#gsbs-tour` | — | enhanced |
| WU-13 | [ ] Per-user SSE stream with 30s heartbeat | `handlers_dashboard.go` | `GET /dashboard/events` | preserved |
| WU-14 | [ ] Restore/Delete flash confirmations | `partials/alerts.html` | `?restored=`/`?deleted=` flags | preserved |

### My Games

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-15 | [ ] Games grid + list views with toggle | `dashboard_games.html` | `GET /dashboard/games(?view=list)` | enhanced |
| WU-16 | [ ] Search (debounced), status filter (all/healthy/stale), sort (recent/name/size/files) | toolbar → HTMX | `/dashboard/partial/games` | preserved |
| WU-17 | [ ] Cover art tiles with generated monogram (`gameIconSVG`) fallback on img error | `partials/game_cards.html`, `app.js` | `GET /covers/{id}.jpg` | enhanced (artwork more prominent per mockup) |
| WU-18 | [ ] Save-metadata export CSV + JSON | header actions | `/dashboard/export/saves.csv`, `.json` | preserved |
| WU-19 | [ ] Whole-account real-file archive export (`?versions=all` option) | header actions | `GET /dashboard/export/archive.zip` | preserved |
| WU-20 | [ ] Archive **import** (gsbs-export/1 zip; re-validated like a client push; 512 MiB cap; hidden in read-only mode) | header actions | `POST /dashboard/games/import` | preserved |
| WU-21 | [ ] Bulk select (list view) + sticky bulk bar + bulk delete with confirm | `#bulk-form` | `POST /dashboard/games/bulk-delete` | preserved |
| WU-22 | [ ] Live refresh of the grid on `save-updated` | HTMX sse-swap | — | preserved |

### Game detail & versions

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-23 | [ ] Game detail page: cover, status pill, metric cards incl. **Encryption** label | `game_detail.html` | `GET /dashboard/games/{id}` (prefix-matched, survives `/` in IDs) | enhanced |
| WU-24 | [ ] Per-category save explorer (groupSaves), per-file lock badge for encrypted saves | same | — | enhanced |
| WU-25 | [ ] Inline text preview — **only for non-encrypted saves** (per-save `Encrypted` flag + textual sniff, 16 KB cap; encrypted → "preview unavailable" message) | preview button `{{if not .Encrypted}}` | `GET /dashboard/save/versions/preview` | preserved |
| WU-26 | [ ] Per-game export (.zip / .zip + history) and delete-all-saves (confirm) | header actions | `archive.zip?game_id=`, `POST /dashboard/game/delete` | preserved |
| WU-27 | [ ] Per-save delete | file row | `POST /dashboard/save/delete` | preserved |
| WU-28 | [ ] Insights side panel (largest change + device, etc.) | `game_detail.html` | — | enhanced (right-rail treatment per mockup) |
| WU-29 | [ ] Version history: version/updated/size/**signed byte delta**/**authoring device**; current highlighted | `save_versions.html` | `GET /dashboard/save/versions?game_id=&path_key=` | enhanced (timeline treatment per mockup; play-session markers join in Stage D) |
| WU-30 | [ ] Per-version download | row action | `GET /dashboard/save/versions/download` | preserved |
| WU-31 | [ ] Restore a version (confirmation dialog with review step) | row action | `POST /dashboard/save/versions/restore` | preserved |

### Insights (user analytics)

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-32 | [ ] Window selector 7/30/90 days | `dashboard_analytics.html` | `GET /dashboard/analytics?days=` | preserved |
| WU-33 | [ ] Metric row + per-day sync-volume and data-synced bar charts | `insights_body.html` | — | enhanced |
| WU-34 | [ ] Per-device activity attribution | same | — | preserved |
| WU-35 | [ ] Play-rhythm charts (weekday / hour) | same | — | preserved |
| WU-36 | [ ] Most-active games, top games by storage, version-history depth, save-vs-config split | same | — | preserved (feeds Storage Explorer in Stage C) |
| WU-37 | [ ] "Protection at a glance": backups / 2FA / encryption coverage / devices online | same | — | enhanced (extends into Setup Health checklist, Stage C) |
| WU-38 | [ ] Backup-health per-device staleness alerts (≥5 days) | same | — | preserved |
| WU-39 | [ ] Live refresh on `save-updated` (2s delay, hx-select on `#insights-root`) | same | — | preserved |

### Devices

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-40 | [ ] Device list: online/offline, last-seen, app version | `dashboard_clients.html`, `partials/clients_list.html` | `GET /dashboard/clients`, `/dashboard/partial/clients-list` | enhanced (Device Health board, Stage C) |
| WU-41 | [ ] Rename device (per-device dialog) | same | `POST /dashboard/clients/rename` | preserved |
| WU-42 | [ ] Revoke device token | same | `POST /dashboard/clients/revoke` | preserved |
| WU-43 | [ ] Auto-refresh: on load, `save-updated`, `client-activity`, every 30s | HTMX triggers | — | preserved |

### Settings

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-44 | [ ] Change password (current + strength + confirm-match + visibility toggle) | `settings.html` | `POST /dashboard/settings` | preserved |
| WU-45 | [ ] Active-sessions table, revoke one / revoke all, current-session badge | same | `POST .../sessions/revoke`, `.../revoke-all` | preserved |
| WU-46 | [ ] 2FA enable: QR + manual secret + confirm code | `enable_2fa.html` | `GET .../2fa/enable`, `POST .../2fa/confirm` | preserved |
| WU-47 | [ ] Recovery codes: 10 one-time, shown ONCE (copy-all + download .txt), remaining-count badge, regenerate, disable-2FA (password+code) | `recovery_codes.html`, settings | `POST .../2fa/recovery`, `.../2fa/disable` | preserved |
| WU-48 | [ ] E2E encryption toggle (audited `encryption_setting`) | settings | `POST .../encryption` | enhanced (Encryption Center, Stage D) |
| WU-49 | [ ] Per-user notifications: webhook/Discord/ntfy + send-test | settings | `POST .../notifications` | preserved |
| WU-50 | [ ] Language picker (renders when >1 locale; `users.locale`) | settings | `POST .../language` | preserved |

### Errors & degraded

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| WU-51 | [ ] Branded 404 and 403 pages | `error.html` | any bad route; non-admin on `/admin` | enhanced |
| WU-52 | [ ] Read-only mode: mutating WebUI actions redirect with `?error=read_only`; import hidden | `handlers_saves.go` etc. | — | preserved (every new screen spec must state its read-only state) |

---

## XC — Cross-cutting web toolkit

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| XC-1 | [ ] Command palette Ctrl/⌘K: nav commands + game search + **Recent** (localStorage, 5) + theme action; keyboard nav | `cmdk.js`, `GET /dashboard/partial/search` → `cmdk_results.html` | enhanced |
| XC-2 | [ ] `g`-chord shortcuts (d/g/i/s/v, 1.2s window) + `?` shortcuts overlay | `app.js` NAV_CHORDS | enhanced (extend to new pages) |
| XC-3 | [ ] Toast system (`window.gsbs.toast`, bottom-right) | `toast.js` | preserved |
| XC-4 | [ ] Loading skeletons (shimmer) + consistent empty states | `loading_skeleton.html`, `empty-state.html` | enhanced |
| XC-5 | [ ] Sortable tables (client-side, `data-sortable`) | `app.js` | preserved |
| XC-6 | [ ] Shared table pager: Prev/Next, 10/25/50/100 per page, count badge | `pager.go` `newPager`, `partials/table_pager.html` | preserved |
| XC-7 | [ ] Per-table "Table ⚙" customization: column show/hide, compact, zebra — persisted `gsbs.table.<id>`; survives HTMX swaps | `app.js` initTables | preserved |
| XC-8 | [ ] Filter forms outside the HTMX swap region (search keeps focus); filters survive SSE refresh | pattern (e.g. audit) | preserved |
| XC-9 | [ ] CSV links auto-sync with active filters (`data-filter-csv`) | `admin.js` | preserved |
| XC-10 | [ ] Relative timestamps with exact-on-hover | `render.go` helpers | preserved |
| XC-11 | [ ] SVG icon set (25 icons, 16×16, stroke 1.4) — no emoji | `iconSVG` funcmap | enhanced (extend set for new nav) |
| XC-12 | [ ] Light/dark theme: explicit toggle > OS preference > dark; `theme-boot.js` pre-paint stamp; `data-theme` attr; meta theme-color sync | `theme-boot.js`, `app.js` | preserved (dark remains design driver) |
| XC-13 | [ ] **Strict CSP** on every response: `default/script/style/font-src 'self'; img-src 'self' data:; connect-src 'self'` — zero inline, zero external; enforced by `template_csp_test.go` (both surfaces) + ui-smoke | `main.go:102`, `runtime.go:269` | preserved (non-negotiable) |
| XC-14 | [ ] Template-name locks (`template_names_test.go`: handlerTemplates / nestedTemplateRefs / pageBlockTemplates + data cases) | server + client | preserved (update lists with every new/renamed template) |
| XC-15 | [ ] i18n `t()` func + `pkg/i18n` catalog (en), `<html lang>`, per-user locale | `render.go:255` | preserved |
| XC-16 | [ ] HTMX + SSE partial-refresh architecture (vendored htmx + sse ext; hx-boost rejected) | `layout.html`, `admin_shell.html` | preserved |
| XC-17 | [ ] Password UX kit: visibility toggle, strength meter, confirm-match | `app.js` | preserved |
| XC-18 | [ ] Confirm guards on destructive forms (`data-confirm`) + `<dialog>`-based modals + action menus | `app.js` | preserved |
| XC-19 | [ ] Shared design-system pipeline: `script/build-webui.sh` compiles `input.css` (Tailwind 3) → `app.css`, syncs app.css/theme-boot.js/fonts/logo to `client/webui/static/` | build script | preserved (foundation of Stage B) |
| XC-20 | [ ] Vendored DM Sans + JetBrains Mono (woff2, self-hosted) | `static/fonts` | preserved |
| XC-21 | [ ] `ui-smoke.sh` route sweep with CSP assertions (41 checks) | `script/ui-smoke.sh` | enhanced (extend to new routes; Stage B gate) |

---

## AD — Web admin surface

### Overview

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| AD-1 | [ ] Branded About card (logo, version) | `admin_overview.html` | `GET /admin` | preserved |
| AD-2 | [ ] 6 stat cards incl. live SSE-connection count | same | — | enhanced |
| AD-3 | [ ] First-run PCGW source prompt (S3 bundle vs live API) | same | `POST /admin/pcgw/source` | preserved |
| AD-4 | [ ] Getting-started checklist | same | — | enhanced (absorbed into Setup Health, Stage C) |
| AD-5 | [ ] Server-config grid (env/effective values) | same | — | preserved |
| AD-6 | [ ] Data Integrity panel: weekly re-hash job, findings table (hash-mismatch/missing/unreadable), **"Verify now"** | same | `POST /admin/integrity/run` | enhanced (Restore Confidence, Stage C) |
| AD-7 | [ ] Jobs panel, SSE-refreshed on `job-progress`/`job-finished`; paging preserved across refresh (`hx-include` state) | `partials/admin_jobs.html` | `/admin/partial/jobs?context=` | preserved |
| AD-8 | [ ] Stats-over-time trend charts | same | — | preserved |

### Users

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| AD-9 | [ ] Users table: status, quota bar, clients, saves, storage | `admin_users.html` | `GET /admin/users` | enhanced |
| AD-10 | [ ] Per-user action menu: view insights / enable / disable / delete / set quota (GB dialog + presets, GB↔bytes in `admin.js`) | same | `POST /admin/user/{create,disable,enable,delete,quota}` | preserved |
| AD-11 | [ ] Create-user dialog (8–72 char rule, strength, confirm) | same | `POST /admin/user/create` | preserved |
| AD-12 | [ ] All-clients table (sortable) + revoke any client | same | `POST /admin/revoke` | preserved |
| AD-13 | [ ] Per-user read-only drill-down reusing `insights_body.html` (LinkGames off) | `admin_user_detail.html` | `GET /admin/users/view?id=` | preserved |

### Manifest / Activity / Logs

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| AD-14 | [ ] Manifest browser: search, paging, per-page select; refresh on `manifest-updated` | `admin_manifest.html` | `/admin/partial/manifest` | preserved |
| AD-15 | [ ] Manifest CSV export + **Push to Clients** broadcast | same | `GET /admin/manifest.csv`, `POST /admin/push-manifest` | preserved |
| AD-16 | [ ] Activity page: jobs (all types, job+status filters), manifest fetches, stats snapshots — all paged w/ shared pager + Table ⚙ | `admin_activity.html` | `/admin/partial/{jobs,fetches,snapshots}` | preserved |
| AD-17 | [ ] Audit log: action dropdown filter + debounced text search + paging + **CSV export honoring filters** | same | `/admin/partial/audit`, `/admin/audit/export.csv` | enhanced (feeds Notification Inbox, Stage D) |
| AD-18 | [ ] Server logs: component/level/text/limit filters, routine-HTTP toggle, auto-refresh interval, newer/older offset paging, CSV | `admin_logs.html`, `admin.js` | `/admin/partial/logs`, `/admin/logs/export.csv` | preserved |

### Settings

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| AD-19 | [ ] Sync source (S3 bundle / live API radios) with env-override notices | `admin_settings.html` | `POST /admin/settings/save` | preserved |
| AD-20 | [ ] Bundle schedule (cron, URL, incremental-fallback) + API schedule (cron + presets + next-run) + first-start auto-run | same | same | preserved |
| AD-21 | [ ] Backups: enable, cron, keep-N, include-covers, last-run display | same | same | enhanced (Restore Confidence, Stage C) |
| AD-22 | [ ] Notifications: webhook/Discord/ntfy, per-event checkboxes (conflict, quota, device_registered, login, backup, stale_device), stale-days (default 14) | same | same | preserved |
| AD-23 | [ ] Retention overrides JSON (per-game 1–50 versions) | same | same | enhanced (Storage Explorer UI, Stage C) |
| AD-24 | [ ] Sync Safety: `legacy_push_protection` strict mode for pre-4.0 clients | same | same | preserved |
| AD-25 | [ ] PCGW filters (title/path excludes JSON) | same | same | preserved |
| AD-26 | [ ] Section anchor nav + dirty-tracking **sticky save bar** | same + `admin.js` | — | preserved |
| AD-27 | [ ] Test-notification button (admin + own sinks) | same | `POST /admin/notify/test` | preserved |
| AD-28 | [ ] "Backup now" trigger | same | `POST /admin/backup/run` | preserved |
| AD-29 | [ ] Cover cache panel: count, dir, clear (removes .jpg + .miss) | same | `POST /admin/covers/clear` | preserved |

### Analytics & PCGW

| # | Capability | Where today | Endpoints/routes | Status |
|---|-----------|-------------|------------------|--------|
| AD-30 | [ ] Analytics tabs (full-page links, deliberately not HTMX): Overview / Fleet Activity / PCGW Catalog / Sync History; `?window=30/90/365` | `admin_analytics.html` | `GET /admin/analytics?tab=` | preserved |
| AD-31 | [ ] Overview tab: platform totals, save coverage, catalog health, top users/games, trends (downsampled), client-version adoption, security adoption, job reliability | same | — | preserved |
| AD-32 | [ ] Fleet tab: all-user sync volume/bytes, active-users/day, manifest downloads, admin/security actions | same | — | preserved |
| AD-33 | [ ] PCGW tab: latest run, parse breakdown, catalog browser, parse failures | same | `/admin/partial/analytics-pcgw` | preserved |
| AD-34 | [ ] PCGW mission control: 6 stat cards, live SSE job status (phase, pages, ETA, pages/sec, cap), filter toolbar, games table | `admin_pcgw.html` | `GET /admin/pcgw`, `/admin/partial/pcgw`, `/admin/partial/pcgw-job-status` | enhanced (guided redesign in Stage D — all capabilities preserved) |
| AD-35 | [ ] PCGW job actions: bundle fetch, full sync, catalog-only, retry-failed (dead-letter), missing-local, refresh-new, **Auto Catch-Up**, rebuild manifest, reset dead-letter, wipe, purge wikitext, cancel job | POST routes under `/admin/pcgw/` + `/admin/run-job`, `/admin/jobs/pcgw/cancel` | preserved |
| AD-36 | [ ] PCGW import/export (per-game JSON, gzip bundle) | `/admin/pcgw/export/{id}.json`, `.../manifest.json.gz`, `POST /admin/pcgw/import` | preserved |
| AD-37 | [ ] PCGW page detail: parsed save locations as tables, raw wikitext sections, full record, per-page refresh | `admin_pcgw_detail.html` | `GET /admin/pcgw/{id}`, `POST /admin/pcgw/{id}/refresh` | preserved |
| AD-38 | [ ] Admin sidebar: collapsible Manage/Insights groups, persisted | `admin_shell.html`, `admin.js` | — | enhanced (aligned with new user sidebar) |

---

## SW — Setup wizard

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| SW-1 | [ ] 5 steps: Account → Access → Storage (GB) → Extras (backups + webhook) → Review; client-side stepping + validation | `setup.html`, `app.js`, `handlers_setup.go` | enhanced |
| SW-2 | [ ] First user becomes admin + auto-login | `handlers_setup.go:122-167` | preserved |
| SW-3 | [ ] Deactivates once any user exists (`/setup` 404s); race-safe (only the request finding zero users wins) | same | preserved |
| SW-4 | [ ] 60-minute claim window after boot; "Locked" message afterwards until restart | `setupClaimWindow` | preserved |
| SW-5 | [ ] Wizard settings persist: allow_register, quota, backups, webhook (env > DB > default precedence) | same | preserved |

---

## TR — Client tray (Windows / Linux / macOS)

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| TR-1 | [ ] Status header: Ready / Syncing… / Paused / Offline / Error / Setup required / "In game: sync deferred" / "Last sync: ago (ok\|failed)" | `formatStatusHeader`, `tray_menu.go:415` | preserved |
| TR-2 | [ ] Progress line while syncing (phase + current/total) | `tray_menu.go:135` | preserved |
| TR-3 | [ ] Status icons: idle/syncing(green)/recovering(yellow)/paused(grey)/error(red)/setup(amber); ICO on Windows, PNG elsewhere | `tray_icons.go` | enhanced (recolor to new accent if design changes) |
| TR-4 | [ ] Sync now | `triggerSyncNow` | preserved |
| TR-5 | [ ] Pause / Resume (persisted; resume triggers sync) | `tray_menu.go:575` | preserved |
| TR-6 | [ ] Synced-games submenu (12 slots): state glyph ✓/⚠/⏳/✗/↻/↑ + title + ago; click → server version-history page; footer "View all in dashboard →" | `formatGameRow`, `openSaveVersions` | preserved |
| TR-7 | [ ] Discovered-games submenu (8 checkbox slots): ○/⊘/✓/⚠ + launcher/reason; toggle to enable sync; "Add a game manually…" → client `/games`; "Rescan installed games" | `formatDiscoveredRow` | preserved |
| TR-8 | [ ] Open dashboard ↗ (server) | `openDashboard` | preserved |
| TR-9 | [ ] "⚠ Resolve N conflicts" submenu — hidden when zero: keep ALL local (push) / use ALL server (pull) / review each in browser → client `/insights` | `tray_menu.go:171`, `tray_extras.go` | preserved (Stage D adds web path; tray path stays) |
| TR-10 | [ ] Pending-uploads row (hidden until >0) | `tray_menu.go:176` | preserved |
| TR-11 | [ ] Last-error row (truncated 60 chars, hidden unless Error) | `tray_menu.go:179` | preserved |
| TR-12 | [ ] Account & Setup submenu: server label, interval label, Login… (browser everywhere; native Walk dialog + console login on Windows), Detect launcher paths, Refresh manifest | `tray_menu.go:184` | preserved |
| TR-13 | [ ] Advanced submenu: edit config, view log, open data folder, local status/settings/insights/about pages, **Copy diagnostics** (zip), **Run at startup** checkbox | `tray_menu.go:205` | preserved |
| TR-14 | [ ] Updater in Advanced: version line, "Check for updates…", "Install update <tag>…" / "Get update from GitHub…"; Flatpak shows "Updates managed by your software center"; background check 30s + 24h | `update_tray.go` | preserved |
| TR-15 | [ ] Tooltip: status + re-login/watcher/manifest-age/retry/games/pending annotations | `formatTrayTooltip` | preserved |
| TR-16 | [ ] OS notifications (beeep): sync complete/failed, push uploaded (30s debounce), conflict, discovery summary, N new games, setup required, config warnings, auth error, push error, quota error, already-running | `tray_notify.go` | preserved |
| TR-17 | [ ] Quit | — | preserved |
| TR-18 | [ ] Per-OS conventions: Windows title "GSBS" + metered awareness; Linux StatusNotifierItem (pure Go, pixmap icon); macOS icon-only (`SetTitle("")`) menu bar | `tray_windows/linux/darwin.go` | preserved |
| TR-19 | [ ] Single instance (named mutex / flock) with "already running" notice | `single_instance*.go` | preserved |
| TR-20 | [ ] Autostart per OS: HKCU Run / XDG autostart (Flatpak variant) / LaunchAgent — all `--minimized` | `autostart_*.go` | preserved |

---

## CW — Client local WebUI (127.0.0.1:41234–41239)

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| CW-1 | [ ] Loopback-only server, ports 41234–41239, strict CSP + security headers, locked page-name test | `setup_server.go`, `template_names_test.go` | preserved |
| CW-2 | [ ] Setup/login page (standalone): server/username/password/client-name/TOTP(optional); 3-step explainer; post-login discovery poll panel | `setup.html`, `POST /login` | enhanced |
| CW-3 | [ ] `/status` JSON poll (5s → 30s backoff): login/auth state, scan, games, sync ok/err, watcher health, pending, conflicts, watched paths, **next_sync_eta_sec**, games_running, updater state | `handleSetupStatus` | preserved |
| CW-4 | [ ] Dashboard: status hero (All synced / Attention / In game — deferred / Checking), Sync now, server-dashboard link, **Log in again** on auth failure; cards: connection, last sync, next sync ETA, watcher, games; update card; pending/conflict cards → insights | `dashboard.html` | enhanced |
| CW-5 | [ ] Add a game: catalog search (manifest, cap 40, unsafe-path warnings) + manual form (game_id, title, directory, include patterns) | `games.html`, `/games/search`, `/games/add` | enhanced |
| CW-6 | [ ] Quick actions: sync now, check updates, add game, open log, server dashboard, insights | `quick_actions.html` | preserved |
| CW-7 | [ ] Settings: sync interval, conflict policy (global), sync content (both/saves/config), max bandwidth, back-up-on-pull, gzip, skip-on-metered (Win); dirty-hint; note that per-game overrides live in config.json | `settings.html`, `/settings/save` | enhanced (per-game override UI arrives Stage D) |
| CW-8 | [ ] Insights: cycles/success%/saves-7d cards, 7-day bar chart, **pending uploads table** (retry ages), **conflicts table** (policy applied), per-game state with **Reveal folder** | `insights.html`, `/open-folder` | enhanced |
| CW-9 | [ ] Logs: component/level/search/limit filters (persisted), auto-refresh interval, refresh now | `logs.html`, `/partial/logs` | preserved (CSV: see FIX-1) |
| CW-10 | [ ] Help (status reference + troubleshooting) and About (version/platform/build/commit/server + links) | `help.html`, `about.html` | preserved |
| CW-11 | [ ] Open log in OS editor (`open_log` page with copy-path) | `/open-log` | preserved |
| CW-12 | [ ] Check-update trigger from WebUI (`/api/check-update`); apply stays tray-side | `handleCheckUpdate` | preserved (see FIX-6) |
| CW-13 | [ ] Topbar nav (Status/Games/Insights/Actions/Settings/Logs/Help/About) + theme toggle; shared design system | `partials/topbar.html` | enhanced (aligned with new shell) |

---

## CB — Client background behaviors (UX-visible)

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| CB-1 | [ ] Game-aware sync: 15s process scan; pulls/pushes deferred in-game; flush + sync on exit; **unavailable under Flatpak** (nil watcher — sandbox hides processes); `game_aware_sync=false` opt-out | `client/gamewatch/`, `gamewatch_wire.go` | preserved (UI must keep explaining the Flatpak limitation) |
| CB-2 | [ ] OS-aware path translation: Windows ↔ Linux ↔ **Proton compatdata** (`.../compatdata/<appID>/pfx/drive_c/...`) ↔ macOS roots | `pkg/paths/` | preserved |
| CB-3 | [ ] Watch-path safety guard: refuses home/XDG/`~/Library/*`/Windows roots etc. with actionable message ("pick the game's specific save folder, or list exact file names"); drops previously-saved unsafe paths; Flatpak permission notice ("Grant access with Flatseal") | `pkg/paths/safety.go`, `addgame.go:171` | preserved (Add Game UX must surface refusals clearly) |
| CB-4 | [ ] Offline outbox: `outbox/*.json`, per-(game,path) dedup, backoff, drain on start/2min/shutdown, 7-day max age, 401 pauses all retries until re-login | `client/sync/outbox.go` | preserved |
| CB-5 | [ ] 507 disk-full = retryable → queued; 413 quota/too-large = non-retryable → quota error notification | `pkg/retry`, `OnQuotaError` | preserved |
| CB-6 | [ ] Conflict records (`conflicts.json`): game, file, hashes, mtimes, **policy applied**; resolution keep-local (push) / use-server (pull + forced keep_server) | `client/sync/conflict.go` | preserved (Stage D mirrors server-side) |
| CB-7 | [ ] Conflict policies: `last_write_wins` (2-min skew window surfaces conflict instead of guessing) / `keep_local` / `keep_server`; per-game `conflict_policy_overrides` | `decide.go`, `config.go` | preserved (override UI in Stage D) |
| CB-8 | [ ] Sync-cycle history persisted (500 cap) → insights | `client/sync_history.go` | preserved |
| CB-9 | [ ] SSE listener (manifest-updated / save-updated) + periodic pull (default 6h) + discovery rescan tickers | `run_sync.go` | preserved |
| CB-10 | [ ] Auto-discovery scanners: **Steam, Epic, GOG, Ubisoft, EA, Heroic, Lutris, Bottles, Prism** (Xbox = launcher-path override only, NOT a scanner); PCGW resolution of unmatched Steam titles; discovery cache | `pkg/discovery/discovery.go:37-63` | preserved |
| CB-11 | [ ] Metered-connection skip (Windows) | `metered_windows.go` | preserved |
| CB-12 | [ ] Self-update: manifest + SHA256 verified; per-OS apply (bat swap / rename dance / **in-bundle swap + ad-hoc re-sign + rollback**); GitHub fallback | `update*.go` | preserved |
| CB-13 | [ ] Backup-on-pull (`.gsbs.bak`), gzip transport, bandwidth cap | config | preserved |

---

## CL — Client CLI

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| CL-1 | [ ] `gsbs-client export [--game ID] [--out DIR]` — decrypts E2E locally, writes `gsbs-export/1` zip | `export.go` | preserved |
| CL-2 | [ ] `gsbs-client login` (console, TOTP prompt) | `login.go:170` | preserved |
| CL-3 | [ ] `gsbs-client list [--dry-run-pull]` | `list.go` | preserved |
| CL-4 | [ ] `gsbs-client debug-sync <game_id> [--dry-run]` | `debug_sync.go` | preserved |
| CL-5 | [ ] `--console`, `--minimized`, `--version`, `login-dialog`, `--apply-update` | `main.go` | preserved |

---

## OPS — Server ops & degraded states (UI-relevant)

| # | Capability | Where today | Status |
|---|-----------|-------------|--------|
| OPS-1 | [ ] `GSBS_READ_ONLY`: API 503 on push/delete; WebUI `?error=read_only` | `runtime.go:162` | preserved |
| OPS-2 | [ ] Disk-full 507 preflight (256 MiB floor + 2× content) | `handler.go:936` | preserved |
| OPS-3 | [ ] Quotas: bytes internally, GB in UI; in-transaction enforcement counting versions; **grandfathering** (growth blocked, shrink allowed); 80% + exceeded notifications (deduped) | `handler.go:966-1107` | preserved |
| OPS-4 | [ ] Weekly integrity check (skips encrypted; findings persisted) | `server/job/integrity.go` | preserved |
| OPS-5 | [ ] Scheduled backups: VACUUM INTO → tar.zst (+ optional S3, env-only creds), keep-N; `gsbs-server restore` (refuses clobber without `--force`) | `server/job/backup.go`, `server/restore.go` | preserved |
| OPS-6 | [ ] History pruning: audit 180d / fetches 30d / snapshots 730d / optional save-version age (keep newest 3); per-game retention overrides | `sqlite.go:1734` | preserved |
| OPS-7 | [ ] `/api/health` (status/version/db; `?ready=1`) + Prometheus `/metrics` (bearer token, auto-generated w/ fingerprint log) | `handler.go:266`, `metrics/collector.go` | preserved |
| OPS-8 | [ ] Crypto fleet negotiation: `crypto_v2_ready` = all clients seen ≤30d are ≥v4; v2 = `gsbs2:` Argon2id; mixed fleets stay legacy automatically; `crypto_v2` client pin | `store.CryptoV2Ready`, `pkg/crypto` | preserved (surfaced in UI for the first time in Stage D Encryption Center) |
| OPS-9 | [ ] Cover proxy: numeric-ID guard, Steam CDN + PCGW-allowlisted `cover_url` fallback (https, image/*, 8 MiB cap), disk cache + atomic writes + 7-day negative `.miss` markers, 7-day browser cache | `covers.go` | enhanced (Stage C/D may add SteamGridDB/upload sources) |
| OPS-10 | [ ] SSE hub: per-user subscription, heartbeat 30s, rolling 90s deadline, deterministic cap eviction | `server/sse/hub.go` | preserved |

---

## FIX — Defects found in Phase 0 (Phase 1 must resolve)

| # | Defect | Where | Resolution |
|---|--------|-------|-----------|
| FIX-1 | [ ] Client logs "Export CSV" is a dead link — `/logs/export.csv` never registered | `client/webui/templates/logs.html:18` vs `setup_server.go` mux | Implement the route (mirror server logs CSV) or remove the button; decide in 04-SCREENS-CLIENT |
| FIX-2 | [ ] Client never calls `/api/token/refresh`; token expiry = silent 401 → manual re-login | `client/login.go`, `run_sync.go:169` | Stage C (Device Health): proactive refresh + expiry countdown UX |
| FIX-3 | [ ] `openapi.json` `info.version` stale at "4.0.0" | `server/api/openapi.json:5` | Bump with Stage B release; consider deriving from build version |
| FIX-4 | [ ] Per-game `conflict_policy_overrides` have no UI (config.json only) | `client/config.go:99` | Stage D (Conflict Center + client settings) |
| FIX-5 | [ ] `/dashboard/partial/saves` route + partial are vestigial (not referenced by any page) | `router.go`, `partials/dashboard_saves.html` | Remove route+template+test entries in Stage B, or re-wire deliberately |
| FIX-6 | [ ] Update *apply* has no WebUI affordance (tray-only); dashboard says "install from tray" | `client/webui/dashboard.html` | Acceptable for Phase 1 (documented); revisit in Stage C quick actions |

---

## Row counts

| Group | Rows |
|-------|------|
| API | 17 |
| WU | 52 |
| XC | 21 |
| AD | 38 |
| SW | 5 |
| TR | 20 |
| CW | 13 |
| CB | 13 |
| CL | 5 |
| OPS | 10 |
| FIX | 6 |
| **Total** | **200** |

Every screen spec in `03-SCREENS-WEB.md` / `04-SCREENS-CLIENT.md` ends with a "Parity rows covered"
list; the cross-check that all 200 rows are cited is part of Stage A verification.
