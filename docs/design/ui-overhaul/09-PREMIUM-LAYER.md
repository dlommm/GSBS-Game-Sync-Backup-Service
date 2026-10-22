# 09 — Premium layer (v6.1 UI pass)

**Audited:** `main` @ `3b81636` (v6.0.0, 2026-10-22), branch `ui/premium-redesign`.
**Scope:** server WebUI + client loopback WebUI (shared `app.css`). No API, data-model,
sync, restore or conflict semantics changed.
**Status:** superseded by `10-CONTINUE.md` (v7). Kept as the audit record; the v6.1
before/after screenshots were not committed.

## 1. How the baseline was taken

- Fresh isolated server (temp DB, save root, cover cache) built from `3b81636`; seeded with
  **synthetic data only**: 14 invented game titles, procedurally drawn cover art for 12 of
  them (two have none, to exercise the monogram fallback), four fake devices (one silent
  19 days), ~100 pushed versions spread over three weeks, one open conflict.
- The real client binary ran headless in a sandboxed fake `HOME` (file token store, its own
  config), logged into that server.
- Playwright screenshots at 1440×900, 1280×760, 834×1112, 390×844, dark and light, every
  design (`?design=`) and layout (`?layout=`).

## 2. Audit findings (baseline)

Confirmed defects first, then quality issues. "Fixed" = addressed in this pass.

| # | Screen | Finding | Impact | Status |
|---|---|---|---|---|
| D1 | Dashboard | Recent-game cards: the filled "View Versions" button overflowed and was clipped by the card edge (`before/dashboard.jpg`), and it linked to the same URL as the card itself | Broken-looking primary surface; duplicate target | Fixed — one tile, one link |
| D2 | Version history | Sidebar highlighted **Dashboard**; breadcrumb skipped the game | Wayfinding | Fixed — My Games / *Game* / Version history |
| D3 | Restore dialog | Copy said "the server rolls back to this version" — restore actually writes the chosen contents as a **new** latest version (verified: restoring v2 of a 6-version slot produced v7; v6 stayed) | Users could fear losing the current save | Fixed — dialog shows *Restoring vN → Becomes vN+1*, "vM stays in history" |
| D4 | Mobile topbar | Search, bell, theme, username and Log out wrapped onto two rows (~100px of chrome) | Content pushed down on phones | Fixed — one row, search collapses to its icon |
| D5 | My Games (mobile) | Four export/import buttons sat above the library | Library started below the fold | Fixed — one "Export & import" menu |
| D6 | Monogram fallback | Square tile floated inside the 2:3 frame with empty bands; saturated green | Missing-art games looked broken | Fixed — `preserveAspectRatio="xMidYMid slice"`, deeper hue, smaller initials on cover-sized tiles |
| D7 | Accessibility (axe, light theme) | Notification badge, active tab/view toggle, success badges, breadcrumb links (colour-only) failed WCAG AA | AA regressions predating this pass | Fixed — all five key screens axe-clean in both themes |
| Q1 | Dashboard | No answer to "are my saves safe?" — four bare number tiles | The product's core promise was invisible | New protection hero |
| Q2 | Game detail | 76px cover, four separate metric tiles, **Delete all saves** beside Export | Weak identity; destructive action too close to routine ones | Artwork hero; delete moved into a separate *More* menu |
| Q3 | Version history | A table whose every row had a filled Restore button | No sense of a timeline; loud repeated primary | Recovery timeline; Restore is a secondary action |
| Q4 | Devices | Cards only; nothing showed the device ↔ server relationship | Sync story hard to read | Static sync map from `last_seen` |
| Q5 | Themes | Six designs differed mainly in accent hue and radius | "Accent swap" feel | Each design now also sets heading voice + artwork scrim tint (§4) |
| Q6 | Tables | Uppercase tracked headers on a filled band | Dated, noisy | Sentence-case quiet headers |

Not changed (behaviour, out of scope for a visual pass — tracked here):
- The client hero can say "All synced" while its Watcher card says "Recovering"; both come from
  the client status API.
- `/dashboard/games/<unknown id>` renders a page instead of the branded 404 (same on baseline).
- The appearance-picker preview generator (`script/gen-appearance-previews.py`) is macOS-only;
  the v6.1 previews were regenerated with equivalent settings (1500×980 → 560px JPEG q85,
  invented titles, monogram tiles only) using Playwright.

## 3. Direction

Three directions were compared on the real dashboard and library:

- **A — Premium Game Library** (artwork-forward, cinematic, restrained chrome)
- **B — Modern Gaming Companion** (expressive, colourful, effects)
- **C — Premium Save Vault** (protective, status-first)

**Chosen: A, with C's status-first hero.** It extends the existing principle *artwork is the
emotion; chrome is quiet* (02 §0.4) instead of fighting it, works across all six designs
because it is token-driven, and keeps the dashboard honest: the hero is a *status*, the
artwork is the *mood*. B was rejected: glows/neon and animated "activity" conflict with the
accent-discipline rules and would imply transfers that are not happening.

Design-skill coordination: the **Taste skill** (`design-taste-frontend`, app-screen rules,
style `true-black-flat`, seed 551927380) set the structure: colour only on data and status,
KPIs as type in one divided strip, flat surfaces with lightness steps, the latest game's
cover as the signature element. **Emil design engineering** set the interaction rules
(press scale 0.97, ease-out entries under 300ms, no animation on keyboard-driven UI,
hover gated to fine pointers). Deliberate deviation from the dealt style: GSBS keeps its
filled teal primary button and active-nav pill (owner-locked brand decisions from the
mockup). Impeccable, UI UX Pro Max, Interface Design and Frontend Design were not installed
and were not used.

## 4. Design-system changes (`input.css`)

### 4.1 New tokens (all derived — designs inherit them automatically)

| Token | Purpose |
|---|---|
| `--surface-raised` | nested panel one lightness step above `--surface` (`color-mix` of surface + text) |
| `--border-subtle` | in-card dividers |
| `--scrim` | neutral scrim under text on artwork (the one gradient the rules allow); per-design tint |
| `--ink-on-art`, `--ink-on-art-muted` | text over artwork |
| `--art-edge` | 1px inner highlight that seats artwork on dark grounds |
| `--shadow-float`, `--shadow-lift` | dialogs/menus; hovered artwork only — resting panels stay flat |
| `--focus-ring` | two-ring focus (ground gap + accent) |
| `--ease-out`, `--dur-press` | entry curve `cubic-bezier(.23,1,.32,1)`; 140ms press |
| `--title-weight`, `--title-tracking`, `--heading-font` | per-design heading voice |
| `--state-synced/-attention/-failed/-live/-idle` | sync-state vocabulary mapped onto the semantic colours |

### 4.2 Design voices

| Design | Heading voice | Scrim tint |
|---|---|---|
| Mint Vault | 700, −0.025em (default) | neutral |
| Night Ops | 600, −0.01em | cold blue |
| Phosphor | **mono face**, 600 | green-black |
| Hearth | 650, −0.02em | ember brown |
| Arcade | 800, −0.035em | violet night |
| Foundry | 600, −0.015em | graphite |

### 4.3 Components

New: `protect-hero` (dashboard), `game-shelf`, `game-card-state`, `state-chip`, `game-hero`
(detail), `version-timeline`/`version-node`, `restore-compare`, `sync-map`.
Evolved in place (names kept): `game-card` (no chrome box; the cover is the card; state chip on
a bottom scrim; healthy = icon-only check, stale/unknown spell themselves out), `stats-row`
(one divided container, label above value), `client-card`, `status-hero` (client), buttons
(icon-aware inline-flex, press feedback), `data-table` headers, dialogs, `action-menu`.
Removed: `game-row-*`, `recent-games-grid` (dashboard rows replaced by the shelf).

The v6.1 rules live in one block at the end of `input.css` ("v6.1 premium layer"), plain
CSS after the component layer so they evolve components without renaming them.

### 4.4 Motion

| What | How | Why |
|---|---|---|
| Buttons, tabs, view toggles | `scale(.97)` on `:active`, 140ms ease-out | confirms the press |
| Game cards | cover rises 3px + lift shadow, `(hover:hover) and (pointer:fine)` only | no sticky hover on touch / Steam Deck |
| Dialogs | fade + `scale(.97)→1` + 6px rise, 200ms ease-out; backdrop fades | spatial entry |
| Menus | 160ms from `top right` origin | origin-aware |
| Command palette | **no animation** | keyboard UI opened many times a day |
| HTMX/SSE refreshes | **no animation** | live data must replace in place |
| Sync map | static | never imply a transfer that isn't happening |

`prefers-reduced-motion` collapses all of it (existing base-layer rule).

## 5. Screens

- **Dashboard** — protection hero answers one question from real data only
  (`loadProtection`): open conflicts → *attention*; devices silent 14+ days or games unsynced
  30+ days → *warn*; no saves → *empty*; else *ok*. Latest save's cover as backdrop and
  portrait; facts list (devices online now, conflicts, silent devices, stale games); the next
  action is the primary button (Review conflicts / Check devices / Connect a device / Open
  library). KPI strip below; recent games as an artwork shelf.
- **My Games** — artwork tiles (grid), list view unchanged in structure; export/import in one
  menu.
- **Game detail** — artwork hero (blurred cover backdrop, 160×240 portrait, facts strip),
  Export as secondary, *Delete all saves…* in a separate More menu (confirm unchanged).
- **Version history** — recovery timeline: latest marked "Latest — on your devices", each
  version shows time, device, size and delta; Download + Restore (secondary). Restore dialog
  compares *Restoring vN* → *Becomes vN+1* and states nothing is deleted.
- **Devices** — sync map (solid green = seen in the last 5 min, dashed = idle, amber =
  silent 14+ days), device cards with platform, version and state.
- **Client** — shares all of the above via `app.css`; status hero tone border, status words in
  the text face.

## 6. Verification (this pass)

- `go vet` + `gofmt` clean; `go test ./server/... ./pkg/... ./client/...` pass.
- `script/ui-smoke.sh`: 46 passed, 0 failed (every route 200, strict CSP header, branded 404).
- `script/check-contrast.py`: all token pairings pass.
- axe-core 4.10 (WCAG 2 A/AA) on dashboard, My Games, game detail, version history, devices:
  **0 violations in dark and light** (baseline: 4 issues dark, 6 light). The check ran on the
  default design only; the other five designs' light themes were reviewed visually, not with axe.
- No new dependencies. No inline styles or scripts (CSP tests pass).
