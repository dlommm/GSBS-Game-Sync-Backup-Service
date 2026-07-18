package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActivityDebouncedPersist(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))

	// Reset package state for a deterministic run.
	activityMu.Lock()
	activityLoaded = true
	activityLog = nil
	activityDirty = false
	if activityTimer != nil {
		activityTimer.Stop()
		activityTimer = nil
	}
	activityMu.Unlock()

	for i := 0; i < 50; i++ {
		RecordActivity(ActivityEntry{GameID: "g", Direction: "pull", OK: true})
	}

	// In-memory feed reflects entries immediately, regardless of disk debounce.
	if got := len(RecentActivity(100)); got != 50 {
		t.Fatalf("in-memory entries = %d, want 50", got)
	}

	// A clean-shutdown flush persists the coalesced log in one write.
	FlushActivityNow()
	data, err := os.ReadFile(activityLogPath())
	if err != nil {
		t.Fatalf("activity log not persisted after flush: %v", err)
	}
	var got []ActivityEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("persisted %d entries, want 50", len(got))
	}
}
