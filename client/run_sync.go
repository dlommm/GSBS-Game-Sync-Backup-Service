package main

import (
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/retry"
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
	warnPlainHTTP(cfg.ServerURL)
	// Rotate the device token if due (monthly, against the 90-day server
	// expiry) so long-lived installs never hit silent token expiry.
	maybeRefreshToken(ctx, cfg)

	// Startup order (single goroutine until the select loop):
	//  1. Restore discovery cache → filter manifest watch paths in discovered mode
	//  2. Fetch manifest (304 uses on-disk cache)
	//  3. Run discovery scan for newly installed games
	//  4. Create sync client + account settings (encryption)
	//  5. Build watch paths from manifest + config merge
	//  6. Initial pull (unless paused / metered)
	//  7. Start file watcher + supervisor
	//  8. Drain outbox from previous session
	//  9. Start SSE listener, tickers (pull, outbox, discovery)
	// 10. Event loop: periodic pull, sync-now, SSE, manifest refresh, discovery rebuild

	initDiscoveryState()
	// Restore discovery state from cache for watch filtering before first scan.
	if cached := loadDiscoveryCache(); len(cached.MatchedGameIDs) > 0 {
		discoveryMu.Lock()
		for _, id := range cached.MatchedGameIDs {
			discoveryState.MatchedGameIDs[id] = true
		}
		for _, id := range cached.DisabledGameIDs {
			discoveryState.DisabledGameIDs[id] = true
		}
		discoveryState.InstalledSteam = installedSteamAppIDs(cached.InstalledGames)
		discoveryMu.Unlock()
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
	cachedManifest := LoadManifestFile()
	since := ""
	if !lastManifestFetch.IsZero() && manifestCacheComplete(cachedManifest) {
		since = lastManifestFetch.UTC().Format(time.RFC3339)
	}
	if res, err := fetchManifestWithRetry(ctx, cfg.ServerURL, cfg.Token, since, manifestInclude, false); err == nil {
		if !res.NotModified {
			if since != "" && len(manifestEntries) > 0 && res.Source == "v1" {
				manifestEntries = MergeManifestDelta(manifestEntries, res.Entries)
			} else if len(res.Entries) > 0 || res.Complete {
				manifestEntries = res.Entries
			}
			log.Printf("manifest: fetched %d entries, %d games from server (source=%s since=%q complete=%v)",
				len(res.Entries), len(res.Games), res.Source, since, res.Complete)
		} else {
			log.Printf("manifest: not modified (304), using cache (%d entries)", len(manifestEntries))
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
		PolicyFor:             cfg.effectiveConflictPolicyFor,
		PullContext:           buildPullContext(cfg),
	}

	client, err := sync.NewClient(cfg.ServerURL, cfg.Token, resolver, currentOS, cfg.MaxSyncKbps, cfg.UseCompression, cfg.VerboseLog)
	if err != nil {
		return err
	}
	client.TokenReload = func() string {
		c, err := loadConfig()
		if err != nil {
			return ""
		}
		return c.Token
	}
	if enc, err := client.FetchAccountSettings(ctx); err == nil {
		client.SetEncryption(enc, cfg.EncryptionPassphrase)
	} else {
		log.Printf("account settings: %v (encryption disabled)", err)
		client.SetEncryption(false, cfg.EncryptionPassphrase)
	}
	// Encryption write-format: follow the server's fleet-readiness signal
	// unless the config pins it (crypto_v2 true/false).
	client.SetCryptoV2Override(cfg.CryptoV2)
	// Always guard the first push of a slot: a fresh device (or one whose
	// push-hash cache was cleared) must surface a conflict instead of
	// silently overwriting another machine's save. last_write_wins still
	// governs every SUBSEQUENT push (If-Hash) and all pull decisions — only
	// the blind first overwrite is gone. Old servers ignore the precondition
	// header, so behavior degrades gracefully there.
	client.SetConflictGuard(true)
	SetSyncClient(client)
	setupTrayCallbacks()
	wireSyncTrayHooks()
	// Flush push hash cache at most once per 5 s; final flush on shutdown.
	sync.StartHashCacheFlusher(ctx)

	activeIDs := activeGameIDSet()
	installRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())
	manifestWP, wpStats := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode, installRoots)
	effectiveWatchPaths := mergeWatchPaths(manifestWP, cfg.WatchPaths)
	log.Printf("sync: %d active watch paths (mode=%s; manifest skipped: platform=%d missing_dir=%d unsafe=%d)",
		len(effectiveWatchPaths), watchMode, wpStats.SkippedPlatform, wpStats.SkippedMissingDir, wpStats.SkippedUnsafe)
	if wpStats.SkippedUnsafe > 0 {
		log.Printf("sync: %d manifest save paths resolve to home/system roots and are not watched (safety guard; details at debug level)", wpStats.SkippedUnsafe)
	}
	// One-time notice if any resolved save folder is blocked (e.g. Flatpak
	// sandbox not granted). Runs on a snapshot off the startup path.
	go warnInaccessibleWatchPaths(effectiveWatchPaths)

	// Detect a path_key scheme change (e.g. server upgraded to SlotLabel-based keys).
	// Build the set of known composite slot keys from the current watch paths and evict
	// any cache entries that no longer match. The watcher is event-driven so an empty
	// cache causes no immediate re-upload storm — files are re-checked only when next changed.
	{
		knownSlotKeys := make(map[string]bool, len(effectiveWatchPaths))
		for _, wp := range effectiveWatchPaths {
			key := wp.RuleKey
			if key == "" {
				key = wp.PathKey
			}
			if wp.GameID != "" && key != "" {
				knownSlotKeys[wp.GameID+"\x00"+key] = true
			}
		}
		if sync.MaybeEvictStaleHashCache(knownSlotKeys) {
			log.Printf("sync: path_key scheme updated — push hash cache cleared; files will be re-checked on next change")
		}
	}
	if len(effectiveWatchPaths) == 0 {
		LogZeroWatchPathsSummary(wpStats)
		logActiveGamesReadiness(activeIDs)
	}

	// Mutex protects effectiveWatchPaths, which is read by resolvePath (called
	// from the pull resolver) and written by doManifestRefresh. Although both
	// currently run in the select loop goroutine, the mutex makes this safe if
	// a future change introduces concurrency.
	var wpMu gosync.RWMutex
	var manifestMu gosync.RWMutex
	// installRootsMu guards the cached install roots used by resolvePath.
	// Roots are rebuilt on discovery/manifest refresh, not on every pull call.
	var installRootsMu gosync.RWMutex

	resolvePath := func(gameID, pathKey string) string {
		wpMu.RLock()
		wp := effectiveWatchPaths
		wpMu.RUnlock()
		manifestMu.RLock()
		entries := manifestEntries
		manifestMu.RUnlock()
		installRootsMu.RLock()
		roots := installRoots
		installRootsMu.RUnlock()
		return resolveSavePath(gameID, pathKey, entries, wp, resolver, currentOS, pullOpts.PullContext, roots)
	}
	watchRoot := func(gameID, pathKey string) string {
		wpMu.RLock()
		wp := effectiveWatchPaths
		wpMu.RUnlock()
		manifestMu.RLock()
		entries := manifestEntries
		manifestMu.RUnlock()
		installRootsMu.RLock()
		roots := installRoots
		installRootsMu.RUnlock()
		return resolveWatchRoot(gameID, pathKey, entries, wp, resolver, currentOS, roots)
	}
	pullOpts.WatchRoot = watchRoot

	// Game-aware sync: skip pulls for running games starting from the very
	// first pull; the file watcher is attached to the poller further below
	// (it does not exist yet at this point).
	var gameWatcherRef *sync.Watcher
	var gameWatcherMu gosync.Mutex
	getGameWatcher := func() *sync.Watcher {
		gameWatcherMu.Lock()
		defer gameWatcherMu.Unlock()
		return gameWatcherRef
	}
	rootsSnapshot := func() map[string][]string {
		installRootsMu.RLock()
		defer installRootsMu.RUnlock()
		out := make(map[string][]string, len(installRoots))
		for k, v := range installRoots {
			out[k] = v
		}
		return out
	}
	gamePoller := startGameWatch(ctx, cfg, getGameWatcher, rootsSnapshot)
	if gamePoller != nil {
		pullOpts.SkipGame = gamePoller.Running
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
			if errors.Is(err, sync.ErrUnauthorized) {
				log.Printf("%s: auth failure detected — outbox retries paused until re-login", label)
			}
		} else {
			log.Printf("%s: complete", label)
			sync.ClearOutboxAuthFailed()
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
		return mapToSyncWatchPaths(wp)
	}

	watcher, err := sync.NewWatcher(resolver, currentOS, client)
	if err != nil {
		return err
	}
	watcher.SetInstallRoots(installRoots)
	if gamePoller != nil {
		watcher.DeferPush = gamePoller.Running
		gameWatcherMu.Lock()
		gameWatcherRef = watcher
		gameWatcherMu.Unlock()
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

	// Startup reconciliation: upload local saves that are missing on the server.
	// Runs in a goroutine with a short delay so the watcher is running first,
	// avoiding echo-suppression gaps when reconcile fires pushes.
	if !SyncPaused.Load() && !(cfg.SkipSyncWhenMetered && IsMeteredConnection()) {
		go func() {
			time.Sleep(2 * time.Second)
			reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer reconcileCancel()
			serverHashes, sumErr := client.FetchServerHashes(reconcileCtx)
			if sumErr != nil {
				// Never reconcile blind: without the server's hashes we cannot
				// tell "missing on server" from "server has a newer copy", and
				// pushing everything could overwrite newer saves. The watcher
				// still pushes real changes safely (If-Hash / If-Absent), and
				// reconcile runs again on the next start.
				log.Printf("reconcile: skipped — could not fetch server hashes after retries: %v", sumErr)
				return
			}
			installRootsMu.RLock()
			reconRoots := installRoots
			installRootsMu.RUnlock()
			reconWPs := buildReconciledWatchPaths(getSyncWatchPaths(), resolver, currentOS, reconRoots)
			n := sync.ReconcileLocalToServer(reconcileCtx, reconWPs, client, serverHashes)
			if n > 0 {
				log.Printf("reconcile: uploaded %d local save(s) missing on server", n)
			} else {
				log.Printf("reconcile: no missing uploads found")
			}
		}()
	}

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
	// Shorter first periodic scan after login when no matches yet.
	discoveryMu.RLock()
	noMatches := len(discoveryState.MatchedGameIDs) == 0
	discoveryMu.RUnlock()
	if noMatches {
		discoveryInterval = minDuration(discoveryInterval, 15*time.Minute)
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
		interval = defaultSyncInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Daily check for the monthly token rotation (long-running installs never
	// restart; mid-run rotation is safe via the 401 → TokenReload path).
	tokenTicker := time.NewTicker(24 * time.Hour)
	defer tokenTicker.Stop()

	doManifestRefresh := func(reason string) {
		log.Printf("manifest refresh (%s): starting", reason)
		forceFull := reason == "manual"
		cachedManifest := LoadManifestFile()
		since := ""
		if !forceFull && manifestCacheComplete(cachedManifest) {
			if lastFetch := cachedManifest.LastFetchedAt; lastFetch != "" {
				if t, err := time.Parse(time.RFC3339, lastFetch); err == nil {
					since = t.UTC().Format(time.RFC3339)
				}
			}
		}
		if res, err := fetchManifestWithRetry(ctx, cfg.ServerURL, cfg.Token, since, manifestInclude, forceFull); err == nil {
			if !res.NotModified {
				manifestMu.Lock()
				if res.Source == "v1" && since != "" && len(manifestEntries) > 0 {
					manifestEntries = MergeManifestDelta(manifestEntries, res.Entries)
				} else if len(res.Entries) > 0 || res.Complete {
					manifestEntries = res.Entries
				}
				manifestMu.Unlock()
				log.Printf("manifest refresh (%s): fetched %d entries, %d games (source=%s complete=%v)",
					reason, len(res.Entries), len(res.Games), res.Source, res.Complete)
			} else {
				log.Printf("manifest refresh (%s): not modified (304), using cache (%d entries)", reason, len(manifestEntries))
			}
			if reason == "discovery" || reason == "sse push" || reason == "manual" {
				runDiscovery(manifestEntries)
				refreshResolver(cfg, resolver)
				pullOpts.PullContext = buildPullContext(cfg)
			}
			activeIDs := activeGameIDSet()
			wpMu.RLock()
			oldWP := append([]watchPath(nil), effectiveWatchPaths...)
			wpMu.RUnlock()
			newInstallRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())
			installRootsMu.Lock()
			installRoots = newInstallRoots
			installRootsMu.Unlock()
			manifestWP, wpStats := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode, newInstallRoots)
			newWP := mergeWatchPaths(manifestWP, cfg.WatchPaths)
			added, removed := watchPathDiff(oldWP, newWP)
			wpMu.Lock()
			effectiveWatchPaths = newWP
			wpMu.Unlock()
			watcher.SetInstallRoots(newInstallRoots)
			newSyncWP := getSyncWatchPaths()
			_ = watcher.AddPaths(newSyncWP)
			watcher.RemoveStalePaths(newSyncWP)
			log.Printf("manifest refresh (%s): watch paths +%d -%d (now %d)", reason, added, removed, len(newWP))
			if len(newWP) == 0 {
				LogZeroWatchPathsSummary(wpStats)
				logActiveGamesReadiness(activeIDs)
			}
		} else {
			log.Printf("manifest refresh (%s): fetch failed: %v", reason, err)
		}
		doPull("manifest refresh pull (" + reason + ")")
	}

	for {
		select {
		case <-ctx.Done():
			log.Printf("client sync: shutting down — flushing watcher and outbox")
			flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
			watcher.FlushPending(flushCtx)
			if n := sync.ProcessOutbox(flushCtx, client); n > 0 {
				log.Printf("outbox: sent %d pending upload(s) on shutdown", n)
			}
			flushCancel()
			_ = watcher.Close()
			return nil
		case <-ticker.C:
			doPull("periodic pull")
		case <-tokenTicker.C:
			maybeRefreshToken(ctx, cfg)
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
				wpMu.RLock()
				oldWP := append([]watchPath(nil), effectiveWatchPaths...)
				wpMu.RUnlock()
				periodicInstallRoots := BuildInstallRootsByGame(cfg, loadDiscoveryCache())
				installRootsMu.Lock()
				installRoots = periodicInstallRoots
				installRootsMu.Unlock()
				manifestWP, wpStats := ManifestToWatchPaths(manifestEntries, resolver, currentOS, includeConfig, activeIDs, watchMode, periodicInstallRoots)
				newWP := mergeWatchPaths(manifestWP, cfg.WatchPaths)
				added, removed := watchPathDiff(oldWP, newWP)
				wpMu.Lock()
				effectiveWatchPaths = newWP
				wpMu.Unlock()
				watcher.SetInstallRoots(periodicInstallRoots)
				newSyncWP := getSyncWatchPaths()
				_ = watcher.AddPaths(newSyncWP)
				watcher.RemoveStalePaths(newSyncWP)
				if added > 0 || removed > 0 {
					log.Printf("discovery rebuild: watch paths +%d -%d (now %d)", added, removed, len(newWP))
				}
				if len(newWP) == 0 {
					LogZeroWatchPathsSummary(wpStats)
					logActiveGamesReadiness(activeIDs)
				}
			}
		}
	}
}

func fetchManifestWithRetry(ctx context.Context, baseURL, token, since, include string, forceFull bool) (manifestFetchResult, error) {
	var res manifestFetchResult
	err := retry.Do(ctx, retry.DefaultBackoff(), 3, func() error {
		var err error
		res, err = FetchManifestFull(ctx, baseURL, token, since, include, forceFull)
		return err
	})
	return res, err
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func watchPathIdentity(wp watchPath) string {
	key := wp.PathKey
	if wp.RuleKey != "" {
		key = wp.RuleKey
	}
	return wp.GameID + "\x00" + key
}

// warnPlainHTTP logs a prominent warning when the server URL uses plain HTTP
// on a non-local host, which would expose the bearer token in cleartext.
func warnPlainHTTP(serverURL string) {
	u, err := url.Parse(serverURL)
	if err != nil || strings.ToLower(u.Scheme) != "http" {
		return
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return
	}
	log.Printf("WARNING: server_url uses plain HTTP on a non-local host (%s); your sync token will be transmitted in cleartext. Use HTTPS in production.", host)
}

func watchPathDiff(old, new []watchPath) (added, removed int) {
	oldSet := make(map[string]bool, len(old))
	for _, wp := range old {
		oldSet[watchPathIdentity(wp)] = true
	}
	newSet := make(map[string]bool, len(new))
	for _, wp := range new {
		id := watchPathIdentity(wp)
		newSet[id] = true
		if !oldSet[id] {
			added++
		}
	}
	for id := range oldSet {
		if !newSet[id] {
			removed++
		}
	}
	return added, removed
}

func mapToSyncWatchPaths(wps []watchPath) []sync.WatchPath {
	var out []sync.WatchPath
	for _, wp := range wps {
		ruleKey := wp.RuleKey
		if ruleKey == "" {
			ruleKey = wp.PathKey
		}
		if wp.Directory != "" {
			syncAll := wp.SyncAll
			if !syncAll && len(wp.IncludePatterns) == 0 {
				syncAll = true
			}
			out = append(out, sync.WatchPath{
				GameID:          wp.GameID,
				RuleKey:         ruleKey,
				Directory:       wp.Directory,
				IncludePatterns: append([]string(nil), wp.IncludePatterns...),
				Recursive:       wp.Recursive,
				SyncAll:         syncAll,
			})
			continue
		}
		for _, t := range wp.PathTemplates {
			// Legacy config shape: nothing writes path_templates anymore, and a
			// template that resolves to a broad root is exactly the July-2026
			// incident class. The watcher/reconcile unsafe-target checks reject
			// those post-resolution; warn so the user migrates the entry.
			log.Printf("sync: deprecated path_templates entry for game %s (%q) — re-add the game to store a directory + include patterns", wp.GameID, t)
			out = append(out, sync.WatchPath{
				GameID:    wp.GameID,
				RuleKey:   ruleKey,
				Directory: t,
				SyncAll:   true,
			})
		}
	}
	return out
}

// buildReconciledWatchPaths resolves WatchPath.Directory templates to absolute paths
// so ReconcileLocalToServer can walk them directly.
func buildReconciledWatchPaths(wps []sync.WatchPath, resolver *paths.Resolver, currentOS paths.OS, installRoots map[string][]string) []sync.WatchPath {
	var out []sync.WatchPath
	for _, wp := range wps {
		if wp.Directory == "" {
			continue
		}
		var roots []string
		if installRoots != nil {
			roots = installRoots[wp.GameID]
		}
		for _, abs := range resolver.ResolveAllForGame(wp.Directory, currentOS, roots) {
			if abs == "" {
				continue
			}
			// Same last-line-of-defense as the watcher: reconcile walks these
			// directories recursively, so an unsafe root here would re-upload
			// a whole home/XDG tree.
			if resolver.UnsafeWatchTarget(abs, wp.SyncAll, wp.Recursive, wp.IncludePatterns) {
				log.Printf("reconcile: unsafe dir skipped (safety guard): dir=%s game_id=%s", abs, wp.GameID)
				continue
			}
			info, err := os.Stat(abs)
			if err != nil || !info.IsDir() {
				continue
			}
			resolved := wp
			resolved.Directory = abs
			out = append(out, resolved)
		}
	}
	return out
}
