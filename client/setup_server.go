// Package main: local HTTP server for setup/login in the browser (works reliably on Windows).

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		gameID := r.URL.Query().Get("game_id")
		cfg, _ := loadConfig()
		if cfg != nil && gameID != "" {
			for _, wp := range cfg.WatchPaths {
				if wp.GameID == gameID && wp.Directory != "" {
					target = wp.Directory
					break
				}
			}
		}
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
			GameID:   g.GameID,
			Title:    g.Title,
			Status:   string(g.Status),
			Conflict: g.HasConflict,
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
	clientwebui.RenderInsightsPage(w, data)
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
	NextSyncETASec int      `json:"next_sync_eta_sec,omitempty"`
	GamesRunning   int      `json:"games_running,omitempty"`
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

	resp := setupStatusResponse{
		LoggedIn:       loggedIn,
		AuthFailed:     snap.AuthFailed,
		LastScanAt:     cache.LastScanAt,
		MatchedGames:   len(cache.MatchedGameIDs),
		GameTitles:     titles,
		WatcherHealthy: WatcherHealthy.Load(),
		PendingUploads: snap.PendingUploads,
		ConflictCount:  snap.ConflictCount,
		WatchedPaths:   len(snap.Games) + len(snap.Discovered),
		LastSyncOK:     syncErr == nil,
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
