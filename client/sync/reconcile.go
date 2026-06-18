package sync

import (
	"context"
	"os"
	"path/filepath"
)

// ReconcileLocalToServer scans each watched path's resolved local files and uploads
// any file that is not yet present on the server (missing from serverHashes map).
// serverHashes maps "gameID\x00pathKey" -> content change hash (plaintext SHA-256). Pass nil to upload everything.
// watchPaths must have resolved absolute directories (not templates).
// Skips empty files and files that already match server hash.
// When a slot key exists on the server with a different hash, it is skipped — pull/conflict
// logic handles those cases.
// Runs serially; intended for startup only. Respects ctx cancellation.
func ReconcileLocalToServer(ctx context.Context, watchPaths []WatchPath, client *Client, serverHashes map[string]string) int {
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
			if walkErr != nil || d.IsDir() {
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

			content, err := os.ReadFile(path)
			if err != nil || len(content) == 0 {
				return nil
			}
			changeHash, err := client.ContentChangeHash(content)
			if err != nil {
				return nil
			}
			if serverHashes != nil {
				if serverHashes[slotKey] == changeHash {
					logSyncDebug("reconcile_skip_unchanged", "game_id", wp.GameID, "path_key", pathKey, "file", path)
					return nil
				}
				if _, existsOnServer := serverHashes[slotKey]; existsOnServer {
					// Server has this save but with different hash — skip; pull/conflict logic handles this
					logSyncDebug("reconcile_skip_server_has_different", "game_id", wp.GameID, "path_key", pathKey)
					return nil
				}
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
