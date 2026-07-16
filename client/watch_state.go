package main

import gosync "sync"

// Latest watch-path build outcome, published after every rebuild (startup,
// manifest refresh, periodic discovery rebuild) so /status and the local
// dashboard report the real watch state instead of a proxy count.
var (
	watchStateMu      gosync.Mutex
	activeWatchPaths  int
	latestUnsafeSkips []UnsafeSkip
)

// publishWatchBuildState records the current number of effective watch paths
// and the manifest entries that matched a game but resolved to an unwatchable
// home/system root (surfaced on the dashboard — previously debug-log only).
func publishWatchBuildState(stats WatchPathBuildStats, watchPathCount int) {
	watchStateMu.Lock()
	activeWatchPaths = watchPathCount
	latestUnsafeSkips = append([]UnsafeSkip(nil), stats.UnsafeDetails...)
	watchStateMu.Unlock()
}

// getWatchBuildState returns the last published watch-path count and
// unsafe-root skip details.
func getWatchBuildState() (int, []UnsafeSkip) {
	watchStateMu.Lock()
	defer watchStateMu.Unlock()
	return activeWatchPaths, append([]UnsafeSkip(nil), latestUnsafeSkips...)
}
