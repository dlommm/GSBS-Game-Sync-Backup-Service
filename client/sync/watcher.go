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

// WatchPath describes a path to watch (from config).
type WatchPath struct {
	GameID        string
	PathKey       string
	PathTemplates []string
}

// Watcher watches local paths and uploads changes to the server.
type Watcher struct {
	resolver        *paths.Resolver
	currentOS       paths.OS
	client          *Client
	fw              *fsnotify.Watcher
	mu              sync.Mutex
	pathMap         map[string]pathInfo
	timers          map[string]*time.Timer
	consecErr       int
	IsPaused        func() bool
	ExcludePatterns []string
	Verbose         bool
}

type pathInfo struct {
	GameID  string
	PathKey string
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
		pathMap:   make(map[string]pathInfo),
		timers:    make(map[string]*time.Timer),
	}, nil
}

// resolvedDirs returns the set of directories that should be watched for watchPaths.
func (w *Watcher) resolvedDirs(watchPaths []WatchPath) map[string]pathInfo {
	out := make(map[string]pathInfo)
	for _, wp := range watchPaths {
		for _, template := range wp.PathTemplates {
			for _, abs := range w.resolver.ResolveAll(template, w.currentOS) {
				if abs == "" {
					continue
				}
				dir := abs
				if info, err := os.Stat(abs); err == nil && !info.IsDir() {
					dir = filepath.Dir(abs)
				}
				if _, err := os.Stat(dir); err != nil {
					continue
				}
				out[dir] = pathInfo{GameID: wp.GameID, PathKey: wp.PathKey}
			}
		}
	}
	return out
}

// AddPaths registers path templates for watching. Resolves templates for current OS and only adds if directory exists.
func (w *Watcher) AddPaths(watchPaths []WatchPath) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := w.resolvedDirs(watchPaths)
	for dir, info := range want {
		if _, ok := w.pathMap[dir]; ok {
			continue
		}
		if err := w.fw.Add(dir); err != nil {
			log.Printf("watch add: %s: %v", dir, err)
			continue
		}
		w.pathMap[dir] = info
	}
	log.Printf("watching %d directories", len(w.pathMap))
	return nil
}

// RemoveStalePaths removes fsnotify watches for directories no longer in watchPaths.
func (w *Watcher) RemoveStalePaths(watchPaths []WatchPath) {
	w.mu.Lock()
	defer w.mu.Unlock()
	want := w.resolvedDirs(watchPaths)
	for dir := range w.pathMap {
		if _, ok := want[dir]; ok {
			continue
		}
		_ = w.fw.Remove(dir)
		delete(w.pathMap, dir)
	}
}

// Recreate closes the current fsnotify watcher and opens a new one, re-adding all paths.
func (w *Watcher) Recreate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	oldPaths := make(map[string]pathInfo, len(w.pathMap))
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
	w.pathMap = make(map[string]pathInfo)
	w.consecErr = 0
	for dir, info := range oldPaths {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := w.fw.Add(dir); err != nil {
			log.Printf("watch recreate add: %s: %v", dir, err)
			continue
		}
		w.pathMap[dir] = info
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

func (w *Watcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return
	}
	dir := filepath.Dir(ev.Name)
	w.mu.Lock()
	info, ok := w.pathMap[dir]
	if !ok {
		w.mu.Unlock()
		return
	}
	if t, exists := w.timers[ev.Name]; exists {
		t.Stop()
	}
	filePath := ev.Name
	gameID := info.GameID
	pathKey := info.PathKey
	w.timers[filePath] = time.AfterFunc(debounceDelay, func() {
		w.pushDebounced(ctx, gameID, pathKey, filePath)
	})
	w.mu.Unlock()
}

func (w *Watcher) pushDebounced(ctx context.Context, gameID, pathKey, filePath string) {
	defer func() {
		w.mu.Lock()
		delete(w.timers, filePath)
		w.mu.Unlock()
	}()
	if w.IsPaused != nil && w.IsPaused() {
		return
	}
	if w.excludeMatch(filePath) {
		return
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("watcher: read %s: %v", filePath, err)
		return
	}
	pushErr := w.client.Push(ctx, gameID, pathKey, filePath, content)
	if pushErr == nil {
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
