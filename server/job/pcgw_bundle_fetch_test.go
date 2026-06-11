package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
	"github.com/stretchr/testify/require"
)

func TestPCGWBundleFetch_NotModified(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	etag := `"abc123"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte("ignored"))
	}))
	t.Cleanup(srv.Close)

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleURL, srv.URL))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleETag, etag))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceGitHub))

	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{ForceFull: true})
	require.NoError(t, err)
	require.True(t, res.NotModified)
}

func TestPCGWBundleFetch_ImportFull(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	data, _, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", store.PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	etag := `"bundle1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleURL, srv.URL))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceGitHub))

	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{ForceFull: true})
	require.NoError(t, err)
	require.False(t, res.NotModified)
	require.NotEmpty(t, res.ETag)
}

func TestPCGWBundleFetch_GapFallbackToFull(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	fullData, fullMeta, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", store.PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	// Seed server with stale full baseline (older anchor than remote cumulative delta).
	staleFull := "2026-06-01T00:00:00Z"
	_, err = st.ImportPCGWManifestBundle(ctx, fullData, "merge")
	require.NoError(t, err)
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleLastExportedAt, staleFull))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleFullExportedAt, staleFull))

	deltaData, deltaMeta, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", store.PCGWBundleExportOpts{
		Lite:               true,
		Since:              fullMeta.FullExportedAt,
		FullExportedAt:     fullMeta.FullExportedAt,
		PreviousExportedAt: fullMeta.FullExportedAt,
	})
	require.NoError(t, err)
	require.NotEmpty(t, deltaMeta.FullExportedAt)

	metaJSON, _ := json.Marshal(deltaMeta)
	fullFetched := false
	deltaFetched := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "manifest.meta.json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(metaJSON)
		case strings.HasSuffix(r.URL.Path, "manifest.delta.json.gz"):
			deltaFetched = true
			_, _ = w.Write(deltaData)
		case strings.HasSuffix(r.URL.Path, "manifest.json.gz"):
			fullFetched = true
			w.Header().Set("ETag", `"full-gap"`)
			_, _ = w.Write(fullData)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	base := srv.URL + "/"
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleURL, base+"manifest.json.gz"))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWBundleDeltaURL, base+"manifest.delta.json.gz"))
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceGitHub))

	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	require.NoError(t, err)
	require.False(t, res.Delta)
	require.True(t, fullFetched)
	require.False(t, deltaFetched)
	require.Equal(t, base+"manifest.json.gz", res.URL)
}
