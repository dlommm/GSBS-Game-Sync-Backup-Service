package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type pushHashCacheFile struct {
	Slots map[string]string `json:"slots"` // gameID\x00pathKey -> wire hash
}

var (
	hashCacheMu      sync.Mutex
	hashCacheLoaded  bool
	hashCachePersist map[string]string
	hashCacheDirty   bool
)

var pushHashCachePathOverride string

// SetPushHashCachePathForTest overrides the on-disk push dedup cache path (tests only).
func SetPushHashCachePathForTest(path string) {
	hashCacheMu.Lock()
	pushHashCachePathOverride = path
	hashCacheLoaded = false
	hashCachePersist = nil
	hashCacheMu.Unlock()
}

// ResetPushHashCacheForTest clears the in-memory push dedup cache (tests only).
func ResetPushHashCacheForTest() {
	hashCacheMu.Lock()
	hashCachePersist = make(map[string]string)
	hashCacheLoaded = true
	hashCacheMu.Unlock()
}

func pushHashCachePath() string {
	if pushHashCachePathOverride != "" {
		return pushHashCachePathOverride
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "push_hash_cache.json")
}

func loadPushHashCache() map[string]string {
	hashCacheMu.Lock()
	defer hashCacheMu.Unlock()
	if hashCacheLoaded {
		return hashCachePersist
	}
	hashCacheLoaded = true
	data, err := os.ReadFile(pushHashCachePath())
	if err != nil {
		hashCachePersist = make(map[string]string)
		return hashCachePersist
	}
	var f pushHashCacheFile
	if json.Unmarshal(data, &f) != nil || f.Slots == nil {
		hashCachePersist = make(map[string]string)
		return hashCachePersist
	}
	hashCachePersist = f.Slots
	return hashCachePersist
}

// MaybeEvictStaleHashCache checks whether the push hash cache contains entries
// whose composite slot key (gameID + "\x00" + pathKey) is absent from knownKeys.
// When stale entries are found the entire cache is cleared so that files will be
// re-hashed and compared against the server after a path_key scheme migration.
// Returns true when the cache was cleared.
//
// Clearing the cache does NOT cause an immediate re-upload storm: the watcher is
// event-driven and only consults the cache when a file-change event arrives.
// Files that have not been modified since the last push will not produce a watcher
// event until the game next writes to them, so only genuinely changed files are
// re-uploaded in the cycle following the eviction.
func MaybeEvictStaleHashCache(knownKeys map[string]bool) bool {
	// Ensure the on-disk cache is loaded into memory before we inspect it.
	loadPushHashCache()

	hashCacheMu.Lock()
	stale := false
	for k := range hashCachePersist {
		if !knownKeys[k] {
			stale = true
			break
		}
	}
	if !stale {
		hashCacheMu.Unlock()
		return false
	}
	// Clear in-memory atomically.
	hashCachePersist = make(map[string]string)
	hashCacheLoaded = true
	hashCacheMu.Unlock()

	logSyncInfo("push_cache_evict", "reason", "path_key scheme updated: clearing local push cache for re-sync")

	// Persist the empty map immediately so the eviction survives a process restart.
	writePushHashCacheFile(make(map[string]string))
	return true
}

// markHashCacheDirty marks the in-memory cache as needing a disk flush.
// The actual write is deferred to the flusher goroutine (max once per 5 s).
func markHashCacheDirty(m map[string]string) {
	hashCacheMu.Lock()
	hashCachePersist = m
	hashCacheLoaded = true
	hashCacheDirty = true
	hashCacheMu.Unlock()
}

// flushHashCacheIfDirty writes the cache to disk if it has been modified
// since the last flush. Safe to call from any goroutine.
func flushHashCacheIfDirty() {
	hashCacheMu.Lock()
	if !hashCacheDirty || hashCachePersist == nil {
		hashCacheMu.Unlock()
		return
	}
	snapshot := make(map[string]string, len(hashCachePersist))
	for k, v := range hashCachePersist {
		snapshot[k] = v
	}
	hashCacheDirty = false
	hashCacheMu.Unlock()

	writePushHashCacheFile(snapshot)
}

// writePushHashCacheFile atomically writes the cache map to disk.
func writePushHashCacheFile(m map[string]string) {
	dir := filepath.Dir(pushHashCachePath())
	_ = os.MkdirAll(dir, 0755)
	f := pushHashCacheFile{Slots: m}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	tmp := pushHashCachePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, pushHashCachePath())
}

// StartHashCacheFlusher starts a background goroutine that persists the push
// hash cache at most once every 5 seconds. On context cancellation it performs
// a final flush so the cache survives graceful shutdown.
func StartHashCacheFlusher(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				flushHashCacheIfDirty()
				return
			case <-t.C:
				flushHashCacheIfDirty()
			}
		}
	}()
}
