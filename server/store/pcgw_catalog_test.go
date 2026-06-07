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
	if err := st.UpdatePCGWSyncRunPhase1Stats(ctx, runID, stats); err != nil {
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
}
