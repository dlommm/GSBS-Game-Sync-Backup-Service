# Web Screen Specifications — GSBS UI/UX Overhaul

Every screen uses the same template. "Parity rows" reference `01-PARITY-INVENTORY.md`. Layout
language locked against the mockup image 2026-07-06 (marked "per mockup"). Common behaviors are
specified once in §0 and inherited — a screen spec only states deviations.

## 0. Inherited behavior (applies to every screen unless overridden)

- **Chrome (per mockup):** dark charcoal app shell floating on the bright mint→teal page gradient
  (rounded `--radius-lg` corners + `--shadow-shell`); left sidebar: logo top, icon+label nav rows
  as rounded pills, active item = filled teal pill; topbar: rounded pill search field (opens the
  command palette) left-of-center, then bell slot + theme toggle + avatar-with-chevron account
  menu at the right edge. Content column sits on `--bg`; cards step to `--surface`. Admin pages
  use the same shell with the admin nav group.
- **States:**
  - *Loading* — `loading_skeleton` blocks matching final layout (no spinners on full pages).
  - *Server unreachable* — browser-level; SSE reconnects automatically (htmx sse ext); a thin
    "reconnecting…" strip appears if the SSE stream drops >10s (new, small `app.js` addition).
  - *Read-only mode* — global amber banner "Server is read-only" (new); mutating controls disabled
    with tooltip; POST attempts keep today's `?error=read_only` redirect as backstop (WU-52).
  - *E2E-encrypted* — default: metadata renders normally, content-derived features (preview) show
    the lock treatment. Screens state specifics.
- **Keyboard:** palette Ctrl/⌘K (XC-1); chords `g d/g/i/s/v` (XC-2); `?` overlay lists all; every
  interactive element focus-visible with `--accent-line` ring; dialogs trap focus, Esc closes.
- **Responsive:** desktop = sidebar + content (+ right rail where specified); tablet = sidebar
  collapses to icons; phone = bottom-sheet/mobile drawer nav, right rail stacks below content,
  tables become card lists or horizontal-scroll within their container.
- **Acceptance (global):** route 200s in `ui-smoke.sh` with strict CSP; no template-test
  regressions; WCAG 2.1 AA contrast in both themes; all states above reachable and rendered.

---

## U1. Login

- **Purpose:** authenticate into the WebUI.
- **User & entry:** any user; `/`, `/login`; redirects from unauthenticated session routes.
- **Data:** `POST /login` (session cookie); `allow_register` flag controls register link.
- **Layout:** standalone centered auth card on the ambient gradient (no shell); logo, username,
  password (visibility toggle), submit; getting-started help collapse; register link when enabled.
- **States:** *loading* button-spinner; *error* inline (invalid credentials, rate-limit); *TOTP
  required* → redirect U2; *read-only* login still works (read-only affects mutations only);
  *encrypted* n/a.
- **Interactions:** Enter submits; autofocus username.
- **Responsive:** card max-width 380px, full-bleed on phone.
- **Acceptance:** wrong password shows inline error without losing username; register link absent
  when registration disabled; login fires `login` notification event (WU-6).
- **Parity rows:** WU-1, WU-4, WU-5, WU-6.

## U2. TOTP & recovery-code login

- **Purpose:** second factor.
- **User & entry:** 2FA users after U1; `/login/totp` (5-min step cookie).
- **Data:** `POST /login/totp`.
- **Layout:** auth card; 6-digit input (numeric, autocomplete=one-time-code); collapsible "Use a
  recovery code instead" swap-in field.
- **States:** *error* wrong/reused code (TOTP replay cache), invalid/consumed recovery code;
  *expired step cookie* → back to U1 with notice.
- **Interactions:** auto-submit on 6th digit; Esc collapses recovery form.
- **Acceptance:** recovery code consumes exactly one slot and decrements the count badge (WU-47);
  low-count (≤2) fires the notification event.
- **Parity rows:** WU-2, WU-47 (login half).

## U3. Registration

- **Purpose:** self-service account creation when enabled.
- **User & entry:** `/register` link from U1.
- **Data:** `POST /register`; password rule 8–72.
- **Layout:** auth card; username, password + strength meter + confirm-match.
- **States:** *disabled* → 404-style branded notice; *error* username taken (409 semantics).
- **Acceptance:** created user lands on dashboard with tour (WU-12).
- **Parity rows:** WU-3, XC-17.

## U4. Setup wizard

- **Purpose:** zero-env first-run: create admin, core settings.
- **User & entry:** first visitor while user table empty; `/setup`; all routes redirect here.
- **Data:** `POST /setup`; persists allow_register, quota (GB), backups toggle, webhook.
- **Layout:** standalone 5-step wizard (Account → Access → Storage → Extras → Review), step rail
  showing done/current/todo, client-side stepping + validation, review step lists all choices.
- **States:** *locked* — >60 min after boot: branded "Locked — restart the server to run setup";
  *raced* — another request created the first user: friendly redirect to login; *validation*
  per-step inline.
- **Interactions:** Enter advances; back preserved per step.
- **Responsive:** single column on phone, step rail becomes dots.
- **Acceptance:** completing wizard auto-logs-in as admin (SW-2); `/setup` 404s afterwards (SW-3);
  completes without reading docs (§9 criterion); settings resolve env > DB > default (SW-5).
- **Parity rows:** SW-1..SW-5.

## U5. Dashboard (home)

- **Purpose:** at-a-glance account state: sync health, devices, storage, recent activity.
- **User & entry:** default post-login route; sidebar "Dashboard"; chord `g d`.
- **Data:** `/dashboard` + partials `stats`/`clients`/`activity?offset=`; SSE `save-updated`,
  `client-activity`; storage from stats partial.
- **Layout (per mockup):** three-zone rhythm — content column: (1) status hero row: storage gauge
  (WU-8) + protection strip; (2) **recent games grid** (3-wide on desktop): game cards with the
  confirmed anatomy — artwork thumb top-left, title + subtitle, "Last synced <time>", status row
  (glyph + label, amber when attention needed), action row with `Restore` (ghost) and a filled
  teal `View Versions` pill on the active/hovered card; selected card = `--surface-active` tint +
  green check badge top-right; (3) Recent Activity feed (tabs All/Saves/Devices/Security, Load
  more). Right rail (per mockup): **Version History** contextual panel for the selected game —
  identity row (thumb + name + health check), vertical timeline (nodes color-coded: success
  check / accent / muted; left label = size or signed delta, right label = time), filled primary
  `View Versions` action at the bottom; a small teal collapse toggle at the rail's top edge
  collapses it. When no game is selected the rail shows the Devices mini-panel (WU-9); Stage C
  adds the Live Sync Pulse slot. The rail is a slot pattern — later stages fill it without
  relayout.
- **States:** *empty* (new account) — hero empty-state with the three-step "connect a client"
  path + copyable client instructions; *loading* skeleton per zone; *over-quota / 80%* banners on
  the gauge (WU-8); *read-only* banner; *encrypted* unchanged (all metadata).
- **Interactions:** activity tabs client-side (WU-10); game card click → U7; Load more appends.
- **Responsive:** rail stacks under content on ≤1024px; cards become full-width rows on phone.
- **Acceptance:** SSE push updates stats + activity within 2s without motion (02 §1.5); tabs
  survive Load more; tour fires once for new users (WU-12); flash confirms render (WU-14).
- **Parity rows:** WU-7..WU-14, XC-4, XC-16; slots for C-1 (pulse), C-5 (restore confidence).

## U6. My Games

- **Purpose:** browse/manage every synced game.
- **User & entry:** sidebar "My Games"; chord `g g`; palette.
- **Data:** `/dashboard/games` (+`?view=list`), partial `games`, covers `/covers/{id}.jpg`;
  exports `saves.csv/.json`, `archive.zip`; `POST games/import`, `games/bulk-delete`.
- **Layout (per mockup):** toolbar (search, status filter, sort, view toggle) above a cover-led card
  grid (poster ratio, monogram fallback); list view = dense rows with checkboxes + sticky bulk
  bar. Header actions: Export CSV/JSON, Export saves (.zip), Import archive….
- **States:** *empty* — "no games yet" + link to client add-game flow; *no results* for filters;
  *import success/failure* notice with imported count; *read-only* — import + bulk delete hidden
  (WU-52); *encrypted* — cards identical (covers/metadata unaffected).
- **Interactions:** debounced search keeps focus (XC-8); grid refresh on `save-updated`;
  select-all in list view; confirm on bulk delete.
- **Responsive:** grid 5→3→2 columns; list view becomes cards on phone (bulk bar bottom-fixed).
- **Acceptance:** all four export/import paths work (WU-18..20); bulk delete only in list view
  (deliberate, WU-21); cover 404 falls back to monogram without layout shift (WU-17).
- **Parity rows:** WU-15..WU-22, XC-8, OPS-9.

## U7. Game detail

- **Purpose:** one game's saves, health, and actions.
- **User & entry:** card click from U5/U6; palette game search; tray synced-game click lands on
  U8 (versions) with a breadcrumb up to U7.
- **Data:** `/dashboard/games/{id}`; preview partial; per-game export/delete routes.
- **Layout (per mockup):** hero: large cover + title + status pill + metric cards (files, size,
  versions, **Encryption**); main: per-category save explorer (groupSaves) — file rows with lock
  badge, Preview (plaintext only), Versions link; right rail: insights panel (largest change +
  device, recent activity for this game) — the mockup's contextual-rail pattern.
- **States:** *encrypted saves* — no Preview button; explorer shows lock badges; metadata/versions
  fully functional (WU-24/25); *empty category* collapsed; *preview of binary* → "binary save
  file" notice; *read-only* — delete/restore disabled w/ tooltip.
- **Interactions:** preview expands inline (16KB cap notice when truncated); export .zip /
  .zip+history; delete-all confirm names the game.
- **Responsive:** rail stacks; explorer rows wrap path under name on phone.
- **Acceptance:** encrypted account: every function except preview works identically; game IDs
  containing `/` resolve (prefix-match, WU-23).
- **Parity rows:** WU-23..WU-28.

## U8. Version history

- **Purpose:** inspect and roll back a file's versions.
- **User & entry:** Versions link in U7; tray synced-game click (TR-6 deep link).
- **Data:** `/dashboard/save/versions?game_id&path_key`; download/restore/preview routes.
- **Layout (per mockup):** vertical timeline (rail nodes color-coded: current=accent,
  ok=success-muted, conflict-era=warning) — each node: version, exact+relative time, size,
  **signed byte delta**, **authoring device**; actions per node: Download, Restore (dialog with
  review step); bottom primary action = Restore latest backup semantics stay unchanged. (Stage D
  adds play-session markers between nodes.)
- **States:** *encrypted* — timeline fully functional minus preview affordances; *single version*
  — timeline degrades gracefully; *restore success* → flash on U7 (WU-14); *read-only* — Restore
  disabled.
- **Interactions:** restore confirm shows what-will-happen review (device pull explanation).
- **Responsive:** timeline keeps rail on phone; metadata stacks.
- **Acceptance:** delta signs match `signedBytes` semantics; device attribution shown for
  versions with client_id; current version visually distinct (WU-29..31).
- **Parity rows:** WU-29, WU-30, WU-31, WU-25 (preview linkage).

## U9. Devices

- **Purpose:** manage connected clients.
- **User & entry:** sidebar "Devices"; chord `g v`; dashboard rail panel link.
- **Data:** `/dashboard/clients`, partial `clients-list`; rename/revoke POSTs; refresh triggers
  load + `save-updated` + `client-activity` + 30s.
- **Layout:** device cards: name (rename dialog), **OS badge** (Stage C fills; slot now), app
  version, online/offline dot, last-seen; revoke with confirm. Stage C upgrades this page into
  the Device Health board (freshness states, token countdown, stale callouts) — the Phase 1
  layout reserves the row space.
- **States:** *empty* — "no devices" + connect instructions; *revoked device* disappears on next
  refresh; *read-only* — revoke/rename still allowed (session-auth actions, not storage writes —
  matches current behavior).
- **Acceptance:** rename round-trips; revoke invalidates the client token (client shows re-login);
  auto-refresh cadence unchanged (WU-43).
- **Parity rows:** WU-40..WU-43.

## U10. Insights

- **Purpose:** personal analytics + protection posture.
- **User & entry:** sidebar "Insights"; chord `g i`.
- **Data:** `/dashboard/analytics?days=7|30|90`; `insights_body` partial; SSE refresh.
- **Layout:** window selector; metric row; protection-at-a-glance strip (backups/2FA/encryption
  coverage/devices online — Stage C extends into Setup Health checklist); charts: sync volume,
  data synced, device attribution, weekday/hour rhythm; panels: most-active, top-by-storage,
  version depth, save-vs-config split; staleness alerts.
- **States:** *sparse data* — charts render single-point/empty variants (02 §2 charts); *encrypted*
  — encryption coverage shows 100%/partial correctly; everything else metadata-based.
- **Acceptance:** window switch preserves scroll; SSE refresh ≤2s after a push without focus loss;
  all chart colors from tokens (WU-32..39).
- **Parity rows:** WU-32..WU-39.

## U11. Settings

- **Purpose:** account security + preferences.
- **User & entry:** sidebar "Settings"; chord `g s`.
- **Data/routes:** password, sessions revoke(-all), 2FA enable/confirm/disable/recovery,
  encryption, notifications, language POSTs.
- **Layout:** sectioned single page (anchor nav like admin settings): Password / Two-factor
  (status, recovery-count badge, regenerate, disable) / Encryption (toggle now; deep-links to
  Encryption Center in Stage D) / My notifications (webhook/Discord/ntfy + send test) / Sessions
  (table, current badge, revoke one/all) / Language (when >1 locale).
- **States:** *2FA enable flow* → U12; *recovery regenerate* → U13 (show-once); *encryption
  toggle* — explains consequences (server can't preview; client passphrase required); *read-only*
  — all these are auth-domain writes and remain allowed (current behavior).
- **Acceptance:** password change revokes other sessions+tokens (API-14 parity); test
  notification delivers to user sinks; language picker hidden with 1 locale (WU-50).
- **Parity rows:** WU-44..WU-50.

## U12. 2FA enrollment  /  U13. Recovery codes (show-once)

- **Purpose:** QR + secret enroll with code confirm (U12); one-time display of 10 recovery codes
  with copy-all + download .txt (U13, also after regenerate).
- **States:** wrong confirm code inline; U13 warns "shown once"; enabling 2FA revokes client
  tokens (explain: devices must re-login).
- **Acceptance:** codes are SHA-256-stored, display-once verified; count badge on U11 updates.
- **Parity rows:** WU-46, WU-47.

## U14. Error pages (404/403)

- Branded, code hero, back-to-dashboard; 403 for non-admin on `/admin`. Parity: WU-51.

## U15. ★ Conflict Center (new — Stage D; spec here for slot-reservation)

- **Purpose:** see and resolve sync conflicts across all devices from the web (today: tray/client
  only — CB-6; server persists nothing — plan correction #2).
- **User & entry:** sidebar badge when unresolved >0; notification deep-link; dashboard callout.
- **Data (new):** `conflicts` table (migration 30) written on every 409 path
  (`handler.go:988-1071`); new endpoints `GET/POST /api/conflicts` (+ web partials); SSE
  `conflict-recorded`.
- **Layout:** queue list grouped by game/device: local vs server metadata (hash, mtime, device),
  policy that applied; actions per row: keep local / keep server / (both = keep server + export
  local copy); per-game policy override editor (writes server-side overrides; distribution to
  clients designed in 06/07).
- **States:** *empty* — "no conflicts, policies working" + policy summary; *encrypted* — metadata
  only (no content diff ever — by design); *client offline* — resolution queues until the device
  next syncs (explained inline); *read-only* — resolution disabled.
- **Acceptance:** a 409 produced by two racing devices appears here ≤2s (SSE), resolving from web
  converges both clients on next sync; tray flow (TR-9) keeps working unchanged.
- **Parity rows:** extends CB-6/CB-7/TR-9 (all preserved); FIX-4.

## U16. ★ Notification Inbox (new — Stage D)

- **Purpose:** in-app mirror of notification events (bell), fed by audit log + SSE.
- **Data (new):** read-state table (migration), `GET /api/inbox`, `POST /api/inbox/read`; sources:
  audit rows + notify events (conflict, quota, device, login, backup, stale, recovery-low).
- **Layout:** topbar bell (slot exists from Phase 1) → panel: unread-first list, per-item icon by
  event type, deep links (conflict → U15, backup → admin, device → U9); mark-all-read.
- **States:** empty; SSE-live; degraded (SSE down → poll on open).
- **Acceptance:** every event type that can push externally (AD-22 list) also lands in the inbox;
  unread count survives reload.
- **Parity rows:** extends WU-49/AD-22/AD-17 (all preserved).

## U17. ★ Encryption Center (new — Stage D)

- **Purpose:** make E2E state legible: fleet `crypto_v2_ready`, per-save encrypted/plaintext
  coverage, which devices are current, migration progress. (Today: bare toggle + coverage %.)
- **Data:** `store.CryptoV2Ready` (exists), per-save `encrypted` flags (exist), `clients.app_version`
  (exists); new read endpoints/partials only, plus client-reported passphrase-presence (designed
  in 06).
- **Layout:** status hero (Ready v2 / Mixed fleet — which device holds it back / Legacy), coverage
  bar encrypted-vs-plaintext with per-game drill, device list with crypto capability, guided
  "encrypt existing saves" explainer (client-driven re-push).
- **States:** plaintext account (CTA to enable), mixed, fully encrypted; *read-only* — display only.
- **Acceptance:** a v3 client seen 10 days ago shows as the fleet blocker by name.
- **Parity rows:** extends WU-48/OPS-8 (preserved).

## U18. ★ Storage Explorer (Stage C — extends U10/admin)

- Per-game / version-history size breakdown with one-click prune (server delete of old versions
  within retention floor) + user-facing retention view; admin keeps override editing (AD-23).
- **States:** over-quota highlights biggest wins; grandfathering explained (OPS-3); read-only —
  prune disabled; encrypted — sizes fine (ciphertext sizes).
- **Parity rows:** extends WU-8/WU-36/AD-23/OPS-3/OPS-6.

## U19. ★ Setup Health / Restore Confidence (Stage C — components, not pages)

- **Setup Health:** guided checklist card (admin overview + user insights variants): TLS detected,
  backup configured + last run ok, first client connected, first sync done, notifications tested,
  2FA enabled. Each row deep-links to the fixing surface. Extends AD-4/WU-37.
- **Restore Confidence:** user-visible last-backup panel (time/size/destination, integrity last
  pass) + "Verify now" surfaced beyond admin (read-only for non-admin). Extends AD-6/AD-21/AD-28.

---

## A1. Admin overview

- **Purpose:** fleet + server health snapshot.
- **Entry:** `/admin` (admin role; 403 otherwise → U14).
- **Data:** stats, integrity findings + run, jobs partial (SSE `job-progress/finished`), backup
  run, source prompt, trend charts.
- **Layout:** About card; stat row (incl. live SSE connections); Setup Health checklist (Stage C
  replaces getting-started list, AD-4); Integrity panel + Verify now + findings table; Jobs panel;
  trends.
- **States:** *first run* — PCGW source prompt dominant (AD-3); *job running* — live progress;
  *integrity findings >0* — error-tinted panel; *read-only* — Verify/Backup now disabled.
- **Acceptance:** Verify/Backup/Test buttons work (regression: the v4.0.0 CSRF bug class —
  covered by ui-smoke POST checks in Stage B); jobs paging survives SSE refresh (AD-7).
- **Parity rows:** AD-1..AD-8.

## A2. Admin users  /  A3. User drill-down

- **Data/routes:** users table + action menu (insights/enable/disable/delete/quota GB dialog),
  create dialog, all-clients table + revoke; drill-down reuses `insights_body` read-only.
- **States:** deleting a user names it and its storage; quota presets; disabled users badged;
  drill-down for empty user shows empty charts.
- **Acceptance:** quota GB↔bytes conversion exact; create enforces 8–72 (v4.3.0 fix preserved);
  action dropdown not clipped by sticky columns (v3.2.1 z-index fix preserved).
- **Parity rows:** AD-9..AD-13.

## A4. Manifest  /  A5. Activity  /  A6. Logs

- As today, re-skinned: manifest search/paging/CSV/Push (AD-14/15, refresh on `manifest-updated`);
  activity = jobs/fetches/audit/snapshots paged tables with Table ⚙ + audit CSV honoring filters
  (AD-16/17); logs = filters, routine-HTTP toggle, auto-refresh, newer/older paging, CSV (AD-18).
- **States:** each table: empty/filtered-empty/loading; logs auto-refresh pauses when tab hidden
  (existing behavior preserved).
- **Parity rows:** AD-14..AD-18, XC-5..XC-9.

## A7. Admin settings

- Sectioned form with anchor nav + dirty sticky save bar; sections per AD-19..AD-26; side panels:
  Test notifications, Backup now, Cover cache (count/clear).
- **States:** env-override notices (env > DB precedence, SW-5); invalid cron inline; retention
  JSON validated (1–50); *read-only* — save disabled, view intact.
- **Acceptance:** save persists atomically + audits `admin_settings_save`; test button routes to
  admin + own sinks.
- **Parity rows:** AD-19..AD-29.

## A8. Admin analytics

- Four tabs as full-page links (deliberate, preserved): Overview / Fleet / PCGW / Sync History;
  `?window=`. All panels per AD-30..33.
- **Parity rows:** AD-30..AD-33.

## A9. PCGW mission control  /  A10. PCGW page detail

- **Phase 1 (visual only):** current capabilities re-skinned: stat cards, SSE job status with
  phase/pages/ETA/pages-sec/cap, filter toolbar, games table, all job actions + cancel,
  import/export (AD-34..36); detail page: parsed-location tables, wikitext, refresh (AD-37).
- **Stage D (guided redesign):** status → recommended-action hero ("Catalog is 3 versions behind →
  Fetch bundle now"; "12 pages dead-lettered → Retry failed"), actions grouped by intent
  (Update / Repair / Rebuild / Danger), dead-letter and Auto Catch-Up explained in place. All
  Phase-1 controls remain reachable.
- **States:** job running (live ETA); dead-letter >0 (warning); bundle N-behind; API mode vs S3.
- **Parity rows:** AD-34..AD-37.

---

## Coverage check

Rows covered above: API-1..17 (contract, unchanged) · WU-1..52 · XC-1..21 (inherited §0 +
per-screen) · AD-1..38 · SW-1..5 · OPS-1..10 (degraded states per §0 + U18/U19/A1/A7) ·
FIX-3/4/5 (web-side). Client-surface rows (TR/CW/CB/CL, FIX-1/2/6) are covered in
`04-SCREENS-CLIENT.md`.
