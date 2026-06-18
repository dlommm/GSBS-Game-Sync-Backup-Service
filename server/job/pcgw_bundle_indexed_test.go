package job

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

// gzBundle builds a minimal gzipped manifest bundle containing the given
// game_save_location game IDs (each a distinct windows save path).
func gzBundle(t *testing.T, gameIDs ...string) []byte {
	t.Helper()
	locs := make([]map[string]any, 0, len(gameIDs))
	for _, gid := range gameIDs {
		locs = append(locs, map[string]any{
			"game_id": gid, "platform": "windows",
			"path_template": "%APPDATA%/" + gid, "is_config": false,
			"updated_at": "2026-01-01T00:00:00Z", "source": "test",
		})
	}
	raw, err := json.Marshal(map[string]any{
		"schema_version":      2,
		"exported_at":         "2026-01-01T00:00:00Z",
		"full_exported_at":    "2026-01-01T00:00:00Z",
		"game_save_locations": locs,
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func countSaveLocations(t *testing.T, st store.Store) int {
	t.Helper()
	locs, err := st.ListGameSaveLocations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(locs)
}

func TestIndexedBundleFetch_CatchUp(t *testing.T) {
	ctx := context.Background()
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	fullV1 := gzBundle(t, "g1", "g2")       // version 1 baseline
	fullV2 := gzBundle(t, "g1", "g2", "g3") // version 2 (adds g3)
	var index, currentFull []byte           // swapped per phase

	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		etag := sha256Hex(index) // content-based, like a CDN/object store
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(index)
	})
	mux.HandleFunc("/full.json.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(currentFull) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("GSBS_PCGW_BUNDLE_INDEX_URL", srv.URL+"/index.json")

	mkIndex := func(idx store.PCGWBundleIndex) []byte {
		b, _ := json.Marshal(idx)
		return b
	}

	// Phase 1: fresh server, index at v1. Expect full import -> merged v1.
	currentFull = fullV1
	index = mkIndex(store.PCGWBundleIndex{
		ManifestVersion: 1,
		Full:            store.PCGWBundleIndexEntry{Version: 1, URL: srv.URL + "/full.json.gz", SHA256: sha256Hex(fullV1)},
	})
	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("phase1: %v", err)
	}
	if !res.Indexed || res.MergedVersion != 1 || res.StepsApplied != 1 {
		t.Fatalf("phase1 result = %+v", res)
	}
	if n := countSaveLocations(t, st); n != 2 {
		t.Fatalf("phase1 save locations = %d, want 2", n)
	}

	// Phase 2: index advances to v2 with a new full (adds g3). Server merges it.
	currentFull = fullV2
	index = mkIndex(store.PCGWBundleIndex{
		ManifestVersion: 2,
		Full:            store.PCGWBundleIndexEntry{Version: 2, URL: srv.URL + "/full.json.gz", SHA256: sha256Hex(fullV2)},
	})
	res, err = PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("phase2: %v", err)
	}
	if res.MergedVersion != 2 || res.StepsApplied != 1 {
		t.Fatalf("phase2 result = %+v", res)
	}
	if n := countSaveLocations(t, st); n != 3 {
		t.Fatalf("phase2 save locations = %d, want 3", n)
	}

	// Phase 3: no change. Index unchanged -> 304 -> no-op, merged stays v2.
	res, err = PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("phase3: %v", err)
	}
	if !res.NotModified || res.StepsApplied != 0 {
		t.Fatalf("phase3 expected 304 no-op, got %+v", res)
	}
}

// TestIndexedBundleFetch_FullCatchUp proves the full-bundle catch-up workflow:
// the index advances across several full publishes, each overwriting the bundle.
// A server that was behind by multiple versions jumps straight to the latest
// full without losing rows, and a brand-new server gets the latest full directly.
func TestIndexedBundleFetch_FullCatchUp(t *testing.T) {
	ctx := context.Background()
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	full1 := gzBundle(t, "g1", "g2")             // v1
	full2 := gzBundle(t, "g1", "g2", "g3")       // v2
	full3 := gzBundle(t, "g1", "g2", "g3", "g4") // v3

	var index, currentFull []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		etag := sha256Hex(index) // content-based, like a CDN/object store
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(index)
	})
	mux.HandleFunc("/manifest.json.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(currentFull) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base := srv.URL + "/"
	t.Setenv("GSBS_PCGW_BUNDLE_INDEX_URL", srv.URL+"/index.json")

	// Build the version line with the real production helper.
	idx1, _ := store.AdvanceBundleIndex(store.PCGWBundleIndex{}, sha256Hex(full1), len(full1), base, "t1")
	idx2, _ := store.AdvanceBundleIndex(idx1, sha256Hex(full2), len(full2), base, "t2")
	idx3, _ := store.AdvanceBundleIndex(idx2, sha256Hex(full3), len(full3), base, "t3")

	apply := func(idx store.PCGWBundleIndex, full []byte) {
		index, _ = json.Marshal(idx)
		currentFull = full
		if _, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{}); err != nil {
			t.Fatalf("fetch v%d: %v", idx.ManifestVersion, err)
		}
	}

	apply(idx1, full1)
	apply(idx2, full2)
	apply(idx3, full3)
	if n := countSaveLocations(t, st); n != 4 {
		t.Fatalf("after v3: %d rows, want 4", n)
	}
	v, _ := st.GetAdminSetting(ctx, store.AdminSettingPCGWBundleMergedVersion)
	if v != "3" {
		t.Fatalf("after catch-up: merged_version=%q, want 3", v)
	}

	// A brand-new server against the latest index jumps straight to full v3.
	st2, _ := store.NewSQLite(":memory:")
	defer st2.Close()
	index, _ = json.Marshal(idx3)
	currentFull = full3
	res, err := PCGWBundleFetch(ctx, st2, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("fresh server fetch: %v", err)
	}
	if res.MergedVersion != 3 || res.StepsApplied != 1 {
		t.Fatalf("fresh server should take full v3 only, got merged=%d steps=%d", res.MergedVersion, res.StepsApplied)
	}
	if n := countSaveLocations(t, st2); n != 4 {
		t.Fatalf("fresh server: %d rows, want 4", n)
	}
}

func TestIndexedBundleFetch_SHA256Mismatch(t *testing.T) {
	ctx := context.Background()
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	full := gzBundle(t, "g1")
	mux := http.NewServeMux()
	idx, _ := json.Marshal(store.PCGWBundleIndex{
		ManifestVersion: 1,
		Full:            store.PCGWBundleIndexEntry{Version: 1, URL: "", SHA256: "deadbeef"},
	})
	var indexBytes []byte
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(indexBytes) })
	mux.HandleFunc("/full.json.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(full) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Point the full URL at the test server with a deliberately wrong sha256.
	idxObj := store.PCGWBundleIndex{
		ManifestVersion: 1,
		Full:            store.PCGWBundleIndexEntry{Version: 1, URL: srv.URL + "/full.json.gz", SHA256: "deadbeef"},
	}
	indexBytes, _ = json.Marshal(idxObj)
	_ = idx

	t.Setenv("GSBS_PCGW_BUNDLE_INDEX_URL", srv.URL+"/index.json")
	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if res.MergedVersion != 0 {
		t.Fatalf("merged version must stay 0 on mismatch, got %d", res.MergedVersion)
	}
	if n := countSaveLocations(t, st); n != 0 {
		t.Fatalf("no rows should be imported on mismatch, got %d", n)
	}
}
