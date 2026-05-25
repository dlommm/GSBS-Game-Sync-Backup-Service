package store

import (
	"context"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_GetManifestSince(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	past := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	mid := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)

	entries := []types.GameSaveLocation{
		{GameID: "1", PCGWPageID: 1, GameTitle: "Old", Platform: "windows", PathTemplate: "a", Source: "pcgw", UpdatedAt: past},
		{GameID: "2", PCGWPageID: 2, GameTitle: "Mid", Platform: "linux", PathTemplate: "b", Source: "pcgw", UpdatedAt: mid},
		{GameID: "3", PCGWPageID: 3, GameTitle: "New", Platform: "linux", PathTemplate: "c", Source: "pcgw", UpdatedAt: recent},
	}
	require.NoError(t, st.UpsertGameSaveLocations(ctx, entries))

	delta, err := st.GetManifestSince(ctx, mid)
	require.NoError(t, err)
	require.Len(t, delta, 1)
	assert.Equal(t, "3", delta[0].GameID)

	empty, err := st.GetManifestSince(ctx, recent)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestSQLite_ManifestFetchLog(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	require.NoError(t, st.LogManifestFetch(ctx, "cid1", "my-client", "alice", 42))
	require.NoError(t, st.LogManifestFetch(ctx, "", "", "", 10))

	rows, err := st.ListManifestFetches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byClient := map[string]ManifestFetchRow{}
	for _, r := range rows {
		byClient[r.ClientID] = r
	}
	assert.Equal(t, "my-client", byClient["cid1"].ClientName)
	assert.Equal(t, "alice", byClient["cid1"].Username)
	assert.Equal(t, 42, byClient["cid1"].EntriesCount)
	assert.Equal(t, 10, byClient[""].EntriesCount)
}
