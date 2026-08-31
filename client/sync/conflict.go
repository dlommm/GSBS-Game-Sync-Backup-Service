package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ConflictRecord tracks a detected sync conflict.
type ConflictRecord struct {
	GameID          string    `json:"game_id"`
	PathKey         string    `json:"path_key"`
	FilePath        string    `json:"file_path"`
	DetectedAt      time.Time `json:"detected_at"`
	LocalHash       string    `json:"local_hash,omitempty"`
	ServerHash      string    `json:"server_hash,omitempty"`
	LocalMtime      string    `json:"local_mtime,omitempty"`
	ServerUpdatedAt string    `json:"server_updated_at,omitempty"`
	PolicyApplied   string    `json:"policy_applied,omitempty"`
}

var conflictMu sync.Mutex
var conflictsPathOverride string

func conflictsPath() string {
	if conflictsPathOverride != "" {
		return conflictsPathOverride
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "conflicts.json")
}

// SetConflictsPathForTest overrides conflicts file location (tests only).
func SetConflictsPathForTest(path string) {
	conflictMu.Lock()
	conflictsPathOverride = path
	conflictMu.Unlock()
}

// RecordConflict persists a conflict for tray/UI display.
func RecordConflict(rec ConflictRecord) {
	conflictMu.Lock()
	defer conflictMu.Unlock()
	var list []ConflictRecord
	if data, err := os.ReadFile(conflictsPath()); err == nil {
		_ = json.Unmarshal(data, &list)
	}
	rec.DetectedAt = time.Now()
	// Replace existing for same slot
	var filtered []ConflictRecord
	for _, c := range list {
		if c.GameID == rec.GameID && c.PathKey == rec.PathKey {
			continue
		}
		filtered = append(filtered, c)
	}
	filtered = append(filtered, rec)
	if data, err := json.MarshalIndent(filtered, "", "  "); err == nil {
		_ = atomicWriteFile(conflictsPath(), data, 0644)
	}
}

// ListConflicts returns pending conflict records.
func ListConflicts() []ConflictRecord {
	conflictMu.Lock()
	defer conflictMu.Unlock()
	data, err := os.ReadFile(conflictsPath())
	if err != nil {
		return nil
	}
	var list []ConflictRecord
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// ClearConflict removes one conflict by game_id and path_key.
func ClearConflict(gameID, pathKey string) {
	conflictMu.Lock()
	defer conflictMu.Unlock()
	var list []ConflictRecord
	data, err := os.ReadFile(conflictsPath())
	if err != nil {
		return
	}
	if json.Unmarshal(data, &list) != nil {
		return
	}
	var out []ConflictRecord
	for _, c := range list {
		if c.GameID == gameID && c.PathKey == pathKey {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		_ = os.Remove(conflictsPath())
		return
	}
	if data, err := json.MarshalIndent(out, "", "  "); err == nil {
		_ = atomicWriteFile(conflictsPath(), data, 0644)
	}
}

// ClearConflicts removes all conflict records.
func ClearConflicts() {
	conflictMu.Lock()
	defer conflictMu.Unlock()
	_ = os.Remove(conflictsPath())
}

// ConflictCount returns the number of pending conflicts.
func ConflictCount() int {
	return len(ListConflicts())
}

// ResolveChoice is keep_local or use_server.
type ResolveChoice string

const (
	ResolveKeepLocal ResolveChoice = "keep_local"
	ResolveUseServer ResolveChoice = "use_server"
)

// ResolveConflict applies the user's choice for a pending conflict.
func ResolveConflict(ctx context.Context, client *Client, gameID, pathKey string, choice ResolveChoice, absPath string) error {
	switch choice {
	case ResolveKeepLocal:
		content, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}
		// Push sends X-GSBS-If-Hash from the last-pushed cache, which for a
		// cross-machine conflict never matches the other machine's server hash
		// — the push would 409 forever. Seed the cache with the server hash the
		// user just reviewed so the resolve is a compare-and-swap against
		// exactly that version; if the server moved again meanwhile, a fresh
		// 409/conflict is the correct outcome.
		for _, rec := range ListConflicts() {
			if rec.GameID == gameID && rec.PathKey == pathKey && rec.ServerHash != "" {
				client.markPushed(gameID, pathKey, rec.ServerHash)
				break
			}
		}
		if err := client.Push(ctx, gameID, pathKey, absPath, "", content); err != nil {
			return err
		}
	case ResolveUseServer:
		out, err := client.pullSingle(ctx, gameID, pathKey)
		if err != nil {
			return err
		}
		if len(out.Saves) == 0 {
			return fmt.Errorf("resolve use_server: server has no save for game=%s path_key=%s", gameID, pathKey)
		}
		for _, item := range out.Saves {
			opts := DefaultPullOptions()
			opts.ConflictPolicy = "keep_server"
			// keep_server still refuses to overwrite a local file that is
			// definitively newer than the server copy — correct for automatic
			// sync, wrong here. Without ForceApply a user who kept playing
			// before picking "use server" saw the conflict disappear while the
			// local file was never touched.
			opts.ForceApply = true
			// The local copy is being discarded on the user's instruction, so
			// keep a backup of it rather than dropping it silently.
			opts.BackupBeforeOverwrite = true
			// Pass the encryption flag and server hash through: applyOneSave
			// hardcodes encrypted=false, which wrote ciphertext to disk as the
			// save for E2E-encrypted accounts and skipped pull verification.
			applied, err := client.applyOneSaveEncrypted(item.GameID, item.PathKey, item.UpdatedAt, item.Content, absPath, opts, item.Encrypted, item.ContentHash)
			if err != nil {
				return err
			}
			if !applied {
				// Never clear a conflict the resolve did not actually settle.
				return fmt.Errorf("resolve use_server: server version was not applied for game=%s path_key=%s (path %s)", gameID, pathKey, absPath)
			}
		}
	}
	ClearConflict(gameID, pathKey)
	return nil
}
