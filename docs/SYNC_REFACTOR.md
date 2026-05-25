# GSBS Sync Reliability Refactor — Implementation Summary

This document summarizes the manifest normalization, watcher, filesystem storage, and reliability overhaul.

## What changed

### Phase 1 — Manifest normalization (P0)

- New **`pkg/saverule`**: parses PCGW path strings into structured `SaveRule` (`directory`, `include_patterns`, `recursive`, `sync_all`).
- Pipe-separated globs merge into one rule (Witcher 3: `gamesaves/*.png|*.sav` → one directory + two patterns).
- Types, DB (`save_rules_json`), PCGW persist, manifest v2, and client manifest loading updated.
- v1 `path_template` remains for backward compatibility (= first rule directory).

### Phase 2 — Watcher refactor (P0)

- Watcher watches **directories only**; uploads filtered by `include_patterns`.
- `SyncAll` required for directory-only rules without patterns.
- Per-file `path_key` via `PathKeyForFile(rule_key, relative_path)`.
- `X-Relative-Path` sent on push.
- Discovery ticker updates watcher paths dynamically.
- Required diagnostic log lines (`watch path added`, `file ignored`, etc.).

### Phase 3 — Multi-user filesystem storage (P0)

- Optional **`GSBS_SAVE_ROOT`**: `{root}/{user_id}/{game_id}/{relative_path}`.
- `EnsureUserStorage` on user create and first upload.
- **`GSBS_MIGRATE_BLOBS_TO_FS=1`**: one-time export of SQLite BLOBs to files.
- Without `GSBS_SAVE_ROOT`, legacy inline BLOB behavior unchanged.

### Phase 4 — Sync reliability (P1)

- Startup order documented; manifest refresh logs watch path diff.
- Push dedup via content hash (client cache + server `unchanged`).
- `resolveSavePath` supports per-file path keys.

### Phase 5 — Config + diagnostics (P1)

- `WatchPathBuildStats` and zero-watch summary logging.
- Docs: `CLIENT.md`, `EXAMPLE_CONFIG.md`.

### Phase 6 — Security (P1)

- Path traversal validation (`pkg/savepath`, client pull `pathsafe`).
- Quota math subtracts replaced save size.
- Key length limits on all save read routes.

## Files changed (by area)

| Area | Files |
|------|-------|
| **pkg/saverule/** | `normalize.go`, `key.go`, `match.go`, tests |
| **pkg/savepath/** | `safe.go`, tests |
| **pkg/types/** | `types.go`, `pcgw.go` |
| **pkg/pcgw/** | `placeholders.go` |
| **pkg/paths/** | `resolve.go` |
| **server/store/** | `sqlite.go`, `store.go`, `pcgw.go`, `save_storage.go`, tests |
| **server/job/** | `pcgw_persist.go` |
| **server/api/** | `handler.go`, tests |
| **client/** | `manifest.go`, `config.go`, `run_sync.go`, tests |
| **client/sync/** | `watcher.go`, `client.go`, `pathsafe.go`, tests |
| **deploy/docs** | `docker-compose*.yml`, `README.md`, `DOCKER.md`, `ARCHITECTURE.md`, `API.md`, `CLIENT.md`, `EXAMPLE_CONFIG.md` |

## Migration impact

| Component | Action |
|-----------|--------|
| **Server DB** | Auto-migration adds `save_rules_json`, `relative_path`, `storage_path`; nullable BLOB |
| **Manifest ETag** | Bumps after PCGW re-sync or import; clients refetch |
| **Existing clients** | Old clients still use `path_template`; upgrade client for pattern filtering |
| **BLOB → FS** | Set `GSBS_SAVE_ROOT`, run once with `GSBS_MIGRATE_BLOBS_TO_FS=1`, verify, unset flag |
| **path_key** | Legacy keys unchanged; multi-file rules get new per-file keys |

## Config / env changes

| Variable | Default | Purpose |
|----------|---------|---------|
| `GSBS_SAVE_ROOT` | unset (BLOB mode) | Filesystem save root |
| `GSBS_MIGRATE_BLOBS_TO_FS` | unset | One-time BLOB export |
| `auto_watch_mode` | `legacy` if omitted in existing config; `discovered` on first install | Watch scope |

Client `watch_paths` may include `directory`, `include_patterns`, `recursive`, `sync_all`, `rule_key`.

## Manual verification checklist

1. [ ] Witcher 3 (or similar): ≥1 watch dir; edit `.sav`/`.png` uploads; `.log` does not.
2. [ ] Admin push-manifest: watchers rebuild without client restart.
3. [ ] Server restart: client reconnects SSE; sync resumes.
4. [ ] Register users A/B: separate dirs under `GSBS_SAVE_ROOT`; no cross-read.
5. [ ] `../../` in `X-Relative-Path` rejected (413/400).
6. [ ] Upgrade with existing BLOBs: migration completes; pulls work.
7. [ ] `go test ./pkg/... ./server/... ./client/...` green.
8. [ ] Zero watch paths: logs show skip breakdown (discovered/platform/missing dir).

## Technical debt remaining

- Version history (`save_versions`) still BLOB-only when using filesystem primary storage.
- macOS manifest rows skipped on Windows/Linux clients (by design).
- Recursive watches on very large trees may need caps.
- `list.go` display could show rules more clearly.

## Risks remaining

- First multi-file sync after upgrade may create new path_keys (one-time re-sync).
- Incomplete PCGW data (e.g. Witcher `.settings` missing from export) until next PCGW sync.
- Windows fsnotify buffer overflow under heavy load (periodic pull backstop).

## Tests

Automated coverage in:

- `pkg/saverule/normalize_test.go` — Witcher, pipe split, dedupe, malformed
- `pkg/saverule/match_test.go` — pattern filtering
- `client/sync/watcher_test.go` — attachment, patterns, relative path header
- `client/manifest_test.go` — stats, PathKeyForFile resolution
- `client/run_sync_test.go` — watch path diff
- `pkg/savepath/safe_test.go` — traversal prevention
- `server/store/save_storage_test.go` — FS storage, multi-user isolation
- `server/api/push_pull_test.go` — quota replace, key length
