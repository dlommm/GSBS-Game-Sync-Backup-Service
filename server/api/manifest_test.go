package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newManifestHandler(t *testing.T) (*Handler, store.Store) {
	t.Helper()
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	h := NewHandler(st, auth.NewService(st), true, nil, nil, nil, nil, nil, nil, 0, false, "", "test")
	return h, st
}

func seedManifestV1(t *testing.T, st store.Store) {
	t.Helper()
	ctx := context.Background()
	past := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	require.NoError(t, st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "1", PCGWPageID: 1, GameTitle: "Alpha", Platform: "windows", PathTemplate: "C:\\Alpha", IsConfig: false, Source: "pcgw", UpdatedAt: past},
		{GameID: "1", PCGWPageID: 1, GameTitle: "Alpha", Platform: "windows", PathTemplate: "C:\\AlphaCfg", IsConfig: true, Source: "pcgw", UpdatedAt: past},
		{GameID: "2", PCGWPageID: 2, GameTitle: "Beta", Platform: "linux", PathTemplate: "/beta", IsConfig: false, Source: "pcgw", UpdatedAt: recent},
	}))
}

func TestHandler_ManifestV1_FullAndSince(t *testing.T) {
	h, st := newManifestHandler(t)
	seedManifestV1(t, st)
	ctx := context.Background()

	req := httptest.NewRequest(http.MethodGet, "/api/manifest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var full struct {
		Entries []types.GameSaveLocation `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&full))
	assert.Len(t, full.Entries, 3)

	// include=saves filters out config rows
	req = httptest.NewRequest(http.MethodGet, "/api/manifest?include=saves", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&full))
	assert.Len(t, full.Entries, 2)
	for _, e := range full.Entries {
		assert.False(t, e.IsConfig)
	}

	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/api/manifest?since="+since, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&full))
	assert.Len(t, full.Entries, 1)
	assert.Equal(t, "2", full.Entries[0].GameID)

	meta, err := st.GetPCGWManifestMeta(ctx)
	require.NoError(t, err)
	if meta != nil && meta.ManifestETag != "" {
		assert.Equal(t, meta.ManifestETag, rec.Header().Get("ETag"))
	}
}

func TestHandler_ManifestV2_ETag304(t *testing.T) {
	h, st := newManifestHandler(t)
	ctx := context.Background()

	_, err := st.BumpManifestVersion(ctx, "sha256:manifest-etag")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/manifest/v2", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "sha256:manifest-etag", rec.Header().Get("ETag"))

	req = httptest.NewRequest(http.MethodGet, "/api/manifest/v2", nil)
	req.Header.Set("If-None-Match", "sha256:manifest-etag")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHandler_ManifestV2_SinceAndPlatform(t *testing.T) {
	h, st := newManifestHandler(t)
	ctx := context.Background()

	require.NoError(t, st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: 100, PageName: "WinGame", Title: "Win Game",
		PlatformsPresent: []string{"windows"}, ParseStatus: "ok",
	}))
	require.NoError(t, st.UpsertPCGWGameData(ctx, &types.PCGWGameData{
		PageID: 100, PlatformKey: "windows", PlatformRawLabel: "Windows",
		SaveLocations: []types.PCGWPathEntry{{PathTemplates: []string{"C:\\save"}}},
	}))

	since := time.Now().UTC().Format(time.RFC3339)
	time.Sleep(1100 * time.Millisecond)

	require.NoError(t, st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: 200, PageName: "LinGame", Title: "Lin Game",
		PlatformsPresent: []string{"linux"}, ParseStatus: "ok",
	}))
	require.NoError(t, st.UpsertPCGWGameData(ctx, &types.PCGWGameData{
		PageID: 200, PlatformKey: "linux", PlatformRawLabel: "Linux",
		SaveLocations: []types.PCGWPathEntry{{PathTemplates: []string{"/save"}}},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/manifest/v2?since="+since, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp types.ManifestV2Response
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Games, 1)
	assert.Equal(t, "200", resp.Games[0].GameID)

	req = httptest.NewRequest(http.MethodGet, "/api/manifest/v2?platform=linux", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Games, 1)
	assert.Equal(t, "200", resp.Games[0].GameID)
	assert.NotEmpty(t, resp.Games[0].SaveLocations)
}
