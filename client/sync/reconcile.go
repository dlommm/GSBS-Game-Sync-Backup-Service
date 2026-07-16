package sync

import (
	"context"
	"os"
	"path/filepath"
)

// ReconcileLocalToServer scans each watched path's resolved local files and uploads
// any file that is not yet present on the server (missing from serverState).
// serverState maps "gameID\x00pathKey" -> {hash, updated_at} (see FetchServerState).
// A nil map means the server state is UNKNOWN and reconcile refuses to run —
// pushing without the different-hash guard could overwrite newer server saves
// after a transient fetch failure. An empty (non-nil) map is a legitimate fresh
// account and uploads everything.
// watchPaths must have resolved absolute directories (not templates).
// Skips empty files and files that already match server hash.
// When a slot exists on the server with a different hash: if the LOCAL file is
// definitively newer (mtime beyond the skew window past the server timestamp),
// it is pushed as a compare-and-swap against the observed server hash — this
// recovers saves whose failed push aged out of the outbox (they would
// otherwise never upload until the file changed again). Any other difference
// is skipped; pull/conflict logic owns those.
// Runs serially; intended for startup only. Respects ctx cancellation.
func ReconcileLocalToServer(ctx context.Context, watchPaths []WatchPath, client *Client, serverState map[string]ServerSaveInfo) int {
	if serverState == nil {
		logSyncWarn("reconcile_skipped_no_server_state", "reason", "server hashes unavailable; refusing blind uploads")
		return 0
	}
	uploaded := 0
	seen := make(map[string]bool)
	for _, wp := range watchPaths {
		select {
		case <-ctx.Done():
			return uploaded
		default:
		}
		if wp.Directory == "" {
			continue
		}
		dir := wp.Directory
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				// Only restrict to the top level for named-file rules (the safety
				// case: a game that saves a known file directly in a broad root
				// like $HOME — we must not walk the whole tree). Plain folder
				// rules recurse so nested save files still upload. Broad-root
				// rules only ever reach here as non-recursive named-file rules
				// (UnsafeWatchTarget blocks sync-all/recursive roots upstream).
				if !wp.Recursive && len(wp.IncludePatterns) > 0 && path != dir {
					return filepath.SkipDir
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return filepath.SkipAll
			default:
			}
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			relPath = filepath.ToSlash(relPath)
			if !matchInclude(relPath, wp.IncludePatterns, wp.SyncAll) {
				return nil
			}
			pathKey := pushPathKey(wp.RuleKey, relPath, wp.IncludePatterns, wp.SyncAll)
			slotKey := wp.GameID + "\x00" + pathKey
			if seen[slotKey] {
				return nil
			}
			seen[slotKey] = true

			content, err := os.ReadFile(path) //nolint:gosec // G122: walk stays under configured watch roots; recursion is bounded by wp.Recursive
			if err != nil || len(content) == 0 {
				return nil
			}
			changeHash, err := client.ContentChangeHash(content)
			if err != nil {
				return nil
			}
			if srv, existsOnServer := serverState[slotKey]; existsOnServer {
				if srv.Hash == changeHash {
					logSyncDebug("reconcile_skip_unchanged", "game_id", wp.GameID, "path_key", pathKey, "file", path)
					return nil
				}
				// Different content. If the local file is definitively newer
				// than the server copy, its push was lost (e.g. aged out of
				// the outbox after 7 days offline) — upload it as a CAS
				// against the observed server hash, exactly like resolving a
				// conflict with keep-local. A concurrent server change 409s
				// into the normal conflict flow instead of overwriting.
				if fi, statErr := os.Stat(path); statErr == nil && !srv.UpdatedAt.IsZero() &&
					fi.ModTime().Sub(srv.UpdatedAt) > DefaultSkewTolerance {
					logSyncInfo("reconcile_upload_local_newer", "game_id", wp.GameID, "path_key", pathKey,
						"relative_path", relPath, "local_mtime", fi.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
						"server_updated_at", srv.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
					client.markPushed(wp.GameID, pathKey, srv.Hash)
					if err := client.Push(ctx, wp.GameID, pathKey, path, relPath, content); err != nil {
						logSyncWarn("reconcile_upload_error", "game_id", wp.GameID, "path_key", pathKey, "error", err)
					} else {
						uploaded++
					}
					return nil
				}
				// Server newer or timestamps ambiguous — pull/conflict logic owns it.
				logSyncDebug("reconcile_skip_server_has_different", "game_id", wp.GameID, "path_key", pathKey)
				return nil
			}

			logSyncInfo("reconcile_upload", "game_id", wp.GameID, "path_key", pathKey, "relative_path", relPath, "bytes", len(content), "file", path)
			if err := client.Push(ctx, wp.GameID, pathKey, path, relPath, content); err != nil {
				logSyncWarn("reconcile_upload_error", "game_id", wp.GameID, "path_key", pathKey, "error", err)
			} else {
				uploaded++
			}
			return nil
		})
		if err != nil {
			logSyncWarn("reconcile_walk_error", "dir", dir, "error", err)
		}
	}
	return uploaded
}
