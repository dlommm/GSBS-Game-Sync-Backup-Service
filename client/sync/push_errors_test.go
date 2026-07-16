package sync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPushTestClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	ResetPushHashCacheForTest()
	c, err := NewClient(srvURL, "test-token", paths.NewResolver(), paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	return c
}

// A 413 (quota) push must make exactly ONE request, classify non-retryable,
// and fire OnQuotaError exactly once. Before 5.4 the "push: 413 …" message
// defeated the string-based status parser, so quota errors were retried 3x
// (three toasts) and then parked in the outbox for up to 7 days of 2-minute
// retry cycles.
func TestPush_QuotaIsNonRetryableSingleRequest(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"error":"quota exceeded"}`))
		}
	}))
	defer srv.Close()

	var quotaToasts atomic.Int32
	origQuota := OnQuotaError
	OnQuotaError = func(msg string) { quotaToasts.Add(1) }
	defer func() { OnQuotaError = origQuota }()

	c := newPushTestClient(t, srv.URL)
	err := c.Push(context.Background(), "g1", "pk1", "/tmp/save.dat", "save.dat", []byte("data"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrQuotaExceeded), "typed quota error: %v", err)
	assert.False(t, retry.IsRetryableError(err), "quota must be non-retryable")
	assert.Equal(t, int32(1), posts.Load(), "exactly one request — no retry storm")
	assert.Equal(t, int32(1), quotaToasts.Load(), "exactly one quota notification")
}

// A 409 (optimistic-concurrency conflict) push must make exactly ONE request,
// classify non-retryable, and record the conflict. Non-retryable also means
// the watcher will never enqueue it to the outbox.
func TestPush_ConflictIsNonRetryableAndRecorded(t *testing.T) {
	dir := t.TempDir()
	SetConflictsPathForTest(filepath.Join(dir, "conflicts.json"))
	defer SetConflictsPathForTest("")

	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"hash mismatch","current_hash":"srv-hash"}`))
		}
	}))
	defer srv.Close()

	c := newPushTestClient(t, srv.URL)
	err := c.Push(context.Background(), "g1", "pk1", "/tmp/save.dat", "save.dat", []byte("data"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConflict), "typed conflict error: %v", err)
	assert.False(t, retry.IsRetryableError(err), "conflict must be non-retryable")
	assert.Equal(t, int32(1), posts.Load())
	assert.Equal(t, 1, ConflictCount(), "conflict recorded")
}

// Token rotation must heal on EVERY rotation, not just the first: the old
// authRetried latch permanently disabled reloads after one success, wedging
// long-running installs on their second monthly rotation.
func TestPush_TokenReloadHealsRepeatedRotations(t *testing.T) {
	current := atomic.Value{}
	current.Store("token-1")
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		posts.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+current.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	reloaded := atomic.Value{}
	reloaded.Store("token-1")
	c := newPushTestClient(t, srv.URL)
	c.TokenReload = func() string { return reloaded.Load().(string) }

	// Rotation 1: server expects token-2; reload provides it → push heals.
	current.Store("token-2")
	reloaded.Store("token-2")
	require.NoError(t, c.Push(context.Background(), "g1", "pk1", "/f", "f", []byte("v1")))

	// Rotation 2: the same must work again (regression for the one-way latch).
	current.Store("token-3")
	reloaded.Store("token-3")
	require.NoError(t, c.Push(context.Background(), "g1", "pk1", "/f", "f", []byte("v2")))
}

// A permanent 401 on the summaries pull must not burn the retry budget:
// exactly one request (plus zero reload attempts without TokenReload).
func TestPullSummaries_PermanentUnauthorizedNoRetryStorm(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newPushTestClient(t, srv.URL)
	_, err := c.pullSummaries(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
	assert.False(t, retry.IsRetryableError(err))
	assert.Equal(t, int32(1), gets.Load(), "401 must not be retried as a transport error")
}

// SetToken updates the live client (the rotation ticker path).
func TestSetTokenUpdatesLiveClient(t *testing.T) {
	expect := atomic.Value{}
	expect.Store("token-a")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+expect.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := newPushTestClient(t, srv.URL)
	c.SetToken("token-a")
	require.NoError(t, c.Push(context.Background(), "g1", "pk1", "/f", "f", []byte("v1")))
	expect.Store("token-b")
	c.SetToken("token-b")
	require.NoError(t, c.Push(context.Background(), "g1", "pk1", "/f", "f", []byte("v2")))
}
