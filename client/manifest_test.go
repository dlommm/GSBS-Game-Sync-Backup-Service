package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
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

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both")
	require.NoError(t, err)
	require.Equal(t, "v2", res.Source)
	require.Len(t, res.Entries, 1)
	assert.Equal(t, "42", res.Entries[0].GameID)
	assert.Equal(t, "Test Game", res.Entries[0].GameTitle)
}

func TestFetchManifestFull_V2EmptyFallsBackV1(t *testing.T) {
	setupManifestTestDir(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/api/manifest/v2":
			_ = json.NewEncoder(w).Encode(types.ManifestV2Response{Version: 2, Games: nil})
		case "/api/manifest":
			_ = json.NewEncoder(w).Encode(manifestResponse{
				Entries: []types.GameSaveLocation{{GameID: "1", Platform: "linux", PathTemplate: "/a"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both")
	require.NoError(t, err)
	assert.Equal(t, "v1", res.Source)
	assert.Len(t, res.Entries, 1)
	assert.GreaterOrEqual(t, calls, 2)
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

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both")
	require.NoError(t, err)
	assert.Equal(t, "v1", res.Source)
	assert.Len(t, res.Entries, 1)
}

func TestFetchManifestFull_IfNoneMatch304(t *testing.T) {
	setupManifestTestDir(t)

	require.NoError(t, SaveManifestFile(manifestFile{
		Entries: []types.GameSaveLocation{{GameID: "cached", Platform: "linux", PathTemplate: "/c"}},
		ETag:    "etag-old",
	}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "etag-old", r.Header.Get("If-None-Match"))
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	res, err := FetchManifestFull(context.Background(), srv.URL, "", "", "both")
	require.NoError(t, err)
	assert.True(t, res.NotModified)
	assert.Len(t, res.Entries, 1)
	assert.Equal(t, "cached", res.Entries[0].GameID)
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
	out := ManifestToWatchPaths(entries, resolver, paths.CurrentOS(), false, map[string]bool{}, "discovered")
	assert.Empty(t, out)
}

func pathsResolverForTest(t *testing.T) *paths.Resolver {
	t.Helper()
	return paths.NewResolver()
}
