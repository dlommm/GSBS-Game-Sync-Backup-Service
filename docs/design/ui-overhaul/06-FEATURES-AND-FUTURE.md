# Feature Proposals & Future-Proofing — GSBS UI/UX Overhaul

## 1. Future-proofing & extensibility (§5 of the brief)

The redesign builds four **slot patterns** now so later capabilities land without redesign:

1. **Right-rail slots** (U5 dashboard, U7 game detail): rails are independent panels rendered
   from a registry, not hardcoded columns. Live Sync Pulse (C), Restore Confidence (C), and any
   plugin panel (future) mount here.
2. **Topbar bell slot** (Phase 1 reserves the space + icon): Notification Inbox (D) fills it;
   a future mobile companion reuses the same `/api/inbox` feed.
3. **Sidebar nav registry**: nav items come from one Go slice (icon, label, route, badge-count
   fn, admin-flag). Conflict Center (D) adds a row with a badge; a plugin system would append
   here. The `g`-chord map and command palette read the same registry — one source for all three.
4. **Settings sections as anchored partials**: new settings (cloud backends, plugin config)
   append sections without page redesign; the dirty-tracking save bar already generalizes.

**Anticipated capabilities and what they need now:**

| Future capability | Built now | Deferred |
|---|---|---|
| Cloud storage backends (S3-class for saves, not just backups) | nothing UI-side beyond A7's sectioned settings pattern | backend abstraction; admin section |
| Plugin system | nav/rail/settings registries above | plugin API, sandboxing, catalog |
| Mobile companion app | `/api/inbox` (D) designed app-agnostic; 17-endpoint contract already covers reads | push transport, app itself |
| Public share links (share a save/version read-only) | U8 timeline rows get a stable anchor scheme | token-scoped share endpoints, expiry UX |
| More locales | i18n keys audited during Stage B re-skin (every new string through `t()`) | translations themselves |

**Explicit non-goals now:** multi-server federation, in-browser save editing, cloud-hosted GSBS.

## 2. Verified §6 proposals

Classification verified in Phase 0 (see plan corrections). Effort: S <1 day, M 1–3 days,
L >3 days (single engineer). **Top 3 marked ★** — chosen for user-visible value per effort and
for exercising the new design system's slots early.

### Partially shipped — completing increments (Stage C, v5.1.0)

| # | Proposal | User problem | Proposed UX | Effort | API? | Phase |
|---|---|---|---|---|---|---|
| 1★ | **Live Sync Pulse** | "Is anything happening right now?" requires refreshing | U5 right-rail live stream (device X syncing game Y, bytes, seconds ago) fed by `client-activity`; per-device `--sync` pulse dots on U9/U5 rail; auto-collapse when idle | M | no (payload enrich only) | C |
| 2★ | **Device Health board** | stale/expiring devices are invisible until they fail | U9 upgrades: OS badge, freshness tier (active/idle/stale ≥14d), "hasn't synced in N days" callouts, token-expiry countdown with **proactive client refresh** (FIX-2) so the countdown normally never ends | M (incl. client work) | no (uses `/api/token/refresh`) | C |
| 3 | **Setup Health checklist** | post-wizard, admins don't know what's left for a trustworthy setup | A1 + U10 guided checklist: TLS detected, backup configured + last run ok, first client, first sync, notifications tested, 2FA — each row deep-links; dismissible when all green | M | no | C |
| 4 | **Storage Explorer** | quota pressure with no way to see/fix what eats space | U18: per-game + version-history breakdown, one-click prune (respects retention floor), user-facing retention view; admin keeps overrides (AD-23) | M | small (prune endpoint) | C |
| 5 | **Restore Confidence panel** | backups run invisibly; trust requires proof | user-visible last-backup panel (time/size/destination + integrity last pass) on U5/U10; "Verify now" for admins from the same card | S | no | C |

### Open — full proposals (Stage D, v5.2.0)

| # | Proposal | User problem | Proposed UX | Effort | API? | Phase |
|---|---|---|---|---|---|---|
| 6 | **Play-session markers** | version lists don't answer "which save was after my long Tuesday session?" | session bands between U8 timeline nodes ("2h4m session on Steam Deck ended 22:14"); client gamewatch reports session start/end | M | yes — session report (client→server) + storage | D |
| 7 | **E2E Encryption Center** | crypto negotiation is invisible; "am I actually encrypted?" unanswerable | U17: fleet-readiness hero naming blockers, coverage bar with per-game drill, device capability list, guided migrate explainer | M | small (read aggregation; client passphrase-presence flag) | D |
| 8 | **PCGW mission control redesign** | power-user opaque; admins fear the buttons | A9 Stage-D pass: status → recommended-action hero, actions grouped Update/Repair/Rebuild/Danger, dead-letter & catch-up explained in place | M | no | D |
| 9★ | **Web Conflict Center** | conflicts only resolvable at the machine; multi-device users need the web view | U15 + persistence: 409s recorded (migration 30), queue with guided resolution, per-game policy editor (FIX-4); tray path untouched | L | yes — conflicts table + `GET/POST /api/conflicts` + SSE event | D |
| 10 | **In-app Notification Inbox** | webhooks require infra; events vanish if you miss the toast | U16: topbar bell, unread-first inbox mirroring all notify events, deep links; mark-read state | M | yes — read-state + `/api/inbox` | D |

### Additional proposals (beyond the brief's seeds)

| # | Proposal | User problem | Proposed UX | Effort | API? | Phase |
|---|---|---|---|---|---|---|
| 11 | **Cover art self-service** | non-Steam games show monograms forever | U7 "set artwork": upload (stored under cover cache, size/type-capped) or paste PCGW/Steam URL (proxied through existing allowlist); admin toggle | M | small (upload endpoint) | D+ |
| 12 | **SteamGridDB source (admin key)** | monograms for GOG/Epic-only titles | A7 setting: API key; cover proxy tries SteamGridDB after Steam CDN/PCGW; same cache | S/M | no (proxy extension) | D+ |
| 13 | **Save-game notes** | "which of these 5 restores was the good one?" | one-line note per version on U8 (metadata only — works encrypted) | S | small | D+ |
| 14 | **Digest notifications** | per-event webhooks are noisy | daily/weekly digest option per sink (summary of syncs, conflicts, backups) | M | no | backlog |

**Priority rationale (top 3):** #1 and #2 make the redesigned dashboard/devices pages feel alive
immediately and reuse existing SSE/refresh plumbing (low risk, high perceived value). #9 is the
largest genuine capability gap the product has (multi-device conflict resolution away from the
machine) and unlocks FIX-4; it justifies its API cost. #3–5 follow as fast wins; #6–8/#10 round
out Stage D.
