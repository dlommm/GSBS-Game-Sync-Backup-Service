package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	clientlogx "github.com/gsbs/gsbs/client/logx"
	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/gsbs/gsbs/pkg/saverule"
	"github.com/gsbs/gsbs/pkg/types"
)

// manifestResponse is the API response for GET /api/manifest.
type manifestResponse struct {
	Entries []types.GameSaveLocation `json:"entries"`
}

const manifestFetchTimeout = 60 * time.Second

// manifestV2PageSize bounds each GET /api/manifest/v2 request so neither side
// holds the entire catalog in one response.
const manifestV2PageSize = 5000

// manifestFetchResult holds the outcome of a manifest download (v2 or v1).
type manifestFetchResult struct {
	Entries     []types.GameSaveLocation
	Games       []types.ManifestV2Game
	GamesTotal  int
	DeletedIDs  []string
	ETag        string
	Source      string // "v2" or "v1"
	NotModified bool
	DeltaOnly   bool // true when request used since= (empty payload means no changes)
	Complete    bool // true when a full (non-delta) paginated download finished
}

// FetchManifest downloads the manifest from the server. Tries v2 first, falls back to v1 on error, 404, or empty entries.
func FetchManifest(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	res, err := FetchManifestFull(ctx, baseURL, token, since, include, false)
	if err != nil {
		return nil, err
	}
	return res.Entries, nil
}

// FetchManifestFull downloads manifest with v2 metadata; updates on-disk cache when not a 304.
// When forceFull is true, skips If-None-Match and since= so the entire catalog is re-downloaded
// (used for manual refresh and recovering incomplete caches).
func FetchManifestFull(ctx context.Context, baseURL, token, since, include string, forceFull bool) (manifestFetchResult, error) {
	cached := LoadManifestFile()
	if forceFull {
		since = ""
	} else if !manifestCacheComplete(cached) {
		since = ""
	}

	if res, err := fetchManifestV2(ctx, baseURL, token, since, include, cached, forceFull); err == nil {
		if res.NotModified {
			if !manifestCacheComplete(cached) {
				log.Printf("manifest: server returned 304 but local cache is incomplete — forcing full re-download")
				return FetchManifestFull(ctx, baseURL, token, "", include, true)
			}
			clientlogx.Event("manifest_not_modified", "entries", len(cached.Entries), "games", len(cached.Games), "etag", cached.ETag)
			return manifestFetchResult{
				Entries:     cached.Entries,
				Games:       cached.Games,
				GamesTotal:  cached.GamesTotal,
				ETag:        cached.ETag,
				Source:      "v2",
				NotModified: true,
			}, nil
		}
		merged := mergeManifestFetch(cached, res)
		if err := SaveManifestFile(merged); err != nil {
			log.Println("save manifest cache:", err)
		}
		clientlogx.Event("manifest_v2_fetched", "entries", len(merged.Entries), "games", len(merged.Games),
			"games_total", merged.GamesTotal, "deleted", len(res.DeletedIDs), "etag", merged.ETag)
		return manifestFetchResult{
			Entries:     merged.Entries,
			Games:       merged.Games,
			GamesTotal:  merged.GamesTotal,
			DeletedIDs:  res.DeletedIDs,
			ETag:        merged.ETag,
			Source:      "v2",
			NotModified: false,
			DeltaOnly:   res.DeltaOnly,
			Complete:    res.Complete,
		}, nil
	}

	entries, err := fetchManifestV1(ctx, baseURL, token, since, include)
	if err != nil {
		return manifestFetchResult{}, err
	}
	// A since= fetch returns only changed entries; merge them into the cached
	// catalog for the on-disk file instead of clobbering it with the delta.
	// (Callers still receive the raw delta and merge in memory themselves.)
	fileEntries := entries
	if since != "" && len(cached.Entries) > 0 {
		fileEntries = MergeManifestDelta(cached.Entries, entries)
	}
	f := manifestFile{
		Entries:       fileEntries,
		LastFetchedAt: time.Now().UTC().Format(time.RFC3339),
		Source:        "v1",
	}
	if err := SaveManifestFile(f); err != nil {
		log.Println("save manifest cache:", err)
	}
	return manifestFetchResult{Entries: entries, Source: "v1"}, nil
}

func manifestPlatformParam() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

func fetchManifestV1(ctx context.Context, baseURL, token, since, include string) ([]types.GameSaveLocation, error) {
	url := baseURL + "/api/manifest"
	params := []string{}
	if since != "" {
		params = append(params, "since="+since)
	}
	if include != "" && include != "both" {
		params = append(params, "include="+include)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: manifestFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest: %s", resp.Status)
	}
	var out manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func fetchManifestV2(ctx context.Context, baseURL, token, since, include string, cached manifestFile, forceFull bool) (manifestFetchResult, error) {
	if forceFull || !manifestCacheComplete(cached) {
		if forceFull {
			log.Printf("manifest: forced full catalog download")
		} else {
			log.Printf("manifest: cache incomplete (%d games, %d entries, total=%d) — forcing full re-download",
				len(cached.Games), len(cached.Entries), cached.GamesTotal)
		}
		since = ""
		cached.ETag = ""
	}

	var allGames []types.ManifestV2Game
	var deletedIDs []string
	var etag string
	var gamesTotal int
	offset := 0
	page := 0
	useETag := cached.ETag != "" && since == ""

	for {
		page++
		out, notModified, err := fetchManifestV2Page(ctx, baseURL, token, since, offset, page == 1 && useETag, cached.ETag)
		if err != nil {
			return manifestFetchResult{}, err
		}
		if notModified {
			return manifestFetchResult{NotModified: true}, nil
		}
		if page == 1 {
			etag = out.ETag
			deletedIDs = out.DeletedGameIDs
			gamesTotal = out.GamesTotal
		}
		allGames = append(allGames, out.Games...)
		got := len(allGames)
		if gamesTotal > 0 && got >= gamesTotal {
			break
		}
		if len(out.Games) < manifestV2PageSize {
			break
		}
		offset += len(out.Games)
		log.Printf("manifest v2: page %d fetched %d games (%d so far)", page, len(out.Games), got)
	}

	if gamesTotal > 0 && len(allGames) < gamesTotal {
		return manifestFetchResult{}, fmt.Errorf("manifest v2: incomplete download (%d/%d games)", len(allGames), gamesTotal)
	}
	if page > 1 {
		log.Printf("manifest v2: downloaded %d games in %d pages", len(allGames), page)
	}

	v2 := types.ManifestV2Response{
		Games:          allGames,
		DeletedGameIDs: deletedIDs,
		ETag:           etag,
		GamesTotal:     gamesTotal,
	}
	entries := manifestV2ToEntries(v2, include)
	complete := since == "" && (gamesTotal == 0 || len(allGames) >= gamesTotal)
	return manifestFetchResult{
		Entries:    entries,
		Games:      allGames,
		GamesTotal: gamesTotal,
		DeletedIDs: deletedIDs,
		ETag:       etag,
		DeltaOnly:  since != "",
		Complete:   complete,
	}, nil
}

func fetchManifestV2Page(ctx context.Context, baseURL, token, since string, offset int, sendETag bool, etag string) (types.ManifestV2Response, bool, error) {
	params := []string{
		"platform=" + manifestPlatformParam(),
		fmt.Sprintf("limit=%d", manifestV2PageSize),
		fmt.Sprintf("offset=%d", offset),
	}
	if since != "" {
		params = append(params, "since="+since)
	}
	url := baseURL + "/api/manifest/v2?" + strings.Join(params, "&")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return types.ManifestV2Response{}, false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if sendETag && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	client := &http.Client{Timeout: manifestFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return types.ManifestV2Response{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return types.ManifestV2Response{}, true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return types.ManifestV2Response{}, false, fmt.Errorf("manifest v2: not found")
	}
	if resp.StatusCode != http.StatusOK {
		return types.ManifestV2Response{}, false, fmt.Errorf("manifest v2: %s", resp.Status)
	}

	var out types.ManifestV2Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return types.ManifestV2Response{}, false, err
	}
	if out.ETag == "" {
		out.ETag = resp.Header.Get("ETag")
	}
	return out, false, nil
}

func manifestV2ToEntries(v2 types.ManifestV2Response, include string) []types.GameSaveLocation {
	var entries []types.GameSaveLocation
	for _, g := range v2.Games {
		addLocs := func(locs []types.ManifestV2Location, isConfig bool) {
			for _, loc := range locs {
				if include == "saves" && isConfig {
					continue
				}
				if include == "config" && !isConfig {
					continue
				}
				if len(loc.SaveRules) > 0 {
					for _, rule := range loc.SaveRules {
						entries = append(entries, types.GameSaveLocation{
							GameID: g.GameID, PCGWPageID: parsePageID(g.GameID),
							GameTitle: g.Title, Platform: loc.Platform, PathTemplate: rule.Directory,
							IsConfig: isConfig, UpdatedAt: g.LastUpdated, Source: "pcgw",
							SaveRules: []types.SaveRule{rule}, Notes: loc.Notes, SteamAppIDs: g.SteamAppIDs,
							GOGID: g.GOGID, EpicID: g.EpicID, UbisoftID: g.UbisoftID,
						})
					}
					continue
				}
				for _, pt := range loc.PathTemplates {
					entries = append(entries, types.GameSaveLocation{
						GameID: g.GameID, PCGWPageID: parsePageID(g.GameID),
						GameTitle: g.Title, Platform: loc.Platform, PathTemplate: pt,
						IsConfig: isConfig, UpdatedAt: g.LastUpdated, Source: "pcgw",
						Notes: loc.Notes, SteamAppIDs: g.SteamAppIDs,
						GOGID: g.GOGID, EpicID: g.EpicID, UbisoftID: g.UbisoftID,
					})
				}
			}
		}
		addLocs(g.SaveLocations, false)
		addLocs(g.ConfigLocations, true)
	}
	return entries
}

func parsePageID(gameID string) int64 {
	n, _ := strconv.ParseInt(gameID, 10, 64)
	return n
}

var manifestPathOverride string

func manifestPath() string {
	if manifestPathOverride != "" {
		return manifestPathOverride
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "manifest.json")
}

// SetManifestPathForTest overrides the on-disk manifest cache path (tests only).
func SetManifestPathForTest(path string) {
	manifestPathOverride = path
	resetManifestFetchedAtCache()
}

// manifestFile is the on-disk manifest cache (flat entries + optional v2 game metadata).
type manifestFile struct {
	Entries          []types.GameSaveLocation `json:"entries"`
	Games            []types.ManifestV2Game   `json:"games,omitempty"`
	GamesTotal       int                      `json:"games_total,omitempty"`
	ManifestComplete bool                     `json:"manifest_complete,omitempty"`
	ETag             string                   `json:"etag,omitempty"`
	Source           string                   `json:"source,omitempty"` // "v2" or "v1"
	LastFetchedAt    string                   `json:"last_fetched_at,omitempty"`
}

// manifestCacheComplete reports whether the on-disk manifest cache holds a full
// catalog download. Incomplete caches must not use If-None-Match or since= deltas.
func manifestCacheComplete(f manifestFile) bool {
	if f.ManifestComplete {
		if f.GamesTotal > 0 && len(f.Games) < f.GamesTotal {
			return false
		}
		return len(f.Entries) > 0 || len(f.Games) > 0
	}
	if f.Source == "v2" {
		if f.GamesTotal > 0 {
			return len(f.Games) >= f.GamesTotal && len(f.Entries) > 0
		}
		if len(f.Games) > 0 {
			// Games present but no recorded total: cannot verify — refetch.
			return false
		}
		// "v2" stamp with no game bookkeeping at all: a v1-shaped catalog
		// mislabeled by pre-5.2.3 delta merges. The entries are still the
		// full catalog — judge by them instead of re-downloading forever.
		return len(f.Entries) > 0
	}
	return len(f.Entries) > 0
}

// LoadManifestFromDisk returns cached manifest entries. Returns nil if file missing or invalid.
func LoadManifestFromDisk() []types.GameSaveLocation {
	f := LoadManifestFile()
	return f.Entries
}

// LoadManifestCache returns cached entries and the last fetch timestamp.
func LoadManifestCache() ([]types.GameSaveLocation, time.Time) {
	f := LoadManifestFile()
	var lastFetched time.Time
	if f.LastFetchedAt != "" {
		lastFetched, _ = time.Parse(time.RFC3339, f.LastFetchedAt)
	}
	return f.Entries, lastFetched
}

// LoadManifestFile returns the full on-disk manifest cache.
func LoadManifestFile() manifestFile {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return manifestFile{}
	}
	var f manifestFile
	if json.Unmarshal(data, &f) != nil {
		return manifestFile{}
	}
	return f
}

// mergeManifestFetch merges a v2 fetch into the cached manifest file.
func mergeManifestFetch(cached manifestFile, res manifestFetchResult) manifestFile {
	out := cached
	out.ETag = res.ETag
	out.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)
	if res.GamesTotal > 0 {
		out.GamesTotal = res.GamesTotal
	}

	if !res.DeltaOnly {
		out.Source = "v2"
		out.Entries = res.Entries
		out.Games = res.Games
		out.ManifestComplete = res.Complete
		return out
	}

	if len(res.DeletedIDs) > 0 {
		out.Entries = ApplyManifestDeletions(out.Entries, res.DeletedIDs)
		out.Games = applyGameDeletions(out.Games, res.DeletedIDs)
	}
	if len(res.Entries) > 0 {
		if len(cached.Entries) > 0 {
			out.Entries = MergeManifestDelta(cached.Entries, res.Entries)
		} else {
			out.Entries = res.Entries
		}
	}
	if len(res.Games) > 0 {
		out.Games = mergeV2Games(cached.Games, res.Games, res.DeletedIDs)
	}
	// Only claim the v2 shape when a verifiably complete game set backs it.
	// A delta applied over a v1-era cache must NOT be relabeled "v2":
	// manifestCacheComplete would then judge it by game bookkeeping it never
	// had and force a full re-download on every start (or distrust a partial
	// delta game set). Deltas over a real v2 cache keep Source via out=cached.
	if out.GamesTotal > 0 && len(out.Games) >= out.GamesTotal {
		out.Source = "v2"
	}
	return out
}

func mergeV2Games(existing, delta []types.ManifestV2Game, deleted []string) []types.ManifestV2Game {
	if len(deleted) > 0 {
		existing = applyGameDeletions(existing, deleted)
	}
	if len(existing) == 0 {
		return delta
	}
	if len(delta) == 0 {
		return existing
	}
	index := make(map[string]int, len(existing))
	for i, g := range existing {
		index[g.GameID] = i
	}
	out := make([]types.ManifestV2Game, len(existing))
	copy(out, existing)
	for _, g := range delta {
		if i, ok := index[g.GameID]; ok {
			out[i] = g
		} else {
			out = append(out, g)
		}
	}
	return out
}

func applyGameDeletions(games []types.ManifestV2Game, deleted []string) []types.ManifestV2Game {
	if len(deleted) == 0 {
		return games
	}
	del := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		del[id] = true
	}
	var out []types.ManifestV2Game
	for _, g := range games {
		if !del[g.GameID] {
			out = append(out, g)
		}
	}
	return out
}

// ApplyManifestDeletions removes all entries for deleted game IDs.
func ApplyManifestDeletions(entries []types.GameSaveLocation, deleted []string) []types.GameSaveLocation {
	if len(deleted) == 0 {
		return entries
	}
	del := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		del[id] = true
	}
	var out []types.GameSaveLocation
	for _, e := range entries {
		if !del[e.GameID] {
			out = append(out, e)
		}
	}
	return out
}

// MergeManifestDelta merges delta entries into existing by game_id, platform, and rule identity.
func MergeManifestDelta(existing, delta []types.GameSaveLocation) []types.GameSaveLocation {
	if len(delta) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return delta
	}
	key := func(e types.GameSaveLocation) string {
		ruleID := e.PathTemplate
		if len(e.SaveRules) == 1 {
			ruleID = saverule.RuleKey(e.GameID, e.SaveRules[0])
		} else if len(e.SaveRules) > 1 {
			ruleID = saverule.RuleKey(e.GameID, e.SaveRules[0]) + "+" + fmt.Sprintf("%d", len(e.SaveRules))
		}
		return e.GameID + "\x00" + e.Platform + "\x00" + ruleID
	}
	index := make(map[string]int, len(existing))
	for i, e := range existing {
		index[key(e)] = i
	}
	out := make([]types.GameSaveLocation, len(existing))
	copy(out, existing)
	for _, e := range delta {
		k := key(e)
		if i, ok := index[k]; ok {
			out[i] = e
		} else {
			out = append(out, e)
		}
	}
	return out
}

// SaveManifestToDisk writes flat entries to disk, preserving v2 metadata when present.
func SaveManifestToDisk(entries []types.GameSaveLocation) error {
	f := LoadManifestFile()
	f.Entries = entries
	f.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)
	return SaveManifestFile(f)
}

// SaveManifestFile writes the full manifest cache atomically.
func SaveManifestFile(f manifestFile) error {
	target := manifestPath()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if f.LastFetchedAt == "" {
		f.LastFetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".manifest-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	setManifestLastFetched(parseManifestFetchedAt(f.LastFetchedAt))
	return nil
}

// manifestFetchedAt caches the manifest's LastFetchedAt timestamp in memory so
// age reads (tray tooltip, status displays) never re-read and re-parse the
// full on-disk manifest cache per call — it used to be loaded inside
// GetTraySnapshot's read lock on every /status poll and menu render. Seeded
// lazily from disk on first read, then kept fresh by SaveManifestFile (the
// single writer path for all manifest fetch/save flows).
var (
	manifestFetchedAtMu     gosync.Mutex
	manifestFetchedAtLoaded bool
	manifestFetchedAt       time.Time
)

func parseManifestFetchedAt(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func setManifestLastFetched(t time.Time) {
	manifestFetchedAtMu.Lock()
	manifestFetchedAt = t
	manifestFetchedAtLoaded = true
	manifestFetchedAtMu.Unlock()
}

func resetManifestFetchedAtCache() {
	manifestFetchedAtMu.Lock()
	manifestFetchedAt = time.Time{}
	manifestFetchedAtLoaded = false
	manifestFetchedAtMu.Unlock()
}

// ManifestETagAge returns how long since the manifest was last fetched, or zero if unknown.
// Served from the in-memory timestamp cache; the manifest file is only read
// once to seed it.
func ManifestETagAge() time.Duration {
	manifestFetchedAtMu.Lock()
	loaded, t := manifestFetchedAtLoaded, manifestFetchedAt
	manifestFetchedAtMu.Unlock()
	if !loaded {
		t = parseManifestFetchedAt(LoadManifestFile().LastFetchedAt)
		setManifestLastFetched(t)
	}
	if t.IsZero() {
		return 0
	}
	return time.Since(t)
}

// pingManifestHealth checks server reachability via manifest v2 (fallback v1).
func pingManifestHealth(baseURL, token string) (*http.Response, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("server URL is empty")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/api/manifest/v2", "/api/manifest"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+path, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusNotModified {
			return resp, nil
		}
		resp.Body.Close()
	}
	return nil, fmt.Errorf("manifest health check failed")
}

// PathKeyForManifestEntry returns a stable path key for (gameID, pathTemplate).
func PathKeyForManifestEntry(gameID, pathTemplate string) string {
	h := sha256.Sum256([]byte(gameID + "\x00" + pathTemplate))
	return hex.EncodeToString(h[:])[:16]
}

// errSSEUnauthorized marks a 401 from the events endpoint so the reconnect
// loop can refresh the token and slow down instead of hot-looping.
var errSSEUnauthorized = errors.New("sse: 401 Unauthorized — token may be invalid or expired; try logging in again from the tray")

// sseIdleTimeout is how long the stream may be silent before the connection
// is presumed half-open and torn down. The server heartbeats every 30s, so
// 90s of silence means at least two missed heartbeats.
const sseIdleTimeout = 90 * time.Second

// ListenSSE connects to the server SSE endpoint and calls onEvent for each received event type.
// getToken is called before every connection attempt so a rotated or
// re-issued token is picked up without restarting the sync loop. It
// auto-reconnects with exponential backoff on disconnect; after two
// consecutive 401s the delay is floored at 5 minutes so a revoked token
// doesn't hammer the server. Blocks until ctx is cancelled.
func ListenSSE(ctx context.Context, baseURL string, getToken func() string, onEvent func(eventType string)) {
	const healthyThreshold = 30 * time.Second
	const authBackoffFloor = 5 * time.Minute
	bo := retry.SSEBackoff()
	consecutiveAuthFailures := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		start := time.Now()
		err := connectSSE(ctx, baseURL, getToken(), onEvent)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errSSEUnauthorized) {
			consecutiveAuthFailures++
		} else {
			consecutiveAuthFailures = 0
			if time.Since(start) >= healthyThreshold {
				bo.Reset()
			}
		}
		delay := bo.Next()
		if consecutiveAuthFailures >= 2 && delay < authBackoffFloor {
			delay = authBackoffFloor
		}
		if err != nil {
			log.Printf("sse: connection error: %v (retrying in %s)", err, delay)
		} else {
			log.Printf("sse: disconnected (retrying in %s)", delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func connectSSE(ctx context.Context, baseURL, token string, onEvent func(eventType string)) error {
	// Child context so the idle watchdog can tear down a half-open stream:
	// without it a dead peer leaves scanner.Scan() blocked forever and SSE
	// events silently stop arriving until the next process restart.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	url := baseURL + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return errSSEUnauthorized
		}
		return fmt.Errorf("sse: %s", resp.Status)
	}

	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	watchdogDone := make(chan struct{})
	defer close(watchdogDone)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastActivity.Load())) > sseIdleTimeout {
					log.Printf("sse: no data for %s (server heartbeats every 30s); dropping connection", sseIdleTimeout)
					cancel()
					return
				}
			}
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	// Default 64KB line cap would error the whole stream on one oversized
	// data: line; allow up to 1MB.
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var eventType string
	for scanner.Scan() {
		// Heartbeat comments and blank lines land here too — any line proves
		// the stream is alive.
		lastActivity.Store(time.Now().UnixNano())
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			if eventType != "" {
				log.Printf("sse: received event: %s", eventType)
				onEvent(eventType)
			}
			eventType = ""
		} else if line == "" {
			eventType = ""
		}
	}
	return scanner.Err()
}

// WatchPathBuildStats counts manifest entries skipped while building watch paths.
type WatchPathBuildStats struct {
	SkippedDiscovered int
	SkippedPlatform   int
	SkippedMissingDir int
	SkippedMalformed  int
	SkippedUnsafe     int // resolved to a home/system root — too broad to watch
	// UnsafeDetails names the skipped-unsafe games/dirs (capped) so the UI can
	// tell the user "this game matched but its save folder can't be watched"
	// instead of hiding it at debug level.
	UnsafeDetails []UnsafeSkip
}

// UnsafeSkip is one manifest entry whose resolved save dir was refused by the
// watch-path safety guard.
type UnsafeSkip struct {
	GameID string `json:"game_id"`
	Title  string `json:"title,omitempty"`
	Dir    string `json:"dir"`
}

// maxUnsafeDetails caps the per-build details list (the counter still counts
// everything; ~a hundred over-broad upstream paths is normal).
const maxUnsafeDetails = 100

// LogZeroWatchPathsSummary logs skip-reason counts when no watch paths were built.
func LogZeroWatchPathsSummary(stats WatchPathBuildStats) {
	clientlogx.Event("watch_paths_zero", "skipped_discovered", stats.SkippedDiscovered,
		"skipped_platform", stats.SkippedPlatform, "skipped_missing_dir", stats.SkippedMissingDir,
		"skipped_malformed", stats.SkippedMalformed, "skipped_unsafe", stats.SkippedUnsafe)
	log.Printf("sync: no watch paths — skipped discovered=%d platform=%d missing_dir=%d malformed=%d unsafe=%d",
		stats.SkippedDiscovered, stats.SkippedPlatform, stats.SkippedMissingDir, stats.SkippedMalformed, stats.SkippedUnsafe)
}

// InstallRootsByGame maps manifest game_id to PCGW install folder hints (for <game-install-folder>).
func InstallRootsByGame(games []types.ManifestV2Game) map[string][]string {
	if len(games) == 0 {
		return nil
	}
	m := make(map[string][]string, len(games))
	for _, g := range games {
		if len(g.CommonInstallPaths) > 0 {
			m[g.GameID] = append([]string(nil), g.CommonInstallPaths...)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// LoadInstallRootsByGame reads v2 install hints from the on-disk manifest cache.
func LoadInstallRootsByGame() map[string][]string {
	return InstallRootsByGame(LoadManifestFile().Games)
}

// BuildInstallRootsByGame merges PCGW install hints, discovered install paths, and config overrides.
// Config overrides are listed first so they take priority when resolving <game-install-folder>.
func BuildInstallRootsByGame(cfg *config, cache discoveryCache) map[string][]string {
	merged := InstallRootsByGame(LoadManifestFile().Games)
	if merged == nil {
		merged = make(map[string][]string)
	}
	for gameID, paths := range DiscoveredInstallRootsByGame(cache) {
		for _, p := range paths {
			merged[gameID] = appendUniqueInstallRoot(merged[gameID], p)
		}
	}
	if cfg != nil {
		for gameID, path := range cfg.GameInstallPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			rest := merged[gameID]
			merged[gameID] = append([]string{path}, rest...)
			merged[gameID] = dedupeInstallRoots(merged[gameID])
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func appendUniqueInstallRoot(slice []string, p string) []string {
	for _, s := range slice {
		if s == p {
			return slice
		}
	}
	return append(slice, p)
}

func dedupeInstallRoots(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func resolveManifestTemplate(resolver *paths.Resolver, template string, currentOS paths.OS, gameID string, installRootsByGame map[string][]string) []string {
	if installRootsByGame == nil {
		installRootsByGame = LoadInstallRootsByGame()
	}
	var roots []string
	if installRootsByGame != nil {
		roots = installRootsByGame[gameID]
	}
	return resolver.ResolveAllForGame(template, currentOS, roots)
}

// resolveProtonPaths resolves a Windows-style save rule as a Proton compatdata path on Linux.
// For each Steam AppID it queries every known Steam library for an installed compatdata directory.
// Returns nil when no Steam libraries are configured, no AppIDs are provided, or no installed
// compatdata directories are found (i.e. the game is not installed via Proton on this machine).
func resolveProtonPaths(resolver *paths.Resolver, rule types.SaveRule, appIDs []string) []string {
	if len(appIDs) == 0 || len(resolver.SteamLibraries) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	for _, appID := range appIDs {
		pp, _ := paths.ResolveWindowsTemplateAsProton(rule.Directory, appID, resolver.SteamLibraries)
		for _, p := range pp {
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// ManifestToWatchPaths converts manifest entries to watch paths for the current OS.
// When mode is "discovered", only includes entries for game IDs in activeGameIDs (or explicit config paths via mergeWatchPaths).
func ManifestToWatchPaths(entries []types.GameSaveLocation, resolver *paths.Resolver, currentOS paths.OS, includeConfig bool, activeGameIDs map[string]bool, mode string, installRootsByGame map[string][]string) ([]watchPath, WatchPathBuildStats) {
	if installRootsByGame == nil {
		installRootsByGame = LoadInstallRootsByGame()
	}
	var out []watchPath
	var stats WatchPathBuildStats
	seen := make(map[string]bool)
	discoveredMode := mode == "discovered"
	for _, e := range entries {
		if discoveredMode && !activeGameIDs[e.GameID] {
			stats.SkippedDiscovered++
			continue
		}
		platform := e.Platform
		// On Linux, a Windows-platform rule for a Steam game can be served via Proton.
		// Treat such entries as candidates and resolve via compatdata rather than skipping them.
		protonCandidate := currentOS == paths.Linux && platform == string(paths.Windows) && len(e.SteamAppIDs) > 0
		if platform != string(currentOS) && !protonCandidate {
			stats.SkippedPlatform++
			continue
		}
		if e.IsConfig && !includeConfig {
			continue
		}
		rules := saveRulesForEntry(e)
		if len(rules) == 0 {
			stats.SkippedMalformed++
			continue
		}
		addedRule := false
		for _, rule := range rules {
			ruleKey := saverule.RuleKey(e.GameID, rule)
			var resolved []string
			if protonCandidate {
				resolved = resolveProtonPaths(resolver, rule, e.SteamAppIDs)
			} else {
				resolved = resolveManifestTemplate(resolver, rule.Directory, currentOS, e.GameID, installRootsByGame)
			}
			if len(resolved) == 0 {
				stats.SkippedMissingDir++
				continue
			}
			for _, abs := range resolved {
				if abs == "" {
					continue
				}
				if resolver.UnsafeWatchTarget(abs, rule.SyncAll, rule.Recursive, rule.IncludePatterns) {
					// A save folder must be game-specific. Refuse home/XDG/system
					// roots unless the rule targets specific named files there, so
					// we never recursively sync dotfiles, caches, etc. This fires
					// for ~a hundred manifest games with over-broad upstream paths
					// on every rebuild — debug-level, with one summary line in the
					// build stats (a warning per game buried real errors).
					stats.SkippedUnsafe++
					if len(stats.UnsafeDetails) < maxUnsafeDetails {
						stats.UnsafeDetails = append(stats.UnsafeDetails, UnsafeSkip{GameID: e.GameID, Title: e.GameTitle, Dir: abs})
					}
					clientlogx.EventDebug("watch_path_unsafe", "game_id", e.GameID, "dir", abs, "template", rule.Directory)
					continue
				}
				if !paths.WatchDirExists(abs) {
					stats.SkippedMissingDir++
					continue
				}
				key := e.GameID + "\x00" + ruleKey
				if seen[key] {
					continue
				}
				seen[key] = true
				addedRule = true
				// For Proton candidates the Windows template (rule.Directory)
				// can't be re-derived later (the watcher/reconcile resolver isn't
				// Proton-aware), so store the already-resolved compatdata path.
				// Native rules keep the template so it re-resolves per sync.
				dir := rule.Directory
				if protonCandidate {
					dir = abs
				}
				out = append(out, watchPath{
					GameID:          e.GameID,
					PathKey:         ruleKey,
					RuleKey:         ruleKey,
					Directory:       dir,
					IncludePatterns: append([]string(nil), rule.IncludePatterns...),
					Recursive:       rule.Recursive,
					SyncAll:         rule.SyncAll,
				})
			}
		}
		if !addedRule && len(rules) > 0 {
			stats.SkippedMissingDir++
		}
	}
	return out, stats
}

func saveRulesForEntry(e types.GameSaveLocation) []types.SaveRule {
	rules := e.SaveRules
	if len(rules) == 0 {
		rules = pcgw.ParseSaveRules(e.PathTemplate, e.Platform, e.IsConfig)
	}
	if len(rules) == 0 && strings.TrimSpace(e.PathTemplate) != "" {
		rules = []types.SaveRule{{Directory: e.PathTemplate, SyncAll: true, Platform: e.Platform, IsConfig: e.IsConfig}}
	}
	valid, skipReasons := saverule.FilterValidRules(rules, e.Platform)
	for _, reason := range skipReasons {
		clientlogx.EventWarn("save_rule_invalid", "game_id", e.GameID, "reason", reason)
	}
	return valid
}

// resolveSavePath finds the local absolute path for a save slot.
func watchPathTemplates(w watchPath) []string {
	if w.Directory != "" {
		if len(w.IncludePatterns) == 1 && !w.SyncAll {
			return []string{w.Directory + "/" + w.IncludePatterns[0]}
		}
		return []string{w.Directory}
	}
	return w.PathTemplates
}

func pathKeyForRelative(ruleKey, relPath string, patterns []string, syncAll bool) string {
	if syncAll || len(patterns) != 1 {
		return saverule.PathKeyForFile(ruleKey, relPath)
	}
	if strings.ContainsAny(patterns[0], "*?[") {
		return saverule.PathKeyForFile(ruleKey, relPath)
	}
	return ruleKey
}

func ruleKeyForWatchPath(w watchPath) string {
	if w.RuleKey != "" {
		return w.RuleKey
	}
	return w.PathKey
}

func syncAllForWatchPath(w watchPath) bool {
	if w.SyncAll {
		return true
	}
	return len(w.IncludePatterns) == 0
}

func resolveSavePath(gameID, pathKey string, manifestEntries []types.GameSaveLocation, watchPaths []watchPath, resolver *paths.Resolver, currentOS paths.OS, pullCtx paths.PullContext, installRootsByGame map[string][]string) string {
	for _, w := range watchPaths {
		if w.GameID != gameID {
			continue
		}
		if w.PathKey == pathKey || w.RuleKey == pathKey {
			if abs := resolveWatchPathDirect(w, resolver, currentOS, gameID, pullCtx, installRootsByGame); abs != "" {
				return abs
			}
		}
	}
	for _, w := range watchPaths {
		if w.GameID != gameID {
			continue
		}
		if abs := resolveSavePathByPathKeyForFile(gameID, pathKey, w, resolver, currentOS, pullCtx, installRootsByGame); abs != "" {
			return abs
		}
	}
	for _, e := range manifestEntries {
		if e.GameID != gameID {
			continue
		}
		// On Linux, try Proton compatdata paths for Windows-platform entries before falling
		// back to native resolution (which would produce incorrect native Linux paths).
		if currentOS == paths.Linux && e.Platform == string(paths.Windows) && len(e.SteamAppIDs) > 0 {
			for _, rule := range saveRulesForEntry(e) {
				if saverule.RuleKey(gameID, rule) == pathKey {
					for _, abs := range resolveProtonPaths(resolver, rule, e.SteamAppIDs) {
						if resolved := eligiblePullPath(abs, gameID, pullCtx); resolved != "" {
							return resolved
						}
					}
				}
			}
		}
		if PathKeyForManifestEntry(e.GameID, e.PathTemplate) == pathKey {
			for _, abs := range resolveManifestTemplate(resolver, e.PathTemplate, currentOS, gameID, installRootsByGame) {
				if abs == "" {
					continue
				}
				if abs := eligiblePullPath(abs, gameID, pullCtx); abs != "" {
					return abs
				}
			}
		}
		for _, rule := range saveRulesForEntry(e) {
			ruleKey := saverule.RuleKey(gameID, rule)
			if ruleKey == pathKey {
				wp := watchPath{
					GameID: gameID, PathKey: ruleKey, RuleKey: ruleKey,
					Directory: rule.Directory, IncludePatterns: rule.IncludePatterns,
					Recursive: rule.Recursive, SyncAll: rule.SyncAll,
				}
				if abs := resolveWatchPathDirect(wp, resolver, currentOS, gameID, pullCtx, installRootsByGame); abs != "" {
					return abs
				}
			}
			wp := watchPath{
				GameID: gameID, PathKey: ruleKey, RuleKey: ruleKey,
				Directory: rule.Directory, IncludePatterns: rule.IncludePatterns,
				Recursive: rule.Recursive, SyncAll: rule.SyncAll,
			}
			if abs := resolveSavePathByPathKeyForFile(gameID, pathKey, wp, resolver, currentOS, pullCtx, installRootsByGame); abs != "" {
				return abs
			}
		}
	}
	clientlogx.EventDebug("resolve_save_path_miss", "game_id", gameID, "path_key", pathKey)
	return ""
}

func watchDirFromResolved(abs string) string {
	if abs == "" {
		return ""
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		return filepath.Clean(abs)
	}
	return filepath.Clean(filepath.Dir(abs))
}

// resolveWatchRoot returns the resolved watch directory anchor for a save slot.
// installRootsByGame must be the SAME merged map (PCGW + discovered + config
// overrides) that resolveSavePath uses: passing nil makes template resolution
// fall back to PCGW-only install hints, so any <game-install-folder> slot
// anchored to a discovered root would resolve to "" here while resolveSavePath
// still produces a path — silently bypassing the pull escape guard.
func resolveWatchRoot(gameID, pathKey string, manifestEntries []types.GameSaveLocation, watchPaths []watchPath, resolver *paths.Resolver, currentOS paths.OS, installRootsByGame map[string][]string) string {
	for _, w := range watchPaths {
		if w.GameID != gameID {
			continue
		}
		if w.PathKey == pathKey || w.RuleKey == pathKey {
			if root := resolveWatchRootDirect(w, resolver, currentOS, installRootsByGame); root != "" {
				return root
			}
		}
	}
	for _, w := range watchPaths {
		if w.GameID != gameID {
			continue
		}
		if root := resolveWatchRootByPathKeyForFile(gameID, pathKey, w, resolver, currentOS, installRootsByGame); root != "" {
			return root
		}
	}
	for _, e := range manifestEntries {
		if e.GameID != gameID {
			continue
		}
		if PathKeyForManifestEntry(e.GameID, e.PathTemplate) == pathKey {
			for _, abs := range resolveManifestTemplate(resolver, e.PathTemplate, currentOS, gameID, installRootsByGame) {
				if root := watchDirFromResolved(abs); root != "" {
					return root
				}
			}
		}
		for _, rule := range saveRulesForEntry(e) {
			ruleKey := saverule.RuleKey(gameID, rule)
			wp := watchPath{
				GameID: gameID, PathKey: ruleKey, RuleKey: ruleKey,
				Directory: rule.Directory, IncludePatterns: rule.IncludePatterns,
				Recursive: rule.Recursive, SyncAll: rule.SyncAll,
			}
			if ruleKey == pathKey {
				if root := resolveWatchRootDirect(wp, resolver, currentOS, installRootsByGame); root != "" {
					return root
				}
			}
			if root := resolveWatchRootByPathKeyForFile(gameID, pathKey, wp, resolver, currentOS, installRootsByGame); root != "" {
				return root
			}
		}
	}
	return ""
}

func resolveWatchRootDirect(w watchPath, resolver *paths.Resolver, currentOS paths.OS, installRootsByGame map[string][]string) string {
	for _, dirTemplate := range watchRootDirs(w) {
		for _, abs := range resolveManifestTemplate(resolver, dirTemplate, currentOS, w.GameID, installRootsByGame) {
			if root := watchDirFromResolved(abs); root != "" {
				return root
			}
		}
	}
	return ""
}

func resolveWatchRootByPathKeyForFile(gameID, pathKey string, w watchPath, resolver *paths.Resolver, currentOS paths.OS, installRootsByGame map[string][]string) string {
	ruleKey := ruleKeyForWatchPath(w)
	syncAll := syncAllForWatchPath(w)
	patterns := w.IncludePatterns
	for _, dirTemplate := range watchRootDirs(w) {
		for _, root := range resolveManifestTemplate(resolver, dirTemplate, currentOS, gameID, installRootsByGame) {
			if root == "" {
				continue
			}
			if !paths.WatchDirExists(root) {
				if info, err := os.Stat(root); err == nil && !info.IsDir() {
					root = filepath.Dir(root)
				} else {
					continue
				}
			}
			root = filepath.Clean(root)
			if findFileForPathKey(root, ruleKey, pathKey, patterns, syncAll, w.Recursive) != "" {
				return root
			}
		}
	}
	return ""
}

func resolveWatchPathDirect(w watchPath, resolver *paths.Resolver, currentOS paths.OS, gameID string, pullCtx paths.PullContext, installRootsByGame map[string][]string) string {
	for _, t := range watchPathTemplates(w) {
		for _, abs := range resolveManifestTemplate(resolver, t, currentOS, gameID, installRootsByGame) {
			if abs == "" {
				continue
			}
			if abs := eligiblePullPath(abs, gameID, pullCtx); abs != "" {
				return abs
			}
		}
	}
	return ""
}

func resolveSavePathByPathKeyForFile(gameID, pathKey string, w watchPath, resolver *paths.Resolver, currentOS paths.OS, pullCtx paths.PullContext, installRootsByGame map[string][]string) string {
	ruleKey := ruleKeyForWatchPath(w)
	syncAll := syncAllForWatchPath(w)
	patterns := w.IncludePatterns
	for _, dirTemplate := range watchRootDirs(w) {
		for _, root := range resolveManifestTemplate(resolver, dirTemplate, currentOS, gameID, installRootsByGame) {
			if root == "" {
				continue
			}
			if !paths.WatchDirExists(root) {
				if info, err := os.Stat(root); err == nil && !info.IsDir() {
					root = filepath.Dir(root)
				} else {
					continue
				}
			}
			if abs := findFileForPathKey(root, ruleKey, pathKey, patterns, syncAll, w.Recursive); abs != "" {
				if abs := eligiblePullPath(abs, gameID, pullCtx); abs != "" {
					return abs
				}
			}
		}
	}
	return ""
}

func watchRootDirs(w watchPath) []string {
	if w.Directory != "" {
		return []string{w.Directory}
	}
	return w.PathTemplates
}

func findFileForPathKey(rootDir, ruleKey, pathKey string, patterns []string, syncAll, recursive bool) string {
	var found string
	walk := func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !saverule.MatchInclude(rel, patterns, syncAll) {
			return nil
		}
		if pathKeyForRelative(ruleKey, rel, patterns, syncAll) == pathKey {
			found = path
		}
		return nil
	}
	if recursive {
		_ = filepath.WalkDir(rootDir, walk)
	} else {
		entries, err := os.ReadDir(rootDir)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			_ = walk(filepath.Join(rootDir, e.Name()), e, nil)
		}
	}
	return found
}

func eligiblePullPath(abs, gameID string, pullCtx paths.PullContext) string {
	if paths.WatchDirExists(abs) {
		return abs
	}
	elig := paths.EvaluatePullEligibility(abs, gameID, pullCtx)
	if elig == paths.ApplyReady || elig == paths.ApplyCreateDir {
		return abs
	}
	return ""
}
