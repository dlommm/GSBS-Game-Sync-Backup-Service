package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSyncHistoryRoundTrip(t *testing.T) {
	syncHistoryPathOverride = filepath.Join(t.TempDir(), "sync_history.json")
	defer func() { syncHistoryPathOverride = "" }()

	if got := LoadSyncHistory(); len(got) != 0 {
		t.Fatalf("expected empty history, got %d", len(got))
	}
	AppendSyncHistory(SyncHistoryEntry{At: time.Now().Add(-time.Minute), OK: true, GamesSynced: 2, SavesSynced: 5})
	AppendSyncHistory(SyncHistoryEntry{At: time.Now(), OK: false, Err: "network down"})

	got := LoadSyncHistory()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	// Newest first.
	if got[0].OK || got[0].Err != "network down" {
		t.Fatalf("newest entry wrong: %+v", got[0])
	}
	if !got[1].OK || got[1].SavesSynced != 5 {
		t.Fatalf("older entry wrong: %+v", got[1])
	}
}

func TestSyncHistoryCap(t *testing.T) {
	syncHistoryPathOverride = filepath.Join(t.TempDir(), "sync_history.json")
	defer func() { syncHistoryPathOverride = "" }()
	for i := 0; i < syncHistoryMax+25; i++ {
		AppendSyncHistory(SyncHistoryEntry{At: time.Now(), OK: true})
	}
	if got := LoadSyncHistory(); len(got) != syncHistoryMax {
		t.Fatalf("expected cap %d, got %d", syncHistoryMax, len(got))
	}
}
