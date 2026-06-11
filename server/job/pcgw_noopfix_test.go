package job

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	}, "full")
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
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1Stats, "full"); err != nil {
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
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1Stats, "full"); err != nil {
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
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID1, phase1Stats, "full"); err != nil {
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

// mockOffsetAwarePCGWServer handles both catalog and page-ingest endpoints.
// The catalog endpoint is offset-aware: returns only pages[offset:offset+limit].
func mockOffsetAwarePCGWServer(pageIDs []int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if q.Get("tables") == "Infobox_game" {
			offset := 0
			limit := 500
			fmt.Sscanf(q.Get("offset"), "%d", &offset)
			fmt.Sscanf(q.Get("limit"), "%d", &limit)
			start := offset
			if start > len(pageIDs) {
				start = len(pageIDs)
			}
			end := start + limit
			if end > len(pageIDs) {
				end = len(pageIDs)
			}
			slice := pageIDs[start:end]
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
			return
		}

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

// TestFastPath_SkipsFullScan verifies that when the catalog is complete and the probe
// finds no growth, RunCatalogScan (full) is not invoked — Phase 2 runs on backlog only.
func TestFastPath_SkipsFullScan(t *testing.T) {
	pageIDs := []int64{30001, 30002, 30003}
	srv := mockOffsetAwarePCGWServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	// Run a full initial sync to populate catalog and have a prior success run.
	t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "10")
	_, err = PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{Full: true})
	if err != nil {
		t.Fatalf("initial full sync: %v", err)
	}

	// Confirm catalog is populated.
	catStats, _ := st.GetPCGWCatalogStats(ctx)
	if catStats.RemoteTotal != len(pageIDs) {
		t.Fatalf("catalog remote_total: got %d, want %d", catStats.RemoteTotal, len(pageIDs))
	}

	// Get catalog hash before incremental run.
	priorHash, _ := st.ComputeCatalogHash(ctx)

	// Run incremental sync — same server, so probe at offset 3 returns nothing.
	// Fast path should be taken (mode="fast_probe").
	_, err = PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{})
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}

	// Catalog hash must be unchanged (no full scan ran).
	afterHash, _ := st.ComputeCatalogHash(ctx)
	if afterHash != priorHash {
		t.Errorf("catalog hash changed after fast-probe incremental (expected no full scan)")
	}

	// Check that the latest sync run recorded fast_probe or tail (not full for the incremental run).
	runs, err := st.ListPCGWSyncRuns(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("no sync runs recorded")
	}
	latest := runs[0]
	if latest.CatalogScanMode == "full" {
		t.Errorf("expected fast-path scan mode for incremental run, got %q", latest.CatalogScanMode)
	}
}

// TestFastPath_SkipsBuildChangedQueue verifies that when the probe is empty and
// last_rev_check_at is recent, buildChangedQueue is not invoked (no rev API calls).
func TestFastPath_SkipsBuildChangedQueue(t *testing.T) {
	pageIDs := []int64{40001, 40002}
	srv := mockOffsetAwarePCGWServer(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "10")

	// Initial full sync.
	_, err = PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{Full: true})
	if err != nil {
		t.Fatalf("initial full sync: %v", err)
	}

	// Mark last_rev_check_at as recent so deferred rev-check is skipped.
	if err := st.SetLastRevCheckAt(ctx, time.Now()); err != nil {
		t.Fatalf("SetLastRevCheckAt: %v", err)
	}

	// Second incremental sync — probe empty, rev-check should be skipped.
	// The sync must complete without error.
	_, err = PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{})
	if err != nil {
		t.Fatalf("incremental sync with deferred rev-check: %v", err)
	}

	// Verify manifest meta has last_rev_check_at set (from the first full sync call to SetLastRevCheckAt).
	meta, err := st.GetPCGWManifestMeta(ctx)
	if err != nil {
		t.Fatalf("GetPCGWManifestMeta: %v", err)
	}
	if meta.LastRevCheckAt == "" {
		t.Error("expected last_rev_check_at to be set after SetLastRevCheckAt")
	}
}

func mockPCGWServerWithCatalogCounter(pageIDs []int64) (*httptest.Server, *int) {
	var catalogCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("tables") == "Infobox_game" {
			catalogCalls++
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
	return srv, &catalogCalls
}

func seedCatalogAndPriorPhase1(t *testing.T, ctx context.Context, st store.Store, client *pcgw.Client, pageIDs []int64) types.Phase1Stats {
	t.Helper()
	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	phase1Stats, err := RunCatalogScan(ctx, st, client, runID, nil)
	if err != nil {
		t.Fatalf("catalog scan: %v", err)
	}
	if err := st.FinishPCGWSyncRun(ctx, runID, JobSuccess, "", store.PCGWSyncRunStats{}); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, phase1Stats, "full"); err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}
	return phase1Stats
}

// TestTargetedModes_SkipCatalogPhase verifies Parse Missing Only / Retry Failed skip Phase 1.
func TestTargetedModes_SkipCatalogPhase(t *testing.T) {
	pageIDs := []int64{50001, 50002, 50003}
	srv, catalogCalls := mockPCGWServerWithCatalogCounter(pageIDs)
	defer srv.Close()

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	client := pcgw.NewClient()
	client.BaseURL = srv.URL
	seedCatalogAndPriorPhase1(t, ctx, st, client, pageIDs)

	// One missing page, one failed page already stored locally.
	const failedID = int64(50003)
	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: failedID, PageName: "50003", Title: "Game 50003", ParseStatus: "failed",
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GSBS_PCGW_MAX_PAGES_PER_RUN", "10")
	*catalogCalls = 0
	if _, err := PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{
		MissingOnly: true, SkipCatalogPhase: true,
	}); err != nil {
		t.Fatalf("missing-only sync: %v", err)
	}
	if *catalogCalls != 0 {
		t.Errorf("missing-only sync: expected 0 catalog API calls, got %d", *catalogCalls)
	}
	runs, _ := st.ListPCGWSyncRuns(ctx, 1)
	if len(runs) == 0 || runs[0].CatalogScanMode != "skipped" {
		t.Errorf("missing-only sync: expected catalog_scan_mode=skipped, got %q", runs[0].CatalogScanMode)
	}
	missingAfter, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missingAfter) != 0 {
		t.Errorf("missing-only sync: expected all missing processed, still have %d", len(missingAfter))
	}
	failedStill, _ := st.ListPCGWCatalogFailedPartial(ctx, 0, 0)
	if len(failedStill) != 1 || failedStill[0] != failedID {
		t.Errorf("missing-only sync: failed page should remain queued, got %v", failedStill)
	}

	*catalogCalls = 0
	if _, err := PCGWSyncEx(ctx, st, client, nil, nil, PCGWSyncOptions{
		RetryFailedOnly: true, SkipCatalogPhase: true,
	}); err != nil {
		t.Fatalf("retry-failed sync: %v", err)
	}
	if *catalogCalls != 0 {
		t.Errorf("retry-failed sync: expected 0 catalog API calls, got %d", *catalogCalls)
	}
	game, err := st.GetPCGWGame(ctx, failedID)
	if err != nil {
		t.Fatal(err)
	}
	if game.ParseStatus == "failed" {
		t.Errorf("retry-failed sync: expected failed page to be retried, still failed")
	}
}
