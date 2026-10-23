package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/pkg/discovery"
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

// Games added by hand on "Add a game" live in config.WatchPaths, not in the
// discovery cache. Status listed only discovered games, so a hand-added game
// synced but never appeared under Matched games and the count stayed at 0.
func TestSetupStatusListsHandAddedGames(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))

	cfg := blankConfig()
	cfg.Token = "tok"
	cfg.WatchPaths = []watchPath{
		{GameID: "hand-1", PathKey: "k1", Directory: filepath.Join(tmp, "a")},
		{GameID: "hand-1", PathKey: "k2", Directory: filepath.Join(tmp, "b")}, // second folder, same game
		{GameID: "disc-1", PathKey: "k3", Directory: filepath.Join(tmp, "c")}, // also discovered
	}
	require.NoError(t, saveConfig(cfg))
	require.NoError(t, saveDiscoveryCache(discoveryCache{
		MatchedGameIDs: []string{"disc-1"},
		MatchedGames:   []discovery.MatchedGame{{ManifestGameID: "disc-1", Title: "Discovered Game"}},
	}))
	cacheGameTitle("hand-1", "Hand Added Game")

	rec := httptest.NewRecorder()
	handleSetupStatus(rec, httptest.NewRequest("GET", "/status", nil))
	require.Equal(t, 200, rec.Code)

	var resp setupStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []string{"Discovered Game", "Hand Added Game"}, resp.GameTitles,
		"hand-added games are listed once each, after the discovered ones")
	assert.Equal(t, 2, resp.MatchedGames, "the count includes hand-added games without double-counting")
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

// A sync restart (Add a game, saving settings) cancels the old run, whose
// watcher supervisor then reports "unhealthy". That late report used to
// overwrite the new run's health, leaving Status on "Watcher recovering"
// for good. Only the current run may report.
func TestWatcherHealthIgnoresReplacedRuns(t *testing.T) {
	t.Cleanup(func() { WatcherHealthy.Store(false) })
	oldRun := newWatcherHealthReporter()
	newRun := newWatcherHealthReporter()
	assert.True(t, WatcherHealthy.Load(), "a new run starts healthy")

	oldRun(false) // the replaced run's supervisor shutting down
	assert.True(t, WatcherHealthy.Load(), "a replaced run cannot mark the watcher unhealthy")

	newRun(false)
	assert.False(t, WatcherHealthy.Load(), "the current run still reports real failures")
	newRun(true)
	assert.True(t, WatcherHealthy.Load())
}
