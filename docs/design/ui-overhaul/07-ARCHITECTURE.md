# Technical Architecture — GSBS UI/UX Overhaul

## 1. Web frontend architecture — decision

**Decision: (a) evolve the incumbent** — Go `html/template` + HTMX 2 (vendored) + SSE partials +
Tailwind-compiled design system, rebuilt on the 02 tokens/components.

### Honest tradeoffs

| Criterion | (a) Evolve incumbent | (b) Svelte/Solid static export | (c) React SPA |
|---|---|---|---|
| Single-binary embed (`go:embed`) | native — already shipping | works (static export) | works, largest payload |
| Strict no-inline CSP | proven — enforced by tests today | achievable but fights hydration inlining defaults; every framework upgrade re-risks it | same risk, plus runtime style injection is idiomatic in the ecosystem |
| Shared design system with client WebUI | one compiled CSS, one pipeline — exists | must either duplicate tokens or keep the client on the old stack (two systems) | same fork problem |
| SSE live updates | wired on every page today | reimplement store/reconnect logic | reimplement |
| Payload / low-power hosts (Pi/NAS) | ~14 KB gz htmx + page HTML; server renders | 30–80 KB gz + hydration cost | 130 KB+ gz framework baseline |
| Parity risk | re-skin of working templates; locked-name + smoke tests carry over | rewrite of ~50 templates + all handlers' view models → weeks of parity bugs | same, more |
| Interactivity ceiling | fine for tables/forms/timelines; complex client state (drag, canvas) would strain | higher | highest |
| Long-term maintenance (solo maintainer) | one language + small JS files | two build ecosystems, framework churn | two ecosystems, faster churn |

The ceiling argument is the only real case for (b)/(c), and nothing in the §6 roadmap needs that
ceiling — the richest planned interactions (timeline, palette, live pulse) are all
server-rendered-fragment-friendly. If a future feature genuinely needs an app-like island (e.g.
a save-diff viewer), embed a single compiled web component for that island rather than converting
the site. **Rule: no runtime framework until a concrete feature defeats HTMX+SSE.**

### Build pipeline (unchanged shape, no runtime Node)

```
script/build-webui.sh:
  npx tailwindcss@3 -i server/webui/static/src/input.css -o server/webui/static/app.css --minify
  cp app.css theme-boot.js fonts/ logo → client/webui/static/
go build (go:embed templates/ + static/)
```

Node/npm at build time only; vendored htmx/sse never fetched at runtime; CI runs the same script.

## 2. Client architecture — decision

**Decision: keep Go tray (fyne.io/systray) + loopback WebUI**, deep-linking tray → local pages →
server pages.

- **Wails — rejected:** WebView2/WebKitGTK runtime deps break the pure-Go Linux client (currently
  CGO_ENABLED=0 → trivial cross-compiles, Flatpak with no GTK modules, arm64/Steam Deck);
  webview-in-Flatpak is its own permissions maze; and we'd still keep systray for the tray.
- **Fyne — rejected:** custom-drawn widgets duplicate the entire design system in canvas form,
  look native nowhere, cost cgo on macOS anyway, and add MBs to the binary. The loopback WebUI
  already renders the real design system pixel-identically to the server.
- **Tray-minimal + deep-link only — rejected:** the tray's synced/discovered/conflict submenus
  are the product's fastest paths (TR-6/7/9); flattening them to "open browser" degrades daily UX.
- **Token sharing:** the client WebUI consumes the same compiled `app.css` (build sync); the only
  native-side mapping is 5 tray-icon hex values (02 §1.8).
- **Platform notes honored:** Flatpak (no game-aware sync, software-center updates,
  StatusNotifierItem), Windows (metered, Walk dialog), macOS (ad-hoc sign, in-bundle self-update,
  menu-bar conventions), Steam Deck (touch targets, Desktop-Mode WebUI, `/run/media` grant).

## 3. Performance budgets (Pi 4 / low-power NAS reference host, Fast-3G-class LAN worst case)

| Metric | Budget | Now (est.) | Gate |
|---|---|---|---|
| JS transferred, any page | ≤ 40 KB gz total (htmx 14 + sse 2 + app/admin/cmdk/toast ≤ 24) | ~30 KB | CI size check in build-webui.sh (Stage B) |
| CSS transferred | ≤ 40 KB gz | ~15 KB | same |
| Server render (p95, warm, 1k saves) | ≤ 150 ms | typically <100 ms | ui-smoke timing assert (soft) |
| TTI dashboard on Pi-class | ≤ 2.0 s first visit, ≤ 1.0 s warm | — | manual Lighthouse per release |
| Cover images | lazy-load (`loading=lazy`), explicit dimensions (no CLS), ≤ 60 KB typical via proxy cache | partial | template audit in B3 |
| SSE reconnect | ≤ 10 s to live after server restart | htmx sse ext default | F8-style smoke |
| Fonts | 2 families, woff2, `font-display: swap`, self-hosted | shipping | keep |

## 4. Accessibility (WCAG 2.1 AA)

- **Contrast:** all token pairs validated in both themes during B1 (automated check against the
  02 palette — script in `script/`); status colors never the sole signal (glyph + text always).
- **Keyboard:** every flow in 05 completable keyboard-only; palette/chords/`?` overlay; dialogs
  focus-trap + Esc; skip-link (exists) kept; visible focus ring (`--accent-line`) everywhere.
- **Landmarks/AT:** `<nav>/<main>/<header>` landmarks in the new shell; tables keep real `<th>`
  scope; live regions: toast container + SSE-updated stat regions get `aria-live=polite`;
  timeline nodes are a list with per-node labels; charts get text summaries (the Go SVG helpers
  emit `<title>` + adjacent visually-hidden text).
- **Reduced motion:** 02 §1.5. **Touch:** 44 px minimum targets (Steam Deck).
- **Audit:** axe pass on every screen in B3/B4 + manual screen-reader walkthrough of F1/F4/F6.

## 5. PWA scope

Installability only, honestly scoped: manifest + icons + theme-color so the dashboard installs
as an app on desktop/mobile; **no offline shell.** A sync dashboard's data is meaningless
offline, and a service worker caching HTML risks stale-CSRF/stale-SSE bugs for zero real value.
"Offline" remains the *client's* domain (outbox, C4). Revisit only if the mobile companion (06)
materializes.

## 6. i18n integration

- Every string introduced/touched in Stage B goes through `t()` (`pkg/i18n`), including the new
  shell, empty states, and error strips; `en.json` grows accordingly (audit: no raw user-facing
  literals in templates — grep gate in CI).
- Client WebUI templates adopt the same catalog (shared keys where meaning matches, `client.*`
  namespace otherwise). Tray strings stay Go-side but move to a single `trayStrings` table to be
  catalog-ready.
- Language picker (WU-50) unchanged; locale files remain drop-in (`locales/<lang>.json`).
- Charts/dates: existing relative-time helpers get locale hooks but ship en-only behavior
  unchanged.

## 7. New API surface (Stage D preview; full design per feature at implementation)

| Addition | Shape | Notes |
|---|---|---|
| Conflicts | `conflicts` table (migration 30); `GET /api/conflicts`, `POST /api/conflicts/resolve`; SSE `conflict-recorded` | written by the three 409 paths; resolution state consumed by clients on sync |
| Inbox | read-state table (migration 31); `GET /api/inbox`, `POST /api/inbox/read`; SSE `inbox-updated` | sources: notify events + audit; web-session AND bearer auth (mobile-ready) |
| Sessions | `POST /api/sessions` (client reports start/end); rows joined into `GET /api/saves/versions` responses | powers U8 markers |
| Prune | `POST /dashboard/storage/prune` (web-session) | respects retention floor; audited |

All additions: registered in the `handler.go` route table, documented in `openapi.json`, covered
by the drift test, and versioned additively — the 17 existing endpoints never change semantics.
