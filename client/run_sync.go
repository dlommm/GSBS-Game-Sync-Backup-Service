package main

import (
	"context"
	"errors"
	"log"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/gsbs/gsbs/pkg/types"
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

// WatcherHealthy indicates the file watcher supervisor is running normally.
var WatcherHealthy atomic.Bool

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
	// Treat whitespace-only or empty token as not logged in (avoids sending "Bearer " with no token and 401 from server).
	if strings.TrimSpace(cfg.Token) == "" {
		return errNotLoggedIn
	}
	log.Printf("client sync: starting server=%s", cfg.ServerURL)

	initDiscoveryState()
	// Restore discovery state from cache for watch filtering before first scan.
	if cached := loadDiscoveryCache(); len(cached.MatchedGameIDs) > 0 {
		for _, id := range cached.MatchedGameIDs {
			discoveryState.MatchedGameIDs[id] = true
		}
		for _, id := range cached.DisabledGameIDs {
			discoveryState.DisabledGameIDs[id] = true
		}
		discoveryState.InstalledSteam = installedSteamAppIDs(cached.InstalledGames)
	}
	currentOS := paths.CurrentOS()
	watchMode := cfg.effectiveAutoWatchMode()

	SyncPaused.Store(cfg.SyncPaused)
	NextRetryAt.Store(time.Time{})

	onRetryIn := func(d time.Duration) {
		if d > 0 {
			NextRetryAt.Store(time.Now().Add(d))
		} else {
			NextRetryAt.Store(time.Time{})
		}
	}

	manifestInclude := cfg.ManifestInclude
	if manifestInclude == "" {
		manifestInclude = "both"
	}
	includeConfig := manifestInclude == "both" || manifestInclude == "config"
	manifestEntries, lastManifestFetch := LoadManifestCache()
	since := ""
	if !lastManifestFetch.IsZero() {
		since = lastManifestFetch.UTC().Format(time.RFC3339)
	}
	if entries, err := fetchManifestWithRetry(ctx, cfg.ServerURL, cfg.Token, since, manifestInclude); err == nil {
		if since != "" && len(manifestEntries) > 0 {
			manifestEntries = MergeManifestDelta(manifestEntries, entries)
		} else {
			manifestEntries = entries
		}
		log.Printf("manifest: fetched %d entries from server (since=%q)", len(entries), since)
		if err := SaveManifestToDisk(manifestEntries); err != nil {
			log.Println("save manifest cache:", err)
		}
	} else {
		log.Printf("fetch manifest (using cache with %d entries): %v", len(manifestEntries), err)
	}

	if n := runDiscovery(manifestEntries); n > 0 {
		log.Printf("discovery: %d new game(s) detected", n)
	}

	resolver := configureResolverFromConfig(cfg)
	pullOpts := sync.PullOptions{
		BackupBeforeOverwrite: cfg.BackupOnPull,
		ConflictPolicy:        cfg.effectiveConflictPolicy(),
		PullContext:           buildPullContext(cfg),
	}

	client, err := sync.NewClient(cfg.ServerURL, cfg.Token, resolver, currentOS, cfg.MaxSyncKbps, cfg.UseCompression, cfg.VerboseLog)
	if err != nil {
		return err
	}
	if enc, err := client.FetchAccountSettings(ctx); err == nil {
		client.SetEncryption(enc, cfg.EncryptionPassphrase)
	} else {
		log.Printf("account settings: %v (encryption disabled)", err)
		client.SetEncryption(false, cfg.EncryptionPassphrase)
	}
	SetSyncClient(client)
	setupTrayCallbacks()
	wireSyncTrayHooks()

	activeIDs := activeGameIDSet()
	effectiveWatchPaths := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode)
	effectiveWatchPaths = mergeWatchPaths(effectiveWatchPaths, cfg.WatchPaths)
	log.Printf("sync: %d active watch paths (mode=%s)", len(effectiveWatchPaths), watchMode)

	// Mutex protects effectiveWatchPaths, which is read by resolvePath (called
	// from the pull resolver) and written by doManifestRefresh. Although both
	// currently run in the select loop goroutine, the mutex makes this safe if
	// a future change introduces concurrency.
	var wpMu gosync.RWMutex
	var manifestMu gosync.RWMutex

	resolvePath := func(gameID, pathKey string) string {
		wpMu.RLock()
		wp := effectiveWatchPaths
		wpMu.RUnlock()
		manifestMu.RLock()
		entries := manifestEntries
		manifestMu.RUnlock()
		return resolveSavePath(gameID, pathKey, entries, wp, resolver, currentOS, pullOpts.PullContext)
	}

	doPull := func(label string) {
		if SyncPaused.Load() || (cfg.SkipSyncWhenMetered && IsMeteredConnection()) {
			return
		}
		if OnSyncStart != nil {
			OnSyncStart()
		}
		UpdateFromSyncStart("Pulling saves", 0)
		var pullErr error
		if err := client.PullAndApplyWithResolver(ctx, resolvePath, pullOpts, onRetryIn); err != nil {
			log.Printf("%s: %v", label, err)
			pullErr = err
		} else {
			log.Printf("%s: complete", label)
		}
		UpdateFromSyncEnd(pullErr, SyncEndStats{})
		setLastSync(time.Now(), pullErr)
	}

	if !SyncPaused.Load() && !(cfg.SkipSyncWhenMetered && IsMeteredConnection()) {
		doPull("initial pull")
	}

	getSyncWatchPaths := func() []sync.WatchPath {
		wpMu.RLock()
		wp := effectiveWatchPaths
		wpMu.RUnlock()
		out := make([]sync.WatchPath, len(wp))
		for i := range wp {
			out[i] = sync.WatchPath{
				GameID:        wp[i].GameID,
				PathKey:       wp[i].PathKey,
				PathTemplates: wp[i].PathTemplates,
			}
		}
		return out
	}

	watcher, err := sync.NewWatcher(resolver, currentOS, client)
	if err != nil {
		return err
	}
	watchPaths := getSyncWatchPaths()
	if err := watcher.AddPaths(watchPaths); err != nil {
		log.Println("watch paths:", err)
	}
	watcher.ExcludePatterns = cfg.WatchExclude
	watcher.Verbose = cfg.VerboseLog
	watcher.IsPaused = func() bool {
		return SyncPaused.Load() || (cfg.SkipSyncWhenMetered && IsMeteredConnection())
	}
	WatcherHealthy.Store(true)
	go sync.RunWatcherSupervisor(ctx, watcher, getSyncWatchPaths, func(ok bool) {
		WatcherHealthy.Store(ok)
	})

	// Process any pending outbox entries from previous sessions.
	if n := sync.ProcessOutbox(ctx, client); n > 0 {
		log.Printf("outbox: sent %d pending upload(s) on startup", n)
	}
	outboxTicker := time.NewTicker(2 * time.Minute)
	defer outboxTicker.Stop()

	discoveryInterval := cfg.DiscoveryInterval.Duration()
	if discoveryInterval <= 0 {
		discoveryInterval = 4 * time.Hour
	}
	discoveryTicker := time.NewTicker(discoveryInterval)
	defer discoveryTicker.Stop()

	// SSE listener: re-fetch manifest and trigger a pull when server pushes an update.
	sseRefreshCh := make(chan struct{}, 1)
	ssePullCh := make(chan struct{}, 1)
	go ListenSSE(ctx, cfg.ServerURL, cfg.Token, func(eventType string) {
		switch eventType {
		case "manifest-updated":
			select {
			case sseRefreshCh <- struct{}{}:
			default:
			}
		case "save-updated":
			select {
			case ssePullCh <- struct{}{}:
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
		cached, lastFetch := LoadManifestCache()
		since := ""
		if !lastFetch.IsZero() {
			since = lastFetch.UTC().Format(time.RFC3339)
		}
		if fresh, err := fetchManifestWithRetry(ctx, cfg.ServerURL, cfg.Token, since, manifestInclude); err == nil {
			if since != "" && len(cached) > 0 {
				manifestEntries = MergeManifestDelta(cached, fresh)
			} else {
				manifestEntries = fresh
			}
			manifestMu.Lock()
			// manifestEntries updated in outer scope
			manifestMu.Unlock()
			log.Printf("manifest refresh (%s): fetched %d entries", reason, len(fresh))
			_ = SaveManifestToDisk(manifestEntries)
			if reason == "discovery" || reason == "sse push" || reason == "manual" {
				runDiscovery(manifestEntries)
				refreshResolver(cfg, resolver)
				pullOpts.PullContext = buildPullContext(cfg)
			}
			activeIDs := activeGameIDSet()
			newWP := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode)
			newWP = mergeWatchPaths(newWP, cfg.WatchPaths)
			wpMu.Lock()
			effectiveWatchPaths = newWP
			wpMu.Unlock()
			newSyncWP := getSyncWatchPaths()
			_ = watcher.AddPaths(newSyncWP)
			watcher.RemoveStalePaths(newSyncWP)
			log.Printf("manifest refresh (%s): %d active watch paths", reason, len(newWP))
		} else {
			log.Printf("manifest refresh (%s): fetch failed: %v", reason, err)
		}
		doPull("manifest refresh pull (" + reason + ")")
	}

	for {
		select {
		case <-ctx.Done():
			_ = watcher.Close()
			return nil
		case <-ticker.C:
			doPull("periodic pull")
		case <-syncNowCh:
			doPull("sync now")
		case <-ssePullCh:
			doPull("sse pull")
		case <-sseRefreshCh:
			doManifestRefresh("sse push")
		case <-refreshManifestCh:
			doManifestRefresh("manual")
		case <-outboxTicker.C:
			if n := sync.ProcessOutbox(ctx, client); n > 0 {
				log.Printf("outbox: sent %d pending upload(s)", n)
				refreshTrayCounts()
				notifyTrayState()
			}
		case <-discoveryTicker.C:
			if n := runDiscovery(manifestEntries); n > 0 {
				log.Printf("discovery: periodic scan found %d new game(s)", n)
				doManifestRefresh("discovery")
			} else {
				// Rebuild watch paths in case save dirs appeared without new games
				activeIDs := activeGameIDSet()
				newWP := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode)
				newWP = mergeWatchPaths(newWP, cfg.WatchPaths)
				wpMu.Lock()
				effectiveWatchPaths = newWP
				wpMu.Unlock()
			}
		}
	}
}

func fetchManifestWithRetry(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	var entries []types.GameSaveLocation
	err := retry.Do(ctx, retry.DefaultBackoff(), 3, func() error {
		var err error
		entries, err = FetchManifest(ctx, baseURL, token, since, include)
		return err
	})
	return entries, err
}
