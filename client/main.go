package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "login" {
		runLogin()
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal("config:", err)
	}
	if cfg.Token == "" {
		log.Fatal("not logged in: run 'gsbs-client login' first")
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
		log.Fatal("sync client:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fetch manifest from server (fallback to disk cache if unavailable)
	manifestEntries := LoadManifestFromDisk()
	if entries, err := FetchManifest(ctx, cfg.ServerURL, ""); err == nil {
		manifestEntries = entries
		if err := SaveManifestToDisk(entries); err != nil {
			log.Println("save manifest cache:", err)
		}
	} else {
		log.Println("fetch manifest (using cache if any):", err)
	}

	// Merge manifest-derived watch paths (where dir exists) with config watch_paths
	effectiveWatchPaths := ManifestToWatchPaths(manifestEntries, resolver, currentOS, true)
	effectiveWatchPaths = mergeWatchPaths(effectiveWatchPaths, cfg.WatchPaths)

	// Build path resolver for pull: (gameID, pathKey) -> absolute path (only where dir exists)
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
	// Initial pull: get all saves and write only where folder exists
	if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
		log.Println("initial pull:", err)
	}

	// Watch effective game paths and upload on change
	watcher, err := sync.NewWatcher(resolver, currentOS, client)
	if err != nil {
		log.Fatal("watcher:", err)
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

	go func() {
		interval := cfg.SyncInterval
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := client.PullAndApplyWithResolver(ctx, resolvePath); err != nil {
					log.Println("periodic pull:", err)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	cancel()
	_ = watcher.Close()
	log.Println("shutdown")
}
