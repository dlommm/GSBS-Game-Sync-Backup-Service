package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The local dashboard must see paused state and the real watch-path count —
// it previously rendered "All synced" while paused and faked "folders
// watched" from the game count.
func TestSetupStatusReportsPausedAndWatchState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))

	SyncPaused.Store(true)
	t.Cleanup(func() { SyncPaused.Store(false) })
	publishWatchBuildState(WatchPathBuildStats{
		UnsafeDetails: []UnsafeSkip{{GameID: "g1", Title: "Some Game", Dir: "/home/user"}},
	}, 7)
	t.Cleanup(func() { publishWatchBuildState(WatchPathBuildStats{}, 0) })

	rec := httptest.NewRecorder()
	handleSetupStatus(rec, httptest.NewRequest("GET", "/status", nil))
	require.Equal(t, 200, rec.Code)

	var resp setupStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Paused, "paused state must be visible to the dashboard")
	assert.Equal(t, 7, resp.WatchedPaths, "watched paths reports the real build count")
	require.Len(t, resp.UnsafeSkips, 1)
	assert.Equal(t, "g1", resp.UnsafeSkips[0].GameID)
}

// saveConfig must be atomic: no temp residue, valid JSON on disk.
func TestSaveConfigAtomic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))

	cfg := blankConfig()
	cfg.ServerURL = "https://example.test"
	require.NoError(t, saveConfig(cfg))

	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs", "config.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var round map[string]any
	require.NoError(t, json.Unmarshal(data, &round))
	assert.Equal(t, "https://example.test", round["server_url"])
	assert.NoFileExists(t, path+".gsbs.tmp", "no temp residue after atomic save")
}
