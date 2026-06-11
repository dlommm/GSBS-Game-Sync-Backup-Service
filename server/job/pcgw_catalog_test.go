package job

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
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
	stats, err := RunCatalogScan(ctx, st, client, runID, "test", nil)
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
	stats1, _ := RunCatalogScan(ctx, st, client, runID, "test", nil)

	runID2, _ := st.StartPCGWSyncRun(ctx, "incremental")
	stats2, _ := RunCatalogScan(ctx, st, client, runID2, "test", nil)

	if stats1.CatalogHash != stats2.CatalogHash {
		t.Errorf("hash not stable: %q vs %q", stats1.CatalogHash, stats2.CatalogHash)
	}
}

// mockOffsetAwareCatalogServer creates a Cargo API server that returns only pages
// at offset >= startResponseAt. This lets tests simulate ProbeCatalogGrowth accurately.
func mockOffsetAwareCatalogServer(allPageIDs []int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("tables") != "Infobox_game" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		offset := 0
		if v := q.Get("offset"); v != "" {
			fmt.Sscanf(v, "%d", &offset)
		}
		limit := 500
		if v := q.Get("limit"); v != "" {
			fmt.Sscanf(v, "%d", &limit)
		}
		start := offset
		if start > len(allPageIDs) {
			start = len(allPageIDs)
		}
		end := start + limit
		if end > len(allPageIDs) {
			end = len(allPageIDs)
		}
		slice := allPageIDs[start:end]
		rows := make([]map[string]interface{}, 0, len(slice))
		for _, id := range slice {
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

func TestProbeCatalogGrowth_Empty(t *testing.T) {
	// Server has exactly 3 pages; probing at offset 3 returns nothing.
	pageIDs := []int64{1, 2, 3}
	srv := mockOffsetAwareCatalogServer(pageIDs)
	defer srv.Close()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL
	ctx := context.Background()

	grew, err := ProbeCatalogGrowth(ctx, client, len(pageIDs))
	if err != nil {
		t.Fatalf("ProbeCatalogGrowth: %v", err)
	}
	if grew {
		t.Error("expected grew=false when no new pages beyond local count")
	}
}

func TestProbeCatalogGrowth_HasPages(t *testing.T) {
	// Server has 5 pages; probing at offset 3 returns the extra 2.
	pageIDs := []int64{1, 2, 3, 4, 5}
	srv := mockOffsetAwareCatalogServer(pageIDs)
	defer srv.Close()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL
	ctx := context.Background()

	grew, err := ProbeCatalogGrowth(ctx, client, 3)
	if err != nil {
		t.Fatalf("ProbeCatalogGrowth: %v", err)
	}
	if !grew {
		t.Error("expected grew=true when new pages exist beyond offset 3")
	}
}

func TestScanCatalogTail_NoGrowth(t *testing.T) {
	// Server has exactly 3 pages; tail scan from offset 3 finds nothing → cached stats returned.
	pageIDs := []int64{10, 20, 30}
	srv := mockOffsetAwareCatalogServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Pre-populate catalog with the 3 known IDs.
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 10, Title: "G10", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 20, Title: "G20", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 30, Title: "G30", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	runID, _ := st.StartPCGWSyncRun(ctx, "incremental")
	cachedTotal := 3
	stats, err := ScanCatalogTail(ctx, st, client, runID, cachedTotal, cachedTotal, nil)
	if err != nil {
		t.Fatalf("ScanCatalogTail: %v", err)
	}
	// No growth → RemoteTotalIDs == cachedTotal.
	if stats.RemoteTotalIDs != cachedTotal {
		t.Errorf("RemoteTotalIDs: got %d, want %d", stats.RemoteTotalIDs, cachedTotal)
	}
}

func TestScanCatalogTail_Growth(t *testing.T) {
	// Server has 5 pages; we already have 3 in catalog — tail finds 2 new ones.
	pageIDs := []int64{10, 20, 30, 40, 50}
	srv := mockOffsetAwareCatalogServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Pre-populate catalog with the first 3.
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 10, Title: "G10", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 20, Title: "G20", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 30, Title: "G30", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	runID, _ := st.StartPCGWSyncRun(ctx, "incremental")
	stats, err := ScanCatalogTail(ctx, st, client, runID, 3, 3, nil)
	if err != nil {
		t.Fatalf("ScanCatalogTail: %v", err)
	}
	// Should have picked up 2 new pages.
	if stats.RemoteTotalIDs != 5 {
		t.Errorf("RemoteTotalIDs: got %d, want 5", stats.RemoteTotalIDs)
	}
	if stats.CatalogHash == "" {
		t.Error("expected non-empty catalog hash after tail growth")
	}

	// Confirm DB state: 5 entries now in catalog.
	catStats, _ := st.GetPCGWCatalogStats(ctx)
	if catStats.RemoteTotal != 5 {
		t.Errorf("catalog remote_total: got %d, want 5", catStats.RemoteTotal)
	}
}

func TestMaxPagesPerRunWithSource(t *testing.T) {
	t.Run("default source", func(t *testing.T) {
		t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "")
		got, source := MaxPagesPerRunWithSource()
		if got != DefaultMaxPagesPerRun {
			t.Fatalf("budget=%d, want %d", got, DefaultMaxPagesPerRun)
		}
		if source != MaxPagesPerRunSourceDefault {
			t.Fatalf("source=%q, want %q", source, MaxPagesPerRunSourceDefault)
		}
	})

	t.Run("env source", func(t *testing.T) {
		t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "750")
		got, source := MaxPagesPerRunWithSource()
		if got != 750 {
			t.Fatalf("budget=%d, want 750", got)
		}
		if source != MaxPagesPerRunSourceEnv {
			t.Fatalf("source=%q, want %q", source, MaxPagesPerRunSourceEnv)
		}
	})

	t.Run("invalid env falls back", func(t *testing.T) {
		t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "oops")
		got, source := MaxPagesPerRunWithSource()
		if got != DefaultMaxPagesPerRun {
			t.Fatalf("budget=%d, want %d", got, DefaultMaxPagesPerRun)
		}
		if source != MaxPagesPerRunSourceInvalidEnv {
			t.Fatalf("source=%q, want %q", source, MaxPagesPerRunSourceInvalidEnv)
		}
	})
}
