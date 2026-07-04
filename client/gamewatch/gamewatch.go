// Package gamewatch detects running games so sync can be deferred while a
// game is writing its save files. Detection is a periodic process scan (pure
// Go on every platform) matched against known game install folders: a
// process whose executable lives under a game's install root means that game
// is running. Platforms without a detector — and Flatpak, whose PID namespace
// hides host processes — degrade to "nothing detected", which simply means
// sync behaves exactly as it did before this feature.
package gamewatch

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ProcessInfo is one running process with a resolved executable path.
type ProcessInfo struct {
	PID     int
	ExePath string
}

// Detector lists running processes. Implementations are per-OS; all are
// best-effort (individual unreadable processes are skipped, never fatal).
type Detector interface {
	Snapshot() ([]ProcessInfo, error)
}

// ErrUnsupported is returned by Snapshot on platforms without a detector.
var ErrUnsupported = errors.New("gamewatch: process detection not supported on this platform")

// DefaultInterval is how often the poller scans for running games.
const DefaultInterval = 15 * time.Second

// stopHysteresis is how many consecutive scans a game must be absent before
// OnGameStop fires — launchers briefly relaunch processes (Steam reaper,
// crash-restart) and a single missed scan must not trigger a premature sync.
const stopHysteresis = 2

// caseInsensitivePaths reports whether path comparison should fold case
// (Windows and the default macOS filesystems are case-insensitive).
var caseInsensitivePaths = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

func normPath(p string) string {
	p = filepath.Clean(p)
	if caseInsensitivePaths {
		p = strings.ToLower(p)
	}
	return p
}

// RunningGames returns the set of game IDs that have at least one process
// whose executable path lies under one of the game's install roots.
func RunningGames(procs []ProcessInfo, rootsByGame map[string][]string) map[string]bool {
	if len(procs) == 0 || len(rootsByGame) == 0 {
		return nil
	}
	type normRoot struct {
		gameID string
		root   string
	}
	roots := make([]normRoot, 0, len(rootsByGame))
	for gameID, rs := range rootsByGame {
		for _, r := range rs {
			if strings.TrimSpace(r) == "" {
				continue
			}
			roots = append(roots, normRoot{gameID: gameID, root: normPath(r)})
		}
	}
	if len(roots) == 0 {
		return nil
	}
	running := make(map[string]bool)
	for _, p := range procs {
		if p.ExePath == "" {
			continue
		}
		exe := normPath(p.ExePath)
		for _, nr := range roots {
			if running[nr.gameID] {
				continue
			}
			if pathUnder(exe, nr.root) {
				running[nr.gameID] = true
			}
		}
	}
	if len(running) == 0 {
		return nil
	}
	return running
}

// pathUnder reports whether path is inside root (both pre-normalized).
func pathUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}

// Poller periodically scans processes and tracks per-game running state with
// stop hysteresis. Callbacks fire from the polling goroutine.
type Poller struct {
	Detector Detector
	Interval time.Duration
	// Roots returns the current game→install-roots map (refreshed each scan
	// so discovery updates take effect without restarting the poller).
	Roots func() map[string][]string
	// OnGameStart / OnGameStop fire on state transitions (may be nil).
	OnGameStart func(gameID string)
	OnGameStop  func(gameID string)

	mu      sync.Mutex
	running map[string]bool
	absent  map[string]int
}

// Running reports whether a specific game is currently detected as running.
func (p *Poller) Running(gameID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running[gameID]
}

// AnyRunning reports whether any tracked game is currently running.
func (p *Poller) AnyRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.running) > 0
}

// RunningCount returns how many tracked games are currently running.
func (p *Poller) RunningCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.running)
}

// Run polls until ctx is done. Safe to call once per Poller.
func (p *Poller) Run(ctx context.Context) {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	p.scan()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scan()
		}
	}
}

// scan performs one detection pass and fires transition callbacks.
func (p *Poller) scan() {
	if p.Detector == nil || p.Roots == nil {
		return
	}
	procs, err := p.Detector.Snapshot()
	if err != nil {
		return // best-effort: a failed scan changes nothing
	}
	seen := RunningGames(procs, p.Roots())

	var started, stopped []string
	p.mu.Lock()
	if p.running == nil {
		p.running = make(map[string]bool)
		p.absent = make(map[string]int)
	}
	for gameID := range seen {
		p.absent[gameID] = 0
		if !p.running[gameID] {
			p.running[gameID] = true
			started = append(started, gameID)
		}
	}
	for gameID := range p.running {
		if seen[gameID] {
			continue
		}
		p.absent[gameID]++
		if p.absent[gameID] >= stopHysteresis {
			delete(p.running, gameID)
			delete(p.absent, gameID)
			stopped = append(stopped, gameID)
		}
	}
	p.mu.Unlock()

	for _, g := range started {
		if p.OnGameStart != nil {
			p.OnGameStart(g)
		}
	}
	for _, g := range stopped {
		if p.OnGameStop != nil {
			p.OnGameStop(g)
		}
	}
}
