# Phased Roadmap & Success Criteria — GSBS UI/UX Overhaul

## Phase 1 — Parity redesign (release v5.0.0)

**Honest sizing:** this is a re-skin + IA pass over ~50 working templates on a proven stack —
not a rebuild. The heavy lifting (design system pipeline, CSP, SSE, table toolkit, tests) shipped
in 3.2.0–4.3.0 and is reused wholesale.

**Scope (milestones, each its own commit + gate):**
- **B1 Foundations** — 02 tokens into `input.css`, all component families re-skinned, light theme
  derived, contrast validation script, tray icon re-tint.
- **B2 Shell & IA** — user-page sidebar shell (nav registry), topbar (palette search field, theme,
  bell slot), admin shell aligned, mobile nav; `template_names_test.go` updated.
- **B3 User screens** — U1–U14 per 03 (dashboard game-card layout + right-rail slots).
- **B4 Admin screens** — A1–A10 visual pass.
- **B5 Client surface** — C1–C11 template alignment (CSS mostly propagates).
- **B6 Closeout** — FIX-1 (client logs CSV route), FIX-3 (openapi version), FIX-5 (vestigial
  partial), screenshot regeneration, CHANGELOG, release.

**No new features in Phase 1.** Rows may only be `preserved/enhanced/relocated`.

**Rollout deviation (argued in 00):** direct cutover instead of the brief's parallel-route toggle —
duplicating ~50 server-rendered templates for a toggle costs more than it insures. Insurance
instead: the 200-row inventory as a checklist, extended `ui-smoke.sh`, screenshot review, and
rollback = `git revert` + point release (no schema changes in Phase 1).

**Exit criteria:** all 200 inventory rows checked; `ui-smoke.sh` green (extended, incl. POST
smoke for admin action buttons); all Go tests + lint green; performance budgets met (07 §3);
axe pass on redesigned screens; screenshots regenerated; wizard completes docs-free.

**Risk register:**
| Risk | Mitigation |
|---|---|
| Visual churn breaks muscle memory | IA keeps all routes; nav registry preserves names; relocations listed in CHANGELOG |
| CSS refactor regresses an untested corner | inventory checklist + screenshot diff of every page dark+light |
| Client/server CSS drift mid-migration | B1 lands the tokens for both surfaces at once (shared pipeline) |
| Mockup arrives late | provisional palette ships B1; 02 §4 palette-swap commit is isolated by design |

**Effort:** ~2–3 focused weeks solo.

## Phase 2 — Partial-seed completions (release v5.1.0)

**Scope:** proposals 1–5 (06): Live Sync Pulse, Device Health (+ proactive token refresh, FIX-2),
Setup Health checklist, Storage Explorer (+ prune endpoint), Restore Confidence.
**Exit:** each feature demoed against its 03 spec states; no API contract changes except the
additive prune route; budgets still met.
**Risks:** SSE payload enrichment must stay backward-compatible with old clients (ignore-unknown);
prune needs a dry-run preview to be trusted. **Effort:** ~1–2 weeks.

## Phase 3 — New-API features (release v5.2.0, split if needed)

**Scope:** proposals 6–10 (06): Conflict Center (migration 30), Notification Inbox (31),
Encryption Center, play-session markers, PCGW guided redesign. Order within phase: 9 → 10 → 7 →
8 → 6 (dependency + value order).
**Exit:** new endpoints in `openapi.json` + drift test; migrations tested up+fresh; F3 web path
green end-to-end with two real clients; tray/client conflict paths still green.
**Risks:** conflict persistence must not double-notify (dedupe with existing notify path);
policy-override distribution to clients needs careful design (07 §7) before code. **Effort:**
~2–3 weeks.

## Phase 4+ — Backlog
Proposals 11–14 (artwork self-service, SteamGridDB, version notes, digests), mobile companion,
share links — each rides the slots built in Phase 1 (06 §1).

## Success criteria & measurement (§9)

**Per-phase definition of done:** exit criteria above, plus:
- zero parity regressions (inventory re-walked each release),
- `ui-smoke.sh` green incl. CSP assertions on every route (old + new),
- performance budgets (07 §3) measured on the reference host and recorded in the release notes,
- WCAG spot-audit (axe + keyboard walkthrough of the release's touched flows),
- setup wizard → first sync completable by a new user without documentation (tested each release).

**Qualitative bar:** side-by-side screenshot review against Immich (density/credibility), Linear
(interaction polish), Steam library (artwork warmth) — recorded in the release PR-less commit
description; the maintainer signs off before tagging.

**Measurement instrumentation:** no telemetry is added (self-hosted ethos); measurement =
CI gates + the release checklist above.
