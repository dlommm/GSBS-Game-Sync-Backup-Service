package store

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestImportPCGWManifestBundle_v2_SmartMergeSkipUnchanged(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	now := time.Now().UTC().Format(time.RFC3339)
	game := types.PCGWGame{
		PageID: 42, Title: "Test Game", PageName: "Test_Game", ParseStatus: "ok",
		LastRevID: 100, UpdatedAt: now,
	}
	require.NoError(t, st.UpsertPCGWGame(ctx, &game))
	require.NoError(t, st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "42", PCGWPageID: 42, GameTitle: "Test Game", Platform: "windows", PathTemplate: "%APPDATA%/game", UpdatedAt: now},
	}))

	data, _, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	res, err := st.ImportPCGWManifestBundle(ctx, data, "merge_skip_unchanged")
	require.NoError(t, err)
	require.True(t, res.NoOp)
	require.Equal(t, 0, res.RowsChanged)
	require.Greater(t, res.SkippedUnchanged, 0)
}

// seedPCGWFullBundle builds an in-memory store containing the given page_ids
// (catalog entry + game + one save location each) and returns its exported full
// bundle bytes.
func seedPCGWFullBundle(t *testing.T, pageIDs ...int64) []byte {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	var catalog []types.PCGWCatalogEntry
	var locs []types.GameSaveLocation
	for _, pid := range pageIDs {
		catalog = append(catalog, types.PCGWCatalogEntry{PageID: pid, Title: "Game", FirstSeenAt: now, LastSeenAt: now})
		require.NoError(t, st.UpsertPCGWGame(ctx, &types.PCGWGame{
			PageID: pid, Title: "Game", PageName: "Game", ParseStatus: "ok", LastRevID: 1, UpdatedAt: now,
		}))
		locs = append(locs, types.GameSaveLocation{
			GameID: itoa(pid), PCGWPageID: pid, GameTitle: "Game", Platform: "windows",
			PathTemplate: "%APPDATA%/g", UpdatedAt: now,
		})
	}
	require.NoError(t, st.UpsertPCGWCatalogBatch(ctx, catalog))
	require.NoError(t, st.UpsertGameSaveLocations(ctx, locs))

	data, _, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)
	return data
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// pageRange returns [1, 2, ..., n].
func pageRange(n int64) []int64 {
	out := make([]int64, 0, n)
	for i := int64(1); i <= n; i++ {
		out = append(out, i)
	}
	return out
}

func TestImportPCGWManifestBundle_ReconcilesDeletions(t *testing.T) {
	ctx := context.Background()
	dst, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dst.Close() })

	// Import a full bundle with 20 games.
	_, err = dst.ImportPCGWManifestBundle(ctx, seedPCGWFullBundle(t, pageRange(20)...), "merge_skip_unchanged")
	require.NoError(t, err)
	locs, _ := dst.ListGameSaveLocations(ctx)
	require.Len(t, locs, 20)

	// A later full bundle drops game 20 (deleted upstream) — 1/20 = 5%, well
	// under the guard threshold, so reconciliation applies it.
	res, err := dst.ImportPCGWManifestBundle(ctx, seedPCGWFullBundle(t, pageRange(19)...), "merge_skip_unchanged")
	require.NoError(t, err)
	require.Equal(t, 1, res.Deleted, "game 20 should be reconciled away")

	g, _ := dst.GetPCGWGame(ctx, 20)
	require.Nil(t, g, "deleted game must be gone from pcgw_games")
	locs, _ = dst.ListGameSaveLocations(ctx)
	require.Len(t, locs, 19, "the deleted game's save location must be removed")
	stats, _ := dst.GetPCGWCatalogStats(ctx)
	require.Equal(t, 19, stats.RemoteTotal, "catalog must shrink to match the bundle")
}

func TestImportPCGWManifestBundle_DeletionGuard(t *testing.T) {
	ctx := context.Background()
	dst, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dst.Close() })

	_, err = dst.ImportPCGWManifestBundle(ctx, seedPCGWFullBundle(t, pageRange(20)...), "merge_skip_unchanged")
	require.NoError(t, err)

	// A bundle with only 5 games would delete 15 of 20 (75% > 25% guard): the
	// reconciliation must refuse and keep the mirror intact.
	res, err := dst.ImportPCGWManifestBundle(ctx, seedPCGWFullBundle(t, pageRange(5)...), "merge_skip_unchanged")
	require.NoError(t, err)
	require.Equal(t, 0, res.Deleted, "guard must block a too-large removal set")
	locs, _ := dst.ListGameSaveLocations(ctx)
	require.Len(t, locs, 20, "no rows should be deleted when the guard trips")
}

func TestImportPCGWManifestBundle_v2_Catalog(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	catalog := []types.PCGWCatalogEntry{
		{PageID: 1, Title: "A", FirstSeenAt: time.Now().UTC().Format(time.RFC3339), LastSeenAt: time.Now().UTC().Format(time.RFC3339)},
	}
	require.NoError(t, st.UpsertPCGWCatalogBatch(ctx, catalog))

	data, _, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	st2, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st2.Close() })

	res, err := st2.ImportPCGWManifestBundle(ctx, data, "merge")
	require.NoError(t, err)
	require.Greater(t, res.PCGWCatalog, 0)

	stats, err := st2.GetPCGWCatalogStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.RemoteTotal)
}

func TestPCGWSyncSourceFromSettings_DefaultS3(t *testing.T) {
	src := PCGWSyncSourceFromSettings(map[string]string{})
	require.Equal(t, PCGWSyncSourceS3, src)
}

func TestPCGWSyncSourceFromSettings_LegacyGitHubNormalized(t *testing.T) {
	src := PCGWSyncSourceFromSettings(map[string]string{AdminSettingPCGWSyncSource: PCGWSyncSourceGitHub})
	require.Equal(t, PCGWSyncSourceS3, src)
}

func TestPCGWSyncSourceFromSettings_EnvOverride(t *testing.T) {
	t.Setenv(EnvPCGWSyncSource, PCGWSyncSourceAPI)
	src := PCGWSyncSourceFromSettings(map[string]string{AdminSettingPCGWSyncSource: PCGWSyncSourceGitHub})
	require.Equal(t, PCGWSyncSourceAPI, src)
}

func TestPCGWBundleCronFromSettings_EnvEmptyDisables(t *testing.T) {
	t.Setenv(EnvPCGWBundleCron, "")
	cron := PCGWBundleCronFromSettings(map[string]string{AdminSettingPCGWBundleCron: DefaultPCGWBundleCron})
	require.Equal(t, "", cron)
}

func TestIsPCGWBundleSeeded(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	ok, err := st.IsPCGWBundleSeeded(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 1, Title: "X", ParseStatus: "ok"}))
	ok, err = st.IsPCGWBundleSeeded(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

// TestImportPCGWManifestBundle_FreshServerWithGames reproduces the production
// scenario that a same-store import misses: importing a bundle that contains
// games (with LastRevID > 0) into a FRESH server. Previously the skip-unchanged
// check treated the absent local row (sql.ErrNoRows) as a fatal error, aborting
// the whole import after game_save_locations but before any games landed.
func TestImportPCGWManifestBundle_FreshServerWithGames(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Source store: a game with a real LastRevID + a save location.
	src, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	require.NoError(t, src.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: 42, Title: "Test Game", PageName: "Test_Game", ParseStatus: "ok",
		LastRevID: 100, UpdatedAt: now,
	}))
	require.NoError(t, src.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "42", PCGWPageID: 42, GameTitle: "Test Game", Platform: "windows", PathTemplate: "%APPDATA%/game", UpdatedAt: now},
	}))
	data, _, err := src.ExportPCGWManifestBundleWithOpts(ctx, "test", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	// Fresh destination server imports the bundle.
	dst, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dst.Close() })

	res, err := dst.ImportPCGWManifestBundle(ctx, data, "merge_skip_unchanged")
	require.NoError(t, err) // previously failed: "sql: no rows in result set"
	require.Equal(t, 1, res.PCGWGames, "game must import on a fresh server")

	g, err := dst.GetPCGWGame(ctx, 42)
	require.NoError(t, err)
	require.NotNil(t, g)
	require.Equal(t, int64(100), g.LastRevID)

	locs, err := dst.ListGameSaveLocations(ctx)
	require.NoError(t, err)
	require.Len(t, locs, 1)
}

// TestImportPCGWManifestBundle_SkipsOrphanChildRows verifies that a bundle
// containing a child row (section) whose page_id has no game — a real
// source-DB drift case that violates the pcgw_games FK — is imported by
// skipping the orphan rather than aborting the entire import on a FK error.
func TestImportPCGWManifestBundle_SkipsOrphanChildRows(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	bundle := map[string]any{
		"schema_version": 2,
		"exported_at":    now,
		"games": []map[string]any{
			{"page_id": 1, "title": "G1", "page_name": "G1", "parse_status": "ok", "last_rev_id": 5, "updated_at": now},
		},
		"sections": map[string]any{
			"notes": []map[string]any{
				{"page_id": 1, "section": "notes", "data": map[string]any{"k": "v"}, "updated_at": now},   // valid
				{"page_id": 999, "section": "notes", "data": map[string]any{"k": "x"}, "updated_at": now}, // orphan (no game 999)
			},
		},
	}
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)

	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	res, err := st.ImportPCGWManifestBundle(ctx, raw, "merge_skip_unchanged")
	require.NoError(t, err) // previously aborted: "FOREIGN KEY constraint failed"
	require.Equal(t, 1, res.PCGWGames)
	require.Equal(t, 1, res.PCGWSections, "valid section row imported")
	require.GreaterOrEqual(t, res.SkippedOrphans, 1, "orphan section row skipped")
}

// TestFullExportRoundTrip proves the full-bundle guarantee: re-exporting a full
// bundle after the source DB gains new rows contains those rows, and a fresh
// server importing ONLY that full ends up with everything — no deltas needed.
func TestFullExportRoundTrip(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. Baseline "full" state: two save locations.
	src, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })
	require.NoError(t, src.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "1", PCGWPageID: 1, GameTitle: "Game One", Platform: "windows", PathTemplate: "%APPDATA%/one", UpdatedAt: now},
		{GameID: "2", PCGWPageID: 2, GameTitle: "Game Two", Platform: "windows", PathTemplate: "%APPDATA%/two", UpdatedAt: now},
	}))

	// 2. A week's worth of new upstream data adds a third save location.
	require.NoError(t, src.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "3", PCGWPageID: 3, GameTitle: "Game Three", Platform: "windows", PathTemplate: "%APPDATA%/three", UpdatedAt: now},
	}))
	locs, _ := src.ListGameSaveLocations(ctx)
	require.Len(t, locs, 3, "the third location should be in the DB")

	// 3. Re-export a fresh FULL from the DB (what the publisher does each run).
	full, _, err := src.ExportPCGWManifestBundleWithOpts(ctx, "compact", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)

	// 4. A fresh server importing ONLY that full gets everything, incl. the delta row.
	dst, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dst.Close() })
	_, err = dst.ImportPCGWManifestBundle(ctx, full, "merge_skip_unchanged")
	require.NoError(t, err)

	dlocs, err := dst.ListGameSaveLocations(ctx)
	require.NoError(t, err)
	require.Len(t, dlocs, 3, "full bundle must contain all rows")
	found := false
	for _, l := range dlocs {
		if l.GameID == "3" {
			found = true
		}
	}
	require.True(t, found, "the newly added save location must be in the full bundle")
}

func TestExportFullSetsFullExportedAt(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	require.NoError(t, st.UpsertPCGWGame(ctx, &types.PCGWGame{PageID: 1, Title: "X", ParseStatus: "ok"}))
	_, meta, err := st.ExportPCGWManifestBundleWithOpts(ctx, "test", PCGWBundleExportOpts{Lite: true})
	require.NoError(t, err)
	require.NotEmpty(t, meta.FullExportedAt)
	require.Equal(t, meta.ExportedAt, meta.FullExportedAt)
}
