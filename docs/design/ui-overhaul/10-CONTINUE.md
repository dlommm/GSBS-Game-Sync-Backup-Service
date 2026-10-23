# 10 — v7 "Continue" redesign

Supersedes the look of 09 (v6.1 premium pass); 09 stays as the audit record.
Branch `ui/premium-redesign`. Screenshots: `docs/images/screenshots/` (README gallery).

## Why

v6.1 polished a stock dashboard skeleton (sidebar + KPI tiles + rail of tables) and still read as
generated. Three ground-up directions were prototyped with the same data and compared:
**A · Continue** (the game's own Continue/Load screen as the dashboard), **B · Memory card**
(the console memory-card browser), **C · Ledger** (status as a sentence + a timeline of devices).
Chosen: **A as the backbone, C's device lanes on Devices, B as a Library view.**

## The system

- **One typeface, two voices.** Archivo variable (width 62–125, vendored woff2, OFL —
  `static/fonts/ARCHIVO-OFL.txt`): extra-condensed caps for titles, normal width for reading.
  JetBrains Mono only for IDs/paths/code. DM Sans was removed.
- **Ink on near-black; colour comes from the art.** The accent marks the one action, live state
  and selection. Status colours (success/warning/error) are separate from the accent.
- **The hero is always a dark "game screen"** (`--hero-ground`, `--hero-ink*`), in both themes.
- **Designs are voices**, not recolours: each sets ground, accent, and the title voice
  (`--title-font`, `--title-stretch`, `--title-weight`, `--title-case`, `--title-tracking`).
  Night Ops: wide tracked caps · Phosphor: mono caps · Hearth: mixed case · Arcade: heaviest
  condensed · Foundry: wide square caps · Mint Vault: condensed caps.
- **Layouts:** Standard and Compact (`data-layout="dense"`). Stored `topnav`/`library`/`widgets`
  stay valid and render Standard (`renderLayout`); the Custom/widgets editor was removed.
- **Shell:** one top bar (Continue · Library · Devices · Insights; search, inbox, theme, account
  menu with Settings / Conflicts / Admin / Log out). A conflict chip appears only when one waits.
  ≤720px: a bottom tab bar. URLs are unchanged.
- **Motion:** press scale 0.97; dialogs/menus ease in (ease-out, ≤200ms); hover lifts only on
  fine pointers; nothing animates on HTMX/SSE refresh or the command palette.

## Screens

| Screen | What it is | Data |
|---|---|---|
| Continue (`/dashboard`) | Latest save full-bleed (Steam hero → portrait → monogram; Steam logo or set title), the ledger (games backed up, versions kept, devices online, attention rows), save slots, device check-ins, account activity | `loadProtection`, `dashboardSavesData` (server-rendered first paint; HTMX refresh) |
| Library | Grid (covers), List (table + bulk delete), **Browser** (save blocks + selected game's newest versions) | `/dashboard/partial/game-panel` |
| Game | Artwork header, facts, files; delete lives in a separate menu | — |
| Load a save | Versions as slots; Load dialog shows *Loading vN → Becomes vN+1, vM stays in history* (verified against `RestoreSaveVersion`) | — |
| Devices | **Lanes**: 14 days, one lane per device, one mark per save it sent; silent devices are empty lanes | `Store.RecentSaveEvents` |

## Artwork

`/covers/<id>.jpg` (portrait, also `/covers/<id>`), `/covers/<id>-hero.jpg` and
`/covers/<id>-logo.png` (Steam library hero + logo, same allowlisted CDN, same cache, lock and
negative cache; hero/logo have no PCGW fallback). `app.js` removes any `.cover-img` that fails,
including ones that failed before the deferred script ran.

## Verified

Go tests (server, client, pkg), `script/ui-smoke.sh` (46/46), `script/check-contrast.py`, and
axe-core WCAG 2 A/AA on Continue, Library (grid + browser), Game, Load a save, Devices and
Settings — 0 violations in dark and light (default design).

## Rollout to every page (after 7.0.0)

The grammar above now covers the rest of the server and the client:

- **Page head** on every screen: a caps noun title, and a sub-line that states the finding (what needs attention) rather than describing the interface.
- **Figures** are one strip split by rules (`.figs`; the carried `.stats-row` is restyled to match): label above a condensed number, no tiles.
- **Settings** are rows (`.set-row`): title and explanation left, controls right, destructive actions last. Checkboxes are switches (`.switch`). Admin's long server-settings form uses the same rows.
- **Admin** shares the user shell; its sections are an underline tab strip (`partials/admin_shell.html`).
- **Lists of states** (setup checks, protection, devices) are `.checks` rows with the fix on the right.
- **Auth pages** are a split screen: the hero ground tinted by the design's accent on the left, the form on the page ground right.
- **Appearance previews** are live: each design's token block also applies to `[data-preview="<key>"]`, so a tile is a miniature Continue screen in that design; choosing one applies it to the page before saving.
- Boxes (`.panel`) only hold dense tables and consoles. No blur, no tinted icon chips, no accent on chrome.

Verified: Go tests, `script/ui-smoke.sh`, `script/check-contrast.py`, and axe-core WCAG 2 A/AA with 0 violations on every server page (user, admin, auth) and every client page, in dark and light.
