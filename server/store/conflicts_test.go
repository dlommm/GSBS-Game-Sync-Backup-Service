package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_ConflictLifecycle(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)
	otherID, err := st.CreateUser(ctx, "other", "h")
	require.NoError(t, err)

	rec := ConflictRecord{
		UserID: userID, GameID: "g1", PathKey: "pk1", ClientID: "dev-a",
		Kind: "if_hash", IncomingHash: "aaa", ServerHash: "bbb", ServerVersion: 3,
	}
	id1, err := st.RecordConflict(ctx, rec)
	require.NoError(t, err)
	require.NotEmpty(t, id1)

	// Outbox retry: same slot+device dedupes into the same open row.
	rec.IncomingHash = "aaa2"
	id2, err := st.RecordConflict(ctx, rec)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	// A different device on the same slot is a separate conflict.
	rec.ClientID = "dev-b"
	id3, err := st.RecordConflict(ctx, rec)
	require.NoError(t, err)
	assert.NotEqual(t, id1, id3)

	open, err := st.ListOpenConflicts(ctx, userID)
	require.NoError(t, err)
	require.Len(t, open, 2)
	byID := map[string]ConflictRow{}
	for _, c := range open {
		byID[c.ID] = c
	}
	assert.Equal(t, 2, byID[id1].Occurrences)
	assert.Equal(t, "aaa2", byID[id1].IncomingHash)
	assert.Equal(t, 1, byID[id3].Occurrences)

	n, err := st.CountOpenConflicts(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Another user cannot resolve someone else's conflict.
	assert.ErrorIs(t, st.ResolveConflict(ctx, otherID, id1, "kept_server"), ErrNotFound)

	// Manual resolution closes exactly one.
	require.NoError(t, st.ResolveConflict(ctx, userID, id1, "kept_server"))
	n, _ = st.CountOpenConflicts(ctx, userID)
	assert.Equal(t, 1, n)
	// Resolving again is ErrNotFound (already closed).
	assert.ErrorIs(t, st.ResolveConflict(ctx, userID, id1, "kept_server"), ErrNotFound)

	// A successful push supersedes the remaining open conflict on the slot.
	resolved, err := st.ResolveConflictsForSlot(ctx, userID, "g1", "pk1", "superseded")
	require.NoError(t, err)
	assert.Equal(t, 1, resolved)
	n, _ = st.CountOpenConflicts(ctx, userID)
	assert.Zero(t, n)

	// Re-recording after resolution opens a fresh row (not the resolved one).
	id4, err := st.RecordConflict(ctx, ConflictRecord{
		UserID: userID, GameID: "g1", PathKey: "pk1", ClientID: "dev-a", Kind: "if_hash",
	})
	require.NoError(t, err)
	assert.NotEqual(t, id1, id4)
	n, _ = st.CountOpenConflicts(ctx, userID)
	assert.Equal(t, 1, n)
}
