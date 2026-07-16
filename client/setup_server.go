// Package main: local HTTP server for setup/login in the browser (works reliably on Windows).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	clientsync "github.com/gsbs/gsbs/client/sync"
	clientwebui "github.com/gsbs/gsbs/client/webui"
)

const setupPortStart = 41234
const setupPortEnd = 41240

var (
	setupURLMu sync.Mutex
	setupURL   string
)

// StartSetupServer starts a local HTTP server for the setup/login page. Call from tray onReady.
// Listens on 127.0.0.1 only. Returns the base URL (e.g. http://127.0.0.1:41234) or "" if no port bound.
func StartSetupServer() string {
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(clientwebui.StaticFiles())))
	mux.HandleFunc("/client-logo", handleClientLogo)

	mux.HandleFunc("/", handleSetupPage)
	mux.HandleFunc("/login", handleSetupLogin)
	mux.HandleFunc("/open-log", handleOpenLog)
	mux.HandleFunc("/status", handleSetupStatus)
	mux.HandleFunc("/dashboard", handleDashboardPage)
	mux.HandleFunc("/quick-actions", handleQuickActionsPage)
	mux.HandleFunc("/settings", handleSettingsPage)
	mux.HandleFunc("/settings/save", handleSettingsSave)
	mux.HandleFunc("/settings/encryption", handleEncryptionEnable)
	mux.HandleFunc("/setup/test-connection", handleTestConnection)
	mux.HandleFunc("/help", handleHelpPage)
	mux.HandleFunc("/logs", handleLogsPage)
	mux.HandleFunc("/partial/logs", handleLogsPartial)
	mux.HandleFunc("/logs/export.csv", handleLogsCSV)
	mux.HandleFunc("/api/apply-update", handleApplyUpdate)
	mux.HandleFunc("/about", handleAboutPage)
	mux.HandleFunc("/api/sync-now", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		triggerSyncNow()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/games", handleAddGamePage)
	mux.HandleFunc("/games/search", handleGamesSearch)
	mux.HandleFunc("/games/add", handleGamesAdd)
	mux.HandleFunc("/insights", handleInsightsPage)
	mux.HandleFunc("/insights/resolve", handleInsightsResolve)
	mux.HandleFunc("/insights/sync-game", handleInsightsSyncGame)
	mux.HandleFunc("/versions", handleVersionsPage)
	mux.HandleFunc("/versions/restore", handleVersionsRestore)
	mux.HandleFunc("/open-folder", handleOpenFolder)
	mux.HandleFunc("/api/check-update", handleCheckUpdate)

	for port := setupPortStart; port < setupPortEnd; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := listenPort(addr)
		if err != nil {
			continue
		}
		url := "http://" + addr
		setupURLMu.Lock()
		setupURL = url
		setupURLMu.Unlock()
		srv := &http.Server{
			Handler:           clientSecurityHeaders(mux),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       time.Minute,
			IdleTimeout:       2 * time.Minute,
		}
		go func() {
			_ = srv.Serve(listener)
		}()
		return url
	}
	log.Println("setup: could not bind a port for local setup page (tried 41234–41239)")
	return ""
}

func listenPort(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// clientSecurityHeaders applies the same strict CSP posture as the server
// WebUI: all scripts/styles are external files, so no 'unsafe-inline' anywhere
// (guarded by client/webui/template_csp_test.go).
func clientSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// openNative opens a file or folder with the platform's default handler.
func openNative(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start() //nolint:gosec // G204: fixed OS utility on our own config-derived path
	case "darwin":
		return exec.Command("open", path).Start() //nolint:gosec // G204: fixed OS utility on our own config-derived path
	default:
		return exec.Command("xdg-open", path).Start() //nolint:gosec // G204: fixed OS utility on our own config-derived path
	}
}

// handleOpenFolder reveals a watched game folder (or the config folder) in the
// OS file manager. Only paths the client already knows about are allowed.
// resolveGameFolder returns the configured save directory for a game ("" when
// unknown). Shared by the local UI's Reveal-folder and the tray's per-game
// Open-save-folder action.
func resolveGameFolder(gameID string) string {
	if gameID == "" {
		return ""
	}
	cfg, _ := loadConfig()
	if cfg == nil {
		return ""
	}
	for _, wp := range cfg.WatchPaths {
		if wp.GameID == gameID && wp.Directory != "" {
			return wp.Directory
		}
	}
	return ""
}

func handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	target := ""
	switch what := r.URL.Query().Get("what"); what {
	case "config":
		dir, _ := os.UserConfigDir()
		target = filepath.Join(dir, "gsbs")
	case "game":
		target = resolveGameFolder(r.URL.Query().Get("game_id"))
	}
	if target == "" {
		http.Error(w, "unknown folder", http.StatusNotFound)
		return
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		http.Error(w, "folder not found on disk", http.StatusNotFound)
		return
	}
	if err := openNative(target); err != nil {
		log.Printf("setup: open folder: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleCheckUpdate runs a manual update check (same data the tray uses).
func handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	updateMu.Lock()
	if updateInProgress {
		updateMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"checking"}`))
		return
	}
	updateInProgress = true
	updateMu.Unlock()
	go func() {
		defer func() {
			updateMu.Lock()
			updateInProgress = false
			lastUpdateCheck = time.Now()
			updateMu.Unlock()
		}()
		repo := ""
		if cfg, _ := loadConfig(); cfg != nil {
			repo = strings.TrimSpace(cfg.UpdateRepo)
		}
		result := CheckForUpdate(repo, true)
		updateMu.Lock()
		pendingUpdate = result.Info
		updateMu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"status":"started"}`))
}

// handleInsightsPage renders the local sync-insights page: persisted cycle
// history, per-game state, conflicts, and the offline outbox.
func handleInsightsPage(w http.ResponseWriter, r *http.Request) {
	history := LoadSyncHistory()
	data := clientwebui.InsightsPageData{
		PageData: clientwebui.PageData{NavActive: "insights", Title: "Sync insights"},
	}
	okCycles, saves7d := 0, 0
	weekAgo := time.Now().AddDate(0, 0, -7)
	perDay := map[string]int{}
	for _, e := range history {
		if e.OK {
			okCycles++
		} else if data.LastFailure == "" {
			data.LastFailure = fmt.Sprintf("%s — %s", e.At.Local().Format("Jan 2 15:04"), e.Err)
		}
		if e.At.After(weekAgo) {
			saves7d += e.SavesSynced
			perDay[e.At.Local().Format("2006-01-02")]++
		}
	}
	data.TotalCycles = len(history)
	data.OKCycles = okCycles
	if data.TotalCycles > 0 {
		data.SuccessPct = okCycles * 100 / data.TotalCycles
	}
	data.SavesSynced7d = saves7d
	maxDay := 1
	for _, n := range perDay {
		if n > maxDay {
			maxDay = n
		}
	}
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i)
		key := day.Local().Format("2006-01-02")
		n := perDay[key]
		data.DayBars = append(data.DayBars, clientwebui.InsightsBar{
			Label: day.Local().Format("Mon"), Count: n, Pct: n * 100 / maxDay,
		})
	}

	snap := GetTraySnapshot()
	for _, g := range snap.Games {
		row := clientwebui.InsightsGameRow{
			GameID:       g.GameID,
			Title:        g.Title,
			Status:       string(g.Status),
			Conflict:     g.HasConflict,
			FirstPathKey: g.FirstPathKey,
		}
		if row.Title == "" {
			row.Title = g.GameID
		}
		if !g.LastSyncAt.IsZero() {
			row.LastSyncAt = g.LastSyncAt.UTC().Format(time.RFC3339)
			row.Direction = string(g.LastDirection)
		}
		data.Games = append(data.Games, row)
	}

	for _, c := range clientsync.ListConflicts() {
		data.Conflicts = append(data.Conflicts, clientwebui.InsightsConflictRow{
			GameID:          c.GameID,
			PathKey:         c.PathKey,
			FilePath:        c.FilePath,
			DetectedAt:      c.DetectedAt.UTC().Format(time.RFC3339),
			Policy:          c.PolicyApplied,
			LocalMtime:      c.LocalMtime,
			ServerUpdatedAt: c.ServerUpdatedAt,
		})
	}
	for _, e := range clientsync.ListOutbox() {
		data.Outbox = append(data.Outbox, clientwebui.InsightsOutboxRow{
			GameID:      e.GameID,
			PathKey:     e.PathKey,
			CreatedAt:   e.CreatedAt.UTC().Format(time.RFC3339),
			Attempts:    e.Attempts,
			SizeBytes:   e.ContentSize,
			NextRetryAt: e.NextRetryAt.UTC().Format(time.RFC3339),
		})
	}

	data.Resolving = r.URL.Query().Get("resolving") == "1"
	for _, a := range RecentActivity(30) {
		data.Activity = append(data.Activity, clientwebui.InsightsActivityRow{
			At: a.At.UTC().Format(time.RFC3339), Title: a.Title, PathKey: a.PathKey,
			Direction: a.Direction, OK: a.OK, Detail: a.Detail,
		})
	}
	fillInsightsStorage(&data)
	clientwebui.RenderInsightsPage(w, data)
}

// fillInsightsStorage adds the server-storage panel data (best-effort with a
// short timeout; the panel hides itself when the server is unreachable or
// pre-5.4). Live fetch, not cached — this page is opened deliberately.
func fillInsightsStorage(data *clientwebui.InsightsPageData) {
	c := getSyncClient()
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	summaries, err := c.FetchSaveSummaries(ctx)
	if err != nil {
		return
	}
	type agg struct {
		title string
		size  int64
	}
	perGame := map[string]*agg{}
	var total int64
	for _, s := range summaries {
		a := perGame[s.GameID]
		if a == nil {
			a = &agg{title: s.GameTitle}
			perGame[s.GameID] = a
		}
		if a.title == "" {
			a.title = s.GameID
		}
		a.size += s.SizeBytes
		total += s.SizeBytes
	}
	rows := make([]clientwebui.InsightsStorageRow, 0, len(perGame))
	var maxSize int64 = 1
	for _, a := range perGame {
		if a.size > maxSize {
			maxSize = a.size
		}
	}
	for _, a := range perGame {
		rows = append(rows, clientwebui.InsightsStorageRow{
			Title: a.title, SizeBytes: a.size, Pct: int(a.size * 100 / maxSize),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SizeBytes > rows[j].SizeBytes })
	if len(rows) > 10 {
		rows = rows[:10]
	}
	data.StorageGames = rows
	data.UsageBytes = total
	data.StorageKnown = true
	if info, ierr := c.FetchAccountInfo(ctx); ierr == nil {
		if info.UsageBytes > 0 {
			// Server truth includes version history; prefer it over the sum
			// of current saves.
			data.UsageBytes = info.UsageBytes
		}
		data.QuotaBytes = info.QuotaBytes
		if info.QuotaBytes > 0 {
			pct := int(data.UsageBytes * 100 / info.QuotaBytes)
			if pct > 100 {
				pct = 100
			}
			data.UsagePct = pct
		}
	}
}

// handleInsightsResolve resolves one conflict from the local UI (per-row
// Keep local / Use server — previously tray-only and all-or-nothing).
func handleInsightsResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	gameID := r.Form.Get("game_id")
	pathKey := r.Form.Get("path_key")
	filePath := r.Form.Get("file_path")
	choice := clientsync.ResolveChoice(r.Form.Get("choice"))
	if gameID == "" || pathKey == "" || (choice != clientsync.ResolveKeepLocal && choice != clientsync.ResolveUseServer) {
		http.Error(w, "missing or invalid parameters", http.StatusBadRequest)
		return
	}
	// resolveConflictAction can block on network for up to 2 minutes — run it
	// in the background and let the page show a "resolving" banner.
	go resolveConflictAction(gameID, pathKey, filePath, choice)
	http.Redirect(w, r, "/insights?resolving=1", http.StatusSeeOther)
}

// handleTestConnection checks server reachability before the user submits
// credentials (the setup form previously logged in blind).
func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	raw := strings.TrimSpace(r.Form.Get("server_url"))
	writeOut := func(ok bool, detail string) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": ok, "detail": detail})
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeOut(false, "Enter a full URL like https://gsbs.example.com")
		return
	}
	target := u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/") + "/api/health"
	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeOut(false, "Invalid URL.")
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		writeOut(false, "Unreachable: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		writeOut(false, fmt.Sprintf("Server answered with status %d — is this a GSBS server?", resp.StatusCode))
		return
	}
	writeOut(true, "Server reachable ✓")
}

// handleInsightsSyncGame flushes one game's pending pushes and triggers a
// pull (the local-UI twin of the tray's per-game "Sync now").
func handleInsightsSyncGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	gameID := r.Form.Get("game_id")
	if gameID == "" {
		http.Error(w, "game_id required", http.StatusBadRequest)
		return
	}
	FlushGame(gameID)
	http.Redirect(w, r, "/insights", http.StatusSeeOther)
}

// handleVersionsPage lists a save slot's server-side version history locally
// (the ListVersions/RestoreVersion client APIs existed since 4.x with no UI —
// the tray used to bounce to the server WebUI).
func handleVersionsPage(w http.ResponseWriter, r *http.Request) {
	gameID := r.URL.Query().Get("game_id")
	pathKey := r.URL.Query().Get("path_key")
	data := clientwebui.VersionsPageData{
		PageData: clientwebui.PageData{NavActive: "insights", Title: "Version history"},
		GameID:   gameID,
		PathKey:  pathKey,
	}
	data.GameTitle = gameTitleFor(gameID)
	data.Restored = r.URL.Query().Get("restored") == "1"
	data.RestoreError = restoreErrorMessage(r.URL.Query().Get("error"))
	c := getSyncClient()
	if c == nil || gameID == "" || pathKey == "" {
		data.NotConnected = true
		clientwebui.RenderVersionsPage(w, data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	versions, err := c.ListVersionsTyped(ctx, gameID, pathKey)
	if err != nil {
		log.Printf("versions: list: %v", err)
		data.NotConnected = true
		clientwebui.RenderVersionsPage(w, data)
		return
	}
	for i, v := range versions {
		data.Versions = append(data.Versions, clientwebui.VersionRow{
			Version: v.Version, UpdatedAt: v.UpdatedAt, SizeBytes: v.SizeBytes,
			ChangeBytes: v.ChangeBytes, ClientName: v.ClientName,
			Current: i == 0,
		})
	}
	clientwebui.RenderVersionsPage(w, data)
}

// restoreErrorMessage maps restore error codes to fixed messages (never
// reflect raw query text into the page).
func restoreErrorMessage(code string) string {
	switch code {
	case "":
		return ""
	case "restore_failed":
		return "the server could not restore that version — it may have been pruned."
	default:
		return "unexpected error; see the client log."
	}
}

func handleVersionsRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	gameID := r.Form.Get("game_id")
	pathKey := r.Form.Get("path_key")
	version, _ := strconv.Atoi(r.Form.Get("version"))
	back := "/versions?game_id=" + url.QueryEscape(gameID) + "&path_key=" + url.QueryEscape(pathKey)
	c := getSyncClient()
	if c == nil || gameID == "" || pathKey == "" || version <= 0 {
		http.Redirect(w, r, back+"&error=restore_failed", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := c.RestoreVersion(ctx, gameID, pathKey, version); err != nil {
		log.Printf("versions: restore v%d game=%s: %v", version, gameID, err)
		http.Redirect(w, r, back+"&error=restore_failed", http.StatusSeeOther)
		return
	}
	// Pull the restored content down right away.
	triggerSyncNow()
	http.Redirect(w, r, back+"&restored=1", http.StatusSeeOther)
}

// GetSetupURL returns the setup page URL if the server started, else "".
func GetSetupURL() string {
	setupURLMu.Lock()
	defer setupURLMu.Unlock()
	return setupURL
}

func resolveClientLogoPath() string {
	// Prefer the professional master assets in assets/images/; fall back to
	// the legacy locations for backward compatibility.
	candidates := []string{
		filepath.Join("assets", "images", "primary-logo.png"),
		filepath.Join("assets", "images", "Logo-Icon-Only.png"),
		filepath.Join("assets", "client-logo.png"),
		filepath.Join("assets", "logo.png"),
		filepath.Join("docs", "images", "gsbs-icon.png"),
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "assets", "images", "primary-logo.png"),
			filepath.Join(exeDir, "assets", "images", "Logo-Icon-Only.png"),
			filepath.Join(exeDir, "assets", "client-logo.png"),
			filepath.Join(exeDir, "assets", "logo.png"),
		)
	}
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func handleClientLogo(w http.ResponseWriter, r *http.Request) {
	if p := resolveClientLogoPath(); p != "" {
		http.ServeFile(w, r, p)
		return
	}
	// Fall back to embedded logo.png from the compiled static assets.
	fs := clientwebui.StaticFiles()
	f, err := fs.Open("logo.png")
	if err == nil {
		defer f.Close()
		stat, err := f.Stat()
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			http.ServeContent(w, r, "logo.png", stat.ModTime(), f.(io.ReadSeeker))
			return
		}
	}
	// Final SVG fallback (should rarely reach here).
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="12" fill="#6366f1"/><path d="M15 24v10h2.2m29 4A21 21 0 0 0 17.2 34M17.2 34H29m20 18V42h-2.2m0 0A21 21 0 0 1 17 37.5M46.8 42H35" fill="none" stroke="#fff" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
}

func handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	done := r.URL.Query().Get("done") != ""
	clientwebui.RenderPage(w, "setup", clientwebui.PageData{
		NavActive:       "setup",
		Title:           "Setup",
		SetupServerURL:  cfg.ServerURL,
		SetupClientName: cfg.ClientName,
		SetupDone:       done,
	})
}

func handleSetupLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		clientwebui.RenderPage(w, "setup", clientwebui.PageData{
			NavActive: "setup",
			Title:     "Setup",
			Error:     "Invalid form",
		})
		return
	}
	server := strings.TrimSpace(r.Form.Get("server_url"))
	user := strings.TrimSpace(r.Form.Get("username"))
	pass := r.Form.Get("password") // don't trim password
	clientName := strings.TrimSpace(r.Form.Get("client_name"))
	if clientName == "" {
		clientName = defaultClientName()
	}
	_, err := DoLoginWithTOTP(server, user, pass, clientName, strings.TrimSpace(r.Form.Get("totp_code")))
	if err == ErrTOTPRequired {
		err = fmt.Errorf("this account has two-factor authentication — enter the 6-digit code from your authenticator app")
	}
	if err != nil {
		clientwebui.RenderPage(w, "setup", clientwebui.PageData{
			NavActive:       "setup",
			Title:           "Setup",
			SetupServerURL:  server,
			SetupClientName: clientName,
			Error:           err.Error(),
		})
		return
	}
	http.Redirect(w, r, "/?done=1", http.StatusSeeOther)
}

type setupStatusResponse struct {
	LoggedIn       bool     `json:"logged_in"`
	AuthFailed     bool     `json:"auth_failed,omitempty"`
	LastScanAt     string   `json:"last_scan_at,omitempty"`
	MatchedGames   int      `json:"matched_games"`
	GameTitles     []string `json:"game_titles,omitempty"`
	ServerURL      string   `json:"server_url,omitempty"`
	LastSyncAt     string   `json:"last_sync_at,omitempty"`
	LastSyncOK     bool     `json:"last_sync_ok"`
	LastSyncErr    string   `json:"last_sync_err,omitempty"`
	WatcherHealthy bool     `json:"watcher_healthy"`
	PendingUploads int      `json:"pending_uploads"`
	ConflictCount  int      `json:"conflict_count"`
	WatchedPaths   int      `json:"watched_paths"`
	Paused         bool     `json:"paused"`
	Metered        bool     `json:"metered"`
	NextSyncETASec int      `json:"next_sync_eta_sec,omitempty"`
	GamesRunning   int      `json:"games_running,omitempty"`
	// UnsafeSkips are manifest-matched games whose save folder resolves to a
	// home/system root the safety guard refuses to watch.
	UnsafeSkips []UnsafeSkip `json:"unsafe_skips,omitempty"`
	// BlockedDirs are watch dirs the process cannot read (Flatpak sandbox
	// grants, permissions); the dashboard lists them with fix commands.
	BlockedDirs []string `json:"blocked_dirs,omitempty"`
	Flatpak     bool     `json:"flatpak,omitempty"`
	// GameAwareLimited: running under Flatpak where game detection covers
	// Steam only (registry signal; the process scan is sandbox-blocked).
	GameAwareLimited bool `json:"game_aware_limited,omitempty"`
	// Updater fields
	UpdateLastCheckedAt  string `json:"update_last_checked_at,omitempty"`
	UpdateLastCheckedAgo string `json:"update_last_checked_ago,omitempty"`
	UpdateStatus         string `json:"update_status,omitempty"` // "up_to_date", "available", "checking", ""
	UpdateAvailableTag   string `json:"update_available_tag,omitempty"`
}

func handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	cfg, _ := loadConfig()
	loggedIn := cfg != nil && strings.TrimSpace(cfg.Token) != ""
	cache := loadDiscoveryCache()
	titles := make([]string, 0, len(cache.MatchedGames))
	for _, g := range cache.MatchedGames {
		name := g.Title
		if name == "" {
			name = g.ManifestGameID
		}
		titles = append(titles, name)
	}
	if len(titles) == 0 {
		titles = append(titles, cache.MatchedGameIDs...)
	}

	snap := GetTraySnapshot()
	syncAt, syncErr := getLastSync()

	watchCount, unsafeSkips := getWatchBuildState()
	resp := setupStatusResponse{
		LoggedIn:       loggedIn,
		AuthFailed:     snap.AuthFailed,
		LastScanAt:     cache.LastScanAt,
		MatchedGames:   len(cache.MatchedGameIDs),
		GameTitles:     titles,
		WatcherHealthy: WatcherHealthy.Load(),
		PendingUploads: snap.PendingUploads,
		ConflictCount:  snap.ConflictCount,
		WatchedPaths: watchCount,
		// Live sources, not the tray snapshot: SyncPaused is the atomic that
		// actually gates doPull/watcher, and "metered" only matters when the
		// skip-on-metered setting is enabled (the snapshot's Metered field is
		// sampled once at menu build and can be stale).
		Paused:      SyncPaused.Load(),
		Metered:     cfg != nil && cfg.SkipSyncWhenMetered && IsMeteredConnection(),
		UnsafeSkips: unsafeSkips,
		BlockedDirs: BlockedWatchDirs(),
		Flatpak:     isFlatpak(),
		GameAwareLimited: isFlatpak() &&
			(cfg == nil || cfg.GameAwareSync == nil || *cfg.GameAwareSync),
		LastSyncOK: syncErr == nil,
	}
	if !syncAt.IsZero() {
		resp.LastSyncAt = syncAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if syncErr != nil {
		resp.LastSyncErr = syncErr.Error()
	}
	if cfg != nil {
		resp.ServerURL = cfg.ServerURL
		if interval := time.Duration(cfg.SyncInterval); interval > 0 && !syncAt.IsZero() {
			if eta := time.Until(syncAt.Add(interval)); eta > 0 {
				resp.NextSyncETASec = int(eta.Seconds())
			}
		}
	}
	resp.GamesRunning = snap.GamesRunning

	// Populate update check state from the tray update loop vars.
	updateMu.Lock()
	lastChecked := lastUpdateCheck
	pending := pendingUpdate
	inProgress := updateInProgress
	updateMu.Unlock()
	if inProgress {
		resp.UpdateStatus = "checking"
	} else if !lastChecked.IsZero() {
		if pending != nil {
			resp.UpdateStatus = "available"
			resp.UpdateAvailableTag = pending.Tag
		} else {
			resp.UpdateStatus = "up_to_date"
		}
		resp.UpdateLastCheckedAt = lastChecked.UTC().Format("2006-01-02T15:04:05Z")
		ago := time.Since(lastChecked)
		switch {
		case ago < time.Minute:
			resp.UpdateLastCheckedAgo = "just now"
		case ago < time.Hour:
			resp.UpdateLastCheckedAgo = fmt.Sprintf("%.0fm ago", ago.Minutes())
		case ago < 24*time.Hour:
			resp.UpdateLastCheckedAgo = fmt.Sprintf("%.0fh ago", ago.Hours())
		default:
			resp.UpdateLastCheckedAgo = fmt.Sprintf("%.0fd ago", ago.Hours()/24)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleOpenLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	logPath := ClientLogPath()
	// Open the log with the platform's default handler (editor/viewer).
	if err := openNative(logPath); err != nil {
		log.Printf("setup: open log: %v", err)
	}
	clientwebui.RenderPage(w, "open_log", clientwebui.PageData{
		NavActive: "help",
		Title:     "Open log",
		LogPath:   logPath,
	})
}

// handleGamesSearch returns manifest matches for the add-game UI as JSON.
func handleGamesSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results := searchManifestGames(q, 40)
	if results == nil {
		results = []manualGameResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
}

// handleGamesAdd processes the add-game form and writes a config watch path.
func handleGamesAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/games", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		clientwebui.RenderPage(w, "games", clientwebui.PageData{
			NavActive: "games",
			Title:     "Add a game",
			Error:     "Invalid form.",
		})
		return
	}
	gameID := strings.TrimSpace(r.Form.Get("game_id"))
	title := strings.TrimSpace(r.Form.Get("title"))
	directory := strings.TrimSpace(r.Form.Get("directory"))
	syncAll := r.Form.Get("sync_all") != "" || strings.TrimSpace(r.Form.Get("patterns")) == ""
	var patterns []string
	for _, p := range strings.Split(r.Form.Get("patterns"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	if err := addManualWatchPath(gameID, title, directory, syncAll, patterns); err != nil {
		clientwebui.RenderPage(w, "games", clientwebui.PageData{
			NavActive: "games",
			Title:     "Add a game",
			Error:     err.Error(),
		})
		return
	}
	name := title
	if name == "" {
		name = gameID
	}
	clientwebui.RenderPage(w, "games", clientwebui.PageData{
		NavActive: "games",
		Title:     "Add a game",
		Success:   fmt.Sprintf("Added %q. Sync restarted — saves in %s will now upload on change.", name, directory),
	})
}

func handleAddGamePage(w http.ResponseWriter, r *http.Request) {
	clientwebui.RenderPage(w, "games", clientwebui.PageData{
		NavActive: "games",
		Title:     "Add a game",
	})
}

func handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	clientwebui.RenderPage(w, "dashboard", clientwebui.PageData{
		NavActive: "dashboard",
		Title:     "Local Status",
	})
}

func handleQuickActionsPage(w http.ResponseWriter, r *http.Request) {
	clientwebui.RenderPage(w, "quick_actions", clientwebui.PageData{
		NavActive: "quick-actions",
		Title:     "Quick actions",
	})
}

func handleHelpPage(w http.ResponseWriter, r *http.Request) {
	clientwebui.RenderPage(w, "help", clientwebui.PageData{
		NavActive: "help",
		Title:     "Help",
	})
}

func handleAboutPage(w http.ResponseWriter, r *http.Request) {
	clientwebui.RenderPage(w, "about", clientwebui.PageData{
		NavActive: "about",
		Title:     "About",
		Version:   Version,
		BuildDate: BuildDate,
		Commit:    Commit,
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	})
}
