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

// activityFlushDelay coalesces rapid RecordActivity calls (e.g. a bulk pull
// applying hundreds of saves) into a single disk write instead of one
// fsync-rename per entry. The in-memory log updates immediately, so the UI is
// unaffected; only persistence is deferred by up to this window.
const activityFlushDelay = 2 * time.Second

var (
	activityMu     gosync.Mutex
	activityLoaded bool
	activityLog    []ActivityEntry
	activityDirty  bool
	activityTimer  *time.Timer
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

// RecordActivity appends one feed entry (ring-buffered) and schedules a
// debounced persist. Persistence is coalesced so a burst of events costs one
// disk write, not one per entry.
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
	activityDirty = true
	if activityTimer == nil {
		activityTimer = time.AfterFunc(activityFlushDelay, flushActivity)
	}
	activityMu.Unlock()
}

// flushActivity writes the pending activity log to disk once, then re-arms only
// if more entries arrive. Runs on the AfterFunc goroutine.
func flushActivity() {
	activityMu.Lock()
	if !activityDirty {
		activityTimer = nil
		activityMu.Unlock()
		return
	}
	snapshot := append([]ActivityEntry(nil), activityLog...)
	activityDirty = false
	activityTimer = nil
	activityMu.Unlock()

	if data, err := json.Marshal(snapshot); err == nil {
		_ = os.MkdirAll(ClientDataDir(), 0755)
		_ = atomicWriteFile(activityLogPath(), data, 0644)
	}
}

// FlushActivityNow persists any pending activity immediately (best-effort),
// for use on a clean shutdown so the last few seconds of feed aren't lost.
func FlushActivityNow() {
	activityMu.Lock()
	if activityTimer != nil {
		activityTimer.Stop()
		activityTimer = nil
	}
	activityMu.Unlock()
	flushActivity()
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
