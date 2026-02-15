package sync

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gsbs/gsbs/pkg/paths"
)

const debounceDelay = 2 * time.Second

// WatchPath describes a path to watch (from config).
type WatchPath struct {
	GameID        string
	PathKey       string
	PathTemplates []string
}

// Watcher watches local paths and uploads changes to the server.
type Watcher struct {
	resolver  *paths.Resolver
	currentOS paths.OS
	client    *Client
	fw        *fsnotify.Watcher
	mu        sync.Mutex
	pathMap   map[string]pathInfo // watched directory -> gameID, pathKey (events under that dir use this info)
	timers    map[string]*time.Timer
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
	w := &Watcher{
		resolver:  resolver,
		currentOS: currentOS,
		client:    client,
		fw:        fw,
		pathMap:   make(map[string]pathInfo),
		timers:    make(map[string]*time.Timer),
	}
	return w, nil
}

// AddPaths registers path templates for watching. Resolves templates for current OS and only adds if directory exists.
func (w *Watcher) AddPaths(watchPaths []WatchPath) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, wp := range watchPaths {
		for _, template := range wp.PathTemplates {
			resolved := w.resolver.Resolve(template, w.currentOS)
			for _, abs := range resolved {
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
				if err := w.fw.Add(dir); err != nil {
					log.Println("watch add:", dir, err)
					continue
				}
				w.pathMap[dir] = pathInfo{GameID: wp.GameID, PathKey: wp.PathKey}
			}
		}
	}
	log.Printf("watching %d directories", len(w.pathMap))
	return nil
}

// Run starts the watch loop and uploads on write/create/rename events with per-file debouncing.
func (w *Watcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			dir := filepath.Dir(ev.Name)
			w.mu.Lock()
			info, ok := w.pathMap[dir]
			if !ok {
				w.mu.Unlock()
				continue
			}
			// Reset or create a debounce timer for this file.
			if t, exists := w.timers[ev.Name]; exists {
				t.Stop()
			}
			filePath := ev.Name
			gameID := info.GameID
			pathKey := info.PathKey
			w.timers[filePath] = time.AfterFunc(debounceDelay, func() {
				content, err := os.ReadFile(filePath)
				if err != nil {
					log.Printf("watcher: read %s: %v", filePath, err)
				} else if err := w.client.Push(ctx, gameID, pathKey, filePath, content); err != nil {
					log.Printf("push after watch: %v", err)
				} else {
					log.Printf("push: game=%s file=%s size=%d", gameID, filePath, len(content))
				}
				// Remove timer from map so it doesn't grow unbounded
				w.mu.Lock()
				delete(w.timers, filePath)
				w.mu.Unlock()
			})
			w.mu.Unlock()
		case err := <-w.fw.Errors:
			if err != nil {
				log.Println("watcher error:", err)
			}
		}
	}
}

func (w *Watcher) Close() error {
	w.mu.Lock()
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	return w.fw.Close()
}
