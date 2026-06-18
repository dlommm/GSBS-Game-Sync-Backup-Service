package job

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

// TestE2E_RealBundle_TwoVersions drives the real production consumer
// (PCGWBundleFetch, the indexed/versioned path) against artifacts built from a
// real exported bundle, served over local HTTP, into a real on-disk SQLite DB.
//
// It publishes two full version sets — v1 (first 5 games) then v2 (all 10
// games) — and asserts the server catches up by merging the full bundle each
// time: v1 -> 5 games at merged_version 1, v2 -> 10 games at merged_version 2.
//
// Set GSBS_E2E_BUNDLE to a full bundle JSON (e.g. the manifest repo's
// bundle/gsbs-pcgw-manifest-2.json) to run; the test skips otherwise so CI does
// not depend on a large external file. Run with:
//
//	GSBS_E2E_BUNDLE=/path/to/bundle.json go test ./server/job/ -run TestE2E_RealBundle_TwoVersions -v
func TestE2E_RealBundle_TwoVersions(t *testing.T) {
	srcPath := os.Getenv("GSBS_E2E_BUNDLE")
	if srcPath == "" {
		t.Skip("set GSBS_E2E_BUNDLE to a full bundle JSON to run this end-to-end test")
	}

	rows := streamGameSaveLocations(t, srcPath, 10)
	if len(rows) < 10 {
		t.Fatalf("need at least 10 game_save_locations in %s, got %d", srcPath, len(rows))
	}
	v1rows := rows[:5]
	v2rows := rows[:10]
	t.Logf("loaded %d real save-location rows from %s", len(rows), filepath.Base(srcPath))
	logTitles(t, "v1 full set", v1rows)
	logTitles(t, "v2 full set", v2rows)

	fullV1Gz := gzBundleRaw(t, v1rows)
	fullV2Gz := gzBundleRaw(t, v2rows)

	// Local "manifest repo" served over HTTP. The served index.json and full
	// bundle are swapped between phases (the bundle URL is content-addressed).
	var indexBytes, currentFull []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		etag := fmt.Sprintf("idx-%d", len(indexBytes))
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(indexBytes)
	})
	mux.HandleFunc("/manifest.json.gz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(currentFull) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	baseURL := srv.URL + "/"

	t.Setenv("GSBS_PCGW_BUNDLE_INDEX_URL", srv.URL+"/index.json")

	// Real on-disk SQLite DB (the application's store), like a live server.
	dbPath := filepath.Join(t.TempDir(), "gsbs.db")
	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// ---- Version 1: publish full (5 games), consume, verify ----
	currentFull = fullV1Gz
	idxV1, err := store.AdvanceBundleIndex(store.PCGWBundleIndex{}, sha256Hex(fullV1Gz), len(fullV1Gz), baseURL, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	indexBytes = mustJSON(t, idxV1)

	res, err := PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("v1 fetch: %v", err)
	}
	t.Logf("v1 fetch: indexed=%v merged_version=%d steps=%d", res.Indexed, res.MergedVersion, res.StepsApplied)
	assertMergedVersion(t, st, 1)
	if got := countSaveLocations(t, st); got != 5 {
		t.Fatalf("after v1: save locations = %d, want 5", got)
	}
	logManifest(t, st, "after v1")

	// ---- Version 2: publish a new full (all 10 games), consume, verify ----
	currentFull = fullV2Gz
	idxV2, err := store.AdvanceBundleIndex(idxV1, sha256Hex(fullV2Gz), len(fullV2Gz), baseURL, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	indexBytes = mustJSON(t, idxV2)

	res, err = PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("v2 fetch: %v", err)
	}
	t.Logf("v2 fetch: indexed=%v merged_version=%d steps=%d", res.Indexed, res.MergedVersion, res.StepsApplied)
	assertMergedVersion(t, st, 2)
	if got := countSaveLocations(t, st); got != 10 {
		t.Fatalf("after v2: save locations = %d, want 10", got)
	}
	logManifest(t, st, "after v2")

	// ---- Version 2 again: already current -> no-op (304) ----
	res, err = PCGWBundleFetch(ctx, st, PCGWBundleFetchOptions{})
	if err != nil {
		t.Fatalf("v2 re-fetch: %v", err)
	}
	if res.StepsApplied != 0 {
		t.Fatalf("re-fetch should be a no-op, applied %d steps", res.StepsApplied)
	}
	t.Logf("re-fetch: no-op (not_modified=%v, merged_version=%d) — workflow verified", res.NotModified, res.MergedVersion)
}

// streamGameSaveLocations reads up to n game_save_locations objects from a full
// bundle JSON without loading the whole (potentially huge) file: it decodes
// top-level keys one at a time and stops as soon as it has n rows.
func streamGameSaveLocations(t *testing.T, path string, n int) []json.RawMessage {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	if _, err := dec.Token(); err != nil { // opening '{'
		t.Fatal(err)
	}
	var out []json.RawMessage
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		key, _ := keyTok.(string)
		if key == "game_save_locations" {
			if _, err := dec.Token(); err != nil { // opening '['
				t.Fatal(err)
			}
			for dec.More() && len(out) < n {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					t.Fatal(err)
				}
				out = append(out, raw)
			}
			return out
		}
		// Skip this value (consumes nested objects/arrays wholesale).
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// gzBundleRaw wraps the given raw game_save_locations rows in a minimal
// schema_version 2 bundle and gzips it.
func gzBundleRaw(t *testing.T, rows []json.RawMessage) []byte {
	t.Helper()
	bundle := map[string]any{
		"schema_version":      2,
		"exported_at":         "2026-01-01T00:00:00Z",
		"full_exported_at":    "2026-01-01T00:00:00Z",
		"game_save_locations": rows,
	}
	raw, err := json.Marshal(bundle)
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

func logTitles(t *testing.T, label string, rows []json.RawMessage) {
	t.Helper()
	var titles []string
	for _, r := range rows {
		var row struct {
			GameID    string `json:"game_id"`
			GameTitle string `json:"game_title"`
		}
		_ = json.Unmarshal(r, &row)
		titles = append(titles, fmt.Sprintf("%s (id=%s)", row.GameTitle, row.GameID))
	}
	t.Logf("%s: %v", label, titles)
}

func logManifest(t *testing.T, st store.Store, label string) {
	t.Helper()
	locs, err := st.ListGameSaveLocations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: manifest has %d entries", label, len(locs))
	for _, l := range locs {
		t.Logf("  - %s [%s] %s", l.GameTitle, l.Platform, l.PathTemplate)
	}
}

func assertMergedVersion(t *testing.T, st store.Store, want int) {
	t.Helper()
	v, _ := st.GetAdminSetting(context.Background(), store.AdminSettingPCGWBundleMergedVersion)
	if v != fmt.Sprintf("%d", want) {
		t.Fatalf("merged_version = %q, want %d", v, want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
