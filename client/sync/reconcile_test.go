package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReconcileTestClient creates a Client pointed at srvURL for reconcile tests.
func newReconcileTestClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	ResetPushHashCacheForTest()
	resolver := paths.NewResolver()
	c, err := NewClient(srvURL, "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	return c
}

// Nil server hashes mean the server state is UNKNOWN (fetch failed):
// reconcile must refuse to upload anything rather than risk overwriting
// newer server saves. An explicit empty map is a fresh account and uploads.
func TestReconcileLocalToServer_NilServerHashes_RefusesBlindUpload(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save1.sav"), []byte("data1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save2.sav"), []byte("data2"), 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:  "g1",
		RuleKey: "rk1",
		// Directory is already resolved (absolute)
		Directory: dir,
		SyncAll:   true,
	}}

	n := ReconcileLocalToServer(context.Background(), wps, client, nil)
	assert.Equal(t, 0, n, "nil map (unknown server state) must upload nothing")
	assert.Equal(t, int32(0), pushCount.Load())

	// Empty non-nil map = fresh account: everything uploads.
	n = ReconcileLocalToServer(context.Background(), wps, client, map[string]ServerSaveInfo{})
	assert.Equal(t, 2, n)
	assert.Equal(t, int32(2), pushCount.Load())
}

func TestReconcileLocalToServer_MatchingHash_SkipsAll(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello-save")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.sav"), content, 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	// Compute the expected wire hash for this content (no encryption).
	wireHash := FileHash(content)

	wps := []WatchPath{{
		GameID:          "g1",
		RuleKey:         "rk1",
		Directory:       dir,
		IncludePatterns: []string{"*.sav"},
	}}

	// pathKey for a single exact-file pattern is the ruleKey itself.
	pathKey := pushPathKey("rk1", "save.sav", []string{"*.sav"}, false)
	serverHashes := map[string]ServerSaveInfo{
		"g1\x00" + pathKey: {Hash: wireHash},
	}

	n := ReconcileLocalToServer(context.Background(), wps, client, serverHashes)
	assert.Equal(t, 0, n)
	assert.Equal(t, int32(0), pushCount.Load())
}

func TestReconcileLocalToServer_MissingSlotKey_Uploads(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.sav"), []byte("new-save"), 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:    "g1",
		RuleKey:   "rk1",
		Directory: dir,
		SyncAll:   true,
	}}

	// Server has saves for a completely different game — our slot key is absent.
	serverHashes := map[string]ServerSaveInfo{
		"other-game\x00some-path": {Hash: "deadbeef"},
	}

	n := ReconcileLocalToServer(context.Background(), wps, client, serverHashes)
	assert.Equal(t, 1, n)
	assert.Equal(t, int32(1), pushCount.Load())
}

func TestReconcileLocalToServer_ServerHasDifferentHash_Skips(t *testing.T) {
	dir := t.TempDir()
	content := []byte("local-content")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.sav"), content, 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:    "g1",
		RuleKey:   "rk1",
		Directory: dir,
		SyncAll:   true,
	}}

	pathKey := pushPathKey("rk1", "save.sav", nil, true)
	// Server has the slot but with a different hash — pull/conflict logic should handle.
	serverHashes := map[string]ServerSaveInfo{
		"g1\x00" + pathKey: {Hash: "differenthash000"},
	}

	n := ReconcileLocalToServer(context.Background(), wps, client, serverHashes)
	assert.Equal(t, 0, n)
	assert.Equal(t, int32(0), pushCount.Load())
}

func TestReconcileLocalToServer_SkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.sav"), []byte{}, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.sav"), []byte("data"), 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:    "g1",
		RuleKey:   "rk1",
		Directory: dir,
		SyncAll:   true,
	}}

	n := ReconcileLocalToServer(context.Background(), wps, client, map[string]ServerSaveInfo{})
	assert.Equal(t, 1, n)
	assert.Equal(t, int32(1), pushCount.Load())
}

func TestReconcileLocalToServer_ContextCancelled_StopsEarly(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "s"+string(rune('0'+i))+".sav"), []byte("data"), 0644))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:    "g1",
		RuleKey:   "rk1",
		Directory: dir,
		SyncAll:   true,
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	n := ReconcileLocalToServer(ctx, wps, client, map[string]ServerSaveInfo{})
	// With the context already cancelled the outer loop exits immediately at the select.
	assert.Equal(t, 0, n)
}

// GSBS's own artifacts (.gsbs.bak pull backups, .gsbs.tmp atomic-write temps)
// must never be uploaded, even under SyncAll with no user exclude patterns —
// regression test for the .gsbs.bak re-upload bug (backups written into the
// watched directory came back as new server slots).
func TestReconcileLocalToServer_SkipsGSBSArtifacts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.dat"), []byte("data"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.dat.gsbs.bak"), []byte("backup"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "save.dat.gsbs.tmp"), []byte("temp"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "slot.sav.gsbs.bak"), []byte("backup"), 0644))

	var pushedPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushedPaths = append(pushedPaths, r.Header.Get("X-Relative-Path"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{
		GameID:    "g1",
		RuleKey:   "rk1",
		Directory: dir,
		SyncAll:   true,
		Recursive: true,
	}}

	n := ReconcileLocalToServer(context.Background(), wps, client, map[string]ServerSaveInfo{})
	assert.Equal(t, 1, n, "only the real save uploads")
	require.Len(t, pushedPaths, 1)
	assert.Equal(t, "save.dat", pushedPaths[0])
}

// A local save that is definitively NEWER than the server's different copy
// (e.g. its failed push aged out of the outbox while offline) must upload as
// a compare-and-swap against the observed server hash; ambiguous timestamps
// must keep skipping.
func TestReconcileLocalToServer_LocalNewer_CASPush(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "save.sav")
	require.NoError(t, os.WriteFile(savePath, []byte("local-newer-content"), 0644))

	const serverHash = "server-old-hash"
	var pushCount atomic.Int32
	var gotIfHash atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			gotIfHash.Store(r.Header.Get("X-GSBS-If-Hash"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := newReconcileTestClient(t, srv.URL)
	wps := []WatchPath{{GameID: "g1", RuleKey: "rk1", Directory: dir, SyncAll: true}}
	pathKey := pushPathKey("rk1", "save.sav", nil, true)

	// Server copy is a day older than the local file — definitively stale.
	state := map[string]ServerSaveInfo{
		"g1\x00" + pathKey: {Hash: serverHash, UpdatedAt: time.Now().Add(-24 * time.Hour)},
	}
	n := ReconcileLocalToServer(context.Background(), wps, client, state)
	assert.Equal(t, 1, n, "definitively newer local save uploads")
	assert.Equal(t, int32(1), pushCount.Load())
	assert.Equal(t, serverHash, gotIfHash.Load(), "push must CAS against the observed server hash")

	// Within the skew window: ambiguous — must skip, not push.
	ResetPushHashCacheForTest()
	client2 := newReconcileTestClient(t, srv.URL)
	state["g1\x00"+pathKey] = ServerSaveInfo{Hash: serverHash, UpdatedAt: time.Now().Add(-30 * time.Second)}
	pushCount.Store(0)
	n = ReconcileLocalToServer(context.Background(), wps, client2, state)
	assert.Equal(t, 0, n, "ambiguous timestamps skip")
	assert.Equal(t, int32(0), pushCount.Load())
}
