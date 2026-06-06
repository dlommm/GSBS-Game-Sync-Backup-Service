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

type pendingPush struct {
	gameID   string
	pathKey  string
	ruleKey  string
	relPath  string
	filePath string
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
	pending         map[string]pendingPush
	installRoots    map[string][]string // manifest game_id -> install folders for <game-install-folder>
	consecErr       int
	IsPaused        func() bool
	ExcludePatterns []string
	Verbose         bool
}

// SetInstallRoots updates per-game install roots used to resolve <game-install-folder>
// templates. Safe to call concurrently with AddPaths/RemoveStalePaths.
func (w *Watcher) SetInstallRoots(roots map[string][]string) {
	w.mu.Lock()
	w.installRoots = roots
	w.mu.Unlock()
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
		pending:   make(map[string]pendingPush),
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
		var roots []string
		if w.installRoots != nil {
			roots = w.installRoots[wp.GameID]
		}
		for _, abs := range w.resolver.ResolveAllForGame(wp.Directory, w.currentOS, roots) {
			if abs == "" {
				continue
			}
			info, err := os.Stat(abs)
			if err != nil {
				logSyncWarn("watcher_dir_missing", "dir", abs, "game_id", wp.GameID, "error", err)
				continue
			}
			if !info.IsDir() {
				abs = filepath.Dir(abs)
				info, err = os.Stat(abs)
				if err != nil || !info.IsDir() {
					logSyncWarn("watcher_dir_missing", "dir", abs, "game_id", wp.GameID)
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
		logSyncWarn("watcher_rejected", "dir", dir, "error", err)
		return
	}
	w.pathMap[dir] = entry
	logSyncInfo("watcher_attached", "dir", dir)
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
			logSyncDebug("watch_path_added", "dir", dir)
		}
	}
	logSyncInfo("watcher_paths", "count", len(w.pathMap))
	return nil
}

func (w *Watcher) cancelTimersUnderDirs(removedDirs map[string]bool) {
	for filePath, t := range w.timers {
		if w.fileUnderRemovedDir(filePath, removedDirs) {
			t.Stop()
			delete(w.timers, filePath)
			delete(w.pending, filePath)
		}
	}
}

func (w *Watcher) fileUnderRemovedDir(filePath string, removedDirs map[string]bool) bool {
	for dir := range removedDirs {
		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// RemoveStalePaths removes fsnotify watches for directories no longer in watchPaths.
func (w *Watcher) RemoveStalePaths(watchPaths []WatchPath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := w.resolvedWatchDirs(watchPaths)
	removed := make(map[string]bool)
	for dir := range w.pathMap {
		if _, ok := want[dir]; ok {
			continue
		}
		_ = w.fw.Remove(dir)
		delete(w.pathMap, dir)
		removed[dir] = true
		logSyncInfo("watch_path_removed", "dir", dir)
	}
	if len(removed) > 0 {
		w.cancelTimersUnderDirs(removed)
	}
}

// FlushPending runs all debounced pushes immediately (e.g. on shutdown).
func (w *Watcher) FlushPending(ctx context.Context) {
	w.mu.Lock()
	timers := w.timers
	pending := w.pending
	w.timers = make(map[string]*time.Timer)
	w.pending = make(map[string]pendingPush)
	w.mu.Unlock()

	for filePath, t := range timers {
		t.Stop()
		if p, ok := pending[filePath]; ok {
			w.pushDebounced(ctx, p.gameID, p.pathKey, p.ruleKey, p.relPath, p.filePath)
		}
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
			logSyncWarn("watcher_recreate_missing", "dir", dir, "error", err)
			continue
		}
		w.attachDir(dir, entry)
	}
	logSyncInfo("watcher_recreated", "count", len(w.pathMap))
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
				logSyncWarn("watcher_error", "error", err, "consecutive", w.consecErr)
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
	// Prune watches for removed or renamed directories to avoid FD exhaustion.
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.mu.Lock()
		if _, ok := w.pathMap[ev.Name]; ok {
			_ = w.fw.Remove(ev.Name)
			delete(w.pathMap, ev.Name)
			logSyncInfo("watcher_pruned", "dir", ev.Name, "op", ev.Op.String())
		}
		w.mu.Unlock()
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
		logSyncDebug("watcher_ignore_pattern", "file", ev.Name, "relative_path", relPath)
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
	w.pending[filePath] = pendingPush{gameID: gameID, pathKey: pathKey, ruleKey: ruleKey, relPath: relPath, filePath: filePath}
	w.timers[filePath] = time.AfterFunc(debounceDelay, func() {
		w.pushDebounced(ctx, gameID, pathKey, ruleKey, relPath, filePath)
	})
	w.mu.Unlock()
	logSyncInfo("watcher_queued", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "file", filePath)
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
		delete(w.pending, filePath)
		w.mu.Unlock()
	}()
	if w.IsPaused != nil && w.IsPaused() {
		logSyncDebug("watcher_push_paused", "game_id", gameID, "file", filePath)
		return
	}
	if w.excludeMatch(filePath, relPath) {
		logSyncDebug("watcher_exclude", "game_id", gameID, "file", filePath, "relative_path", relPath)
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		logSyncWarn("watcher_stat", "game_id", gameID, "file", filePath, "error", err)
		return
	}
	if info.Size() == 0 {
		logSyncDebug("watcher_skip_empty", "game_id", gameID, "file", filePath)
		return
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		logSyncWarn("watcher_read", "game_id", gameID, "file", filePath, "error", err)
		return
	}
	hash, err := w.client.ContentWireHash(content)
	if err != nil {
		logSyncWarn("watcher_hash", "game_id", gameID, "file", filePath, "error", err)
		return
	}
	if w.client.ShouldSkipPush(gameID, pathKey, hash) {
		logSyncDebug("push_skip_duplicate", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "file", filePath)
		return
	}
	logSyncInfo("push_start", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "bytes", len(content), "file", filePath)
	pushErr := w.client.Push(ctx, gameID, pathKey, filePath, relPath, content)
	if pushErr == nil {
		logSyncInfo("push_complete", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "file", filePath)
		if w.Verbose {
			log.Printf("push: game=%s file=%s size=%d", gameID, filePath, len(content))
		}
		if OnSaveEvent != nil {
			OnSaveEvent(gameID, pathKey, "", SaveDirPush, nil)
		}
		return
	}
	if !retry.IsRetryableError(pushErr) {
		logSyncWarn("push_non_retryable", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "error", pushErr)
		if OnPushError != nil {
			OnPushError(gameID, pathKey, pushErr.Error())
		}
		return
	}
	logSyncWarn("push_outbox", "game_id", gameID, "path_key", pathKey, "relative_path", relPath, "error", pushErr)
	if err := EnqueueOutbox(gameID, pathKey, filePath, relPath, content, hash); err != nil {
		logSyncError("outbox_enqueue_failed", "game_id", gameID, "path_key", pathKey, "error", err)
	} else if OnOutboxEnqueued != nil {
		OnOutboxEnqueued(gameID, pathKey)
	}
}

// excludeMatch returns true if the file matches any ExcludePatterns glob (basename or full relative path).
func (w *Watcher) excludeMatch(filePath, relPath string) bool {
	if len(w.ExcludePatterns) == 0 {
		return false
	}
	base := filepath.Base(filePath)
	rel := filepath.ToSlash(relPath)
	for _, pat := range w.ExcludePatterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if strings.Contains(pat, "/") || strings.Contains(pat, `\`) {
			if ok, err := filepath.Match(pat, rel); err == nil && ok {
				return true
			}
			continue
		}
		if ok, err := filepath.Match(pat, base); err == nil && ok {
			return true
		}
		if ok, err := filepath.Match(pat, rel); err == nil && ok {
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
	w.timers = make(map[string]*time.Timer)
	w.pending = make(map[string]pendingPush)
	w.mu.Unlock()
	if w.fw != nil {
		return w.fw.Close()
	}
	return nil
}
