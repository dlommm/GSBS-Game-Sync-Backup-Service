package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestUpsertPCGWCatalogBatch(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	entries := []types.PCGWCatalogEntry{
		{PageID: 1, Title: "Game A", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z", LastSeenRunID: "run1"},
		{PageID: 2, Title: "Game B", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z", LastSeenRunID: "run1"},
		{PageID: 3, Title: "Game C", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z", LastSeenRunID: "run1"},
	}
	if err := st.UpsertPCGWCatalogBatch(ctx, entries); err != nil {
		t.Fatalf("upsert batch: %v", err)
	}

	// Update entry 1 with a new title — upsert should overwrite title but keep first_seen_at.
	entries2 := []types.PCGWCatalogEntry{
		{PageID: 1, Title: "Game A Updated", LastSeenAt: "2025-02-01T00:00:00Z", LastSeenRunID: "run2"},
	}
	if err := st.UpsertPCGWCatalogBatch(ctx, entries2); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	stats, err := st.GetPCGWCatalogStats(ctx)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.RemoteTotal != 3 {
		t.Errorf("remote_total: got %d, want 3", stats.RemoteTotal)
	}
}

func TestGetPCGWCatalogStats_MissingExtra(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Insert 3 pages in catalog.
	entries := []types.PCGWCatalogEntry{
		{PageID: 10, Title: "G10", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 20, Title: "G20", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 30, Title: "G30", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	}
	if err := st.UpsertPCGWCatalogBatch(ctx, entries); err != nil {
		t.Fatal(err)
	}

	// Insert pages 10 and 99 into pcgw_games (20 and 30 are missing; 99 is extra).
	for _, id := range []int64{10, 99} {
		if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
			PageID: id, PageName: "p", Title: "t", ParseStatus: "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := st.GetPCGWCatalogStats(ctx)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.MissingLocal != 2 {
		t.Errorf("missing: got %d, want 2", stats.MissingLocal)
	}
	if stats.ExtraLocal != 1 {
		t.Errorf("extra: got %d, want 1", stats.ExtraLocal)
	}
}

func TestIncrementCatalogRetryAndDeadLetter(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// First insert so page 5 exists in catalog.
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 5, Title: "G5", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})

	// Increment 5 times — should cross threshold and become dead_letter.
	for i := 0; i < deadLetterThreshold; i++ {
		if err := st.IncrementCatalogRetry(ctx, 5, "test error"); err != nil {
			t.Fatalf("increment: %v", err)
		}
	}

	dl, err := st.ListPCGWCatalogDeadLetter(ctx, 10)
	if err != nil {
		t.Fatalf("list dead letter: %v", err)
	}
	if len(dl) != 1 || dl[0].PageID != 5 {
		t.Errorf("expected page 5 in dead letter, got %v", dl)
	}

	// Clear dead letter.
	if err := st.ClearCatalogDeadLetter(ctx, 5); err != nil {
		t.Fatalf("clear dead letter: %v", err)
	}
	dl2, _ := st.ListPCGWCatalogDeadLetter(ctx, 10)
	if len(dl2) != 0 {
		t.Errorf("expected empty dead letter after clear, got %d", len(dl2))
	}
}

func TestComputeCatalogHash(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	entries := []types.PCGWCatalogEntry{
		{PageID: 100, Title: "A", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 200, Title: "B", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	}
	_ = st.UpsertPCGWCatalogBatch(ctx, entries)

	h1, err := st.ComputeCatalogHash(ctx)
	if err != nil || h1 == "" {
		t.Fatalf("hash1: %v %q", err, h1)
	}

	// Same set → same hash.
	h2, _ := st.ComputeCatalogHash(ctx)
	if h1 != h2 {
		t.Errorf("hashes differ: %q vs %q", h1, h2)
	}

	// Add a new entry → hash changes.
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 300, Title: "C", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})
	h3, _ := st.ComputeCatalogHash(ctx)
	if h3 == h1 {
		t.Error("hash should change after adding entry")
	}
}

func TestListPCGWCatalogMissing(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 1, Title: "G1", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 2, Title: "G2", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 3, Title: "G3", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})
	// Only page 1 in pcgw_games.
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 1, PageName: "p1", Title: "G1", ParseStatus: "ok"})

	missing, err := st.ListPCGWCatalogMissing(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list missing: %v", err)
	}
	if len(missing) != 2 {
		t.Errorf("missing count: got %d, want 2", len(missing))
	}
}

func TestListPCGWCatalogMissing_ZeroLimitReturnsAll(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	entries := make([]types.PCGWCatalogEntry, 0, 620)
	for i := int64(1); i <= 620; i++ {
		entries = append(entries, types.PCGWCatalogEntry{
			PageID: i, Title: "G", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z",
		})
	}
	if err := st.UpsertPCGWCatalogBatch(ctx, entries); err != nil {
		t.Fatalf("upsert catalog: %v", err)
	}

	// Seed one local row so not all are missing.
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 1, PageName: "p1", Title: "G1", ParseStatus: "ok"})

	missing, err := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list missing with zero limit: %v", err)
	}
	if len(missing) != 619 {
		t.Fatalf("missing count with zero limit: got %d, want 619", len(missing))
	}
}

func TestListPCGWCatalogTitleBackfill(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 1, Title: "Catalog One", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 2, Title: "Catalog Two", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 3, Title: "Catalog Three", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 4, Title: "", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 1, PageName: "Catalog One", Title: "", ParseStatus: "ok"})
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 2, PageName: "Catalog Two", Title: "Catalog Two", ParseStatus: "ok"})
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 3, PageName: "", Title: "Catalog Three", ParseStatus: "ok"})
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 4, PageName: "", Title: "", ParseStatus: "ok"})

	rows, err := st.ListPCGWCatalogTitleBackfill(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list title backfill: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].PageID != 1 || rows[0].Title != "Catalog One" {
		t.Fatalf("unexpected first row: page_id=%d title=%q", rows[0].PageID, rows[0].Title)
	}
	if rows[1].PageID != 3 || rows[1].Title != "Catalog Three" {
		t.Fatalf("unexpected second row: page_id=%d title=%q", rows[1].PageID, rows[1].Title)
	}
}

func TestWipePCGWMirrorOnly(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// Seed some games and catalog entries.
	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 42, PageName: "p42", Title: "G42", ParseStatus: "ok"})
	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 42, Title: "G42", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})
	_ = st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "42", PCGWPageID: 42, GameTitle: "G42", Platform: "windows", PathTemplate: "%APPDATA%", Source: "pcgw", UpdatedAt: "2025-01-01T00:00:00Z"},
	})

	if err := st.WipePCGWMirrorOnly(ctx); err != nil {
		t.Fatalf("wipe mirror only: %v", err)
	}

	// pcgw_games should be empty.
	games, total, _ := st.ListPCGWGames(ctx, PCGWGameListFilter{Limit: 10})
	if total != 0 || len(games) != 0 {
		t.Errorf("pcgw_games not empty after wipe mirror only: got %d", total)
	}

	// game_save_locations should still have the entry (mirror_only doesn't touch it).
	locs, _ := st.ListGameSaveLocations(ctx)
	if len(locs) == 0 {
		t.Error("game_save_locations should not be wiped by mirror_only")
	}

	// pcgw_catalog should still have the entry.
	catStats, _ := st.GetPCGWCatalogStats(ctx)
	if catStats.RemoteTotal == 0 {
		t.Error("pcgw_catalog should not be wiped by mirror_only")
	}
}

func TestWipePCGWMirrorAndManifest(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	_ = st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 42, PageName: "p42", Title: "G42", ParseStatus: "ok"})
	_ = st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "42", PCGWPageID: 42, GameTitle: "G42", Platform: "windows", PathTemplate: "%APPDATA%", Source: "pcgw", UpdatedAt: "2025-01-01T00:00:00Z"},
	})

	if err := st.WipePCGWMirrorAndManifest(ctx); err != nil {
		t.Fatalf("wipe mirror+manifest: %v", err)
	}

	games, total, _ := st.ListPCGWGames(ctx, PCGWGameListFilter{Limit: 10})
	if total != 0 || len(games) != 0 {
		t.Errorf("pcgw_games not empty after full wipe: got %d", total)
	}

	locs, _ := st.ListGameSaveLocations(ctx)
	if len(locs) != 0 {
		t.Errorf("game_save_locations should be wiped by mirror_and_manifest, got %d", len(locs))
	}
}

func TestResetPCGWDeadLetter(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	_ = st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{
		{PageID: 10, Title: "G10", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 20, Title: "G20", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
		{PageID: 30, Title: "G30", FirstSeenAt: "2025-01-01T00:00:00Z", LastSeenAt: "2025-01-01T00:00:00Z"},
	})

	// Dead-letter page 10 and 20 (5 failures each).
	for i := 0; i < deadLetterThreshold; i++ {
		_ = st.IncrementCatalogRetry(ctx, 10, "net error")
		_ = st.IncrementCatalogRetry(ctx, 20, "net error")
	}

	dl, _ := st.ListPCGWCatalogDeadLetter(ctx, 10)
	if len(dl) != 2 {
		t.Fatalf("expected 2 dead-lettered pages before reset, got %d", len(dl))
	}

	// After dead-lettering, pages 10 and 20 are absent from ListPCGWCatalogMissing.
	missing, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing) != 1 || missing[0] != 30 {
		t.Fatalf("expected only page 30 in missing before reset, got %v", missing)
	}

	n, err := st.ResetPCGWDeadLetter(ctx)
	if err != nil {
		t.Fatalf("reset dead-letter: %v", err)
	}
	if n != 2 {
		t.Errorf("reset rows affected: got %d, want 2", n)
	}

	// After reset all three pages are missing again.
	missing2, _ := st.ListPCGWCatalogMissing(ctx, 0, 0)
	if len(missing2) != 3 {
		t.Errorf("expected 3 missing pages after reset, got %d", len(missing2))
	}

	dl2, _ := st.ListPCGWCatalogDeadLetter(ctx, 10)
	if len(dl2) != 0 {
		t.Errorf("expected 0 dead-letter entries after reset, got %d", len(dl2))
	}
}

func TestUpdatePCGWSyncRunPhase1Stats(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	runID, err := st.StartPCGWSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}

	stats := types.Phase1Stats{
		RemoteTotalIDs:  1000,
		MissingLocalIDs: 50,
		ExtraLocalIDs:   5,
		CatalogHash:     "abc123",
		CompletedAt:     "2025-06-01T00:00:00Z",
	}
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, stats, "full"); err != nil {
		t.Fatalf("update phase1 stats: %v", err)
	}

	run, err := st.GetLatestPCGWSyncRun(ctx)
	if err != nil {
		t.Fatalf("get latest run: %v", err)
	}
	if run.RemoteTotalIDs != 1000 {
		t.Errorf("remote_total_ids: got %d, want 1000", run.RemoteTotalIDs)
	}
	if run.CatalogHash != "abc123" {
		t.Errorf("catalog_hash: got %q, want abc123", run.CatalogHash)
	}
	if run.CheckpointPhase != "ingest" {
		t.Errorf("checkpoint_phase: got %q, want ingest", run.CheckpointPhase)
	}
	if run.CatalogScanMode != "full" {
		t.Errorf("catalog_scan_mode: got %q, want full", run.CatalogScanMode)
	}
}

func TestGetLastSuccessfulPhase1Stats(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	// No runs yet — should return nil.
	stats, err := st.GetLastSuccessfulPhase1Stats(ctx)
	if err != nil {
		t.Fatalf("GetLastSuccessfulPhase1Stats (empty): %v", err)
	}
	if stats != nil {
		t.Errorf("expected nil when no runs exist, got %+v", stats)
	}

	// Insert a failed run — should still return nil.
	runID1, _ := st.StartPCGWSyncRun(ctx, "incremental")
	_ = st.FinishPCGWSyncRun(ctx, runID1, "failed", "err", PCGWSyncRunStats{})

	stats, err = st.GetLastSuccessfulPhase1Stats(ctx)
	if err != nil {
		t.Fatalf("GetLastSuccessfulPhase1Stats (failed run only): %v", err)
	}
	if stats != nil {
		t.Errorf("expected nil when no success runs, got %+v", stats)
	}

	// Insert a successful run with remote_total_ids=0 — should still return nil.
	runID2, _ := st.StartPCGWSyncRun(ctx, "incremental")
	_ = st.FinishPCGWSyncRun(ctx, runID2, "success", "", PCGWSyncRunStats{})

	stats, err = st.GetLastSuccessfulPhase1Stats(ctx)
	if err != nil {
		t.Fatalf("GetLastSuccessfulPhase1Stats (zero total): %v", err)
	}
	if stats != nil {
		t.Errorf("expected nil when remote_total_ids=0, got %+v", stats)
	}

	// Insert a successful run with real phase1 stats.
	runID3, _ := st.StartPCGWSyncRun(ctx, "incremental")
	want := types.Phase1Stats{
		RemoteTotalIDs:  2500,
		MissingLocalIDs: 10,
		ExtraLocalIDs:   2,
		CatalogHash:     "deadbeef",
		CompletedAt:     "2025-06-01T00:00:00Z",
	}
	_ = st.UpdatePCGWSyncRunPhase1Stats(ctx, runID3, want, "full")
	_ = st.FinishPCGWSyncRun(ctx, runID3, "success", "", PCGWSyncRunStats{})

	stats, err = st.GetLastSuccessfulPhase1Stats(ctx)
	if err != nil {
		t.Fatalf("GetLastSuccessfulPhase1Stats: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats after successful run")
		return
	}
	if stats.RemoteTotalIDs != want.RemoteTotalIDs {
		t.Errorf("RemoteTotalIDs: got %d, want %d", stats.RemoteTotalIDs, want.RemoteTotalIDs)
	}
	if stats.CatalogHash != want.CatalogHash {
		t.Errorf("CatalogHash: got %q, want %q", stats.CatalogHash, want.CatalogHash)
	}
}
