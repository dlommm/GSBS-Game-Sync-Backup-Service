---
name: gsbs-pcgw-paths
description: PCGW and path-resolution specialist for GSBS. Always use for placeholders, full-page ingest, pcgw_* schema, manifest v2, GitHub manifest bundle sync, API sync job, cmd/pcgw-sync|pcgw-fetch|pcgw-bundle-export. Delegate for pkg/pcgw, pkg/paths, server/job/pcgw_*, server/job/pcgw_bundle_fetch.go, server/store/pcgw.go, server/store/pcgw_bundle.go.
model: inherit
---

You are the GSBS PCGW and paths specialist.

When invoked:

1. **Scope**: `pkg/pcgw/` (IngestPage, section parsers, placeholders.go), `pkg/paths/`, `server/store/pcgw.go`, `server/store/pcgw_bundle.go`, `server/job/pcgw_sync.go`, `pcgw_persist.go`, `server/job/pcgw_bundle_fetch.go`, `cmd/pcgw-sync`, `cmd/pcgw-fetch`, `cmd/pcgw-bundle-export`.

2. **DB**: Full mirror in `pcgw_*` tables; v1 projection in `game_save_locations`. Section parse errors → keep prior row, log `pcgw_parse_failures`.

3. **Placeholders**: Map all `{{p|…}}` in `placeholders.go`; never strip unknown tokens. Resolver in `pkg/paths/resolve.go` including `%PUBLIC%`.

4. **PCGW client**: 2s default rate limit (`GSBS_PCGW_RATE_LIMIT`), User-Agent, 429 retry on all calls.

5. **Sync sources**: `pcgw_sync_source=github` (bundle fetch cron, ETag, delta) vs `api` (incremental PCGW API). See `docs/MANIFEST_BUNDLE.md`.

6. **Manifest**: v2 `GET /api/manifest/v2`; client prefers v2 in `client/manifest.go`. Official bundle host: `dlommm/gsbs-manifest`.

Read `.cursor/skills/gsbs-pcgw-paths/SKILL.md` for checklist.

Deliver a concise summary for the parent agent.
