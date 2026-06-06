package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	gosync "sync"
	"time"

	clientsync "github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/discovery"
)

// TrayGlobalStatus is the overall tray/sync state.
type TrayGlobalStatus string

const (
	TrayStatusIdle    TrayGlobalStatus = "idle"
	TrayStatusSyncing TrayGlobalStatus = "syncing"
	TrayStatusPaused  TrayGlobalStatus = "paused"
	TrayStatusError   TrayGlobalStatus = "error"
	TrayStatusOffline TrayGlobalStatus = "offline"
	TrayStatusSetup   TrayGlobalStatus = "setup"
)

// SaveDirection is push or pull for per-game events.
type SaveDirection string

const (
	SaveDirPush SaveDirection = "push"
	SaveDirPull SaveDirection = "pull"
)

// GameSaveStatus is per-game sync status.
type GameSaveStatus string

const (
	GameStatusOK       GameSaveStatus = "ok"
	GameStatusConflict GameSaveStatus = "conflict"
	GameStatusPending  GameSaveStatus = "pending"
	GameStatusError    GameSaveStatus = "error"
	GameStatusSyncing  GameSaveStatus = "syncing"
)

// GameRow is one game's tray display state.
type GameRow struct {
	GameID        string         `json:"game_id"`
	Title         string         `json:"title"`
	Launcher      string         `json:"launcher,omitempty"`
	MatchReason   string         `json:"match_reason,omitempty"`
	SyncReason    string         `json:"sync_reason,omitempty"` // why a discovered game is/ isn't syncing
	Disabled      bool           `json:"disabled,omitempty"`
	LastSyncAt    time.Time      `json:"last_sync_at,omitempty"`
	LastDirection SaveDirection  `json:"last_direction,omitempty"`
	Status        GameSaveStatus `json:"status"`
	PathKeyCount  int            `json:"path_key_count"`
	FirstPathKey  string         `json:"first_path_key,omitempty"`
	HasConflict   bool           `json:"has_conflict"`
}

// SyncProgress tracks in-flight sync progress.
type SyncProgress struct {
	Phase   string `json:"phase,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

// SyncEndStats summarizes a completed sync cycle.
type SyncEndStats struct {
	GamesSynced int
	SavesSynced int
}

// TraySnapshot is a read-only view for menu rendering.
type TraySnapshot struct {
	Status         TrayGlobalStatus
	LastSyncAt     time.Time
	LastSyncErr    string
	NextRetryIn    time.Duration
	Progress       SyncProgress
	PendingUploads int
	ConflictCount  int
	Metered        bool
	Paused         bool
	WatcherHealthy bool
	ManifestAge    time.Duration
	Games          []GameRow
	Discovered     []GameRow
}

type trayState struct {
	mu             gosync.RWMutex
	status         TrayGlobalStatus
	lastSyncAt     time.Time
	lastSyncErr    string
	progress       SyncProgress
	pendingUploads int
	conflictCount  int
	metered        bool
	paused         bool
	games          map[string]*GameRow
	discovered     map[string]*GameRow
	titleCache     map[string]string
	subscribers    []chan struct{}
}

var globalTrayState = &trayState{
	status:     TrayStatusSetup,
	games:      make(map[string]*GameRow),
	discovered: make(map[string]*GameRow),
	titleCache: make(map[string]string),
}

func trayStatePath() string {
	return filepath.Join(ClientDataDir(), "tray_state.json")
}

type trayStateFile struct {
	Games map[string]GameRow `json:"games,omitempty"`
}

func loadTrayStateFromDisk() {
	data, err := os.ReadFile(trayStatePath())
	if err != nil {
		return
	}
	var f trayStateFile
	if json.Unmarshal(data, &f) != nil {
		return
	}
	globalTrayState.mu.Lock()
	defer globalTrayState.mu.Unlock()
	for id, row := range f.Games {
		r := row
		globalTrayState.games[id] = &r
		if r.Title != "" {
			globalTrayState.titleCache[id] = r.Title
		}
	}
}

func persistTrayState() {
	globalTrayState.mu.RLock()
	games := make(map[string]GameRow, len(globalTrayState.games))
	for id, row := range globalTrayState.games {
		if row != nil {
			games[id] = *row
		}
	}
	globalTrayState.mu.RUnlock()
	data, err := json.MarshalIndent(trayStateFile{Games: games}, "", "  ")
	if err != nil {
		return
	}
	dir := ClientDataDir()
	_ = os.MkdirAll(dir, 0755)
	tmp := trayStatePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, trayStatePath())
}

func initTrayState() {
	loadTrayStateFromDisk()
	refreshTrayCounts()
}

func subscribeTrayState() <-chan struct{} {
	ch := make(chan struct{}, 1)
	globalTrayState.mu.Lock()
	globalTrayState.subscribers = append(globalTrayState.subscribers, ch)
	globalTrayState.mu.Unlock()
	return ch
}

func notifyTrayState() {
	globalTrayState.mu.Lock()
	subs := globalTrayState.subscribers
	globalTrayState.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func refreshTrayCounts() {
	conflicts := clientsync.ListConflicts()
	conflictGames := make(map[string]string)
	for _, c := range conflicts {
		conflictGames[c.GameID] = c.PathKey
	}
	globalTrayState.mu.Lock()
	globalTrayState.pendingUploads = clientsync.OutboxCount()
	globalTrayState.conflictCount = len(conflicts)
	for id, row := range globalTrayState.games {
		if row == nil {
			continue
		}
		if pk, ok := conflictGames[id]; ok {
			row.HasConflict = true
			row.Status = GameStatusConflict
			if pk != "" {
				row.FirstPathKey = pk
			}
		} else if row.Status == GameStatusConflict {
			row.HasConflict = false
			row.Status = GameStatusOK
		}
	}
	globalTrayState.mu.Unlock()
}

func gameTitleFor(gameID string) string {
	globalTrayState.mu.RLock()
	if t := globalTrayState.titleCache[gameID]; t != "" {
		globalTrayState.mu.RUnlock()
		return t
	}
	globalTrayState.mu.RUnlock()
	for _, e := range LoadManifestFromDisk() {
		if e.GameID == gameID && e.GameTitle != "" {
			cacheGameTitle(gameID, e.GameTitle)
			return e.GameTitle
		}
	}
	cache := loadDiscoveryCache()
	for _, g := range cache.InstalledGames {
		key := cache.IDMap[g.Launcher+":"+g.GameID]
		if key == gameID && g.Title != "" {
			cacheGameTitle(gameID, g.Title)
			return g.Title
		}
	}
	if len(gameID) > 24 {
		return gameID[:21] + "..."
	}
	return gameID
}

func ensureGameRow(gameID string) *GameRow {
	row, ok := globalTrayState.games[gameID]
	if !ok {
		row = &GameRow{GameID: gameID, Title: gameTitleFor(gameID), Status: GameStatusOK}
		globalTrayState.games[gameID] = row
	}
	if row.Title == "" {
		row.Title = gameTitleFor(gameID)
	}
	return row
}

// UpdateTrayPaused reflects pause state in tray status.
func UpdateTrayPaused(paused bool) {
	globalTrayState.mu.Lock()
	globalTrayState.paused = paused
	if paused && globalTrayState.status != TrayStatusSyncing {
		globalTrayState.status = TrayStatusPaused
	} else if !paused && globalTrayState.status == TrayStatusPaused {
		globalTrayState.status = TrayStatusIdle
		if globalTrayState.lastSyncErr != "" {
			globalTrayState.status = TrayStatusError
		}
	}
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// UpdateTrayMetered reflects metered connection state.
func UpdateTrayMetered(metered bool) {
	globalTrayState.mu.Lock()
	globalTrayState.metered = metered
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// UpdateTraySetup marks setup-needed state.
func UpdateTraySetup(needsSetup bool) {
	globalTrayState.mu.Lock()
	if needsSetup {
		globalTrayState.status = TrayStatusSetup
	} else if globalTrayState.status == TrayStatusSetup {
		globalTrayState.status = TrayStatusIdle
	}
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// UpdateFromSyncStart marks sync in progress.
func UpdateFromSyncStart(phase string, total int) {
	globalTrayState.mu.Lock()
	globalTrayState.status = TrayStatusSyncing
	globalTrayState.progress = SyncProgress{Phase: phase, Current: 0, Total: total}
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// UpdateSyncProgress updates pull progress.
func UpdateSyncProgress(current, total int) {
	globalTrayState.mu.Lock()
	globalTrayState.progress.Current = current
	if total > 0 {
		globalTrayState.progress.Total = total
	}
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// UpdateFromSyncEnd records sync completion.
func UpdateFromSyncEnd(err error, stats SyncEndStats) {
	globalTrayState.mu.Lock()
	globalTrayState.lastSyncAt = time.Now()
	globalTrayState.progress = SyncProgress{}
	if err != nil {
		globalTrayState.lastSyncErr = err.Error()
		globalTrayState.status = TrayStatusError
	} else {
		globalTrayState.lastSyncErr = ""
		if globalTrayState.paused {
			globalTrayState.status = TrayStatusPaused
		} else {
			globalTrayState.status = TrayStatusIdle
		}
	}
	globalTrayState.pendingUploads = clientsync.OutboxCount()
	globalTrayState.conflictCount = clientsync.ConflictCount()
	globalTrayState.mu.Unlock()
	notifyTrayState()
	go persistTrayState()
}

// RecordSaveEvent records a per-game push/pull event.
func RecordSaveEvent(gameID, pathKey string, direction SaveDirection, err error) {
	if gameID == "" {
		return
	}
	globalTrayState.mu.Lock()
	row := ensureGameRow(gameID)
	row.LastSyncAt = time.Now()
	row.LastDirection = direction
	if pathKey != "" && row.FirstPathKey == "" {
		row.FirstPathKey = pathKey
	}
	if pathKey != "" {
		row.PathKeyCount++
	}
	if err != nil {
		row.Status = GameStatusError
	} else if direction == SaveDirPush {
		row.Status = GameStatusOK
	} else {
		row.Status = GameStatusOK
	}
	globalTrayState.mu.Unlock()
	refreshTrayCounts()
	notifyTrayState()
}

// RecordGameConflict marks a game as having a conflict.
func RecordGameConflict(gameID, pathKey string) {
	if gameID == "" {
		return
	}
	globalTrayState.mu.Lock()
	row := ensureGameRow(gameID)
	row.HasConflict = true
	row.Status = GameStatusConflict
	if pathKey != "" {
		row.FirstPathKey = pathKey
	}
	globalTrayState.conflictCount = clientsync.ConflictCount()
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// RecordDiscovery updates discovered games from a scan.
func RecordDiscovery(matched []discovery.MatchedGame, newCount int) {
	// Compute sync readiness before taking the lock (it loads config/manifest/resolver).
	ids := make([]string, 0, len(matched))
	for _, g := range matched {
		ids = append(ids, g.ManifestGameID)
	}
	readiness := diagnoseGamesReadiness(ids)

	globalTrayState.mu.Lock()
	seen := make(map[string]bool)
	for _, g := range matched {
		id := g.ManifestGameID
		seen[id] = true
		title := g.Title
		if title == "" {
			title = gameTitleFor(id)
		}
		globalTrayState.titleCache[id] = title
		reason := string(readiness[id].Reason)
		if _, synced := globalTrayState.games[id]; synced {
			continue
		}
		globalTrayState.discovered[id] = &GameRow{
			GameID:      id,
			Title:       title,
			Launcher:    g.Launcher,
			MatchReason: g.MatchReason,
			SyncReason:  reason,
			Disabled:    isGameDisabled(id),
			Status:      GameStatusOK,
		}
	}
	for id := range globalTrayState.discovered {
		if seen[id] {
			if row := globalTrayState.discovered[id]; row != nil {
				row.Disabled = isGameDisabled(id)
				row.SyncReason = string(readiness[id].Reason)
			}
			continue
		}
		if _, synced := globalTrayState.games[id]; synced {
			delete(globalTrayState.discovered, id)
		}
	}
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// RecordPendingUpload marks a game with a pending outbox upload.
func RecordPendingUpload(gameID, pathKey string) {
	if gameID == "" {
		return
	}
	globalTrayState.mu.Lock()
	row := ensureGameRow(gameID)
	row.Status = GameStatusPending
	row.LastDirection = SaveDirPush
	if pathKey != "" {
		row.FirstPathKey = pathKey
	}
	globalTrayState.pendingUploads = clientsync.OutboxCount()
	globalTrayState.mu.Unlock()
	notifyTrayState()
}
func ClearGameConflict(gameID string) {
	globalTrayState.mu.Lock()
	if row, ok := globalTrayState.games[gameID]; ok {
		row.HasConflict = false
		if row.Status == GameStatusConflict {
			row.Status = GameStatusOK
		}
	}
	globalTrayState.conflictCount = clientsync.ConflictCount()
	globalTrayState.mu.Unlock()
	notifyTrayState()
}

// GetTraySnapshot returns current state for menu rendering.
func GetTraySnapshot() TraySnapshot {
	globalTrayState.mu.RLock()
	defer globalTrayState.mu.RUnlock()

	games := make([]GameRow, 0, len(globalTrayState.games))
	for _, row := range globalTrayState.games {
		if row != nil {
			games = append(games, *row)
		}
	}
	sort.Slice(games, func(i, j int) bool {
		if games[i].LastSyncAt.Equal(games[j].LastSyncAt) {
			return games[i].Title < games[j].Title
		}
		return games[i].LastSyncAt.After(games[j].LastSyncAt)
	})

	discovered := make([]GameRow, 0, len(globalTrayState.discovered))
	for _, row := range globalTrayState.discovered {
		if row != nil {
			discovered = append(discovered, *row)
		}
	}
	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Title < discovered[j].Title
	})

	retryIn := GetNextRetryIn()
	return TraySnapshot{
		Status:         globalTrayState.status,
		LastSyncAt:     globalTrayState.lastSyncAt,
		LastSyncErr:    globalTrayState.lastSyncErr,
		NextRetryIn:    retryIn,
		Progress:       globalTrayState.progress,
		PendingUploads: globalTrayState.pendingUploads,
		ConflictCount:  globalTrayState.conflictCount,
		Metered:        globalTrayState.metered,
		Paused:         globalTrayState.paused,
		WatcherHealthy: WatcherHealthy.Load(),
		ManifestAge:    ManifestETagAge(),
		Games:          games,
		Discovered:     discovered,
	}
}
