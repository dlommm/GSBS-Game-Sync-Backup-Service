package job

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/store"
)

// mockCatalogServer creates a minimal PCGW Cargo API server returning a fixed set of page IDs.
func mockCatalogServer(pageIDs []int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		tables := q.Get("tables")
		if tables != "Infobox_game" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Build cargo response rows.
		rows := make([]map[string]interface{}, 0, len(pageIDs))
		for _, id := range pageIDs {
			rows = append(rows, map[string]interface{}{
				"_pageName": fmt.Sprintf("Game_%d", id),
				"PageID":    fmt.Sprintf("%d", id),
				"Title":     fmt.Sprintf("Game %d", id),
			})
		}
		resp := map[string]interface{}{"cargoquery": makeCargoRows(rows)}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func makeCargoRows(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, len(rows))
	for i, r := range rows {
		out[i] = map[string]interface{}{"title": r}
	}
	return out
}

func TestRunCatalogScan_ProducesCorrectCounts(t *testing.T) {
	pageIDs := []int64{1001, 1002, 1003}
	srv := mockCatalogServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL
	ctx := context.Background()

	runID, _ := st.StartPCGWSyncRun(ctx, "incremental")
	stats, err := RunCatalogScan(ctx, st, client, runID, nil)
	if err != nil {
		t.Fatalf("catalog scan: %v", err)
	}
	if stats.RemoteTotalIDs != len(pageIDs) {
		t.Errorf("remote total: got %d, want %d", stats.RemoteTotalIDs, len(pageIDs))
	}
	if stats.CatalogHash == "" {
		t.Error("catalog hash should not be empty")
	}
	// All 3 IDs are missing from pcgw_games.
	if stats.MissingLocalIDs != len(pageIDs) {
		t.Errorf("missing local: got %d, want %d", stats.MissingLocalIDs, len(pageIDs))
	}

	// Verify DB state.
	catStats, _ := st.GetPCGWCatalogStats(ctx)
	if catStats.RemoteTotal != len(pageIDs) {
		t.Errorf("catalog remote_total: got %d", catStats.RemoteTotal)
	}
}

func TestRunCatalogScan_HashStability(t *testing.T) {
	pageIDs := []int64{500, 501, 502}
	srv := mockCatalogServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL
	ctx := context.Background()

	runID, _ := st.StartPCGWSyncRun(ctx, "incremental")
	stats1, _ := RunCatalogScan(ctx, st, client, runID, nil)

	runID2, _ := st.StartPCGWSyncRun(ctx, "incremental")
	stats2, _ := RunCatalogScan(ctx, st, client, runID2, nil)

	if stats1.CatalogHash != stats2.CatalogHash {
		t.Errorf("hash not stable: %q vs %q", stats1.CatalogHash, stats2.CatalogHash)
	}
}
