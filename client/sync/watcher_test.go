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
	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSyncClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	ResetPushHashCacheForTest()
	resolver := paths.NewResolver()
	client, err := NewClient(srvURL, "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	return client
}

func TestWatcherExcludePatterns(t *testing.T) {
	w := &Watcher{ExcludePatterns: []string{"*.tmp", "*.bak", "thumb.db"}}
	assert.True(t, w.excludeMatch("/game/save.tmp", "save.tmp"))
	assert.True(t, w.excludeMatch("/game/autosave.bak", "autosave.bak"))
	assert.False(t, w.excludeMatch("/game/save.dat", "save.dat"))
	assert.False(t, w.excludeMatch("/game/profile.bin", "profile.bin"))
}

func TestWatcherExcludePatterns_RelativePathGlob(t *testing.T) {
	w := &Watcher{ExcludePatterns: []string{"cache/*", "nested/temp/*.tmp"}}
	assert.True(t, w.excludeMatch("/game/saves/cache/foo.dat", "cache/foo.dat"))
	assert.True(t, w.excludeMatch("/game/saves/nested/temp/x.tmp", "nested/temp/x.tmp"))
	assert.False(t, w.excludeMatch("/game/saves/save.dat", "save.dat"))
}

func TestMatchInclude_PatternFilter(t *testing.T) {
	assert.True(t, matchInclude("save.sav", []string{"*.sav"}, false))
	assert.False(t, matchInclude("save.tmp", []string{"*.sav"}, false))
	assert.True(t, matchInclude("nested/save.sav", []string{"*.sav"}, false))
	assert.True(t, matchInclude("anything.dat", nil, true))
}

func TestPushPathKey(t *testing.T) {
	ruleKey := "abc123"
	assert.Equal(t, ruleKey, pushPathKey(ruleKey, "save.sav", []string{"save.sav"}, false))
	assert.Equal(t, saverule.PathKeyForFile(ruleKey, "a.sav"), pushPathKey(ruleKey, "a.sav", []string{"*.sav"}, false))
	assert.Equal(t, saverule.PathKeyForFile(ruleKey, "a.sav"), pushPathKey(ruleKey, "a.sav", []string{"*.sav", "profile.dat"}, false))
	assert.Equal(t, saverule.PathKeyForFile(ruleKey, "a.sav"), pushPathKey(ruleKey, "a.sav", nil, true))
}

func TestWatcherAddPaths_Attachment(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "saves")
	require.NoError(t, os.MkdirAll(sub, 0755))

	resolver := paths.NewResolver()
	client, err := NewClient("http://127.0.0.1:1", "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	err = w.AddPaths([]WatchPath{{
		GameID:          "g1",
		RuleKey:         "rk1",
		Directory:       dir,
		IncludePatterns: []string{"*.dat"},
		SyncAll:         false,
	}})
	require.NoError(t, err)

	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.pathMap[dir]
	require.True(t, ok, "expected watch on resolved directory")
	require.Len(t, entry.rules, 1)
	assert.Equal(t, "g1", entry.rules[0].GameID)
	assert.Equal(t, "rk1", entry.rules[0].RuleKey)
}

func TestWatcherPatternFilter_IgnoresNonMatching(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	ignoredFile := filepath.Join(dir, "readme.txt")
	require.NoError(t, os.WriteFile(saveFile, []byte("save"), 0644))
	require.NoError(t, os.WriteFile(ignoredFile, []byte("txt"), 0644))

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := testSyncClient(t, srv.URL)
	resolver := paths.NewResolver()

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	require.NoError(t, w.AddPaths([]WatchPath{{
		GameID:          "g1",
		RuleKey:         "rk1",
		Directory:       dir,
		IncludePatterns: []string{"*.dat"},
	}}))

	ctx := context.Background()
	w.handleEvent(ctx, fsnotify.Event{Name: ignoredFile, Op: fsnotify.Write})
	w.handleEvent(ctx, fsnotify.Event{Name: saveFile, Op: fsnotify.Write})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, int32(1), pushCount.Load())
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

	client := testSyncClient(t, srv.URL)
	resolver := paths.NewResolver()

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	require.NoError(t, w.AddPaths([]WatchPath{{
		GameID:    "g1",
		RuleKey:   "pk1",
		Directory: dir,
		SyncAll:   true,
	}}))

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

	client := testSyncClient(t, srv.URL)
	resolver := paths.NewResolver()

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	require.NoError(t, w.AddPaths([]WatchPath{{
		GameID:    "g1",
		RuleKey:   "pk1",
		Directory: dir,
		SyncAll:   true,
	}}))

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

func TestWatcherPushRelativePathHeader(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(saveFile, []byte("unique-content-for-rel-header-test"), 0644))

	var relHeader atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			relHeader.Store(r.Header.Get("X-Relative-Path"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := testSyncClient(t, srv.URL)
	resolver := paths.NewResolver()

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(50 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	require.NoError(t, w.AddPaths([]WatchPath{{
		GameID:    "g1",
		RuleKey:   "pk1",
		Directory: dir,
		SyncAll:   true,
	}}))

	ctx := context.Background()
	w.handleEvent(ctx, fsnotify.Event{Name: saveFile, Op: fsnotify.Write})
	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, "save.dat", relHeader.Load())
}
