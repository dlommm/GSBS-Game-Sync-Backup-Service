// Package sync provides the file watcher and sync client.
//
// On Windows, the watcher uses fsnotify (ReadDirectoryChangesW). Under very heavy
// write load, the system buffer can overflow and some events may be dropped; the
// next periodic pull will still sync state. Reduce watch scope or write frequency
// if you need every change under load.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/gsbs/gsbs/pkg/saverule"
)

const maxConsecutiveErrors = 5

var debounceDelay = 2 * time.Second

// SetDebounceDelayForTest overrides debounceDelay (tests only). Pass 0 to reset to default.
func SetDebounceDelayForTest(d time.Duration) {
	if d <= 0 {
		debounceDelay = 2 * time.Second
		return
	}
	debounceDelay = d
}

var errWatcherClosed = errors.New("watcher: events channel closed")

// WatchPath describes a save rule directory to watch (from manifest or config).
type WatchPath struct {
	GameID          string
	RuleKey         string
	Directory       string // OS-specific template; resolved for current OS
	IncludePatterns []string
	Recursive       bool
	SyncAll         bool
}

type watchRuleInfo struct {
	GameID          string
	RuleKey         string
	IncludePatterns []string
	SyncAll         bool
	Recursive       bool
}

type pathEntry struct {
	rules     []watchRuleInfo
	recursive bool
}

// Watcher watches local directories and uploads matching file changes to the server.
type Watcher struct {
	resolver        *paths.Resolver
	currentOS       paths.OS
	client          *Client
	fw              *fsnotify.Watcher
	mu              sync.Mutex
	pathMap         map[string]pathEntry
	timers          map[string]*time.Timer
	consecErr       int
	IsPaused        func() bool
	ExcludePatterns []string
	Verbose         bool
}

// NewWatcher creates a file watcher that uploads on change.
func NewWatcher(resolver *paths.Resolver, currentOS paths.OS, client *Client) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		resolver:  resolver,
		currentOS: currentOS,
		client:    client,
		fw:        fw,
		pathMap:   make(map[string]pathEntry),
		timers:    make(map[string]*time.Timer),
	}, nil
}

func (w *Watcher) resolvedWatchDirs(watchPaths []WatchPath) map[string]pathEntry {
	out := make(map[string]pathEntry)
	for _, wp := range watchPaths {
		if strings.TrimSpace(wp.Directory) == "" {
			continue
		}
		rule := watchRuleInfo{
			GameID:          wp.GameID,
			RuleKey:         wp.RuleKey,
			IncludePatterns: append([]string(nil), wp.IncludePatterns...),
			SyncAll:         wp.SyncAll,
			Recursive:       wp.Recursive,
		}
		for _, abs := range w.resolver.ResolveAll(wp.Directory, w.currentOS) {
			if abs == "" {
				continue
			}
			info, err := os.Stat(abs)
			if err != nil {
				log.Printf("directory missing: %s (game=%s)", abs, wp.GameID)
				continue
			}
			if !info.IsDir() {
				abs = filepath.Dir(abs)
				info, err = os.Stat(abs)
				if err != nil || !info.IsDir() {
					log.Printf("directory missing: %s (game=%s)", abs, wp.GameID)
					continue
				}
			}
			dirs := []string{abs}
			if wp.Recursive {
				_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
					if walkErr != nil {
						return nil
					}
					if d.IsDir() && path != abs {
						dirs = append(dirs, path)
					}
					return nil
				})
			}
			for _, dir := range dirs {
				entry := out[dir]
				entry.rules = appendRule(entry.rules, rule)
				if wp.Recursive {
					entry.recursive = true
				}
				out[dir] = entry
			}
		}
	}
	return out
}

func appendRule(rules []watchRuleInfo, rule watchRuleInfo) []watchRuleInfo {
	for _, r := range rules {
		if r.GameID == rule.GameID && r.RuleKey == rule.RuleKey {
			return rules
		}
	}
	return append(rules, rule)
}

func (w *Watcher) attachDir(dir string, entry pathEntry) {
	if _, ok := w.pathMap[dir]; ok {
		w.pathMap[dir] = mergePathEntry(w.pathMap[dir], entry)
		return
	}
	if err := w.fw.Add(dir); err != nil {
		log.Printf("watcher rejected: %s: %v", dir, err)
		return
	}
	w.pathMap[dir] = entry
	log.Printf("watcher attached: %s", dir)
}

func mergePathEntry(a, b pathEntry) pathEntry {
	out := a
	out.recursive = a.recursive || b.recursive
	for _, rule := range b.rules {
		out.rules = appendRule(out.rules, rule)
	}
	return out
}

// AddPaths registers directory templates for watching. Resolves templates for current OS and only adds if directory exists.
func (w *Watcher) AddPaths(watchPaths []WatchPath) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := w.resolvedWatchDirs(watchPaths)
	for dir, entry := range want {
		if existing, ok := w.pathMap[dir]; ok {
			w.pathMap[dir] = mergePathEntry(existing, entry)
			continue
		}
		w.attachDir(dir, entry)
	}
	for dir := range want {
		if _, ok := w.pathMap[dir]; ok {
			log.Printf("watch path added: %s", dir)
		}
	}
	log.Printf("watching %d directories", len(w.pathMap))
	return nil
}

// RemoveStalePaths removes fsnotify watches for directories no longer in watchPaths.
func (w *Watcher) RemoveStalePaths(watchPaths []WatchPath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := w.resolvedWatchDirs(watchPaths)
	for dir := range w.pathMap {
		if _, ok := want[dir]; ok {
			continue
		}
		_ = w.fw.Remove(dir)
		delete(w.pathMap, dir)
		log.Printf("watch path skipped (removed): %s", dir)
	}
}

// Recreate closes the current fsnotify watcher and opens a new one, re-adding all paths.
func (w *Watcher) Recreate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	oldPaths := make(map[string]pathEntry, len(w.pathMap))
	for k, v := range w.pathMap {
		oldPaths[k] = v
	}
	if w.fw != nil {
		_ = w.fw.Close()
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.fw = fw
	w.pathMap = make(map[string]pathEntry)
	w.consecErr = 0
	for dir, entry := range oldPaths {
		if _, err := os.Stat(dir); err != nil {
			log.Printf("directory missing: %s", dir)
			continue
		}
		w.attachDir(dir, entry)
	}
	log.Printf("watch recreated: %d directories", len(w.pathMap))
	return nil
}

// Run starts the watch loop. Returns a retriable error if the fsnotify channel closes.
func (w *Watcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.fw.Events:
			if !ok {
				return errWatcherClosed
			}
			w.consecErr = 0
			w.handleEvent(ctx, ev)
		case err, ok := <-w.fw.Errors:
			if !ok {
				return errWatcherClosed
			}
			if err != nil {
				w.consecErr++
				log.Printf("watcher error: %v (consecutive=%d)", err, w.consecErr)
				if w.consecErr >= maxConsecutiveErrors {
					if recErr := w.Recreate(); recErr != nil {
						return fmt.Errorf("watcher recreate: %w", recErr)
					}
				}
			}
		}
	}
}

func (w *Watcher) findWatchDir(filePath string) (watchDir string, entry pathEntry, ok bool) {
	dir := filepath.Dir(filePath)
	for {
		w.mu.Lock()
		entry, found := w.pathMap[dir]
		w.mu.Unlock()
		if found {
			return dir, entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", pathEntry{}, false
		}
		dir = parent
	}
}

func (w *Watcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			w.maybeAttachRecursiveSubdir(ev.Name)
		}
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return
	}
	info, err := os.Stat(ev.Name)
	if err != nil || info.IsDir() {
		return
	}

	watchDir, entry, found := w.findWatchDir(ev.Name)
	if !found {
		return
	}

	relPath, err := filepath.Rel(watchDir, ev.Name)
	if err != nil {
		return
	}
	relPath = filepath.ToSlash(relPath)

	var matched *watchRuleInfo
	for i := range entry.rules {
		rule := &entry.rules[i]
		if matchInclude(relPath, rule.IncludePatterns, rule.SyncAll) {
			matched = rule
			break
		}
	}
	if matched == nil {
		log.Printf("file ignored: %s (no matching include pattern)", ev.Name)
		return
	}

	pathKey := pushPathKey(matched.RuleKey, relPath, matched.IncludePatterns, matched.SyncAll)

	w.mu.Lock()
	if t, exists := w.timers[ev.Name]; exists {
		t.Stop()
	}
	filePath := ev.Name
	gameID := matched.GameID
	ruleKey := matched.RuleKey
	w.timers[filePath] = time.AfterFunc(debounceDelay, func() {
		w.pushDebounced(ctx, gameID, pathKey, ruleKey, relPath, filePath)
	})
	w.mu.Unlock()
	log.Printf("file queued: game=%s file=%s path_key=%s", gameID, filePath, pathKey)
}

func (w *Watcher) maybeAttachRecursiveSubdir(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	parent := filepath.Dir(dir)
	entry, ok := w.pathMap[parent]
	if !ok || !entry.recursive {
		if e, ok := w.pathMap[dir]; ok && e.recursive {
			entry = e
		} else {
			return
		}
	}
	if _, ok := w.pathMap[dir]; ok {
		return
	}
	childEntry := pathEntry{rules: append([]watchRuleInfo(nil), entry.rules...), recursive: true}
	w.attachDir(dir, childEntry)
}

func matchInclude(relativePath string, patterns []string, syncAll bool) bool {
	return saverule.MatchInclude(relativePath, patterns, syncAll)
}

func pushPathKey(ruleKey, relPath string, patterns []string, syncAll bool) string {
	if syncAll || len(patterns) != 1 {
		return saverule.PathKeyForFile(ruleKey, relPath)
	}
	if strings.ContainsAny(patterns[0], "*?[") {
		return saverule.PathKeyForFile(ruleKey, relPath)
	}
	return ruleKey
}

func (w *Watcher) pushDebounced(ctx context.Context, gameID, pathKey, ruleKey, relPath, filePath string) {
	defer func() {
		w.mu.Lock()
		delete(w.timers, filePath)
		w.mu.Unlock()
	}()
	if w.IsPaused != nil && w.IsPaused() {
		return
	}
	if w.excludeMatch(filePath) {
		log.Printf("file ignored: %s (exclude pattern)", filePath)
		return
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("watcher: read %s: %v", filePath, err)
		return
	}
	hash, err := w.client.ContentWireHash(content)
	if err != nil {
		log.Printf("watcher: hash %s: %v", filePath, err)
		return
	}
	if w.client.ShouldSkipPush(gameID, pathKey, hash) {
		log.Printf("push skipped: duplicate content game=%s file=%s path_key=%s", gameID, filePath, pathKey)
		return
	}
	log.Printf("upload started: game=%s file=%s path_key=%s rule_key=%s", gameID, filePath, pathKey, ruleKey)
	pushErr := w.client.Push(ctx, gameID, pathKey, filePath, relPath, content)
	if pushErr == nil {
		log.Printf("upload complete: game=%s file=%s path_key=%s", gameID, filePath, pathKey)
		if w.Verbose {
			log.Printf("push: game=%s file=%s size=%d", gameID, filePath, len(content))
		}
		if OnSaveEvent != nil {
			OnSaveEvent(gameID, pathKey, "", SaveDirPush, nil)
		}
		return
	}
	if !retry.IsRetryableError(pushErr) {
		log.Printf("push: non-retryable error game=%s file=%s: %v", gameID, filePath, pushErr)
		return
	}
	log.Printf("push: giving up after retries: game=%s file=%s — queuing to outbox", gameID, filePath)
	if err := EnqueueOutbox(gameID, pathKey, filePath, content); err != nil {
		log.Printf("outbox: enqueue failed: %v", err)
	} else if OnOutboxEnqueued != nil {
		OnOutboxEnqueued(gameID, pathKey)
	}
}

// excludeMatch returns true if the file path matches any ExcludePatterns glob.
func (w *Watcher) excludeMatch(filePath string) bool {
	if len(w.ExcludePatterns) == 0 {
		return false
	}
	base := filepath.Base(filePath)
	for _, pat := range w.ExcludePatterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		ok, err := filepath.Match(pat, base)
		if err != nil {
			continue
		}
		if ok {
			return true
		}
	}
	return false
}

func (w *Watcher) Close() error {
	w.mu.Lock()
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	if w.fw != nil {
		return w.fw.Close()
	}
	return nil
}
