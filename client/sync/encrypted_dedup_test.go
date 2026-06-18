package sync

import (
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/require"
)

// TestEncryptedDedup_ChangeHashIsStable is the regression test for the bug where
// encrypted saves were never detected as unchanged: AES-GCM uses a random
// salt+nonce per call, so hashing the encrypted wire bytes produced a different
// value every cycle and defeated both the client push-skip cache and the server
// "unchanged" short-circuit. ContentChangeHash must hash plaintext, so identical
// content yields a stable key even though the ciphertext differs each time.
func TestEncryptedDedup_ChangeHashIsStable(t *testing.T) {
	c, err := NewClient("http://127.0.0.1:1", "tok", nil, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	c.SetEncryption(true, "correct-horse-battery-staple")

	plaintext := []byte("level=12;gold=999;name=hero")

	// The encrypted wire bytes differ on every encryption (random salt+nonce).
	wire1, enc1, err := c.encodeContent(plaintext)
	require.NoError(t, err)
	wire2, _, err := c.encodeContent(plaintext)
	require.NoError(t, err)
	require.True(t, enc1, "content should be encrypted")
	require.NotEqual(t, string(wire1), string(wire2), "ciphertext must be non-deterministic")

	// ...but the change hash is stable across encryptions.
	h1, err := c.ContentChangeHash(plaintext)
	require.NoError(t, err)
	h2, err := c.ContentChangeHash(plaintext)
	require.NoError(t, err)
	require.Equal(t, h1, h2, "change hash must be stable for identical plaintext")

	// And it equals the plaintext hash (so it matches what the server records
	// and what reconcile/pull compare against).
	require.Equal(t, FileHash(plaintext), h1)

	// After a push is recorded, an unchanged re-push for the same slot is skipped.
	require.False(t, c.ShouldSkipPush("game1", "0", h1))
	c.markPushed("game1", "0", h1)
	require.True(t, c.ShouldSkipPush("game1", "0", h1), "unchanged encrypted save must skip re-push")

	// Changed content yields a different key and is not skipped.
	h3, err := c.ContentChangeHash([]byte("level=13;gold=1000;name=hero"))
	require.NoError(t, err)
	require.False(t, c.ShouldSkipPush("game1", "0", h3))
}
