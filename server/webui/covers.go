package webui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/server/logx"
)

const (
	// steamCoverURLFmt is Steam's public CDN portrait library art (no API key).
	steamCoverURLFmt = "https://cdn.cloudflare.steamstatic.com/steam/apps/%s/library_600x900.jpg"
	coverMaxBytes    = 8 << 20                  // 8 MiB cap per cover
	coverCacheCtl    = "public, max-age=604800" // 7-day browser cache
	defaultCoverRoot = "/app/data/covers"
)

// coverHTTPClient fetches cover art from Steam's CDN with a short timeout.
var coverHTTPClient = &http.Client{Timeout: 15 * time.Second}

// coverFetchLocks serializes concurrent fetches for the same game so a burst of
// requests for an uncached cover results in a single upstream fetch.
var coverFetchLocks sync.Map // game_id -> *sync.Mutex

// coverRootFromEnv resolves the on-disk cover cache directory. Covers are a
// regenerable, server-global cache (not per-user), so a single flat dir keyed
// by game_id is enough. Falls back to a temp dir when the default isn't writable.
func coverRootFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("GSBS_COVER_ROOT")); v != "" {
		if err := os.MkdirAll(v, 0o750); err == nil {
			return v
		}
	}
	if err := os.MkdirAll(defaultCoverRoot, 0o750); err == nil {
		return defaultCoverRoot
	}
	tmp := filepath.Join(os.TempDir(), "gsbs-covers")
	_ = os.MkdirAll(tmp, 0o750)
	return tmp
}

// isNumericGameID reports whether id is a short all-digits string. game_id is a
// PCGW page_id, so this both validates input and guards against path traversal.
func isNumericGameID(s string) bool {
	if s == "" || len(s) > 12 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// serveCover serves a game's cover art from the local disk cache, fetching it
// once from Steam's CDN on a miss. Public (no session): covers are shared,
// non-sensitive metadata, like /static/. Missing/absent art returns 404 so the
// UI falls back to the generated icon tile.
func (h *WebHandler) serveCover(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/covers/"), ".jpg")
	if !isNumericGameID(id) || h.coverRoot == "" {
		http.NotFound(w, r)
		return
	}
	jpgPath := filepath.Join(h.coverRoot, id+".jpg")
	missPath := filepath.Join(h.coverRoot, id+".miss")

	if isFile(jpgPath) {
		h.serveCoverFile(w, r, jpgPath)
		return
	}
	if isFile(missPath) {
		http.NotFound(w, r)
		return
	}

	// Cache miss — fetch under a per-game lock and re-check inside it.
	muIface, _ := coverFetchLocks.LoadOrStore(id, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if isFile(jpgPath) {
		h.serveCoverFile(w, r, jpgPath)
		return
	}
	if isFile(missPath) {
		http.NotFound(w, r)
		return
	}

	appID := h.steamAppIDForGame(r.Context(), id)
	if appID == "" {
		writeCoverMiss(missPath)
		http.NotFound(w, r)
		return
	}
	data, err := fetchSteamCover(r.Context(), appID)
	if err != nil {
		logx.Logger().Debug().Str("game_id", id).Str("appid", appID).Err(err).Msg("cover fetch failed")
		writeCoverMiss(missPath)
		http.NotFound(w, r)
		return
	}
	if err := atomicWriteCover(jpgPath, data); err != nil {
		logx.Logger().Error().Str("game_id", id).Err(err).Msg("cover cache write failed")
		w.Header().Set("Cache-Control", coverCacheCtl)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(data)
		return
	}
	h.serveCoverFile(w, r, jpgPath)
}

func (h *WebHandler) serveCoverFile(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", coverCacheCtl)
	http.ServeFile(w, r, path)
}

func (h *WebHandler) steamAppIDForGame(ctx context.Context, id string) string {
	pageID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return ""
	}
	g, err := h.store.GetPCGWGame(ctx, pageID)
	if err != nil || g == nil || len(g.SteamAppIDs) == 0 {
		return ""
	}
	appID := strings.TrimSpace(g.SteamAppIDs[0])
	if !isNumericGameID(appID) {
		return ""
	}
	return appID
}

func fetchSteamCover(ctx context.Context, appID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(steamCoverURLFmt, appID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := coverHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam cover HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("steam cover unexpected content-type %q", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, coverMaxBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty cover body")
	}
	return data, nil
}

func writeCoverMiss(missPath string) {
	_ = os.WriteFile(missPath, []byte{}, 0o640)
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// atomicWriteCover writes data to path via a temp file + rename so a partial
// download never leaves a corrupt cover in the cache.
func atomicWriteCover(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cover-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// coverCacheCount returns the number of cached cover JPEGs.
func (h *WebHandler) coverCacheCount() int {
	if h.coverRoot == "" {
		return 0
	}
	entries, err := os.ReadDir(h.coverRoot)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") {
			n++
		}
	}
	return n
}

// clearCoverCache deletes cached covers and negative markers so they re-fetch on
// next view. Returns the number of cover JPEGs removed.
func (h *WebHandler) clearCoverCache() (int, error) {
	if h.coverRoot == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(h.coverRoot)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".miss") {
			if os.Remove(filepath.Join(h.coverRoot, name)) == nil && strings.HasSuffix(name, ".jpg") {
				removed++
			}
		}
	}
	return removed, nil
}

func (h *WebHandler) handleClearCoverCache(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	n, err := h.clearCoverCache()
	if err != nil {
		logx.Logger().Error().Err(err).Msg("clear cover cache failed")
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "clear_cover_cache", "", fmt.Sprintf("removed=%d", n))
	Redirect(w, r, "/admin/settings?ok=covers_cleared")
}
