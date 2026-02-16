package main

import (
	"context"
	"errors"
	"log"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
)

var errNotLoggedIn = errors.New("not logged in: run 'gsbs-client login' or use Login from the tray menu")

// LastSyncState holds the last sync time and error for tray/UI display.
var LastSyncState struct {
	Mu  gosync.Mutex
	At  time.Time
	Err error
}

// OnSyncResult is called after each sync completion (success or failure).
// The tray sets this on Windows to show a balloon/toast notification. May be nil.
var OnSyncResult func(success bool, errMsg string)

// OnSyncStart is called when a sync (pull) starts. The tray may set this to show a "syncing" icon. May be nil.
var OnSyncStart func()

// SyncPaused is the global pause state for sync (pull and watcher push). Tray and run_sync both use it.
var SyncPaused atomic.Bool

// NextRetryAt is when the next pull retry is scheduled (for tray tooltip). Zero means no retry pending.
var NextRetryAt atomic.Value // time.Time

// GetNextRetryIn returns the duration until the next retry, or 0 if none pending.
func GetNextRetryIn() time.Duration {
	t, _ := NextRetryAt.Load().(time.Time)
	if t.IsZero() || t.Before(time.Now()) {
		return 0
	}
	return time.Until(t)
}

func setLastSync(at time.Time, err error) {
	LastSyncState.Mu.Lock()
	LastSyncState.At = at
	LastSyncState.Err = err
	LastSyncState.Mu.Unlock()
	if OnSyncResult != nil {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		OnSyncResult(err == nil, errMsg)
	}
}

func getLastSync() (at time.Time, err error) {
	LastSyncState.Mu.Lock()
	at, err = LastSyncState.At, LastSyncState.Err
	LastSyncState.Mu.Unlock()
	return at, err
}

// runSync runs the sync loop (watcher + periodic pull) until ctx is done.
// syncNowCh: when a value is sent, an immediate pull is run.
// refreshManifestCh: when a value is sent, the manifest is re-fetched and a pull is triggered.
// Pass channels you never send on if not needed.
// Returns nil when ctx is cancelled, or an error if setup fails (e.g. no token).
func runSync(ctx context.Context, cfg *config, syncNowCh <-chan struct{}, refreshManifestCh <-chan struct{}) error {
	if cfg.Token == "" {
		return errNotLoggedIn
	}
	log.Printf("client sync: starting server=%s", cfg.ServerURL)

	resolver := paths.NewResolver()
	if cfg.UbisoftConnectFolder != "" {
		resolver.UbisoftConnect = cfg.UbisoftConnectFolder
	}
	if cfg.GOGGalaxyFolder != "" {
		resolver.GOGGalaxy = cfg.GOGGalaxyFolder
	}
	if cfg.EpicGamesFolder != "" {
		resolver.EpicGames = cfg.EpicGamesFolder
	}
	if cfg.XboxAppFolder != "" {
		resolver.XboxApp = cfg.XboxAppFolder
	}
	if cfg.LauncherUserID != "" {
		resolver.UserID = cfg.LauncherUserID
	}
	currentOS := paths.CurrentOS()

	SyncPaused.Store(cfg.SyncPaused)
	NextRetryAt.Store(time.Time{})

	onRetryIn := func(d time.Duration) {
		if d > 0 {
			NextRetryAt.Store(time.Now().Add(d))
		} else {
			NextRetryAt.Store(time.Time{})
		}
	}

	client, err := sync.NewClient(cfg.ServerURL, cfg.Token, resolver, currentOS, cfg.MaxSyncKbps, cfg.UseCompression, cfg.VerboseLog)
	if err != nil {
		return err
	}

	manifestInclude := cfg.ManifestInclude
	if manifestInclude == "" {
		manifestInclude = "both"
	}
	includeConfig := manifestInclude == "both" || manifestInclude == "config"
	manifestEntries := LoadManifestFromDisk()
	if entries, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, "", manifestInclude); err == nil {
		manifestEntries = entries
		log.Printf("manifest: fetched %d entries from server", len(manifestEntries))
		if err := SaveManifestToDisk(entries); err != nil {
			log.Println("save manifest cache:", err)
		}
	} else {
		log.Printf("fetch manifest (using cache with %d entries): %v", len(manifestEntries), err)
	}

	effectiveWatchPaths := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig)
	effectiveWatchPaths = mergeWatchPaths(effectiveWatchPaths, cfg.WatchPaths)
	log.Printf("sync: %d active watch paths", len(effectiveWatchPaths))

	// Mutex protects effectiveWatchPaths, which is read by resolvePath (called
	// from the pull resolver) and written by doManifestRefresh. Although both
	// currently run in the select loop goroutine, the mutex makes this safe if
	// a future change introduces concurrency.
	var wpMu gosync.RWMutex

	resolvePath := func(gameID, pathKey string) string {
		wpMu.RLock()
		wp := effectiveWatchPaths
		wpMu.RUnlock()
		for _, w := range wp {
			if w.GameID != gameID || w.PathKey != pathKey {
				continue
			}
			for _, t := range w.PathTemplates {
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

	if !SyncPaused.Load() && !(cfg.SkipSyncWhenMetered && IsMeteredConnection()) {
		if OnSyncStart != nil {
			OnSyncStart()
		}
		if err := client.PullAndApplyWithResolver(ctx, resolvePath, cfg.BackupOnPull, cfg.SkipOverwriteWhenLocalNewer, onRetryIn); err != nil {
			log.Println("initial pull:", err)
			setLastSync(time.Now(), err)
		} else {
			log.Println("initial pull: complete")
			setLastSync(time.Now(), nil)
		}
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
	watcher.ExcludePatterns = cfg.WatchExclude
	watcher.Verbose = cfg.VerboseLog
	watcher.IsPaused = func() bool {
		return SyncPaused.Load() || (cfg.SkipSyncWhenMetered && IsMeteredConnection())
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
		if fresh, err := FetchManifest(ctx, cfg.ServerURL, cfg.Token, "", manifestInclude); err == nil {
			manifestEntries = fresh
			log.Printf("manifest refresh (%s): fetched %d entries", reason, len(fresh))
			_ = SaveManifestToDisk(fresh)
			newWP := ManifestToWatchPaths(fresh, resolver, currentOS, includeConfig)
			newWP = mergeWatchPaths(newWP, cfg.WatchPaths)
			wpMu.Lock()
			effectiveWatchPaths = newWP
			wpMu.Unlock()
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
		if !SyncPaused.Load() && !(cfg.SkipSyncWhenMetered && IsMeteredConnection()) {
			if OnSyncStart != nil {
				OnSyncStart()
			}
			if err := client.PullAndApplyWithResolver(ctx, resolvePath, cfg.BackupOnPull, cfg.SkipOverwriteWhenLocalNewer, onRetryIn); err != nil {
				log.Printf("manifest refresh pull (%s): %v", reason, err)
				setLastSync(time.Now(), err)
			} else {
				log.Printf("manifest refresh pull (%s): complete", reason)
				setLastSync(time.Now(), nil)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			_ = watcher.Close()
			return nil
		case <-ticker.C:
			if SyncPaused.Load() {
				continue
			}
			if cfg.SkipSyncWhenMetered && IsMeteredConnection() {
				continue
			}
			if OnSyncStart != nil {
				OnSyncStart()
			}
			if err := client.PullAndApplyWithResolver(ctx, resolvePath, cfg.BackupOnPull, cfg.SkipOverwriteWhenLocalNewer, onRetryIn); err != nil {
				log.Println("periodic pull:", err)
				setLastSync(time.Now(), err)
			} else {
				log.Println("periodic pull: complete")
				setLastSync(time.Now(), nil)
			}
		case <-syncNowCh:
			if SyncPaused.Load() {
				continue
			}
			if cfg.SkipSyncWhenMetered && IsMeteredConnection() {
				continue
			}
			if OnSyncStart != nil {
				OnSyncStart()
			}
			if err := client.PullAndApplyWithResolver(ctx, resolvePath, cfg.BackupOnPull, cfg.SkipOverwriteWhenLocalNewer, onRetryIn); err != nil {
				log.Println("sync now:", err)
				setLastSync(time.Now(), err)
			} else {
				log.Println("sync now: complete")
				setLastSync(time.Now(), nil)
			}
		case <-sseRefreshCh:
			doManifestRefresh("sse push")
		case <-refreshManifestCh:
			doManifestRefresh("manual")
		}
	}
}
