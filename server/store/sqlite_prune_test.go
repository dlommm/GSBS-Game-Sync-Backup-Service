package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_PruneVersionsForGame(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	sq := st.(*sqliteStore)
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)

	// Two slots for g1 (4 and 3 versions — within the default retention of 5,
	// so write-time pruning leaves them alone), plus one slot for g2 that must
	// stay untouched by g1's prune.
	for i := 1; i <= 4; i++ {
		_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte(fmt.Sprintf("pk1-version-%d", i)), nil)
		require.NoError(t, err)
	}
	for i := 1; i <= 3; i++ {
		_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk2", []byte(fmt.Sprintf("pk2-version-%d", i)), nil)
		require.NoError(t, err)
	}
	for i := 1; i <= 3; i++ {
		_, err = st.UpsertSaveWithMeta(ctx, userID, "g2", "pk1", []byte(fmt.Sprintf("g2-version-%d", i)), nil)
		require.NoError(t, err)
	}

	perGame, err := st.VersionStorageByGame(ctx, userID)
	require.NoError(t, err)
	byGame := map[string]GameVersionStorage{}
	for _, g := range perGame {
		byGame[g.GameID] = g
	}
	assert.Equal(t, 7, byGame["g1"].Versions)
	assert.Equal(t, 3, byGame["g2"].Versions)
	assert.Greater(t, byGame["g1"].Bytes, int64(0))

	// Admin lowers g1's retention to 2; bust the 60s override cache so the
	// change is visible immediately (in-package reach into the cache).
	require.NoError(t, st.SetAdminSetting(ctx, AdminSettingRetentionOverrides, `{"g1":2}`))
	sq.retentionOv.mu.Lock()
	sq.retentionOv.at = time.Time{}
	sq.retentionOv.mu.Unlock()
	assert.Equal(t, 2, st.RetentionForGame(ctx, "g1"))

	deleted, freed, err := st.PruneVersionsForGame(ctx, userID, "g1")
	require.NoError(t, err)
	assert.Equal(t, 3, deleted) // (4-2) + (3-2)
	assert.Greater(t, freed, int64(0))

	// Newest versions per slot survive.
	vs, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	require.Len(t, vs, 2)
	assert.Equal(t, 4, vs[0].Version)
	assert.Equal(t, 3, vs[1].Version)

	// g2 untouched; pruning again is a no-op.
	vs2, err := st.ListSaveVersions(ctx, userID, "g2", "pk1", 10)
	require.NoError(t, err)
	assert.Len(t, vs2, 3)
	deleted2, freed2, err := st.PruneVersionsForGame(ctx, userID, "g1")
	require.NoError(t, err)
	assert.Zero(t, deleted2)
	assert.Zero(t, freed2)
}
