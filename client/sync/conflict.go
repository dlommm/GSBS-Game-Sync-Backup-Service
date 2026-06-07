package sync

import (
	"context"
	"encoding/json"
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
		if err := client.Push(ctx, gameID, pathKey, absPath, "", content); err != nil {
			return err
		}
	case ResolveUseServer:
		out, err := client.pullSingle(ctx, gameID, pathKey)
		if err != nil {
			return err
		}
		for _, item := range out.Saves {
			opts := DefaultPullOptions()
			opts.ConflictPolicy = "keep_server"
			if err := client.applyOneSave(item.GameID, item.PathKey, item.UpdatedAt, item.Content, absPath, opts); err != nil {
				return err
			}
		}
	}
	ClearConflict(gameID, pathKey)
	return nil
}
