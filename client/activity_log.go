package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	gosync "sync"
	"time"
)

// ActivityEntry is one row of the local sync activity feed ("14:03 Elden
// Ring — pushed save.dat"). Only this device's activity is knowable
// client-side; pulls carry no origin-device info, so none is invented.
type ActivityEntry struct {
	At        time.Time `json:"at"`
	GameID    string    `json:"game_id"`
	Title     string    `json:"title,omitempty"`
	PathKey   string    `json:"path_key,omitempty"`
	Direction string    `json:"direction"` // push, pull, queued
	OK        bool      `json:"ok"`
	Detail    string    `json:"detail,omitempty"` // error text when !OK
}

const activityLogCap = 300

var (
	activityMu     gosync.Mutex
	activityLoaded bool
	activityLog    []ActivityEntry
)

func activityLogPath() string {
	return filepath.Join(ClientDataDir(), "activity.json")
}

func loadActivityLocked() {
	if activityLoaded {
		return
	}
	activityLoaded = true
	data, err := os.ReadFile(activityLogPath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &activityLog)
	if len(activityLog) > activityLogCap {
		activityLog = activityLog[len(activityLog)-activityLogCap:]
	}
}

// RecordActivity appends one feed entry (ring-buffered, persisted).
func RecordActivity(e ActivityEntry) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	activityMu.Lock()
	loadActivityLocked()
	activityLog = append(activityLog, e)
	if len(activityLog) > activityLogCap {
		activityLog = activityLog[len(activityLog)-activityLogCap:]
	}
	snapshot := append([]ActivityEntry(nil), activityLog...)
	activityMu.Unlock()

	if data, err := json.Marshal(snapshot); err == nil {
		_ = os.MkdirAll(ClientDataDir(), 0755)
		_ = atomicWriteFile(activityLogPath(), data, 0644)
	}
}

// RecentActivity returns up to n entries, newest first.
func RecentActivity(n int) []ActivityEntry {
	activityMu.Lock()
	defer activityMu.Unlock()
	loadActivityLocked()
	if n <= 0 || n > len(activityLog) {
		n = len(activityLog)
	}
	out := make([]ActivityEntry, 0, n)
	for i := len(activityLog) - 1; i >= len(activityLog)-n; i-- {
		out = append(out, activityLog[i])
	}
	return out
}
