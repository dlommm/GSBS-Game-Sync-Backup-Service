package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
)

var errNotLoggedIn = errors.New("not logged in: run 'gsbs-client login' or use Login from the tray menu")

// runSync runs the sync loop (watcher + periodic pull) until ctx is done.
// syncNowCh: when a value is sent, an immediate pull is run.
// refreshManifestCh: when a value is sent, the manifest is re-fetched and a pull is triggered.
// Pass channels you never send on if not needed.
// Returns nil when ctx is cancelled, or an error if setup fails (e.g. no token).
func runSync(ctx context.Context, cfg *config, syncNowCh <-chan struct{}, refreshManifestCh <-chan struct{}) error {
	if cfg.Token == "" {
		return errNotLoggedIn
	}

	resolver := paths.NewResolver()
	if cfg.UbisoftConnectFolder != "" {
		resolver.UbisoftConnect = cfg.UbisoftConnectFolder
	}
	if cfg.LauncherUserID != "" {
		resolver.UserID = cfg.LauncherUserID
	}
	currentOS := paths.CurrentOS()

	client, err := sync.NewClient(cfg.ServerURL, cfg.Token, resolver, currentOS)
	if err != nil {
		return err
	}

	manifestEntries := LoadManifestFromDisk()
	if entries, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, ""); err == nil {
		manifestEntries = entries
		log.Printf("manifest: fetched %d entries from server", len(manifestEntries))
		if err := SaveManifestToDisk(entries); err != nil {
			log.Println("save manifest cache:", err)
		}
	} else {
		log.Printf("fetch manifest (using cache with %d entries): %v", len(manifestEntries), err)
	}

	effectiveWatchPaths := ManifestToWatchPaths(manifestEntries, resolver, currentOS, true)
	effectiveWatchPaths = mergeWatchPaths(effectiveWatchPaths, cfg.WatchPaths)
	log.Printf("sync: %d active watch paths", len(effectiveWatchPaths))

	resolvePath := func(gameID, pathKey string) string {
		for _, wp := range effectiveWatchPaths {
			if wp.GameID != gameID || wp.PathKey != pathKey {
				continue
			}
			for _, t := range wp.PathTemplates {
				resolved := resolver.Resolve(t, currentOS)
				for _, abs := range resolved {
					if abs != "" && paths.PathExists(abs) {
						return abs
					}
				}
			}
		}
		return ""
	}

	if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
		log.Println("initial pull:", err)
	} else {
		log.Println("initial pull: complete")
	}

	watcher, err := sync.NewWatcher(resolver, currentOS, client)
	if err != nil {
		return err
	}
	watchPaths := make([]sync.WatchPath, len(effectiveWatchPaths))
	for i := range effectiveWatchPaths {
		watchPaths[i] = sync.WatchPath{
			GameID:        effectiveWatchPaths[i].GameID,
			PathKey:       effectiveWatchPaths[i].PathKey,
			PathTemplates: effectiveWatchPaths[i].PathTemplates,
		}
	}
	if err := watcher.AddPaths(watchPaths); err != nil {
		log.Println("watch paths:", err)
	}
	go watcher.Run(ctx)

	// SSE listener: re-fetch manifest and trigger a pull when server pushes an update.
	sseRefreshCh := make(chan struct{}, 1)
	go ListenSSE(ctx, cfg.ServerURL, cfg.Token, func(eventType string) {
		if eventType == "manifest-updated" {
			select {
			case sseRefreshCh <- struct{}{}:
			default:
			}
		}
	})

	interval := cfg.SyncInterval.Duration()
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	doManifestRefresh := func(reason string) {
		log.Printf("manifest refresh (%s): starting", reason)
		if fresh, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, ""); err == nil {
			manifestEntries = fresh
			log.Printf("manifest refresh (%s): fetched %d entries", reason, len(fresh))
			_ = SaveManifestToDisk(fresh)
			newWP := ManifestToWatchPaths(fresh, resolver, currentOS, true)
			newWP = mergeWatchPaths(newWP, cfg.WatchPaths)
			effectiveWatchPaths = newWP
			newSyncWP := make([]sync.WatchPath, len(newWP))
			for i := range newWP {
				newSyncWP[i] = sync.WatchPath{
					GameID:        newWP[i].GameID,
					PathKey:       newWP[i].PathKey,
					PathTemplates: newWP[i].PathTemplates,
				}
			}
			_ = watcher.AddPaths(newSyncWP)
			log.Printf("manifest refresh (%s): %d active watch paths", reason, len(newWP))
		} else {
			log.Printf("manifest refresh (%s): fetch failed: %v", reason, err)
		}
		if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
			log.Printf("manifest refresh pull (%s): %v", reason, err)
		} else {
			log.Printf("manifest refresh pull (%s): complete", reason)
		}
	}

	for {
		select {
		case <-ctx.Done():
			_ = watcher.Close()
			return nil
		case <-ticker.C:
			if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
				log.Println("periodic pull:", err)
			} else {
				log.Println("periodic pull: complete")
			}
		case <-syncNowCh:
			if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
				log.Println("sync now:", err)
			} else {
				log.Println("sync now: complete")
			}
		case <-sseRefreshCh:
			doManifestRefresh("sse push")
		case <-refreshManifestCh:
			doManifestRefresh("manual")
		}
	}
}
