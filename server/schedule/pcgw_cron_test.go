package schedule

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/server/store"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func TestPCGWCron_View_S3Source(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceS3))

	pc := NewPCGWCron(cron.New(), st, nil)
	view := pc.View(ctx)

	require.Equal(t, store.PCGWSyncSourceS3, view.SyncSource)
	require.Equal(t, store.DefaultPCGWBundleCron, view.BundleExpr)
	require.False(t, view.BundleNext.IsZero())
	require.True(t, view.NextRun.IsZero(), "API sync next run should be empty in S3 mode")
}

func TestPCGWCron_View_LegacyGitHubNormalizesToS3(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceGitHub))

	pc := NewPCGWCron(cron.New(), st, nil)
	view := pc.View(ctx)

	require.Equal(t, store.PCGWSyncSourceS3, view.SyncSource, "legacy github must normalize to s3")
}

func TestPCGWCron_View_APISource(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceAPI))

	pc := NewPCGWCron(cron.New(), st, nil)
	view := pc.View(ctx)

	require.Equal(t, store.PCGWSyncSourceAPI, view.SyncSource)
	require.False(t, view.NextRun.IsZero())
	require.Equal(t, store.DefaultPCGWCron, view.Expr)
}

func TestPCGWCron_View_BundleCronEnvOverride(t *testing.T) {
	t.Setenv(store.EnvPCGWBundleCron, "0 6 * * *")

	st, err := store.NewSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	require.NoError(t, st.SetAdminSetting(ctx, store.AdminSettingPCGWSyncSource, store.PCGWSyncSourceS3))

	pc := NewPCGWCron(cron.New(), st, nil)
	view := pc.View(ctx)

	require.Equal(t, "0 6 * * *", view.BundleExpr)
}
