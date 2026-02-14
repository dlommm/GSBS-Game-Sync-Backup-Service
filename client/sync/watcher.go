package sync

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/gsbs/gsbs/pkg/paths"
)

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
	return nil
}

// Run starts the watch loop and uploads on write events.
func (w *Watcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if ev.Op&fsnotify.Write != fsnotify.Write {
				continue
			}
			dir := filepath.Dir(ev.Name)
			w.mu.Lock()
			info, ok := w.pathMap[dir]
			w.mu.Unlock()
			if !ok {
				continue
			}
			content, err := os.ReadFile(ev.Name)
			if err != nil {
				continue
			}
			if err := w.client.Push(ctx, info.GameID, info.PathKey, ev.Name, content); err != nil {
				log.Println("push after watch:", err)
			}
		case err := <-w.fw.Errors:
			if err != nil {
				log.Println("watcher error:", err)
			}
		}
	}
}

func (w *Watcher) Close() error {
	return w.fw.Close()
}
