---
name: gsbs-pcgw-paths
description: Works with PCGamingWiki integration and path resolution: pkg/pcgw (API, full-page ingest, section parsers), pkg/paths (resolver, placeholders), pcgw_* DB tables, manifest v1/v2, GitHub manifest bundle sync, cmd/pcgw-sync, pcgw-fetch, pcgw-bundle-export. Use when adding placeholders, parsing PCGW templates, or extending PCGW sync/manifest/bundle publish.
---

# GSBS PCGW & Paths

**To keep context low:** For implementation or multi-file work in pkg/pcgw, pkg/paths, or the PCGW job/cmd tools, invoke the **gsbs-pcgw-paths** subagent at the start of the task.

## Scope

- **pkg/pcgw/** — `IngestPage`, section parsers, `placeholders.go`, Cargo + MediaWiki client (2s default rate limit, User-Agent). Golden fixtures in `testdata/`.
- **pkg/paths/** — `resolve.go`, `steam_vdf.go`; must match `placeholders.go` mappings including `%PUBLIC%`, GOG/Epic/Heroic/Lutris/Bottles/Flatpak.
- **DB**: `pcgw_*` tables (full mirror) + `game_save_locations` (v1 projection). Store methods in `server/store/pcgw.go`.
- **Sync (API mode)**: `server/job/pcgw_persist.go`, `pcgw_catalog.go` — incremental uses `ProbeCatalogGrowth` (1 API call) + `ScanCatalogTail` instead of a full Phase 1 rescan. Full scan on first run, incomplete catalog, `ForceFull`, or periodic interval (`GSBS_PCGW_FULL_CATALOG_DAYS`, default 7). `buildChangedQueue` is deferred and skipped when probe finds no new IDs and rev-check interval has not elapsed. `catalog_scan_mode` on each sync run records how Phase 1 ran.
- **Manifest bundle (GitHub mode)**: `server/job/pcgw_bundle_fetch.go`, `server/store/pcgw_bundle.go` — ETag fetch, full vs delta URL, import modes `merge`, `merge_skip_unchanged`, `delta`. Official bundle repo: `dlommm/gsbs-manifest`. Publish manually via Admin export or `cmd/pcgw-bundle-export`. Docs: `docs/MANIFEST_BUNDLE.md`.
- **Manifest**: v1 `GET /api/manifest` (flat entries); v2 `GET /api/manifest/v2` (rich per-game). Client tries v2 first in `client/manifest.go`.
- **cmd/pcgw-sync**, **cmd/pcgw-fetch**, **cmd/pcgw-bundle-export** — CLI sync/fetch/export tools.
- **WebUI**: `/admin/pcgw` — browse, refresh, API sync actions, bundle fetch status; `/admin/settings` — sync source toggle.

## Env

- API sync: `GSBS_PCGW_RATE_LIMIT` (default `2s`), `GSBS_PCGW_USER_AGENT`, `GSBS_PCGW_STORE_FULL_WIKITEXT` (default true), `GSBS_PCGW_CRON`, `GSBS_PCGW_FULL_CRON`.
- Bundle sync: `GSBS_PCGW_SYNC_SOURCE`, `GSBS_PCGW_BUNDLE_URL`, `GSBS_PCGW_BUNDLE_DELTA_URL`, `GSBS_PCGW_BUNDLE_CRON`.
- Admin settings keys: `pcgw_sync_source`, `pcgw_bundle_*`, `pcgw_bundle_incremental_fallback`.

## Conventions

- Never strip unknown `{{p|…}}` in paths.
- On section parse error: keep prior DB row; log `pcgw_parse_failures`; `parse_status=partial`.
- Project manifest paths only when game_data parse succeeds.
- Bundle import: bump manifest version only when `RowsChanged > 0`.

## Checklist

- [ ] Placeholders in pkg/pcgw match pkg/paths resolver tokens.
- [ ] New section/template: add parser + store upsert + golden fixture.
- [ ] After sync or bundle import with changes: `BumpManifestVersion`, `InvalidateManifestCache`, SSE `manifest-updated`.
- [ ] Bundle schema changes: update export/import in `pcgw_bundle.go`, validate workflow in gsbs-manifest repo.
