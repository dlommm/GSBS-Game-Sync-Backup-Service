package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_GameSessions(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)

	now := time.Now().UTC()
	id, err := st.RecordGameSession(ctx, userID, "dev-1", "g1",
		now.Add(-2*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339))
	require.NoError(t, err)
	require.NotEmpty(t, id)

	sessions, err := st.ListGameSessions(ctx, userID, "g1", 10)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "g1", sessions[0].GameID)
	assert.InDelta(t, 2*time.Hour, sessions[0].Duration(), float64(time.Minute))

	// Other games / users see nothing.
	other, _ := st.ListGameSessions(ctx, userID, "g2", 10)
	assert.Empty(t, other)

	// Per-slot cap: oldest sessions are trimmed.
	for i := 0; i < gameSessionsPerSlotCap+10; i++ {
		end := now.Add(time.Duration(i) * time.Minute)
		_, err := st.RecordGameSession(ctx, userID, "dev-1", "g1",
			end.Add(-10*time.Minute).Format(time.RFC3339), end.Format(time.RFC3339))
		require.NoError(t, err, fmt.Sprintf("insert %d", i))
	}
	sessions, err = st.ListGameSessions(ctx, userID, "g1", gameSessionsPerSlotCap)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(sessions), gameSessionsPerSlotCap)
}
