---
name: gsbs-pcgw-paths
description: Works with PCGamingWiki integration and path resolution: pkg/pcgw (API, full-page ingest, section parsers), pkg/paths (resolver, placeholders), pcgw_* DB tables, manifest v1/v2, cmd/pcgw-sync and pcgw-fetch. Use when adding placeholders, parsing PCGW templates, or extending PCGW sync/manifest.
---

# GSBS PCGW & Paths

**To keep context low:** For implementation or multi-file work in pkg/pcgw, pkg/paths, or the PCGW job/cmd tools, invoke the **gsbs-pcgw-paths** subagent at the start of the task.

## Scope

- **pkg/pcgw/** — `IngestPage`, section parsers, `placeholders.go`, Cargo + MediaWiki client (2s default rate limit, User-Agent). Golden fixtures in `testdata/`.
- **pkg/paths/** — `resolve.go`, `steam_vdf.go`; must match `placeholders.go` mappings including `%PUBLIC%`, GOG/Epic/Heroic/Lutris/Bottles/Flatpak.
- **DB**: `pcgw_*` tables (full mirror) + `game_save_locations` (v1 projection). Store methods in `server/store/pcgw.go`.
- **Sync**: `server/job/pcgw_persist.go`, `pcgw_catalog.go` — incremental uses `ProbeCatalogGrowth` (1 API call) + `ScanCatalogTail` instead of a full Phase 1 rescan. Full scan on first run, incomplete catalog, `ForceFull`, or periodic interval (`GSBS_PCGW_FULL_CATALOG_DAYS`, default 7). `buildChangedQueue` is deferred and skipped when probe finds no new IDs and rev-check interval has not elapsed. `catalog_scan_mode` on each sync run records how Phase 1 ran.
- **Manifest**: v1 `GET /api/manifest` (flat entries); v2 `GET /api/manifest/v2` (rich per-game). Client tries v2 first in `client/manifest.go`.
- **cmd/pcgw-sync**, **cmd/pcgw-fetch** — CLI sync/fetch tools.
- **WebUI**: `/admin/pcgw` — browse, refresh, full sync, export.

## Env

- `GSBS_PCGW_RATE_LIMIT` (default `2s`), `GSBS_PCGW_USER_AGENT`, `GSBS_PCGW_STORE_FULL_WIKITEXT` (default true), `GSBS_PCGW_CRON`, `GSBS_PCGW_FULL_CRON`.

## Conventions

- Never strip unknown `{{p|…}}` in paths.
- On section parse error: keep prior DB row; log `pcgw_parse_failures`; `parse_status=partial`.
- Project manifest paths only when game_data parse succeeds.

## Checklist

- [ ] Placeholders in pkg/pcgw match pkg/paths resolver tokens.
- [ ] New section/template: add parser + store upsert + golden fixture.
- [ ] After sync: `BumpManifestVersion`, `InvalidateManifestCache`, SSE `manifest-updated`.
