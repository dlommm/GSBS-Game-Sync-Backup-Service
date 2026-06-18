package job

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceS3))

	// ForceFull bypasses the stored ETag, so a 304 only happens when the server
	// itself reports not-modified for a fresh conditional request.
	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
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
	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceS3))

	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{ForceFull: true})
	require.NoError(t, err)
	require.False(t, res.NotModified)
	require.NotEmpty(t, res.ETag)
}
