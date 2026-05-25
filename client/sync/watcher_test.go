package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcherExcludePatterns(t *testing.T) {
	w := &Watcher{ExcludePatterns: []string{"*.tmp", "*.bak", "thumb.db"}}
	assert.True(t, w.excludeMatch("/game/save.tmp"))
	assert.True(t, w.excludeMatch("/game/autosave.bak"))
	assert.False(t, w.excludeMatch("/game/save.dat"))
	assert.False(t, w.excludeMatch("/game/profile.bin"))
}

func TestWatcherDebounce(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(saveFile, []byte("content"), 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	resolver := paths.NewResolver()
	client, err := NewClient(srv.URL, "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	w.mu.Lock()
	w.pathMap[dir] = pathInfo{GameID: "g1", PathKey: "pk1"}
	w.mu.Unlock()

	ctx := context.Background()
	ev := fsnotify.Event{Name: saveFile, Op: fsnotify.Write}
	for i := 0; i < 5; i++ {
		w.handleEvent(ctx, ev)
	}

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, int32(1), pushCount.Load())
}

func TestWatcherDebounceSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.dat")
	fileB := filepath.Join(dir, "b.dat")
	require.NoError(t, os.WriteFile(fileA, []byte("a"), 0644))
	require.NoError(t, os.WriteFile(fileB, []byte("b"), 0644))

	var pushCount atomic.Int32
	var mu sync.Mutex
	pushed := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			mu.Lock()
			pushed[r.Header.Get("X-File-Path")] = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	resolver := paths.NewResolver()
	client, err := NewClient(srv.URL, "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	w.mu.Lock()
	w.pathMap[dir] = pathInfo{GameID: "g1", PathKey: "pk1"}
	w.mu.Unlock()

	ctx := context.Background()
	w.handleEvent(ctx, fsnotify.Event{Name: fileA, Op: fsnotify.Write})
	w.handleEvent(ctx, fsnotify.Event{Name: fileB, Op: fsnotify.Write})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, int32(2), pushCount.Load())
	mu.Lock()
	assert.True(t, pushed[fileA])
	assert.True(t, pushed[fileB])
	mu.Unlock()
}
