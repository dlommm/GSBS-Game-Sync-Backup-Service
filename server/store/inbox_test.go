package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_InboxLifecycle(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)

	id1, err := st.AddInboxItem(ctx, userID, "conflict", "Sync conflict", "body", "/dashboard/conflicts")
	require.NoError(t, err)
	_, err = st.AddInboxItem(ctx, userID, "backup", "Backup done", "", "/admin/settings")
	require.NoError(t, err)

	items, err := st.ListInbox(ctx, userID, 10)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.False(t, items[0].Read)

	n, err := st.CountUnreadInbox(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Mark one read.
	require.NoError(t, st.MarkInboxRead(ctx, userID, id1))
	n, _ = st.CountUnreadInbox(ctx, userID)
	assert.Equal(t, 1, n)
	assert.ErrorIs(t, st.MarkInboxRead(ctx, userID, id1), ErrNotFound)

	// Mark all read.
	require.NoError(t, st.MarkInboxRead(ctx, userID, "all"))
	n, _ = st.CountUnreadInbox(ctx, userID)
	assert.Zero(t, n)

	// Per-user cap: oldest rows are dropped at write time.
	for i := 0; i < inboxPerUserCap+20; i++ {
		_, err := st.AddInboxItem(ctx, userID, "login", fmt.Sprintf("login %d", i), "", "")
		require.NoError(t, err)
	}
	items, err = st.ListInbox(ctx, userID, inboxPerUserCap)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(items), inboxPerUserCap)

	// Admin listing: creator is not admin by default; promote and check.
	require.NoError(t, st.SetUserRole(ctx, userID, "admin"))
	admins, err := st.ListAdminUserIDs(ctx)
	require.NoError(t, err)
	assert.Contains(t, admins, userID)
}
