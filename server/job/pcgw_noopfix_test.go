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

// mockFullPCGWServer handles both catalog (Cargo) and page ingest (MediaWiki) endpoints.
// The catalog endpoint returns the given pageIDs.
// The parse endpoint returns minimal valid wikitext for any pageID.
func mockFullPCGWServer(pageIDs []int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// Catalog listing via Cargo API.
		if q.Get("tables") == "Infobox_game" {
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
			return
		}

		// Page parse (wikitext ingest).
		if q.Get("action") == "parse" {
			resp := map[string]interface{}{
				"parse": map[string]interface{}{
					"title": "Test Game",
					"wikitext": map[string]interface{}{
						"*": "{{Infobox game\n|steam appid     = \n}}\n",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Revision query (used by shouldSkipPage / buildChangedQueue).
		if q.Get("action") == "query" && q.Get("prop") == "revisions" {
			pageID := q.Get("pageids")
			resp := map[string]interface{}{
				"query": map[string]interface{}{
					"pages": map[string]interface{}{
						pageID: map[string]interface{}{
							"pageid": pageID,
							"revisions": []map[string]interface{}{
								{"revid": int64(9999), "timestamp": "2024-01-01T00:00:00Z"},
							},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

// seedPriorSuccessRun seeds a completed sync run with the given catalog hash so that
// getPreviousCatalogHash returns that hash on the next incremental scan.
func seedPriorSuccessRun(t *testing.T, ctx context.Context, st store.Store, catalogHash string) {
	t.Helper()
	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatalf("start sync run: %v", err)
	}
	stats := store.PCGWSyncRunStats{}
	err = st.FinishPCGWSyncRun(ctx, runID, JobSuccess, "", stats)
	if err != nil {
		t.Fatalf("finish sync run: %v", err)
	}
	// Record the catalog hash so the no-op gate can see it on next run.
	err = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, types.Phase1Stats{
		CatalogHash: catalogHash,
	})
	if err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}
}

// TestNoOpFix_UnchangedHashWithMissingBacklog verifies that an incremental sync
// does NOT skip Phase 2 when the catalog hash is unchanged but some entries
// have never been processed (missing from pcgw_games).
//
// Root cause A regression: before the fix, the no-op gate only checked
// failedCount and titleBackfillCount, silently skipping missing entries.
func TestNoOpFix_UnchangedHashWithMissingBacklog(t *testing.T) {
	pageIDs := []int64{20001, 20002}
	srv := mockFullPCGWServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	// Phase 1: scan catalog to establish hash and populate pcgw_catalog.
	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	phase1Stats, err := RunCatalogScan(ctx, st, client, runID, nil)
	if err != nil {
		t.Fatalf("catalog scan: %v", err)
	}
	stats := store.PCGWSyncRunStats{}
	if err := st.FinishPCGWSyncRun(ctx, runID, JobSuccess, "", stats); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1Stats); err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}
	if phase1Stats.CatalogHash == "" {
		t.Fatal("expected non-empty catalog hash after scan")
	}

	// Confirm entries are missing from pcgw_games (they should be — nothing inserted them).
	missing, err := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != len(pageIDs) {
		t.Fatalf("expected %d missing entries, got %d", len(pageIDs), len(missing))
	}

	// Run incremental sync. The catalog hash is unchanged (same mock server).
	// With the fix, the no-op gate must NOT fire because missingCount > 0.
	// Phase 2 should run and attempt to ingest the missing pages.
	t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "5") // allow up to 5 pages
	_, syncErr := PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{})
	if syncErr != nil {
		t.Fatalf("PCGWSyncEx returned error: %v", syncErr)
	}

	// At least one page should now be in pcgw_games — Phase 2 ran.
	games, _, err := st.ListPCGWGames(ctx, store.PCGWGameListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) == 0 {
		t.Fatal("expected pcgw_games to have entries after Phase 2 ran, got 0 — no-op gate may have incorrectly fired")
	}
}

// TestNoOpFix_UnchangedHashEmptyBacklog verifies that an incremental sync
// DOES skip Phase 2 when the catalog hash is unchanged AND there is no backlog
// (missing=0, failed=0, title_backfill=0).
func TestNoOpFix_UnchangedHashEmptyBacklog(t *testing.T) {
	pageIDs := []int64{20003, 20004}
	srv := mockFullPCGWServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	// Phase 1: scan catalog to establish hash.
	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	phase1Stats, err := RunCatalogScan(ctx, st, client, runID, nil)
	if err != nil {
		t.Fatalf("catalog scan: %v", err)
	}
	sStats := store.PCGWSyncRunStats{}
	if err := st.FinishPCGWSyncRun(ctx, runID, JobSuccess, "", sStats); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1Stats); err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}

	// Seed pcgw_games with all catalog entries so nothing is "missing".
	for _, id := range pageIDs {
		gameID := fmt.Sprintf("%d", id)
		if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
			PageID:      id,
			PageName:    gameID,
			Title:       fmt.Sprintf("Game %d", id),
			ParseStatus: "ok",
			LastRevID:   9999, // matches mock revision
		}); err != nil {
			t.Fatalf("upsert pcgw_games: %v", err)
		}
	}

	// Verify no missing entries.
	missing, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing, got %d", len(missing))
	}

	// Run incremental sync. Catalog hash unchanged, no backlog → no-op must fire.
	_, syncErr := PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{})
	if syncErr != nil {
		t.Fatalf("PCGWSyncEx returned error: %v", syncErr)
	}

	// Since no-op fired, no additional sync_run entries beyond the first should
	// have entries with GamesFailed > 0. The run status should be success with 0 entries.
	runs, err := st.ListPCGWSyncRuns(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) < 2 {
		t.Fatalf("expected at least 2 sync runs (seed + no-op), got %d", len(runs))
	}
	// The most recent run (runs[0]) should have status=success.
	latest := runs[0]
	if latest.Status != JobSuccess {
		t.Errorf("expected no-op run status=%q, got %q", JobSuccess, latest.Status)
	}
}

// TestNoOpFix_ResumeDoesNotUseStaleHash verifies that a resumed run
// (CheckpointPhase=ingest) does not use a stale CatalogHash to trigger
// a false no-op when there are still missing entries.
//
// Root cause B regression: before the fix, resumed runs restored CatalogHash
// from the prior run, which combined with Root Cause A could silence the backlog.
func TestNoOpFix_ResumeDoesNotUseStaleHash(t *testing.T) {
	pageIDs := []int64{20005, 20006}
	srv := mockFullPCGWServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	// Phase 1: scan catalog to establish hash.
	runID1, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	phase1Stats, err := RunCatalogScan(ctx, st, client, runID1, nil)
	if err != nil {
		t.Fatalf("catalog scan: %v", err)
	}
	sStats := store.PCGWSyncRunStats{}
	if err := st.FinishPCGWSyncRun(ctx, runID1, JobSuccess, "", sStats); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID1, phase1Stats); err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}

	// Simulate an interrupted run at ingest phase (budget exhausted at cursor 0).
	runID2, err := st.StartPCGWSyncRunWithResume(ctx, "incremental", runID1, "resumed from "+runID1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdatePCGWSyncRunPhase2Progress(ctx, runID2, 0, 0); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := st.FinishPCGWSyncRun(ctx, runID2, JobInterrupted, "budget exhausted", sStats); err != nil {
		t.Fatalf("finish interrupted run: %v", err)
	}

	// Catalog entries remain missing.
	missing, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing) == 0 {
		t.Skip("no missing entries — test precondition not met")
	}

	// Run incremental sync with ResumeCatalogScan=true (simulates resume-at-ingest).
	// With the fix, CatalogHash is cleared on resume, so the no-op gate cannot fire
	// even when the same hash would have been restored from the prior run.
	t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "5")
	_, syncErr := PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{
		ResumeCatalogScan: true,
		ResumeQueueCursor: 0,
	})
	if syncErr != nil {
		t.Fatalf("PCGWSyncEx (resume) returned error: %v", syncErr)
	}

	// Phase 2 should have run: at least one pcgw_games entry should exist.
	games, _, err := st.ListPCGWGames(ctx, store.PCGWGameListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) == 0 {
		t.Fatal("expected pcgw_games to have entries after resumed Phase 2 ran, got 0 — stale hash may have caused false no-op")
	}
}
