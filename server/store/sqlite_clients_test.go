package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_MultiClientList(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)

	token1, err := st.RegisterClient(ctx, userID, "laptop", "linux")
	require.NoError(t, err)
	token2, err := st.RegisterClient(ctx, userID, "desktop", "windows")
	require.NoError(t, err)
	assert.NotEmpty(t, token1)
	assert.NotEmpty(t, token2)
	assert.NotEqual(t, token1, token2)

	clients, err := st.ListClientsByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, clients, 2)
	names := map[string]bool{clients[0].Name: true, clients[1].Name: true}
	assert.True(t, names["laptop"])
	assert.True(t, names["desktop"])
}

func TestSQLite_RefreshClientToken(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	oldToken, err := st.RegisterClient(ctx, userID, "pc", "linux")
	require.NoError(t, err)

	newToken, err := st.RefreshClientToken(ctx, oldToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken)
	assert.NotEqual(t, oldToken, newToken)

	_, _, _, _, err = st.ClientByToken(ctx, oldToken)
	assert.Error(t, err)

	uid, _, name, osName, err := st.ClientByToken(ctx, newToken)
	require.NoError(t, err)
	assert.Equal(t, userID, uid)
	assert.Equal(t, "pc", name)
	assert.Equal(t, "linux", osName)
}

func TestSQLite_RegenerateClientToken(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	token, err := st.RegisterClient(ctx, userID, "pc", "linux")
	require.NoError(t, err)

	_, clientID, _, _, err := st.ClientByToken(ctx, token)
	require.NoError(t, err)

	require.NoError(t, st.RegenerateClientToken(ctx, clientID))

	_, _, _, _, err = st.ClientByToken(ctx, token)
	assert.Error(t, err, "old token should be invalid after regenerate")

	clients, err := st.ListClientsByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, clients, 1, "regenerate keeps the client row for re-login")
	assert.Equal(t, clientID, clients[0].ID)
}

func TestSQLite_RevokeClient(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	token, err := st.RegisterClient(ctx, userID, "pc", "linux")
	require.NoError(t, err)

	_, clientID, _, _, err := st.ClientByToken(ctx, token)
	require.NoError(t, err)

	require.NoError(t, st.RevokeClient(ctx, clientID))

	_, _, _, _, err = st.ClientByToken(ctx, token)
	assert.Error(t, err, "token should be invalid after revoke")

	clients, err := st.ListClientsByUserID(ctx, userID)
	require.NoError(t, err)
	assert.Empty(t, clients, "revoked client should not appear in list")

	all, err := st.ListAllClients(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)

	err = st.RevokeClient(ctx, clientID)
	assert.Error(t, err, "revoking again should fail")
}
