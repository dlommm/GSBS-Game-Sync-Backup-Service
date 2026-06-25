package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLite_SaveVersionsListRestore(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "u", "h")
	require.NoError(t, err)

	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("v1"), nil)
	require.NoError(t, err)
	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("v2"), nil)
	require.NoError(t, err)
	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("v3"), nil)
	require.NoError(t, err)

	versions, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	assert.Equal(t, 3, versions[0].Version)
	assert.Equal(t, 1, versions[2].Version)

	require.NoError(t, st.RestoreSaveVersion(ctx, userID, "g1", "pk1", 1))
	blob, err := st.GetSave(ctx, userID, "g1", "pk1")
	require.NoError(t, err)
	require.NotNil(t, blob)
	assert.Equal(t, "v1", string(blob.Content))

	versions, err = st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	assert.Len(t, versions, 4)
	assert.Equal(t, 4, versions[0].Version)
}

func TestSQLite_SaveVersionChangeMeta(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	token, err := st.RegisterClient(ctx, userID, "Gaming PC", "windows")
	require.NoError(t, err)
	_, clientID, _, _, err := st.ClientByToken(ctx, token)
	require.NoError(t, err)

	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("12345"), &SaveMeta{ClientID: clientID}) // v1: 5 bytes
	require.NoError(t, err)
	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("123456789012"), &SaveMeta{ClientID: clientID}) // v2: 12 bytes (+7)
	require.NoError(t, err)

	versions, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	// newest first
	assert.Equal(t, int64(7), versions[0].ChangeBytes)
	assert.Equal(t, "Gaming PC", versions[0].ClientName)
	assert.Equal(t, int64(5), versions[1].ChangeBytes) // first version: full size

	row, ok, err := st.LargestChangeForGame(ctx, userID, "g1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(7), row.ChangeBytes)
	assert.Equal(t, "Gaming PC", row.ClientName)
}

func TestSQLite_SaveVersionRetention(t *testing.T) {
	t.Setenv("GSBS_SAVE_VERSION_RETENTION", "5")
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	for i := 0; i < 7; i++ {
		_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte(fmt.Sprintf("content-%d", i)), nil)
		require.NoError(t, err)
	}

	versions, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	assert.Len(t, versions, 5)
	assert.Equal(t, 7, versions[0].Version)
	assert.Equal(t, 3, versions[4].Version)
}

func TestSQLite_SaveVersionHashDedup(t *testing.T) {
	st, err := NewSQLite(":memory:")
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	content := []byte("same-content")
	meta := &SaveMeta{ContentHash: hashContent(content)}

	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", content, meta)
	require.NoError(t, err)
	skipped, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", content, meta)
	require.NoError(t, err)
	assert.True(t, skipped)

	versions, err := st.ListSaveVersions(ctx, userID, "g1", "pk1", 10)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
}
