# Design System & Naming — GSBS UI/UX Overhaul

> **PALETTE FINALIZED 2026-07-06** against the dashboard mockup image. Values below are sampled
> from it. One deliberate deviation, per our accessibility bar: the mockup renders light text on
> the filled teal pills (≈2.3:1 — fails WCAG AA); we keep the mockup's teal hue but pair filled
> accent elements with dark text (`--text-on-accent`, ≥6:1). The mockup's known AI artifacts
> (duplicated nav item, garbled titles, copyrighted art) are not part of the reference.

## 0. Principles

1. **Dark-first.** Dark is the design driver; light is a derived theme (see §1.7). Explicit user
   choice > OS preference > dark fallback — the existing `theme-boot.js` mechanism is kept.
2. **One system, two surfaces.** Tokens live once in `server/webui/static/src/input.css`;
   `script/build-webui.sh` compiles and syncs to `client/webui/static/`. Nothing in this document
   may fork the pipeline.
3. **Accent discipline.** One dominant accent (teal). Green means *success/healthy synced state*
   and nothing else. Amber means *warning/attention*. Red means *error/destructive*. Blue-cyan is
   reserved for *live sync activity* (pulses, progress). No decorative accent use.
4. **Artwork is the emotion; chrome is quiet.** Game covers carry the visual interest; surfaces
   stay near-black and low-contrast so artwork pops. Where no artwork exists, the monogram tile
   (existing `gameIconSVG`) inherits a deterministic hue from the game ID.
5. **CSP is a design constraint.** Zero inline styles/scripts. All state-driven styling goes
   through class toggles or `data-*` attributes read by external JS (existing `data-width-pct`
   CSSOM pattern). Enforced by `template_csp_test.go` on both surfaces.
6. **Evolve, don't rename.** The ~300 existing component classes keep their names wherever the
   concept survives (`panel`, `stat-card`, `badge-*`, `btn-*`, `data-table`…). Renames are allowed
   only when a concept genuinely changes, and require updating `template_names_test.go` data cases
   in the same commit.

## 1. Design tokens

All tokens are CSS custom properties on `:root` in `input.css`, replacing/extending the existing
block (currently lines 40–69). Native clients map tokens per §1.8.

### 1.1 Color — core neutrals (dark)

```css
:root {
  /* Backdrop & surfaces — sampled from the mockup (2026-07-06)      */
  --bg:            #101413;  /* content backdrop inside the shell    */
  --bg-gradient-a: #93e2b3;  /* page gradient, mint (top-left)       */
  --bg-gradient-b: #2fa693;  /* page gradient, teal (bottom-right)   */
  --bg-raised:     #151917;  /* shell: sidebar + topbar              */
  --surface:       #1e2321;  /* panels, cards (lighter than shell)   */
  --surface-hover: #242a27;  /* hover state of interactive surfaces  */
  --surface-active:#22332e;  /* selected card tint (teal-shifted)    */
  --border:        #2a302d;  /* hairlines                            */
  --border-strong: #364039;  /* emphasized separators                */
  --border-focus:  var(--accent);
  /* Text */
  --text:          #eef2f0;  /* primary                              */
  --text-secondary:#a8b3ae;  /* labels, secondary copy               */
  --text-muted:    #6f7b76;  /* timestamps, tertiary                 */
  --text-on-accent:#08211d;  /* dark text on filled accent (AA note) */
}
```

The mockup's page backdrop is a **bright** diagonal mint→teal gradient (`--bg-gradient-a` top-left
→ `--bg-gradient-b` bottom-right, fixed, GPU-cheap, no image asset) with the dark charcoal shell
(`--bg-raised`) floating on it: `--radius-lg` corners + `--shadow-shell`. The gradient is only
visible as the frame around the shell on desktop; on phones the frame collapses and the shell is
edge-to-edge. Inside the shell, the content column sits on `--bg` and cards step up to
`--surface` — three visible luminance steps, exactly as in the mockup.

### 1.2 Color — accent & semantics

```css
:root {
  /* Dominant accent: teal — sampled from the mockup's pills/active nav */
  --accent:        #3fbfae;
  --accent-hover:  #55cdbc;
  --accent-press:  #2fa091;
  --accent-muted:  rgba(63, 191, 174, 0.16);  /* tints, selected nav pill  */
  --accent-line:   rgba(63, 191, 174, 0.40);  /* focus rings, chart lines  */

  /* Semantics (dark) — success matches the mockup's check badge AND the
     existing tray "syncing" green (2ec27e): tray icons keep their hue.   */
  --success:       #2ec27e;   --success-muted: rgba(46, 194, 126, 0.14);
  --warning:       #eab54e;   --warning-muted: rgba(234, 181, 78, 0.14);
  --error:         #e5605e;   --error-muted:   rgba(229, 96, 94, 0.14);
  --info:          #4cc3e0;   --info-muted:    rgba(76, 195, 224, 0.14);
  /* Live sync activity (pulse dots, progress shimmer) */
  --sync:          var(--info);
}
```

Usage rules (these are review criteria, not suggestions):
- `--accent` — primary buttons, active nav pill, selected card tint, focus ring, links.
- `--success` — synced/healthy/online states, the check badge on a healthy game card.
- `--warning` — conflicts, stale devices, quota 80%, pending-attention.
- `--error` — failures, revocation, destructive confirms, over-quota.
- `--sync` — *transient* activity only (a device syncing right now, SSE liveness dot).
- Status pills, badges, tray icon variants, and chart series all derive from these five; no
  ad-hoc hex values anywhere in components.

### 1.3 Typography

```css
:root {
  --font: "DM Sans", system-ui, sans-serif;        /* vendored woff2, unchanged */
  --mono: "JetBrains Mono", ui-monospace, monospace;
  --text-xs: 0.75rem;  --text-sm: 0.8125rem;  --text-base: 0.875rem;
  --text-md: 1rem;     --text-lg: 1.125rem;   --text-xl: 1.375rem;
  --text-2xl: 1.75rem;
  --leading-tight: 1.25; --leading-normal: 1.5;
  --weight-normal: 400; --weight-medium: 500; --weight-bold: 700;
}
```

Numbers in stat/metric cards, byte deltas, version numbers, timestamps, paths, and log/code
content use `--mono` (tabular where supported). Page titles `--text-xl/--weight-bold`; panel
headers `--text-md/--weight-medium`; body `--text-base`. No font sizes outside the scale.

### 1.4 Spacing, radii, elevation

```css
:root {
  --space-1: 4px; --space-2: 8px; --space-3: 12px; --space-4: 16px;
  --space-5: 24px; --space-6: 32px; --space-7: 48px;

  --radius-xs: 4px;   /* badges, pills-in-tables       */
  --radius-sm: 8px;   /* buttons, inputs               */
  --radius:    12px;  /* panels, cards                 */
  --radius-lg: 20px;  /* app shell, dialogs            */
  --radius-full: 999px; /* nav pills, search field     */

  --shadow-sm:    0 1px 2px rgba(0,0,0,.35);
  --shadow:       0 4px 16px rgba(0,0,0,.35);
  --shadow-shell: 0 24px 64px rgba(0,0,0,.45);  /* the floating shell */
}
```

Elevation is used sparingly: the shell, dialogs, action menus, and toasts get shadows; panels
inside the shell separate by `--border` + surface step, not shadow.

### 1.5 Motion

```css
:root {
  --dur-fast: 120ms;   /* hovers, toggles                 */
  --dur:      200ms;   /* panel/dialog enter, accordion   */
  --dur-slow: 320ms;   /* page-level transitions, toasts  */
  --ease:     cubic-bezier(.2,.8,.2,1);
}
```

**What animates:** hover/press states (fast), dialog & palette enter (scale 0.98→1 + fade),
toast slide-in, skeleton shimmer, the `--sync` pulse dot, chart bar growth on first paint,
"Table ⚙" menu enter. **What never animates:** layout (no width/height tweens on content),
table sorting/paging swaps, SSE partial refreshes (content must replace in place with zero
motion — live data may update several times a minute), text color.
**Reduced motion:** `@media (prefers-reduced-motion: reduce)` collapses all durations to 0 and
replaces the sync pulse with a static dot; shimmer becomes a flat placeholder block.

### 1.6 Iconography & illustration

- Extend the existing inline-SVG `iconSVG` set (16×16, stroke 1.4) — new icons needed for
  sidebar IA: `inbox` (bell), `shield-lock` (encryption), `pulse` (live activity), `archive`
  (backups), `layers` (storage). Same grid, same stroke.
- Monogram game tiles: existing `gameIconSVG`, hue seeded from game ID; contrast floor per WCAG.
- Empty states keep the current pattern (icon + one sentence + primary action); no mascots.

### 1.7 Light theme derivation

Light is generated from dark by rule, not redesigned. `:root[data-theme="light"]` overrides:

| Token family | Rule |
|---|---|
| `--bg*` / `--surface*` | invert ramp: shell `#ffffff`, content `#f4f8f6`, surface `#ffffff`, hover `#eef4f1`, active = accent-muted. **The mint→teal page gradient stays in light theme** (softened ~20% toward white) — it reads naturally behind a white shell, giving both themes the same signature backdrop |
| `--border*` | `#dfe6e3` / `#c9d3cf` |
| `--text*` | `#17201d` / `#4d5a56` / `#7d8a86`; `--text-on-accent` stays dark |
| Accent + semantics | same hues, darkened one step for AA on white (`--accent: #149571` etc. — final values from the contrast validator during B1) |
| Shadows | lighter alphas (.12/.16/.2) |

Rule of thumb: hue is stable across themes; only luminance ramps flip. Both themes ship in
`app.css`; the existing toggle/boot mechanism is untouched.

### 1.8 Token mapping for native clients

- **Tray icons** (`tray_icons.go`): the six state variants re-derive from the token palette —
  syncing = `--success`… today's `2ec27e` moves to the new `--success` value; error = `--error`;
  setup/paused/recovering unchanged conceptually. One Go constant block mirrors the five semantic
  hex values with a comment pointing at `input.css` (single source documented, mechanically
  copied — acceptable for 5 values).
- **Client local WebUI**: no mapping needed — it consumes the same compiled `app.css`.
- **OS notifications, menus**: native-rendered; no theming possible or attempted.

## 2. Component library inventory

Existing classes keep their names; states listed are the acceptance surface for B1. New
components marked ★.

| Component | Classes (existing → evolved) | States required |
|---|---|---|
| App shell ★ | `app-shell`, `sidebar`, `sidebar-nav`, `topbar` (user pages gain the sidebar; admin keeps `admin-shell`, restyled) | default, collapsed (tablet), mobile drawer, active nav pill |
| Buttons | `btn-primary`, `btn-secondary`, `btn-ghost`, `btn-danger-sm` (+ `-sm` variants) | default/hover/focus-visible/active/disabled/loading (spinner replaces label, width locked) |
| Panels | `panel`, `panel-compact`, `panel-header(-actions)`, `panel-body`, `panel-empty`, `panel-subtitle` | default, loading (skeleton), empty, error strip |
| Stat & metric cards | `stat-card`, `stats-row(-5,-6)`, `metric-card` | default, loading, alert-tinted (warning/error), live-updating (no motion) |
| Game card ★ (mockup centerpiece) | `game-card`: artwork thumb, title/subtitle, last-synced, status row w/ glyph, inline actions (`Restore`, filled `View Versions` pill) | default/hover/selected (accent surface tint + success check badge)/conflict (warning)/syncing (`--sync` pulse)/no-artwork (monogram) |
| Version timeline ★ (right rail) | `timeline-rail`, nodes color-coded by state, per-node size + time, bottom primary action | default/empty/encrypted (metadata-only note)/loading |
| Tables | `data-table` + shared `table_pager` + "Table ⚙" (`data-table-id`) | default/sorted/paged/filtered/customized/empty/loading |
| Badges & pills | `badge-success/-warn/-failed/-running`, `os-badge`, `status-pill`, `count-badge`, `pill-yes/no` | static (no hover) |
| Progress | quota gauge (`gaugeSVG`), bars (`data-width-pct`), progress rings | normal/80% warning/over-quota error |
| Forms | `text-input`, `form-group`, selects, checkbox/radio, password kit (toggle/strength/match) | default/focus/invalid/disabled + dirty-tracking sticky save bar |
| Dialogs & menus | `<dialog>`-based modals, action menus, confirm guards (`data-confirm`) | open/closing/destructive variant |
| Toasts | `toast.js` stack | info/success/warning/error, auto-dismiss, reduced-motion |
| Command palette | `cmdk` | open/results/empty/recents |
| Skeletons & empty states | `loading-skeleton`, `skeleton-bar`, `empty-state` | shimmer / reduced-motion flat |
| Alerts/notices | `alerts.html` flash + inline notices | info/success/warning/error/read-only banner ★ (global, when `GSBS_READ_ONLY`) |
| Wizard | `wizard-steps` (setup + restore review) | step states: done/current/todo; validation errors |
| Charts | Go-rendered inline SVG (`barsSVG`, `bytesBarsSVG`, gauge, rings) | populated/empty/single-point; colors only from §1.2 tokens |

## 3. Naming conventions

| Domain | Convention | Examples | Rationale |
|---|---|---|---|
| CSS component classes | flat kebab-case, component-prefixed, no BEM | `game-card`, `game-card-actions`, `timeline-rail-node` | Matches the existing 300-class vocabulary; BEM migration would churn every template and the locked test data for zero benefit. |
| CSS tokens | `--<concept>[-<variant>]` kebab-case | `--surface-hover`, `--dur-fast`, `--radius-lg` | Extends the existing token names in place; grep-friendly; theme overrides stay one selector. |
| Utility classes | `u-` prefix | `u-hidden`, `u-mt-4` | Existing convention from the CSP cleanup (inline-style replacement); keeps utilities visually distinct from components. |
| Templates | `snake_case.html`; pages at root, fragments in `partials/`; page blocks `<page>_title/_content/_scripts` | `dashboard_games.html`, `partials/table_pager.html` | Locked by `template_names_test.go`; the pageBlock mechanism is shared with the client WebUI. |
| Routes | Web session routes: `/dashboard/...`, `/admin/...`, HTMX fragments under `.../partial/<thing>`; actions as POST verbs (`/rename`, `/revoke`) | `/dashboard/partial/clients-list` | Existing pattern; partial-vs-page distinction is visible in the URL, which keeps ui-smoke and CSP sweeps trivial to extend. |
| Go handlers | `serve<Thing>` (GET page), `serve<Thing>Partial` (fragment), `handle<Action>` (POST) | `serveDashboardGames`, `handleRestoreVersion` | Existing convention throughout `server/webui`; encodes method + kind in the name. |
| JS | one file per concern (`app.js`, `admin.js`, `cmdk.js`, `toast.js`); behavior bound via `data-action`/`data-*` attributes, delegated listeners only | `data-action="toggle-theme"`, `data-table-id` | CSP forbids inline handlers; `data-*` contracts keep templates declarative and testable. |
| SSE events | kebab-case `<subject>-<verb>` | `save-updated`, `job-finished` | Existing; new events (Stage C/D) follow it: `session-started`, `conflict-recorded`, `inbox-updated`. |
| New API endpoints (Stage D) | nouns under `/api/`, kebab-case, no verbs except established `revoke/refresh/restore` | `/api/conflicts`, `/api/inbox`, `/api/sessions` | Consistent with the 17-endpoint contract; additions must land in `openapi.json` + the drift test. |
| i18n keys | dot-namespaced `<area>.<key>` | `settings.language`, `tray.status_paused` | Existing `en.json` shape; flat enough for translators, structured enough to spot orphans. |
| Design docs | numbered `NN-TOPIC.md` in `docs/design/ui-overhaul/` | `03-SCREENS-WEB.md` | Reading order is explicit; stable anchors for cross-references from commits. |

## 4. Change control

- Any new/renamed template ⇒ update all three lists + data case in `template_names_test.go`
  (server) or `pageNames` (client) in the same commit.
- Any new component class ⇒ defined in `input.css` `@layer components`, tokens only (no raw hex).
- Any new page ⇒ added to `script/ui-smoke.sh` sweep + `g`-chord/palette registry where relevant.
- Palette changes after the mockup pass ⇒ single commit touching only §1.1/§1.2 values + tray
  icon constants, so the diff is reviewable against the image.
