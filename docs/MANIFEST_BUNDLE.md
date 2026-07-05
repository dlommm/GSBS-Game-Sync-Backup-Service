# Manifest bundle sync

GSBS can populate and update its PCGW mirror from a **pre-built manifest bundle** hosted on public object storage (Cloudflare R2), instead of calling the PCGamingWiki API on every install or cron run.

End users never download the bundle manually — the server fetches it on a schedule (or on first start) and merges it into the existing SQL tables. Clients still call `GET /api/manifest/v2` on your server.

**Official publisher:** [dlommm/VPS-Sync-GSBS](https://github.com/dlommm/VPS-Sync-GSBS) — a self-contained weekly pipeline (PCGW API sync → SQLite snapshot → bundle export → validation → R2 upload) that produces the bundle every GSBS install fetches by default. It reuses this repo's exporter and `index.json` schema as a Go library, so publisher and consumer can never drift.

## Sync sources

| Mode | Setting / env | Scheduled job | Manual fallback |
|------|---------------|---------------|-----------------|
| **S3 bundle** (default for fresh installs) | `pcgw_sync_source=s3` or `GSBS_PCGW_SYNC_SOURCE=s3` | `pcgw_bundle_fetch` on `pcgw_bundle_cron` (default daily 04:00) | Incremental Sync / Auto Catch-Up on **Admin → PCGW** |
| **PCGW API** (default for existing DBs with games) | `pcgw_sync_source=api` | Existing incremental PCGW sync cron | Same admin actions |

The legacy value `github` is accepted and normalized to `s3`. Switch source in **Admin → Settings**; changing source reschedules cron immediately.

**Seeded gate (no override):** an API sync never runs against an empty mirror — fresh installs must seed from the S3 bundle first (switching to API mode seeds automatically; air-gapped hosts import a bundle file via **Admin → Import**). This guarantees a fleet of new installs can never fall back to full API crawls of PCGamingWiki.

**API-mode change detection** uses the MediaWiki `recentchanges` feed (a handful of requests per sync) to find edited, new, and deleted pages, with already-known revisions reused during ingest. Windows older than the wiki's change-history retention fall back to a batched revision sweep (50 pages per request). Upstream deletions cascade locally (tombstoned) behind the same 25% safety valve as bundle imports.

## How updates work

Publishing is **full-bundle-only** with a versioned index. Three layers avoid redundant work:

1. **Versioned index** — the server fetches `index.json` first (tiny, ETag-cached, usually HTTP 304). It carries a monotonic `manifest_version` and the current full bundle's URL/SHA256/size.
2. **One-step catch-up** — when the server's merged version is behind, it downloads the full bundle (SHA256-verified against the index) and merges it. Merging is safe from any prior version; there are no deltas to chain.
3. **Smart merge** — import mode `merge_skip_unchanged` upserts only rows that differ and reconciles deletions against the bundle's complete catalog.

The bundle URL carries a content-addressed cache key (`?h=<sha-prefix>`), so a freshly published bundle can never be masked by a stale CDN cache.

Stored in `admin_settings`: `pcgw_bundle_index_etag`, `pcgw_bundle_merged_version`, `pcgw_bundle_latest_version`, `pcgw_bundle_last_fetched_at`, `pcgw_bundle_last_fetch_error`. If the publisher has no `index.json` (HTTP 404), the server falls back to fetching the bundle URL directly with ETag change detection.

Optional: enable **incremental API fallback** in Settings to run a lightweight PCGW sync after a successful bundle check.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GSBS_PCGW_SYNC_SOURCE` | (from DB) | `s3` or `api` (`github` = legacy alias for `s3`). Overrides admin Settings when set. |
| `GSBS_PCGW_BUNDLE_URL` | Official public URL | Full bundle URL |
| `GSBS_PCGW_BUNDLE_INDEX_URL` | Derived from official URL | `index.json` URL (set when self-hosting a custom bundle) |
| `GSBS_PCGW_BUNDLE_CRON` | `0 4 * * *` | Bundle fetch cron when source is `s3`. Set to `""` to disable. |

Default URLs (Cloudflare R2 behind a custom domain; read-only, no credentials):

```text
https://gsbs.ohhcloud.com/manifest/manifest.json.gz
https://gsbs.ohhcloud.com/manifest/index.json
```

## Bundle format (JSON schema v2)

Gzip-compressed JSON exported by `ExportPCGWManifestBundleWithOpts`:

| Field | Purpose |
|-------|---------|
| `game_save_locations` | v1 manifest projection |
| `games`, `game_data`, sections, metadata | PCGW mirror tables |
| `catalog` | `pcgw_catalog` rows (v2) — complete catalog, used to reconcile deletions |
| `exported_at`, `full_exported_at`, `gsbs_version`, `schema_version` | Metadata |

**Lite** export omits heavy full-page wikitext (`metadata` blobs); the official published bundle is lite. Schema v1 bundles still import.

Import modes: `merge`, `full_replace`, `merge_skip_unchanged`.

## Admin UI

- **Settings** — sync source, bundle cron, URLs, incremental fallback
- **PCGW** — bundle status (merged/latest version, last fetch, errors), **Fetch bundle now**, **Force full bundle**
- **Import / Export** — unchanged; use for air-gapped installs or migration

## Publishing (maintainers)

The bundle is published automatically by [VPS-Sync-GSBS](https://github.com/dlommm/VPS-Sync-GSBS): a weekly cron pulls the latest code for both repos, rebuilds, runs an incremental PCGW API sync, exports the bundle from a consistent SQLite snapshot, validates it, and uploads to R2 with `index.json` written last (atomic cutover). See that repo's README for setup and operations.

For one-off local exports from a server database:

```bash
go run ./cmd/pcgw-bundle-export -db gsbs.db -out . -full -lite -version 4.1.1
```

## Failure handling

| Condition | Behavior |
|-----------|----------|
| Fetch fails (network, 404) | Log error; keep serving existing SQL; show admin warning |
| SHA256 mismatch on download | Import aborted; merged version unchanged; retried next cron |
| Empty DB + first start + bundle fails | No API fallback (seeded gate); bundle retried on next cron |
| Import validation fails | Import aborted; prior data retained |

## See also

- [ARCHITECTURE.md](ARCHITECTURE.md) — data model and job runner
- [DOCKER.md](DOCKER.md) — all server env vars
- [API.md](API.md) — manifest endpoints (unchanged)
