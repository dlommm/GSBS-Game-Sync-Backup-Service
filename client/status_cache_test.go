package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The /status poll must not re-read config.json (and the OS keyring) on every
// tick: loadConfigCached serves the same parsed config until the file changes.
func TestLoadConfigCachedReusesUntilFileChanges(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))

	dir, _ := os.UserConfigDir()
	gsbsDir := filepath.Join(dir, "gsbs")
	if err := os.MkdirAll(gsbsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(gsbsDir, "config.json")
	writeCfg := func(serverURL string) {
		if err := os.WriteFile(path, []byte(`{"server_url":"`+serverURL+`","watch_paths":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCfg("https://one.example")

	first, err := loadConfigCached()
	if err != nil {
		t.Fatalf("loadConfigCached: %v", err)
	}
	if first.ServerURL != "https://one.example" {
		t.Fatalf("ServerURL = %q, want one.example", first.ServerURL)
	}
	second, err := loadConfigCached()
	if err != nil {
		t.Fatalf("loadConfigCached (cached): %v", err)
	}
	if first != second {
		t.Error("unchanged file must serve the cached *config (no reload per tick)")
	}

	// Rewrite the file and force a clearly different mtime (some filesystems
	// have coarse mtime granularity).
	writeCfg("https://two.example")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	third, err := loadConfigCached()
	if err != nil {
		t.Fatalf("loadConfigCached (after change): %v", err)
	}
	if third.ServerURL != "https://two.example" {
		t.Errorf("ServerURL after change = %q, want two.example", third.ServerURL)
	}
}

// Same contract for the discovery cache: unchanged file → cached parse,
// changed file → reload.
func TestLoadDiscoveryCacheCachedInvalidatesOnChange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))

	path := discoveryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"last_scan_at":"2026-07-01T00:00:00Z","matched_game_ids":["g1"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	first := loadDiscoveryCacheCached()
	if len(first.MatchedGameIDs) != 1 || first.MatchedGameIDs[0] != "g1" {
		t.Fatalf("unexpected first cache: %+v", first)
	}

	if err := os.WriteFile(path, []byte(`{"last_scan_at":"2026-07-02T00:00:00Z","matched_game_ids":["g1","g2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	second := loadDiscoveryCacheCached()
	if len(second.MatchedGameIDs) != 2 {
		t.Fatalf("cache did not invalidate on file change: %+v", second)
	}
}

// /api/sync-now must report honestly: ok:false when no sync loop is running
// (previously it always claimed ok:true and the UI toasted a fake success).
func TestSyncNowHandlerHonesty(t *testing.T) {
	syncMu.Lock()
	oldCh := syncNowCh
	syncNowCh = nil
	syncMu.Unlock()
	t.Cleanup(func() {
		syncMu.Lock()
		syncNowCh = oldCh
		syncMu.Unlock()
	})

	rec := httptest.NewRecorder()
	handleSyncNow(rec, httptest.NewRequest("POST", "/api/sync-now", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "not_running") {
		t.Fatalf("no-loop response = %s, want ok:false + not_running", body)
	}

	ch := make(chan struct{}, 1)
	syncMu.Lock()
	syncNowCh = ch
	syncMu.Unlock()

	rec = httptest.NewRecorder()
	handleSyncNow(rec, httptest.NewRequest("POST", "/api/sync-now", nil))
	if body := rec.Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("running-loop response = %s, want ok:true", body)
	}
	select {
	case <-ch:
	default:
		t.Error("sync loop was not signaled")
	}

	// A full channel still counts as signaled — a sync-now is already queued.
	ch <- struct{}{}
	if !triggerSyncNow() {
		t.Error("full channel should still report true (already pending)")
	}

	rec = httptest.NewRecorder()
	handleSyncNow(rec, httptest.NewRequest("GET", "/api/sync-now", nil))
	if rec.Code != 405 {
		t.Errorf("GET should be 405, got %d", rec.Code)
	}
}

func trayStateSubscriberCount() int {
	globalTrayState.mu.Lock()
	defer globalTrayState.mu.Unlock()
	return len(globalTrayState.subscribers)
}

// The /events SSE stream must answer immediately with stream headers, emit a
// coalesced state-changed event after a tray-state notification, and
// unsubscribe cleanly when the client goes away.
func TestClientEventsSSE(t *testing.T) {
	before := trayStateSubscriberCount()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handleClientEvents(rec, req)
		close(done)
	}()

	// Wait until the handler has subscribed.
	deadline := time.Now().Add(2 * time.Second)
	for trayStateSubscriberCount() != before+1 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("handler never subscribed to tray state")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A burst of notifications must coalesce into (at least) one event.
	notifyTrayState()
	notifyTrayState()
	time.Sleep(900 * time.Millisecond) // > 500ms coalesce window

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit on context cancellation")
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: state-changed") {
		t.Errorf("body missing state-changed event: %q", body)
	}
	if got := trayStateSubscriberCount(); got != before {
		t.Errorf("subscriber leak: %d before, %d after", before, got)
	}
}
