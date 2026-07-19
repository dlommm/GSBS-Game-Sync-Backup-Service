# Appearance System (color scheme + layout, synced to clients) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-user appearance preferences — color scheme (the five `data-design` token layers + default) and layout (sidebar / topnav / dense / library) — picked in Settings, rendered flash-free on the server WebUI, and synced to the client's local WebUI through the existing account-settings fetch.

**Architecture:** A generic `user_prefs` key/value table (migration 33) stores `appearance.design` and `appearance.layout`. The server renders `data-design`/`data-layout` on `<html>` from the signed-in user's prefs (PageData fields); theme-boot.js keeps `?design=`/`?layout=` as preview overrides that win over everything. `GET /api/account` exposes the two values; the client persists them in its account-settings cache and its local WebUI renders the same attributes. Layouts are CSS layers keyed off `data-layout` (grid-template/spacing swaps, no new pages). The Widget Grid layout is a separate follow-up plan (drag-drop + per-user panel JSON is its own subsystem).

**Tech Stack:** Go (server/store/api/webui, client), html/template, hand-written CSS layers in `server/webui/static/src/input.css` (compiled by `script/build-webui.sh`), no new dependencies.

## Global Constraints

- Valid designs: `"" (default), hud, crt, hearth, synth, slate`. Valid layouts: `"" (sidebar default), topnav, dense, library`. Reject anything else server-side; unknown values render as default.
- Migration 33: bump `schemaVersion` in `server/store/migrate.go` (currently 32); append one `migrationStep`; migrations run in their own tx (existing `runMigrationStep`).
- Theme (dark/light) stays per-browser (localStorage `gsbs.theme`) — a Deck in dark and a desktop in light must coexist; do NOT sync theme.
- Strict CSP everywhere: no inline scripts/styles (guarded by `template_csp_test.go` on both webuis).
- New templates/blocks must be registered in `template_names_test.go` (server) — the drift guard.
- API endpoint list drift is tested (`server/api/handler.go` route table + openapi.json test).
- After any CSS change: `bash script/build-webui.sh`; after any template/static change: rebuild server binary before screenshotting (embedded FS).
- Full gate before each commit: `go test ./... `(touched pkgs at minimum)`, gofmt, golangci-lint`; ui-smoke.sh after route/template changes.

---

### Task 1: `user_prefs` store (migration 33)

**Files:**
- Modify: `server/store/migrate.go` (schemaVersion 32→33; append step in `migrationSteps()`)
- Modify: `server/store/store.go` (interface + doc comments)
- Modify: `server/store/sqlite.go` (implementations)
- Test: `server/store/user_prefs_test.go` (create)

**Interfaces:**
- Produces: `GetUserPref(ctx, userID, key string) (string, error)` — "" when unset; `SetUserPref(ctx, userID, key, value string) error` — upsert, empty value deletes the row.

- [ ] **Step 1: failing test** — `server/store/user_prefs_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestUserPrefsRoundTrip(t *testing.T) {
	s := newTestStore(t) // same helper the other store tests use
	ctx := context.Background()
	uid := createTestUser(t, s, "prefuser")

	if v, err := s.GetUserPref(ctx, uid, "appearance.design"); err != nil || v != "" {
		t.Fatalf("unset pref = %q, %v; want \"\", nil", v, err)
	}
	if err := s.SetUserPref(ctx, uid, "appearance.design", "hud"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetUserPref(ctx, uid, "appearance.design"); v != "hud" {
		t.Fatalf("pref = %q, want hud", v)
	}
	if err := s.SetUserPref(ctx, uid, "appearance.design", "crt"); err != nil { // upsert
		t.Fatal(err)
	}
	if v, _ := s.GetUserPref(ctx, uid, "appearance.design"); v != "crt" {
		t.Fatalf("pref = %q, want crt", v)
	}
	if err := s.SetUserPref(ctx, uid, "appearance.design", ""); err != nil { // delete
		t.Fatal(err)
	}
	if v, _ := s.GetUserPref(ctx, uid, "appearance.design"); v != "" {
		t.Fatalf("pref after clear = %q, want \"\"", v)
	}
}
```

(Adjust `newTestStore`/`createTestUser` to the file's actual sibling helpers — copy whatever `TestUserNotifySettings`-style tests use.)

- [ ] **Step 2: run** `go test ./server/store/ -run TestUserPrefsRoundTrip` — FAIL (undefined methods).
- [ ] **Step 3: implement.** migrate.go: `const schemaVersion = 33`; append to `migrationSteps()`:

```go
{version: 33, fn: func(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS user_prefs (
		user_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (user_id, key),
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`)
	return err
}},
```

sqlite.go (next to the notify-settings methods):

```go
// GetUserPref returns a per-user preference value ("" when unset).
func (s *sqliteStore) GetUserPref(ctx context.Context, userID, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM user_prefs WHERE user_id = ? AND key = ?`, userID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetUserPref upserts a per-user preference; an empty value deletes the row.
func (s *sqliteStore) SetUserPref(ctx context.Context, userID, key, value string) error {
	if value == "" {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM user_prefs WHERE user_id = ? AND key = ?`, userID, key)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_prefs (user_id, key, value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		userID, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}
```

store.go interface (near the notify-settings entries):

```go
// GetUserPref returns a per-user preference value ("" when unset).
GetUserPref(ctx context.Context, userID, key string) (string, error)
// SetUserPref upserts a per-user preference; empty value deletes it.
SetUserPref(ctx context.Context, userID, key, value string) error
```

- [ ] **Step 4: run** the test — PASS. Also `go test ./server/store/` (full pkg — migration runs in every test store).
- [ ] **Step 5: commit** `feat(store): user_prefs table (migration 33) + Get/SetUserPref`

---

### Task 2: server WebUI renders per-user appearance

**Files:**
- Modify: `server/webui/render.go` (PageData fields + appearance helper + keep `designDefault` as fallback)
- Modify: `server/webui/router.go` or handler sites: populate via `adminPageData` and the user-facing full-page handlers (dashboard, games, game detail, devices, conflicts, analytics, settings, save versions)
- Modify: `server/webui/templates/layout.html`
- Test: extend `server/webui/handlers_auth_test.go`-style webui test (create `server/webui/appearance_test.go`)

**Interfaces:**
- Consumes: `store.GetUserPref` (Task 1).
- Produces: `PageData.Design string`, `PageData.Layout string`; helper `(h *WebHandler) appearance(ctx context.Context, userID string) (design, layout string)`; validators `validDesign(string) bool`, `validLayout(string) bool` (shared with Task 3/4).

- [ ] **Step 1: failing test** — `server/webui/appearance_test.go`: set prefs for a user via the store, GET /dashboard with their session, assert body contains `data-design="hud"` and `data-layout="topnav"`. Use the package's existing test harness (`newTestWebHandler` / session cookie helpers used by `handlers_auth_test.go`).
- [ ] **Step 2: run** — FAIL (attrs absent).
- [ ] **Step 3: implement.** render.go:

```go
// Valid appearance values — shared by the Settings form, the API, and rendering.
func validDesign(d string) bool {
	switch d {
	case "", "hud", "crt", "hearth", "synth", "slate":
		return true
	}
	return false
}

func validLayout(l string) bool {
	switch l {
	case "", "topnav", "dense", "library":
		return true
	}
	return false
}

// appearance resolves a user's stored appearance (design, layout), falling
// back to the server-wide GSBS_DESIGN default. Invalid stored values render
// as default rather than erroring.
func (h *WebHandler) appearance(ctx context.Context, userID string) (string, string) {
	design, _ := h.store.GetUserPref(ctx, userID, "appearance.design")
	layout, _ := h.store.GetUserPref(ctx, userID, "appearance.layout")
	if !validDesign(design) {
		design = ""
	}
	if design == "" {
		design = defaultDesign
	}
	if !validLayout(layout) {
		layout = ""
	}
	return design, layout
}
```

PageData gains `Design string` and `Layout string`. `adminPageData` populates both (one call site covers all admin pages). For user pages, populate in each full-page handler right after `requireSession`, e.g. dashboard:

```go
design, uiLayout := h.appearance(r.Context(), userID)
// ... PageData{ ..., Design: design, Layout: uiLayout }
```

layout.html `<html>` tag:

```html
<html lang="{{if .Locale}}{{.Locale}}{{else}}en{{end}}"{{if .Design}} data-design="{{.Design}}"{{end}}{{if .Layout}} data-layout="{{.Layout}}"{{end}}>
```

theme-boot.js: extend the design block to also handle `?layout=` (same pattern, key `gsbs.layout`, list `['default','topnav','dense','library']`, attr `data-layout`) — preview override only; server attr already present for authed pages, and boot only overwrites when a query/stored value exists. IMPORTANT: when localStorage has a stored value AND the server rendered an attr, the SERVER value wins unless a `?design=`/`?layout=` param is present — clear the stored key when the server attr exists so signed-in state is authoritative:

```js
var serverSet = document.documentElement.hasAttribute('data-design');
// query param → apply + persist (preview); else if !serverSet → stored/meta fallback
```

- [ ] **Step 4: run** the new test + `go test ./server/webui/` (template_names_test data: `PageData` literals unchanged — new fields zero-value fine) — PASS. `node --check` theme-boot.
- [ ] **Step 5: commit** `feat(webui): render per-user appearance (data-design/data-layout) server-side`

---

### Task 3: Settings → Appearance section

**Files:**
- Modify: `server/webui/templates/settings.html` (new panel before Sessions)
- Modify: `server/webui/handlers_settings.go` (settingsData fields + POST handler)
- Modify: `server/webui/router.go` (route `POST /dashboard/settings/appearance`)
- Modify: `server/webui/handlers_auth.go` (`adminQuerySuccess`: `appearance_saved=1` → "Appearance saved. It follows you to every device.")
- Modify: `server/webui/static/src/input.css` (swatch styles)
- Test: extend `server/webui/handlers_settings_test.go`; ui-smoke sweeps `/dashboard/settings` already

**Interfaces:**
- Consumes: `validDesign`/`validLayout`, `SetUserPref`, `appearance` (Task 2).
- Produces: form POST `design=<name>&layout=<name>&csrf=…` → 303 `/dashboard/settings?appearance_saved=1`.

- [ ] **Step 1: failing test** — POST `/dashboard/settings/appearance` with `design=synth&layout=dense` + valid CSRF/session; assert 303 and `GetUserPref` returns the values; POST `design=bogus` asserts 303 with `?error=invalid_appearance` and pref unchanged.
- [ ] **Step 2: run** — FAIL (404).
- [ ] **Step 3: implement.** Handler in handlers_settings.go:

```go
// handleAppearanceSave stores the user's color scheme + layout choice.
func (h *WebHandler) handleAppearanceSave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	design := strings.TrimSpace(r.FormValue("design"))
	uiLayout := strings.TrimSpace(r.FormValue("layout"))
	if design == "default" {
		design = ""
	}
	if uiLayout == "sidebar" {
		uiLayout = ""
	}
	if !validDesign(design) || !validLayout(uiLayout) {
		Redirect(w, r, "/dashboard/settings?error=invalid_appearance")
		return
	}
	if err := h.store.SetUserPref(r.Context(), userID, "appearance.design", design); err != nil {
		Redirect(w, r, "/dashboard/settings?error=save_failed")
		return
	}
	if err := h.store.SetUserPref(r.Context(), userID, "appearance.layout", uiLayout); err != nil {
		Redirect(w, r, "/dashboard/settings?error=save_failed")
		return
	}
	Redirect(w, r, "/dashboard/settings?appearance_saved=1")
}
```

Route (router.go, near the other settings POSTs): `case path == "/dashboard/settings/appearance" && r.Method == http.MethodPost: h.handleAppearanceSave(w, r)`.

settingsData gains `Design string` / `UILayout string` (populated from `h.appearance` in the settings GET handler — note stored value, not the env-fallback: read prefs directly so "default" stays selected when unset).

settings.html panel (token swatches are static CSS backgrounds — they show each design's accent on its surface):

```html
<section class="panel" aria-labelledby="appearance-heading">
  <div class="panel-header"><h2 id="appearance-heading">Appearance</h2></div>
  <form method="post" action="/dashboard/settings/appearance" class="panel-body appearance-form">
    <input type="hidden" name="csrf" value="{{.CSRFToken}}">
    <fieldset class="appearance-group">
      <legend>Color scheme</legend>
      <div class="swatch-row">
        {{range $d := designChoices}}
        <label class="swatch swatch-{{$d.Key}}">
          <input type="radio" name="design" value="{{$d.Key}}" {{if eq $.Design $d.Key}}checked{{end}}>
          <span class="swatch-chip" aria-hidden="true"></span>{{$d.Name}}
        </label>
        {{end}}
      </div>
    </fieldset>
    <fieldset class="appearance-group">
      <legend>Layout</legend>
      <div class="swatch-row">
        {{range $l := layoutChoices}}
        <label class="swatch">
          <input type="radio" name="layout" value="{{$l.Key}}" {{if eq $.UILayout $l.Key}}checked{{end}}>{{$l.Name}}
        </label>
        {{end}}
      </div>
    </fieldset>
    <p class="cell-muted">Applies on every device you sign in from, including the client's local pages. The dark/light toggle stays per-browser.</p>
    <button type="submit" class="btn-primary-sm">Save appearance</button>
  </form>
</section>
```

funcMap additions in render.go:

```go
"designChoices": func() []struct{ Key, Name string } {
	return []struct{ Key, Name string }{
		{"default", "Mint Vault"}, {"hud", "Night Ops"}, {"crt", "Phosphor"},
		{"hearth", "Hearth"}, {"synth", "Arcade"}, {"slate", "Foundry"},
	}
},
"layoutChoices": func() []struct{ Key, Name string } {
	return []struct{ Key, Name string }{
		{"sidebar", "Sidebar"}, {"topnav", "Top nav"}, {"dense", "Dense"}, {"library", "Library"},
	}
},
```

(The settings GET handler maps stored "" → selected keys "default"/"sidebar".) CSS: `.swatch` = radio pill (accent ring when checked via `:has(input:checked)`), `.swatch-chip` = 1.1rem circle whose background is the design's accent (static hexes fine: `.swatch-hud .swatch-chip { background:#35c7f0 }` etc.). Add `invalid_appearance` to `adminQueryError` ("That appearance choice isn't available.") and the success flash.

- [ ] **Step 4: run** tests + `bash script/build-webui.sh` + ui-smoke — PASS/46.
- [ ] **Step 5: commit** `feat(webui): Appearance settings — color scheme + layout picker`

---

### Task 4: API exposes appearance

**Files:**
- Modify: `server/api/handler.go` (`handleAccount` response)
- Modify: `server/api/openapi.go` spec JSON if the account schema is enumerated there
- Test: extend the account API test in `server/api/`

**Interfaces:**
- Produces: `GET /api/account` response gains `"appearance": {"design": "...", "layout": "..."}` (empty strings = defaults).

- [ ] **Step 1: failing test** — set prefs via store in the API test harness; GET /api/account; assert `appearance.design == "hud"`.
- [ ] **Step 2: run** — FAIL.
- [ ] **Step 3: implement** in `handleAccount` (after the quota lines):

```go
design, _ := h.store.GetUserPref(r.Context(), userID, "appearance.design")
uiLayout, _ := h.store.GetUserPref(r.Context(), userID, "appearance.layout")
resp["appearance"] = map[string]string{"design": design, "layout": uiLayout}
```

No new endpoint → route table + drift tests untouched; update openapi.json account schema if present.

- [ ] **Step 4: run** `go test ./server/api/` — PASS.
- [ ] **Step 5: commit** `feat(api): /api/account exposes appearance prefs`

---

### Task 5: client syncs appearance to its local WebUI

**Files:**
- Modify: `client/account_cache.go` (cache fields `Design`, `Layout` + setter plumb-through)
- Modify: the `FetchAccountSettings` call path (`client/handlers_settings.go` / wherever the response is decoded) to parse `appearance` and persist it
- Modify: `client/webui/render.go` (replace env-only `defaultDesign` with cached values accessor set by client at startup/fetch: `SetAppearance(design, layout string)` package func guarded by mutex)
- Modify: `client/webui/templates/layout.html` + `setup.html` (`data-design`/`data-layout` attrs via funcMap `designDefault`/`layoutDefault` now reading the setter)
- Test: `client/webui` render test asserting attrs after `SetAppearance("synth","dense")`; account-fetch test asserting cache write

**Interfaces:**
- Consumes: `GET /api/account` `appearance` object (Task 4).
- Produces: `clientwebui.SetAppearance(design, layout string)`; env `GSBS_DESIGN` stays as initial fallback until first successful fetch.

- [ ] **Step 1: failing test** — clientwebui render test: `SetAppearance("synth", "dense")`, render dashboard page, assert `data-design="synth"` and `data-layout="dense"` in output.
- [ ] **Step 2: run** — FAIL.
- [ ] **Step 3: implement.** clientwebui:

```go
var (
	appearanceMu sync.RWMutex
	appDesign    = defaultDesign // GSBS_DESIGN fallback until the first account fetch
	appLayout    = ""
)

// SetAppearance updates the design/layout the local pages render with
// (called by the client after each successful account-settings fetch).
func SetAppearance(design, layout string) {
	appearanceMu.Lock()
	appDesign, appLayout = design, layout
	appearanceMu.Unlock()
}
```

funcMap: `designDefault`/`layoutDefault` read under RLock. layout.html/setup.html `<html>` attrs same pattern as server. Client fetch path: decode `appearance` from the account response, validate against the same allow-lists (duplicate the two tiny validators client-side), call `clientwebui.SetAppearance` + persist in `accountSettingsCache` (fields `Design`, `Layout`) so offline restarts keep the look; on startup, load cache → `SetAppearance`.

- [ ] **Step 4: run** `go test ./client/...` — PASS.
- [ ] **Step 5: commit** `feat(client): local WebUI follows the account's appearance prefs`

---

### Task 6: Top-nav Wide layout (`data-layout="topnav"`)

**Files:**
- Modify: `server/webui/static/src/input.css` (one layer)

**Interfaces:** none new — pure CSS keyed on the attr Task 2 renders.

- [ ] **Step 1: implement layer** (after the design-variant blocks):

```css
/* ── Layout: Top-nav Wide ── the sidebar becomes a horizontal nav strip. */
:root[data-layout="topnav"] .shell { flex-direction: column; }
:root[data-layout="topnav"] .sidebar {
  position: static; width: 100%; height: auto; flex-direction: row; align-items: center;
  gap: 0.25rem; padding: 0.4rem 1rem; border-right: none; border-bottom: 1px solid var(--border);
}
:root[data-layout="topnav"] .sidebar-nav { flex-direction: row; gap: 0.25rem; flex: 1; overflow-x: auto; }
:root[data-layout="topnav"] .sidebar-nav a { white-space: nowrap; }
:root[data-layout="topnav"] .sidebar-footer, /* admin link moves inline */
:root[data-layout="topnav"] .sidebar-divider { border: none; margin: 0; padding: 0; }
:root[data-layout="topnav"] .app-main .page-content { max-width: 90rem; }
:root[data-layout="topnav"] .dash-grid { grid-template-columns: minmax(0, 1fr) 22rem; }
@media (max-width: 900px) { /* drawer/burger unchanged: topnav collapses to the same mobile drawer */ }
```

EXACT selectors must be verified against `partials/sidebar.html` at implementation time (`.sidebar`, `.sidebar-nav`, footer class names) — adjust to the real class names; the layer touches ONLY layout properties (direction/size/border), never colors.

- [ ] **Step 2: rebuild css, restart seeded scratch server, screenshot dashboard + admin with `?layout=topnav`** (admin shell: apply the equivalent `.admin-sidebar` row treatment or explicitly leave admin on sidebar — decide by eye; plan default: admin also goes horizontal via the same properties on `.admin-sidebar`/`.admin-main`).
- [ ] **Step 3: fix what the screenshot shows** (nav overflow, brand placement) — iterate once.
- [ ] **Step 4: gates** (build-webui, template tests, ui-smoke) — PASS.
- [ ] **Step 5: commit** `feat(webui): Top-nav Wide layout`

---

### Task 7: Dense Ops layout (`data-layout="dense"`)

**Files:** same as Task 6.

- [ ] **Step 1: implement layer** — compression via the spacing-bearing components (no color changes):

```css
/* ── Layout: Dense Ops ── power-admin density. */
:root[data-layout="dense"] { --radius: 8px; --radius-sm: 5px; }
:root[data-layout="dense"] .page-content { padding: 1rem; max-width: 96rem; }
:root[data-layout="dense"] .panel { padding: 0.75rem 1rem; }
:root[data-layout="dense"] .panel-header, :root[data-layout="dense"] .panel-header-actions { margin-bottom: 0.5rem; }
:root[data-layout="dense"] .data-table { font-size: 0.8125rem; }
:root[data-layout="dense"] .data-table th, :root[data-layout="dense"] .data-table td { padding: 0.35rem 0.75rem; }
:root[data-layout="dense"] .stat-card { padding: 0.75rem 0.85rem; }
:root[data-layout="dense"] .stat-value { font-size: 1.25rem; }
:root[data-layout="dense"] .stats-row { gap: 0.5rem; margin-bottom: 0.9rem; }
:root[data-layout="dense"] .game-grid { grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 0.6rem; }
:root[data-layout="dense"] .dash-grid { gap: 0.75rem; }
:root[data-layout="dense"] .timeline-item { padding-bottom: 0.5rem; }
```

- [ ] **Step 2-4: screenshot admin overview + activity + dashboard with `?layout=dense`, iterate once, gates.**
- [ ] **Step 5: commit** `feat(webui): Dense Ops layout`

---

### Task 8: Library-first layout (`data-layout="library"`)

**Files:**
- Modify: `server/webui/static/src/input.css`
- Modify: `server/webui/handlers_dashboard.go` (recent-games cap 6 → 12 when the user's layout is `library` — the partial handler calls `h.appearance` and picks the cap)

- [ ] **Step 1: handler test** — with `appearance.layout=library`, `/dashboard/partial/saves` returns up to 12 cards (seed 8+ games in the webui test store; assert count > 6 rendered). Run: FAIL.
- [ ] **Step 2: implement handler** — in `serveDashboardSavesPartial`:

```go
limit := dashboardRecentGames // 6
if _, uiLayout := h.appearance(r.Context(), userID); uiLayout == "library" {
	limit = 12
}
```

- [ ] **Step 3: CSS layer** — the dashboard leads with the library:

```css
/* ── Layout: Library-first ── the dashboard is your shelf. */
:root[data-layout="library"] .dash-grid { display: flex; flex-direction: column; }
:root[data-layout="library"] .dash-rail { position: static; flex-direction: row; flex-wrap: wrap; }
:root[data-layout="library"] .dash-rail .panel { flex: 1 1 20rem; }
:root[data-layout="library"] .recent-games-grid { grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr)); }
:root[data-layout="library"] .game-row-card { flex-direction: column; }
:root[data-layout="library"] .game-row-main { flex-direction: column; }
:root[data-layout="library"] .game-row-cover { width: 100%; height: auto; aspect-ratio: 2 / 3; }
:root[data-layout="library"] #dashboard-stats .stats-row { grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 0.75rem; }
```

(The recent-games row cards become mini cover cards — full-width portrait art, text below; exact selectors verified against `partials/dashboard_saves.html`.)

- [ ] **Step 4: screenshots (`?layout=library`), iterate, gates.**
- [ ] **Step 5: commit** `feat(webui): Library-first layout`

---

### Task 9: docs + preview + CHANGELOG

**Files:**
- Modify: `docker-compose.designs.yml` header comment (mention `?layout=` too)
- Modify: `CHANGELOG.md` `[Unreleased]`
- Modify: `docs/wiki/` pages if Settings docs enumerate sections (check `Settings.md`)

- [ ] **Step 1:** document `?design=` / `?layout=` preview params + the Appearance section; CHANGELOG entry (migration 33 auto-runs note — first schema change since 32).
- [ ] **Step 2:** full gate: `go test ./... ./cmd/...`, lint, gofmt, ui-smoke, `script/check-wiki.sh`.
- [ ] **Step 3: commit** `docs: appearance system (color scheme + layout, client sync)`

---

### Out of scope (separate plan)

**Widget Grid layout** — user-arrangeable dashboard panels (drag-drop, per-user panel-order JSON in `user_prefs` key `dashboard.widgets`, keyboard-accessible reordering, reset-to-default). It is its own subsystem with real interaction design; plan it after the four layouts above ship, reusing `user_prefs` from Task 1.

## Self-Review

- Spec coverage: color selector (T3), layout selector (T3), whole-layout changes for dashboard+admin (T6-T8), client sync (T4-T5), picker persistence (T1-T2). Widget Grid explicitly deferred with its storage already provisioned. ✔
- Placeholder scan: Task 6/8 CSS marked "verify exact selectors at implementation" — deliberate (class names must match live partials), with the properties fully specified. ✔
- Type consistency: `validDesign`/`validLayout`, `appearance()`, `SetUserPref` signatures match across T1-T5; `uiLayout` local naming consistent (avoid shadowing the stdlib-ish `layout`). ✔
