package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// syncHistoryMax caps the persisted history (newest first).
const syncHistoryMax = 500

// SyncHistoryEntry records one completed sync cycle. Persisted so the local
// Insights page can chart activity and success rate across restarts —
// previously this data evaporated when the cycle ended.
type SyncHistoryEntry struct {
	At          time.Time `json:"at"`
	OK          bool      `json:"ok"`
	Err         string    `json:"err,omitempty"`
	GamesSynced int       `json:"games_synced"`
	SavesSynced int       `json:"saves_synced"`
}

var syncHistoryMu sync.Mutex

// syncHistoryPathOverride lets tests redirect the history file.
var syncHistoryPathOverride string

func syncHistoryPath() string {
	if syncHistoryPathOverride != "" {
		return syncHistoryPathOverride
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "sync_history.json")
}

// AppendSyncHistory records a completed sync cycle (newest first, capped).
// Best-effort: failures only affect the Insights page, never syncing.
func AppendSyncHistory(entry SyncHistoryEntry) {
	syncHistoryMu.Lock()
	defer syncHistoryMu.Unlock()
	entries := loadSyncHistoryLocked()
	entries = append([]SyncHistoryEntry{entry}, entries...)
	if len(entries) > syncHistoryMax {
		entries = entries[:syncHistoryMax]
	}
	data, err := json.MarshalIndent(entries, "", " ")
	if err != nil {
		return
	}
	path := syncHistoryPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// LoadSyncHistory returns the persisted sync cycles, newest first.
func LoadSyncHistory() []SyncHistoryEntry {
	syncHistoryMu.Lock()
	defer syncHistoryMu.Unlock()
	return loadSyncHistoryLocked()
}

func loadSyncHistoryLocked() []SyncHistoryEntry {
	data, err := os.ReadFile(syncHistoryPath())
	if err != nil {
		return nil
	}
	var entries []SyncHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}
