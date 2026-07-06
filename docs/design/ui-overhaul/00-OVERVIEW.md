# GSBS UI/UX Overhaul — Master Document Overview

**Baseline:** v4.3.0 (`e671fe3`, 2026-07-05). **Program:** full blueprint + full implementation,
shipped in three releases (v5.0.0 parity redesign → v5.1.0 seed completions → v5.2.0 new-API
features).

## Vision

Make GSBS the most polished self-hosted gaming tool available: Immich's information-dense
credibility, Linear's keyboard-first restraint, the Steam library's artwork warmth — one design
language across the server WebUI, the client's loopback WebUI, and three native trays. Dark-first;
game artwork as the emotional centerpiece; one disciplined teal accent; green/amber/red/cyan
reserved for meaning (02 §1.2).

## Tonal reference

The dashboard mockup (charcoal shell floating on a mint→teal gradient; sidebar pills; game-card
grid with status glyphs and inline `Restore`/`View Versions`; version-history right rail with a
color-coded timeline; rounded search, bell, avatar chrome). Captured as *feel*, not literal
content — its artifacts (duplicated nav item, placeholder titles, copyrighted art) are not
reproduced. **Status: image received and palette locked 2026-07-06** — values sampled into 02
§1.1/§1.2, layout language confirmed in 03 ("per mockup" markers). One deliberate deviation:
dark text on filled teal elements (the mockup's white-on-teal fails WCAG AA — see 02 notice).

## Reading order

| Doc | Contents |
|---|---|
| `01-PARITY-INVENTORY.md` | 200-row acceptance checklist — the contract that nothing is lost |
| `02-DESIGN-SYSTEM.md` | tokens, components, motion, naming (brief §1–2) |
| `03-SCREENS-WEB.md` | per-screen specs, web user + admin + new screens (brief §3) |
| `04-SCREENS-CLIENT.md` | tray, loopback WebUI, first-run, updater (brief §3) |
| `05-FLOWS.md` | nine core flows with failure paths (brief §4) |
| `06-FEATURES-AND-FUTURE.md` | extensibility slots + verified feature proposals (brief §5–6) |
| `07-ARCHITECTURE.md` | stack decisions, budgets, a11y, PWA, i18n, new-API preview (brief §7) |
| `08-ROADMAP.md` | phases, risks, success criteria (brief §8–9) |

## Hard constraints honored throughout

Single binary (`go:embed`), build-time-only Node; strict no-inline/no-external CSP on both
surfaces (test-enforced); the 17-endpoint API + client loopback behavior as the only frozen
contract; Pi-class performance budgets; Go clients (tray + loopback WebUI kept); legal artwork
via the existing hardened cover proxy; every screen specs its E2E-encrypted and read-only states;
light + dark themes with dark as driver.

## Explicit deviations from the brief (with rationale)

1. **Direct cutover, not a parallel-route toggle** for Phase 1 — duplicating ~50 server-rendered
   templates costs more than it insures; insurance = inventory checklist + smoke/CSP gates +
   screenshot review; rollback = git revert (no schema changes). (08)
2. **Corrections from code verification:** Xbox is a launcher-path override, not a discovery
   scanner; conflicts currently have *no* server-side persistence (Conflict Center therefore
   needs a table + endpoints); play sessions never reach the server (markers need a small API);
   "recovery codes low" rides the `login` notify event; preview gating is per-save, not
   per-account; `crypto_v2_ready` is currently invisible in every UI. (01/06/07)
3. **PWA = installability only, no offline shell** — offline is the client's domain. (07 §5)
4. **No telemetry** for §9 measurement — CI gates + release checklist instead. (08)

## Defects found during Phase 0 (tracked as FIX rows in 01)

Client logs CSV dead link; no proactive client token refresh; stale `openapi.json` version;
config-only per-game conflict policies; tray-only update apply; one vestigial dashboard partial.
