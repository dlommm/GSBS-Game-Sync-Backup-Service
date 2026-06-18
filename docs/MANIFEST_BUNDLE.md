# Manifest bundle sync

GSBS can populate and update its PCGW mirror from a **pre-built manifest bundle** hosted on GitHub (Ludusavi-style), instead of calling the PCGamingWiki API on every install or cron run.

End users never download from GitHub manually — the server fetches the bundle on a schedule (or on first start) and merges it into the existing SQL tables. Clients still call `GET /api/manifest/v2` on your server.

Official bundle repository: [dlommm/gsbs-manifest](https://github.com/dlommm/gsbs-manifest)

## Sync sources

| Mode | Setting / env | Scheduled job | Manual fallback |
|------|---------------|---------------|-----------------|
| **GitHub bundle** (default for fresh installs) | `pcgw_sync_source=github` or `GSBS_PCGW_SYNC_SOURCE=github` | `pcgw_bundle_fetch` on `pcgw_bundle_cron` (default daily 04:00) | Incremental Sync / Auto Catch-Up on **Admin → PCGW** |
| **PCGW API** (default for existing DBs with games) | `pcgw_sync_source=api` | Existing incremental PCGW sync cron | Same admin actions |

Switch source in **Admin → Settings**. Changing source reschedules cron immediately.

## How updates work

Three layers avoid redundant work:

1. **ETag / If-None-Match** — most cron runs get HTTP 304 and exit in seconds.
2. **Smart merge** — import mode `merge_skip_unchanged` upserts only rows that differ (`last_rev_id`, `updated_at`, save rules).
3. **Delta bundle** — seeded servers fetch `manifest.delta.json.gz` instead of the full bundle; full bundle on empty DB or **Force full bundle**. Deltas are **cumulative since the last full bundle** (`full_exported_at`), so missing intermediate deltas is safe when the server still has that full baseline. Before applying a delta, the server fetches `manifest.meta.json` and falls back to the full bundle if a **gap** is detected (stale full baseline or legacy chained mismatch).

Stored in `admin_settings`: `pcgw_bundle_etag`, `pcgw_bundle_delta_etag`, `pcgw_bundle_last_fetched_at`, `pcgw_bundle_last_exported_at`, `pcgw_bundle_full_exported_at`.

Optional: enable **incremental API fallback** in Settings to run a lightweight PCGW sync after a successful bundle check (when bundle was unchanged or import succeeded).

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GSBS_PCGW_SYNC_SOURCE` | (from DB) | `github` or `api`. Overrides admin Settings when set. |
| `GSBS_PCGW_BUNDLE_URL` | Official raw URL | Full bundle URL |
| `GSBS_PCGW_BUNDLE_DELTA_URL` | Official delta raw URL | Delta bundle URL |
| `GSBS_PCGW_BUNDLE_CRON` | `0 4 * * *` | Bundle fetch cron when source is `github`. Set to `""` to disable. |

Default full URL:

```text
https://raw.githubusercontent.com/dlommm/gsbs-manifest/main/manifest.json.gz
```

## Bundle format (JSON schema v2)

Gzip-compressed JSON exported by `ExportPCGWManifestBundleWithOpts`:

| Field | Purpose |
|-------|---------|
| `game_save_locations` | v1 manifest projection |
| `games`, `game_data`, sections, metadata | PCGW mirror tables |
| `catalog` | `pcgw_catalog` rows (v2) |
| `deleted_game_ids` | Delta bundles only — tombstones + row cleanup |
| `exported_at`, `full_exported_at`, `gsbs_version`, `schema_version` | Metadata; `full_exported_at` is the cumulative delta anchor (last full publish) |

**Lite** export omits heavy full-page wikitext (`metadata` blobs). Schema v1 bundles still import.

Import modes: `merge`, `full_replace`, `merge_skip_unchanged`, `delta`.

## Admin UI

- **Settings** — sync source, bundle cron, URLs, incremental fallback
- **PCGW** — bundle status (last fetch, ETag, exported_at), **Fetch bundle now**, **Force full bundle**
- **Import / Export** — unchanged; use for air-gapped installs or migration

## Publishing (maintainers)

The [gsbs-manifest](https://github.com/dlommm/gsbs-manifest) repo does **not** auto-update. After PCGW data changes on your server:

1. Export from **Admin → PCGW → Export** (or `GET /admin/pcgw/export/manifest.json.gz`).
2. Place files in the manifest repo [`bundle/`](https://github.com/dlommm/gsbs-manifest/tree/main/bundle) folder.
3. Copy `manifest.json.gz` and `manifest.meta.json` to the repo root, commit, and push.

See [gsbs-manifest/bundle/README.md](https://github.com/dlommm/gsbs-manifest/blob/main/bundle/README.md) for details. CI validates `manifest.meta.json` SHA256 checksums on push.

### CLI export (optional)

```bash
go run ./cmd/pcgw-bundle-export -db gsbs.db -out . -full -lite -version 3.0.1

# Cumulative delta (since last full — reads full_exported_at from manifest.meta.json)
go run ./cmd/pcgw-bundle-export -db gsbs.db -out . -delta -lite
```

Each export also updates `manifest.releases.json` (release history with type, timestamps, SHA256).

## Failure handling

| Condition | Behavior |
|-----------|----------|
| Fetch fails (network, 404) | Log error; keep serving existing SQL; show admin warning |
| Delta gap (stale full baseline) | Log info; automatically fetch full bundle instead |
| Empty DB + first start + bundle fails | Falls back to incremental API sync (existing first-start path) |
| Import validation fails | Import aborted; prior data retained |

## See also

- [ARCHITECTURE.md](ARCHITECTURE.md) — data model and job runner
- [DOCKER.md](DOCKER.md) — all server env vars
- [API.md](API.md) — manifest endpoints (unchanged)
