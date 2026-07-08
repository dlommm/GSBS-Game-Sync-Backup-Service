package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gsbs/gsbs/pkg/discovery"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupManifestTestDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetManifestPathForTest(filepath.Join(dir, "manifest.json"))
	t.Cleanup(func() { SetManifestPathForTest("") })
}

func TestFetchManifestFull_V2PaginatesAllPages(t *testing.T) {
	setupManifestTestDir(t)
	const pageSize = manifestV2PageSize
	const total = pageSize + 3
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/manifest/v2" {
			http.NotFound(w, r)
			return
		}
		calls++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = pageSize
		}
		var games []types.ManifestV2Game
		for i := offset; i < total && i < offset+limit; i++ {
			id := strconv.Itoa(i)
			games = append(games, types.ManifestV2Game{
				GameID: id,
				Title:  "Game " + id,
				SaveLocations: []types.ManifestV2Location{{
					Platform:      manifestPlatformParam(),
					PathTemplates: []string{"/tmp/" + id},
				}},
			})
		}
		_ = json.NewEncoder(w).Encode(types.ManifestV2Response{
			Version:    1,
			ETag:       "full-etag",
			Games:      games,
			GamesTotal: total,
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, total, len(res.Games))
	assert.Equal(t, total, res.GamesTotal)
	assert.Len(t, res.Entries, total)

	f := LoadManifestFile()
	assert.Equal(t, total, len(f.Games))
	assert.Equal(t, total, f.GamesTotal)
	assert.True(t, f.ManifestComplete)
}

func TestFetchManifestFull_V2RecoversTruncatedCache(t *testing.T) {
	setupManifestTestDir(t)
	const total = manifestV2PageSize + 1
	require.NoError(t, SaveManifestFile(manifestFile{
		Source:     "v2",
		GamesTotal: total,
		Games:      make([]types.ManifestV2Game, manifestV2PageSize),
		ETag:       "old",
	}))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Empty(t, r.Header.Get("If-None-Match"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var games []types.ManifestV2Game
		for i := offset; i < total && i < offset+limit; i++ {
			id := strconv.Itoa(i)
			games = append(games, types.ManifestV2Game{
				GameID: id,
				Title:  "Game " + id,
				SaveLocations: []types.ManifestV2Location{{
					Platform:      manifestPlatformParam(),
					PathTemplates: []string{"/tmp/" + id},
				}},
			})
		}
		_ = json.NewEncoder(w).Encode(types.ManifestV2Response{
			Games: games, GamesTotal: total, ETag: "new",
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.Equal(t, total, len(res.Games))
	assert.GreaterOrEqual(t, calls, 2)
}

func TestFetchManifestFull_V2Success(t *testing.T) {
	setupManifestTestDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/manifest/v2" {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, manifestPlatformParam(), r.URL.Query().Get("platform"))
		_ = json.NewEncoder(w).Encode(types.ManifestV2Response{
			Version: 2,
			ETag:    "abc123",
			Games: []types.ManifestV2Game{{
				GameID: "42",
				Title:  "Test Game",
				SaveLocations: []types.ManifestV2Location{{
					Platform:      "linux",
					PathTemplates: []string{"/tmp/saves"},
				}},
			}},
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	require.Equal(t, "v2", res.Source)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "42", res.Entries[0].GameID)
	assert.Equal(t, "Test Game", res.Entries[0].GameTitle)
}

func TestFetchManifestFull_V2EmptyIsAuthoritative(t *testing.T) {
	setupManifestTestDir(t)
	require.NoError(t, SaveManifestFile(manifestFile{
		Entries: []types.GameSaveLocation{{GameID: "old", Platform: "linux", PathTemplate: "/old"}},
		ETag:    "etag-prev",
	}))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/api/manifest/v2" {
			_ = json.NewEncoder(w).Encode(types.ManifestV2Response{Version: 2, ETag: "etag-new", Games: nil})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.Equal(t, "v2", res.Source)
	assert.Empty(t, res.Entries)
	assert.Equal(t, 1, calls)
}

func TestFetchManifestFull_V2EmptyDeltaKeepsCache(t *testing.T) {
	setupManifestTestDir(t)
	require.NoError(t, SaveManifestFile(manifestFile{
		Entries:       []types.GameSaveLocation{{GameID: "cached", Platform: "linux", PathTemplate: "/cached"}},
		ETag:          "etag-prev",
		LastFetchedAt: "2026-01-01T00:00:00Z",
	}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/manifest/v2" {
			assert.NotEmpty(t, r.URL.Query().Get("since"))
			_ = json.NewEncoder(w).Encode(types.ManifestV2Response{Version: 2, ETag: "etag-prev", Games: nil})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "2026-01-01T00:00:00Z", "both", false)
	require.NoError(t, err)
	assert.Equal(t, "v2", res.Source)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "cached", res.Entries[0].GameID)
}

func TestFetchManifestFull_V2ErrorFallsBackV1(t *testing.T) {
	setupManifestTestDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/manifest/v2" {
			http.Error(w, "fail", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(manifestResponse{
			Entries: []types.GameSaveLocation{{GameID: "9", Platform: "linux", PathTemplate: "/b"}},
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.Equal(t, "v1", res.Source)
	assert.Len(t, res.Entries, 1)
}

func TestFetchManifestFull_IfNoneMatch304(t *testing.T) {
	setupManifestTestDir(t)

	require.NoError(t, SaveManifestFile(manifestFile{
		Entries:          []types.GameSaveLocation{{GameID: "cached", Platform: "linux", PathTemplate: "/c"}},
		Games:            []types.ManifestV2Game{{GameID: "cached", Title: "Cached"}},
		GamesTotal:       1,
		ManifestComplete: true,
		Source:           "v2",
		ETag:             "etag-old",
	}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "etag-old", r.Header.Get("If-None-Match"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.True(t, res.NotModified)
	assert.Len(t, res.Entries, 1)
	assert.Equal(t, "cached", res.Entries[0].GameID)
}

func TestFetchManifestFull_304IgnoredWhenCacheIncomplete(t *testing.T) {
	setupManifestTestDir(t)
	require.NoError(t, SaveManifestFile(manifestFile{
		Entries: []types.GameSaveLocation{{GameID: "partial", Platform: "linux", PathTemplate: "/p"}},
		Games:   make([]types.ManifestV2Game, 581),
		Source:  "v2",
		ETag:    "etag-old",
	}))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			assert.Empty(t, r.Header.Get("If-None-Match"))
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		const total = manifestV2PageSize + 2
		var games []types.ManifestV2Game
		for i := offset; i < total && i < offset+limit; i++ {
			id := strconv.Itoa(i)
			games = append(games, types.ManifestV2Game{
				GameID: id,
				Title:  "Game " + id,
				SaveLocations: []types.ManifestV2Location{{
					Platform:      manifestPlatformParam(),
					PathTemplates: []string{"/tmp/" + id},
				}},
			})
		}
		_ = json.NewEncoder(w).Encode(types.ManifestV2Response{
			Games: games, GamesTotal: total, ETag: "etag-new",
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", false)
	require.NoError(t, err)
	assert.False(t, res.NotModified)
	assert.Equal(t, manifestV2PageSize+2, len(res.Games))
	assert.True(t, res.Complete)
	f := LoadManifestFile()
	assert.True(t, f.ManifestComplete)
}

func TestFetchManifestFull_ForceFullBypasses304(t *testing.T) {
	setupManifestTestDir(t)
	require.NoError(t, SaveManifestFile(manifestFile{
		Entries:          []types.GameSaveLocation{{GameID: "cached", Platform: "linux", PathTemplate: "/c"}},
		Games:            []types.ManifestV2Game{{GameID: "cached", Title: "Cached"}},
		GamesTotal:       1,
		ManifestComplete: true,
		Source:           "v2",
		ETag:             "etag-old",
	}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		_ = json.NewEncoder(w).Encode(types.ManifestV2Response{
			Games: []types.ManifestV2Game{{
				GameID: "fresh",
				Title:  "Fresh",
				SaveLocations: []types.ManifestV2Location{{
					Platform:      manifestPlatformParam(),
					PathTemplates: []string{"/fresh"},
				}},
			}},
			GamesTotal: 1,
			ETag:       "etag-new",
		})
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both", true)
	require.NoError(t, err)
	assert.False(t, res.NotModified)
	assert.Equal(t, "fresh", res.Entries[0].GameID)
}

func TestApplyManifestDeletions(t *testing.T) {
	entries := []types.GameSaveLocation{
		{GameID: "1", Platform: "linux", PathTemplate: "/a"},
		{GameID: "2", Platform: "linux", PathTemplate: "/b"},
	}
	out := ApplyManifestDeletions(entries, []string{"1"})
	require.Len(t, out, 1)
	assert.Equal(t, "2", out[0].GameID)
}

func TestMergeManifestFetch_DeletedGames(t *testing.T) {
	cached := manifestFile{
		Entries: []types.GameSaveLocation{
			{GameID: "1", Platform: "linux", PathTemplate: "/a"},
			{GameID: "2", Platform: "linux", PathTemplate: "/b"},
		},
		Games: []types.ManifestV2Game{{GameID: "1"}, {GameID: "2"}},
	}
	res := manifestFetchResult{
		DeletedIDs: []string{"1"},
		ETag:       "new-etag",
		DeltaOnly:  true,
	}
	merged := mergeManifestFetch(cached, res)
	assert.Len(t, merged.Entries, 1)
	assert.Equal(t, "2", merged.Entries[0].GameID)
	assert.Len(t, merged.Games, 1)
	assert.Equal(t, "new-etag", merged.ETag)
}

func TestManifestToWatchPaths_DiscoveredModeEmpty(t *testing.T) {
	entries := []types.GameSaveLocation{
		{GameID: "1", Platform: "linux", PathTemplate: "%USERPROFILE%/saves"},
	}
	resolver := pathsResolverForTest(t)
	out, stats := ManifestToWatchPaths(entries, resolver, paths.CurrentOS(), false, map[string]bool{}, "discovered", nil)
	assert.Empty(t, out)
	assert.Equal(t, 1, stats.SkippedDiscovered)
}

func TestManifestToWatchPaths_PlatformSkip(t *testing.T) {
	entries := []types.GameSaveLocation{
		{GameID: "1", Platform: "windows", PathTemplate: "C:/saves"},
	}
	resolver := pathsResolverForTest(t)
	current := string(paths.CurrentOS())
	if current == "windows" {
		t.Skip("need non-windows platform for this test")
	}
	out, stats := ManifestToWatchPaths(entries, resolver, paths.CurrentOS(), false, map[string]bool{"1": true}, "discovered", nil)
	assert.Empty(t, out)
	assert.Equal(t, 1, stats.SkippedPlatform)
}

func TestResolveSavePath_PathKeyForFile(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "slot.sav")
	require.NoError(t, os.WriteFile(saveFile, []byte("data"), 0644))

	resolver := paths.NewResolver()
	gameID := "42"
	ruleKey := saverule.RuleKey(gameID, types.SaveRule{
		Directory:       dir,
		IncludePatterns: []string{"*.sav"},
	})
	pathKey := saverule.PathKeyForFile(ruleKey, "slot.sav")

	abs := resolveSavePath(gameID, pathKey, nil, []watchPath{{
		GameID:          gameID,
		PathKey:         ruleKey,
		RuleKey:         ruleKey,
		Directory:       dir,
		IncludePatterns: []string{"*.sav"},
	}}, resolver, paths.CurrentOS(), paths.PullContext{}, nil)
	assert.Equal(t, saveFile, abs)
}

func pathsResolverForTest(t *testing.T) *paths.Resolver {
	t.Helper()
	return paths.NewResolver()
}

func TestBuildInstallRootsByGame_MergesSources(t *testing.T) {
	setupManifestTestDir(t)
	require.NoError(t, SaveManifestFile(manifestFile{
		Games: []types.ManifestV2Game{{
			GameID:             "42",
			CommonInstallPaths: []string{"/wiki/hint"},
		}},
	}))
	cache := discoveryCache{
		InstalledGames: []discovery.InstalledGame{{
			GameID:      "12345",
			Launcher:    "steam",
			InstallPath: "/steam/common/game",
		}},
		IDMap: map[string]string{"steam:12345": "42"},
	}
	cfg := &config{GameInstallPaths: map[string]string{"42": "/custom/override"}}
	got := BuildInstallRootsByGame(cfg, cache)
	require.NotNil(t, got)
	assert.Equal(t, []string{"/custom/override", "/wiki/hint", "/steam/common/game"}, got["42"])
}

// Regression (v5.2.3): a v2 delta merged over a v1-era cache (entries only,
// no game set) must not relabel the cache "v2" — that made
// manifestCacheComplete judge it by game bookkeeping it never had, forcing a
// full manifest re-download on every startup, forever.
func TestMergeManifestFetch_DeltaOverV1CacheKeepsSource(t *testing.T) {
	cached := manifestFile{
		Source: "v1",
		Entries: []types.GameSaveLocation{
			{GameID: "1", Platform: "macos", PathTemplate: "/a"},
			{GameID: "2", Platform: "macos", PathTemplate: "/b"},
		},
	}
	res := manifestFetchResult{DeltaOnly: true, ETag: "e2"} // "nothing changed" delta
	merged := mergeManifestFetch(cached, res)
	assert.Equal(t, "v1", merged.Source, "delta over v1 cache must not claim v2")
	assert.Len(t, merged.Entries, 2, "catalog preserved")
	assert.True(t, manifestCacheComplete(merged), "merged cache stays usable")

	// A delta carrying a PARTIAL game set (no verifiable total) must also
	// stay v1 — the partial set can't be trusted as a v2 catalog.
	res2 := manifestFetchResult{DeltaOnly: true, Games: []types.ManifestV2Game{{GameID: "1"}}}
	merged2 := mergeManifestFetch(cached, res2)
	assert.Equal(t, "v1", merged2.Source)
	assert.True(t, manifestCacheComplete(merged2))

	// Full fetches always stamp v2 (unchanged behavior).
	full := manifestFetchResult{Entries: cached.Entries, Games: []types.ManifestV2Game{{GameID: "1"}, {GameID: "2"}}, GamesTotal: 2, Complete: true}
	merged3 := mergeManifestFetch(cached, full)
	assert.Equal(t, "v2", merged3.Source)
	assert.True(t, merged3.ManifestComplete)
}

// Regression (v5.2.3): caches already poisoned in the wild — Source "v2",
// entries present, but zero games and zero total — must be judged complete
// by their entries so existing installs heal without a re-download.
func TestManifestCacheComplete_MislabeledV2Cache(t *testing.T) {
	poisoned := manifestFile{
		Source:  "v2",
		Entries: []types.GameSaveLocation{{GameID: "1", Platform: "macos", PathTemplate: "/a"}},
	}
	assert.True(t, manifestCacheComplete(poisoned))

	// Real v2 semantics unchanged: a partial game set is still incomplete...
	partial := manifestFile{
		Source:     "v2",
		GamesTotal: 5,
		Games:      []types.ManifestV2Game{{GameID: "1"}},
		Entries:    []types.GameSaveLocation{{GameID: "1", Platform: "macos", PathTemplate: "/a"}},
	}
	assert.False(t, manifestCacheComplete(partial))
	// ...and an empty cache is incomplete.
	assert.False(t, manifestCacheComplete(manifestFile{Source: "v2"}))
}
