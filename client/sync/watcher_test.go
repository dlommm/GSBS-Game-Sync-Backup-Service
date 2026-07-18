package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// The write-stability gate must never leave a mutating file in a torn state
// on the server: whatever lands last is the settled final content, and a
// constantly-changing file still gets pushed eventually (no starvation).
func TestWatcherStabilityGate_PushesSettledContent(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(saveFile, []byte("v0"), 0644))

	var lastBody atomic.Value
	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saves" && r.Method == http.MethodPost {
			b := make([]byte, r.ContentLength)
			_, _ = io.ReadFull(r.Body, b)
			lastBody.Store(string(b))
			pushCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := testSyncClient(t, srv.URL)
	w, err := NewWatcher(paths.NewResolver(), paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	SetDebounceDelayForTest(30 * time.Millisecond)
	t.Cleanup(func() { SetDebounceDelayForTest(0) })

	require.NoError(t, w.AddPaths([]WatchPath{{
		GameID: "g1", RuleKey: "pk1", Directory: dir, SyncAll: true,
	}}))

	ctx := context.Background()
	ev := fsnotify.Event{Name: saveFile, Op: fsnotify.Write}
	// Simulate a game writing in bursts: sizes change on every write so the
	// gate's re-stat sees movement whenever a read races a write.
	for i := 0; i < 6; i++ {
		require.NoError(t, os.WriteFile(saveFile, []byte(strings.Repeat("x", 10+i*7)), 0644))
		w.handleEvent(ctx, ev)
		time.Sleep(15 * time.Millisecond)
	}
	final := "final-settled-content"
	require.NoError(t, os.WriteFile(saveFile, []byte(final), 0644))
	w.handleEvent(ctx, ev)

	require.Eventually(t, func() bool {
		v, _ := lastBody.Load().(string)
		return pushCount.Load() >= 1 && v == final
	}, 3*time.Second, 25*time.Millisecond, "final settled content must be the last push")
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

func TestIsFileLockError(t *testing.T) {
	cases := []struct {
		name    string
		errMsg  string
		expects bool
	}{
		{"nil error", "", false},
		{"sharing violation", "The process cannot access the file because it is being used by another process: sharing violation", true},
		{"process cannot access", "open C:\\foo: The process cannot access the file", true},
		{"being used by another", "file is being used by another process", true},
		{"unrelated error", "no such file or directory", false},
		{"permission denied", "permission denied", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = fmt.Errorf("%s", tc.errMsg)
			}
			assert.Equal(t, tc.expects, isFileLockError(err))
		})
	}
}

func TestFindWatchDir_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	resolver := paths.NewResolver()
	client, err := NewClient("http://127.0.0.1:1", "test-token", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)

	w, err := NewWatcher(resolver, paths.CurrentOS(), client)
	require.NoError(t, err)
	defer w.Close()

	// Register the directory with its canonical casing.
	canonical := dir
	entry := pathEntry{rules: []watchRuleInfo{{GameID: "g1", RuleKey: "rk1", SyncAll: true}}}
	w.mu.Lock()
	w.pathMap[canonical] = entry
	w.mu.Unlock()

	// Look up using the same path — exact match.
	foundDir, _, ok := w.findWatchDir(filepath.Join(canonical, "save.dat"))
	require.True(t, ok, "expected exact match to succeed")
	assert.Equal(t, canonical, foundDir)

	// Look up using uppercase version — should find via case-insensitive fallback.
	upper := strings.ToUpper(canonical)
	if upper == canonical {
		t.Skip("filesystem path has no uppercase letters to test case-insensitive lookup")
	}
	foundDir2, _, ok2 := w.findWatchDir(filepath.Join(upper, "save.dat"))
	require.True(t, ok2, "expected case-insensitive match to succeed")
	assert.Equal(t, canonical, foundDir2, "should return the canonical registered path")
}

func TestEffectiveMaxSaveBytes(t *testing.T) {
	w := &Watcher{}
	if got := w.effectiveMaxSaveBytes(); got != DefaultMaxSaveBytes {
		t.Fatalf("default = %d, want %d", got, DefaultMaxSaveBytes)
	}
	w.MaxSaveBytes = 5 << 20
	if got := w.effectiveMaxSaveBytes(); got != 5<<20 {
		t.Fatalf("override = %d, want %d", got, 5<<20)
	}
}
