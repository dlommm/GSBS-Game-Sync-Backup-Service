package store

import (
	"context"
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

func TestPCGWSyncSourceFromSettings_DefaultGitHub(t *testing.T) {
	src := PCGWSyncSourceFromSettings(map[string]string{})
	require.Equal(t, PCGWSyncSourceGitHub, src)
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

func TestPCGWBundleMetaURLFromSettings(t *testing.T) {
	url := PCGWBundleMetaURLFromSettings(map[string]string{
		AdminSettingPCGWBundleURL: DefaultPCGWBundleURL,
	})
	require.Equal(t, "https://raw.githubusercontent.com/dlommm/gsbs-manifest/main/manifest.meta.json", url)
}

func TestCanApplyRemoteDelta(t *testing.T) {
	fullAt := "2026-06-01T00:00:00Z"
	midAt := "2026-06-05T00:00:00Z"
	newAt := "2026-06-10T00:00:00Z"

	t.Run("empty lastExported", func(t *testing.T) {
		meta := PCGWBundleMeta{FullExportedAt: fullAt}
		require.False(t, CanApplyRemoteDelta("", fullAt, meta))
	})

	t.Run("cumulative ok when lastExported at anchor", func(t *testing.T) {
		meta := PCGWBundleMeta{FullExportedAt: fullAt, ExportedAt: newAt}
		require.True(t, CanApplyRemoteDelta(fullAt, fullAt, meta))
	})

	t.Run("cumulative ok when lastExported after anchor", func(t *testing.T) {
		meta := PCGWBundleMeta{FullExportedAt: fullAt, ExportedAt: newAt}
		require.True(t, CanApplyRemoteDelta(midAt, fullAt, meta))
	})

	t.Run("cumulative gap when lastExported before anchor", func(t *testing.T) {
		meta := PCGWBundleMeta{FullExportedAt: newAt, ExportedAt: "2026-06-11T00:00:00Z"}
		require.False(t, CanApplyRemoteDelta(fullAt, newAt, meta))
	})

	t.Run("legacy chained exact match", func(t *testing.T) {
		meta := PCGWBundleMeta{PreviousExportedAt: midAt}
		require.True(t, CanApplyRemoteDelta(midAt, "", meta))
	})

	t.Run("legacy chained gap", func(t *testing.T) {
		meta := PCGWBundleMeta{PreviousExportedAt: newAt}
		require.False(t, CanApplyRemoteDelta(fullAt, "", meta))
	})
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
