package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ConflictRecord tracks a detected sync conflict.
type ConflictRecord struct {
	GameID    string    `json:"game_id"`
	PathKey   string    `json:"path_key"`
	FilePath  string    `json:"file_path"`
	DetectedAt time.Time `json:"detected_at"`
}

var conflictMu sync.Mutex

func conflictsPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "conflicts.json")
}

// RecordConflict persists a conflict for tray/UI display.
func RecordConflict(gameID, pathKey, filePath string) {
	conflictMu.Lock()
	defer conflictMu.Unlock()
	var list []ConflictRecord
	if data, err := os.ReadFile(conflictsPath()); err == nil {
		_ = json.Unmarshal(data, &list)
	}
	list = append(list, ConflictRecord{
		GameID: gameID, PathKey: pathKey, FilePath: filePath, DetectedAt: time.Now(),
	})
	if data, err := json.MarshalIndent(list, "", "  "); err == nil {
		_ = os.WriteFile(conflictsPath(), data, 0644)
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
