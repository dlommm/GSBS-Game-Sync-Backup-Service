package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	clientsync "github.com/gsbs/gsbs/client/sync"
	clientwebui "github.com/gsbs/gsbs/client/webui"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isolateHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
}

// The local Versions page renders server-side history rows with a Restore
// form per non-current version — the ListVersions client API existed since
// 4.x with no UI at all.
func TestVersionsPageRendersHistory(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves/versions" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"versions":[
				{"version":3,"updated_at":"2026-07-15T10:00:00Z","size_bytes":2048,"change_bytes":512,"client_name":"Desktop"},
				{"version":2,"updated_at":"2026-07-14T10:00:00Z","size_bytes":1536,"change_bytes":1536,"client_name":"Laptop"}
			]}`))
		}
	}))
	defer srv.Close()

	c, err := clientsync.NewClient(srv.URL, "tok", paths.NewResolver(), paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	SetSyncClient(c)
	t.Cleanup(func() { SetSyncClient(nil) })

	rec := httptest.NewRecorder()
	handleVersionsPage(rec, httptest.NewRequest("GET", "/versions?game_id=g1&path_key=pk1", nil))
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "v3")
	assert.Contains(t, body, "Desktop")
	assert.Contains(t, body, "/versions/restore", "non-current versions get a restore form")
	// v3 is current — exactly one restore form (for v2).
	assert.Equal(t, 1, strings.Count(body, `action="/versions/restore"`))
}

func TestVersionsPageNotConnected(t *testing.T) {
	isolateHome(t)
	SetSyncClient(nil)
	rec := httptest.NewRecorder()
	handleVersionsPage(rec, httptest.NewRequest("GET", "/versions?game_id=g1&path_key=pk1", nil))
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "Not connected")
}

// Restore POSTs to the server API and redirects back with a success flag.
func TestVersionsRestoreRedirects(t *testing.T) {
	isolateHome(t)
	restored := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves/versions/restore" && r.Method == http.MethodPost {
			restored = true
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	c, err := clientsync.NewClient(srv.URL, "tok", paths.NewResolver(), paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	SetSyncClient(c)
	t.Cleanup(func() { SetSyncClient(nil) })

	form := strings.NewReader("game_id=g1&path_key=pk1&version=2")
	req := httptest.NewRequest("POST", "/versions/restore", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleVersionsRestore(rec, req)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "restored=1")
	assert.True(t, restored, "server restore endpoint must be called")
}

// Invalid resolve requests are rejected; valid ones redirect immediately
// (the resolution itself runs in the background).
func TestInsightsResolveValidation(t *testing.T) {
	isolateHome(t)
	req := httptest.NewRequest("POST", "/insights/resolve",
		strings.NewReader("game_id=g1&path_key=pk1&choice=nonsense"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleInsightsResolve(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Activity feed: ring-buffer append, newest-first read, persistence across a
// fresh read from disk.
func TestActivityLogRoundTrip(t *testing.T) {
	isolateHome(t)
	activityMu.Lock()
	activityLoaded = true // isolate from any prior test state
	activityLog = nil
	activityMu.Unlock()

	RecordActivity(ActivityEntry{GameID: "g1", Title: "Game One", Direction: "push", OK: true})
	RecordActivity(ActivityEntry{GameID: "g2", Title: "Game Two", Direction: "pull", OK: false, Detail: "boom"})

	recent := RecentActivity(10)
	require.Len(t, recent, 2)
	assert.Equal(t, "g2", recent[0].GameID, "newest first")
	assert.Equal(t, "g1", recent[1].GameID)

	// Persistence is debounced now; flush before forcing a reload from disk.
	FlushActivityNow()
	activityMu.Lock()
	activityLoaded = false
	activityLog = nil
	activityMu.Unlock()
	recent = RecentActivity(10)
	require.Len(t, recent, 2, "persisted entries survive reload")
	assert.Equal(t, "boom", recent[0].Detail)
}

// Test-connection: reachable server, error status, and unreachable host.
func TestTestConnectionHandler(t *testing.T) {
	isolateHome(t)
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer healthy.Close()

	post := func(serverURL string) map[string]interface{} {
		req := httptest.NewRequest("POST", "/setup/test-connection",
			strings.NewReader("server_url="+serverURL))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handleTestConnection(rec, req)
		var out map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	assert.Equal(t, true, post(healthy.URL)["ok"])
	assert.Equal(t, false, post("http://127.0.0.1:1")["ok"], "closed port is unreachable")
	assert.Equal(t, false, post("ftp://example.com")["ok"], "non-http scheme rejected")
	assert.Equal(t, false, post("not a url")["ok"])
}

// The settings page must never render the stored passphrase value.
func TestSettingsPageNeverRendersPassphrase(t *testing.T) {
	isolateHome(t)
	const secret = "super-secret-passphrase-value"
	cfg := blankConfig()
	cfg.EncryptionPassphrase = secret
	data := settingsPageData(cfg)
	require.True(t, data.PassphraseSet)

	rec := httptest.NewRecorder()
	clientwebui.RenderSettingsPage(rec, data)
	require.Equal(t, 200, rec.Code)
	assert.NotContains(t, rec.Body.String(), secret, "passphrase value must never reach HTML")
}
