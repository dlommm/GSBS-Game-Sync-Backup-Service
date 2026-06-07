// Package main: local HTTP server for setup/login in the browser (works reliably on Windows).

package main

import (
	"encoding/json"
	"fmt"
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
)

const setupPortStart = 41234
const setupPortEnd = 41240

var (
	setupURLMu sync.Mutex
	setupURL   string
)

type localNavLink struct {
	Key   string
	Label string
	Href  string
}

var localNavLinks = []localNavLink{
	{Key: "dashboard", Label: "Status", Href: "/dashboard"},
	{Key: "games", Label: "Games", Href: "/games"},
	{Key: "quick-actions", Label: "Actions", Href: "/quick-actions"},
	{Key: "help", Label: "Help", Href: "/help"},
	{Key: "setup", Label: "Setup", Href: "/"},
}

// StartSetupServer starts a local HTTP server for the setup/login page. Call from tray onReady.
// Listens on 127.0.0.1 only. Returns the base URL (e.g. http://127.0.0.1:41234) or "" if no port bound.
func StartSetupServer() string {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleSetupPage)
	mux.HandleFunc("/login", handleSetupLogin)
	mux.HandleFunc("/open-log", handleOpenLog)
	mux.HandleFunc("/status", handleSetupStatus)
	mux.HandleFunc("/dashboard", handleDashboardPage)
	mux.HandleFunc("/quick-actions", handleQuickActionsPage)
	mux.HandleFunc("/help", handleHelpPage)
	mux.HandleFunc("/about", handleAboutPage)
	mux.HandleFunc("/client-logo", handleClientLogo)
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
		go func() {
			_ = http.Serve(listener, mux)
		}()
		return url
	}
	log.Println("setup: could not bind a port for local setup page (tried 41234–41239)")
	return ""
}

func listenPort(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// GetSetupURL returns the setup page URL if the server started, else "".
func GetSetupURL() string {
	setupURLMu.Lock()
	defer setupURLMu.Unlock()
	return setupURL
}

func localBrandHTML(title, subtitle string) string {
	return fmt.Sprintf(`
<div class="mb-6 flex items-center gap-3">
  <img src="/client-logo" alt="GSBS logo" class="h-11 w-11 rounded-xl object-cover ring-1 ring-emerald-200 shadow-sm dark:ring-emerald-800" />
  <div>
    <h1 class="text-xl font-bold text-gray-900 dark:text-white">%s</h1>
    <p class="text-xs text-gray-500 dark:text-gray-400">%s</p>
  </div>
</div>`, htmlEsc(title), htmlEsc(subtitle))
}

func localNavHTML(active string) string {
	items := make([]string, 0, len(localNavLinks))
	for _, link := range localNavLinks {
		className := "whitespace-nowrap rounded-md px-2.5 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white sm:px-3 sm:py-2 sm:text-sm"
		if link.Key == active {
			className = "whitespace-nowrap rounded-md bg-emerald-600 px-2.5 py-1.5 text-xs font-semibold text-white shadow-sm sm:px-3 sm:py-2 sm:text-sm"
		}
		items = append(items, fmt.Sprintf(`<a href="%s" class="%s">%s</a>`, htmlEsc(link.Href), className, htmlEsc(link.Label)))
	}
	return fmt.Sprintf(`
<header class="sticky top-0 z-20 border-b border-gray-200 bg-white/95 backdrop-blur dark:border-gray-700 dark:bg-gray-800/95">
  <div class="mx-auto max-w-5xl px-3 py-2.5 sm:px-4 sm:py-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex min-w-0 items-center gap-2">
        <img src="/client-logo" alt="GSBS logo" class="h-7 w-7 shrink-0 rounded-md object-cover ring-1 ring-emerald-200 dark:ring-emerald-800 sm:h-8 sm:w-8" />
        <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">GSBS Local Client</span>
      </div>
      <a href="/about" class="whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-white sm:text-sm">
        About
      </a>
    </div>
    <nav class="mt-2 flex flex-wrap items-center gap-1.5 sm:gap-2">%s</nav>
  </div>
</header>`, strings.Join(items, ""))
}

func resolveClientLogoPath() string {
	candidates := []string{
		filepath.Join("assets", "client-logo.png"),
		filepath.Join("assets", "client-logo.jpg"),
		filepath.Join("assets", "logo.png"),
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "assets", "client-logo.png"),
			filepath.Join(exeDir, "assets", "client-logo.jpg"),
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
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="12" fill="#059669"/><path d="M15 24v10h2.2m29 4A21 21 0 0 0 17.2 34M17.2 34H29m20 18V42h-2.2m0 0A21 21 0 0 1 17 37.5M46.8 42H35" fill="none" stroke="#fff" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
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
	success := r.URL.Query().Get("success") != ""
	done := r.URL.Query().Get("done") != ""
	writeSetupHTML(w, cfg.ServerURL, cfg.ClientName, "", success, done)
}

func handleSetupLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeSetupHTML(w, "", "", "Invalid form", false, false)
		return
	}
	server := strings.TrimSpace(r.Form.Get("server_url"))
	user := strings.TrimSpace(r.Form.Get("username"))
	pass := r.Form.Get("password") // don't trim password
	clientName := strings.TrimSpace(r.Form.Get("client_name"))
	if clientName == "" {
		clientName = defaultClientName()
	}
	_, err := DoLogin(server, user, pass, clientName)
	if err != nil {
		writeSetupHTML(w, server, clientName, err.Error(), false, false)
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
	}

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
	// On Windows, try to open the log file in the default editor.
	if runtime.GOOS == "windows" {
		if err := exec.Command("cmd", "/c", "start", "", logPath).Start(); err != nil {
			log.Printf("setup: open log: %v", err)
		}
	} else {
		if err := exec.Command("xdg-open", logPath).Start(); err != nil {
			log.Printf("setup: open log: %v", err)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Open log</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="min-h-full bg-gray-50 dark:bg-gray-900">
%s
<main class="mx-auto max-w-3xl px-4 py-8">
  %s
  <div class="rounded-xl bg-white p-6 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
    <p class="text-sm text-gray-700 dark:text-gray-300">Opening your local client log in the default app.</p>
    <p class="mt-2 break-all text-xs text-gray-500 dark:text-gray-400">%s</p>
    <p class="mt-4 text-xs text-gray-500 dark:text-gray-400">If nothing opens, copy this path and open it manually.</p>
  </div>
</main>
</body>
</html>`, localNavHTML("help"), localBrandHTML("Open client log", "Troubleshoot sync and watcher issues"), htmlEsc(logPath))
	w.Write([]byte(page))
}

func writeSetupHTML(w http.ResponseWriter, serverURL, clientName, errMsg string, success, done bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var statusHTML string
	if errMsg != "" {
		statusHTML = fmt.Sprintf(`
<div class="mb-5 flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 p-4 text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
  <svg class="mt-0.5 h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
  <span>%s</span>
</div>`, htmlEsc(errMsg))
	} else if done || success {
		statusHTML = `
<div id="discoveryPanel" class="mb-5 rounded-lg border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-800 dark:bg-emerald-900/20">
  <div class="mb-2 flex items-center gap-2">
    <svg class="h-5 w-5 text-emerald-600 dark:text-emerald-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
    <span class="font-semibold text-emerald-700 dark:text-emerald-300">Connected — Step 2: Discovery</span>
  </div>
  <p id="discStatus" class="text-sm text-emerald-700 dark:text-emerald-400">Scanning installed games…</p>
  <ul id="gameList" class="mt-2 space-y-1 text-sm text-emerald-800 dark:text-emerald-300"></ul>
  <p class="mt-3 text-xs text-emerald-600 dark:text-emerald-500">When your games appear, close this page — sync continues from the tray icon.</p>
  <div class="mt-3 flex gap-3">
    <a id="dashLink" href="#" target="_blank" rel="noopener"
       class="text-sm font-medium text-emerald-700 underline hover:text-emerald-900 dark:text-emerald-400 dark:hover:text-emerald-200">
      Open server dashboard →
    </a>
    <a href="/dashboard"
       class="text-sm font-medium text-emerald-700 underline hover:text-emerald-900 dark:text-emerald-400 dark:hover:text-emerald-200">
      Local status →
    </a>
  </div>
</div>
<script>
(function poll() {
  fetch('/status').then(function(r){return r.json();}).then(function(s) {
    var el = document.getElementById('discStatus');
    if (!el) return;
    if (s.matched_games > 0) {
      el.textContent = 'Found ' + s.matched_games + ' game(s) ready to sync.';
      var ul = document.getElementById('gameList');
      if (ul && s.game_titles) {
        ul.innerHTML = s.game_titles.slice(0, 12).map(function(t) {
          return '<li class="flex items-center gap-1"><span class="text-emerald-500">✓</span>' + t.replace(/</g,'&lt;') + '</li>';
        }).join('');
        if (s.matched_games > 12) {
          ul.innerHTML += '<li class="text-emerald-600">… and ' + (s.matched_games - 12) + ' more</li>';
        }
      }
    } else if (s.logged_in) {
      el.textContent = 'Logged in — discovery in progress…';
      setTimeout(poll, 2000);
    }
    if (s.server_url) {
      var dash = document.getElementById('dashLink');
      if (dash) dash.href = s.server_url.replace(/\/$/, '') + '/dashboard';
    }
  }).catch(function() { setTimeout(poll, 3000); });
})();
</script>`
	}

	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Setup</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="h-full bg-gray-50 dark:bg-gray-900">
<div class="flex min-h-full items-center justify-center p-4">
  <div class="w-full max-w-md">

    %s

    <div class="mb-6 flex items-center gap-2 text-xs font-medium">
      <span class="rounded-full bg-emerald-600 px-2.5 py-0.5 text-white">1 — Connect</span>
      <span class="text-gray-400">→</span>
      <span class="rounded-full bg-gray-200 px-2.5 py-0.5 text-gray-500 dark:bg-gray-700 dark:text-gray-400">2 — Discover</span>
      <span class="text-gray-400">→</span>
      <span class="rounded-full bg-gray-200 px-2.5 py-0.5 text-gray-500 dark:bg-gray-700 dark:text-gray-400">3 — Sync</span>
    </div>

    %s

    <div class="rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <div class="p-6">
        <h2 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">Server connection</h2>
        <form method="post" action="/login" id="loginForm" onsubmit="return validateForm(this);" class="space-y-4">
          <div>
            <label for="server_url" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
              Server URL
            </label>
            <input type="text" name="server_url" id="server_url" value="%s"
              placeholder="https://your-server:8080"
              required
              class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">e.g. https://your-server:8080 or http://localhost:8080</p>
          </div>
          <div>
            <label for="username" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
              Username
            </label>
            <input type="text" name="username" id="username" required autocomplete="username"
              class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white">
          </div>
          <div>
            <label for="password" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
              Password
            </label>
            <input type="password" name="password" id="password" required autocomplete="current-password"
              class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white">
          </div>
          <div>
            <label for="client_name" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
              Client name <span class="font-normal text-gray-400">(optional)</span>
            </label>
            <input type="text" name="client_name" id="client_name" value="%s"
              placeholder="this PC name"
              class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
          </div>
          <div class="flex gap-3 pt-1">
            <button type="submit"
              class="flex-1 rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white shadow-sm hover:bg-emerald-700 focus:outline-none focus:ring-2 focus:ring-emerald-500 focus:ring-offset-2 dark:focus:ring-offset-gray-800">
              Connect
            </button>
          </div>
        </form>
      </div>
    </div>

    <p class="mt-4 text-center text-xs text-gray-400 dark:text-gray-500">
      <a href="/open-log" class="hover:underline">Open log file</a>
      &nbsp;·&nbsp;
      <a href="/dashboard" class="hover:underline">Local status</a>
      &nbsp;·&nbsp;
      <a href="/games" class="hover:underline">Add a game</a>
      &nbsp;·&nbsp;
      <a href="/help" class="hover:underline">Help</a>
    </p>
  </div>
</div>
<script>
function validateForm(form) {
  var server = (form.server_url && form.server_url.value) ? form.server_url.value.trim() : '';
  if (!server) { alert('Please enter the server URL.'); return false; }
  if (server.indexOf('http://') !== 0 && server.indexOf('https://') !== 0) {
    alert('Server URL should start with http:// or https://');
    return false;
  }
  if (!form.username.value.trim()) { alert('Please enter username.'); return false; }
  if (!form.password.value) { alert('Please enter password.'); return false; }
  return true;
}
</script>
</body>
</html>`, localBrandHTML("GSBS Setup", "Game Sync & Backup Service"), statusHTML, htmlEsc(serverURL), htmlEsc(clientName))
	w.Write([]byte(page))
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
		writeAddGameHTML(w, "Invalid form.", "")
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
		writeAddGameHTML(w, err.Error(), "")
		return
	}
	name := title
	if name == "" {
		name = gameID
	}
	writeAddGameHTML(w, "", fmt.Sprintf("Added %q. Sync restarted — saves in %s will now upload on change.", name, directory))
}

func handleAddGamePage(w http.ResponseWriter, r *http.Request) {
	writeAddGameHTML(w, "", "")
}

func writeAddGameHTML(w http.ResponseWriter, errMsg, okMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var bannerHTML string
	if errMsg != "" {
		bannerHTML = fmt.Sprintf(`
<div class="mb-5 flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
  <svg class="mt-0.5 h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
  <span>%s</span>
</div>`, htmlEsc(errMsg))
	} else if okMsg != "" {
		bannerHTML = fmt.Sprintf(`
<div class="mb-5 flex items-start gap-3 rounded-lg border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-700 dark:border-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-400">
  <svg class="mt-0.5 h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>
  <span>%s</span>
</div>`, htmlEsc(okMsg))
	}

	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Add a game</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="bg-gray-50 dark:bg-gray-900">
%s
<div class="mx-auto max-w-xl px-4 py-8">

  %s

  %s

  <div class="rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
    <div class="p-6">
      <p class="mb-4 text-sm text-gray-600 dark:text-gray-400">
        Search by game name to fill the save folder automatically, or paste the full path manually.
        The save folder must already exist on this PC.
      </p>

      <div class="mb-4">
        <label for="search" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
          Search games
        </label>
        <input type="text" id="search" placeholder="e.g. Witcher 3" autocomplete="off"
          class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
      </div>

      <ul id="results" class="mb-4 space-y-2"></ul>
    </div>
  </div>

  <div class="mt-4 rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
    <div class="p-6">
      <h2 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">Save folder details</h2>
      <form method="post" action="/games/add" class="space-y-4">
        <div>
          <label for="game_id" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">Game ID</label>
          <input type="text" name="game_id" id="game_id" required placeholder="manifest game id"
            class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
        </div>
        <div>
          <label for="title" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
            Title <span class="font-normal text-gray-400">(optional)</span>
          </label>
          <input type="text" name="title" id="title" placeholder="display name"
            class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
        </div>
        <div>
          <label for="directory" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">Save folder (absolute path)</label>
          <input type="text" name="directory" id="directory" required
            placeholder="e.g. C:\Users\you\Documents\The Witcher 3"
            class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Subfolders are watched too. The folder must exist.</p>
        </div>
        <div>
          <label for="patterns" class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">
            Include patterns <span class="font-normal text-gray-400">(optional, comma-separated)</span>
          </label>
          <input type="text" name="patterns" id="patterns"
            placeholder="*.sav, *.save  (leave blank to sync all files)"
            class="block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 shadow-sm placeholder:text-gray-400 focus:border-emerald-500 focus:outline-none focus:ring-1 focus:ring-emerald-500 dark:border-gray-600 dark:bg-gray-700 dark:text-white dark:placeholder:text-gray-500">
        </div>
        <button type="submit"
          class="w-full rounded-lg bg-emerald-600 px-4 py-2 text-sm font-semibold text-white shadow-sm hover:bg-emerald-700 focus:outline-none focus:ring-2 focus:ring-emerald-500 focus:ring-offset-2 dark:focus:ring-offset-gray-800">
          Add game
        </button>
      </form>
    </div>
  </div>

</div>
<script>
var searchTimer;
var searchBox = document.getElementById('search');
searchBox.addEventListener('input', function() {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(runSearch, 250);
});
function runSearch() {
  var q = searchBox.value;
  fetch('/games/search?q=' + encodeURIComponent(q)).then(function(r){return r.json();}).then(function(d){
    var ul = document.getElementById('results');
    ul.innerHTML = '';
    (d.results || []).forEach(function(g){
      var li = document.createElement('li');
      var badgeCls = g.exists
        ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
        : 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400';
      var badgeText = g.exists ? 'folder found' : 'folder missing';
      li.className = 'rounded-lg border border-gray-200 bg-white p-3 dark:border-gray-700 dark:bg-gray-750';
      li.innerHTML =
        '<div class="flex items-start justify-between gap-2">' +
          '<div>' +
            '<div class="flex items-center gap-2">' +
              '<span class="font-medium text-gray-900 dark:text-white text-sm">' + esc(g.title) + '</span>' +
              '<span class="rounded-full px-2 py-0.5 text-xs font-medium ' + badgeCls + '">' + badgeText + '</span>' +
            '</div>' +
            '<div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">' + esc(g.game_id) + (g.directory ? ' &middot; ' + esc(g.directory) : ' &middot; (no path resolved)') + '</div>' +
          '</div>' +
          '<button type="button" class="use shrink-0 rounded-md bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-emerald-100 hover:text-emerald-700 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-emerald-900/30 dark:hover:text-emerald-400">Use this</button>' +
        '</div>';
      li.querySelector('.use').addEventListener('click', function(){
        document.getElementById('game_id').value = g.game_id;
        document.getElementById('title').value = g.title;
        document.getElementById('directory').value = g.directory || '';
        window.scrollTo(0, document.body.scrollHeight);
      });
      ul.appendChild(li);
    });
  }).catch(function(){});
}
function esc(s){ return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
runSearch();
</script>
</body>
</html>`, localNavHTML("games"), localBrandHTML("Add a game to sync", "Manually add save folders when auto-discovery misses one"), bannerHTML)
	w.Write([]byte(page))
}

func handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(dashboardPageHTML, localNavHTML("dashboard"), localBrandHTML("GSBS local status", "Live sync health for this machine"))
	w.Write([]byte(page))
}

func handleQuickActionsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Quick actions</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="min-h-full bg-gray-50 dark:bg-gray-900">
%s
<main class="mx-auto max-w-4xl px-4 py-8">
  %s
  <div class="grid gap-3 sm:grid-cols-2">
    <button type="button" onclick="triggerSync(this)" class="rounded-xl bg-emerald-600 px-4 py-3 text-left text-sm font-semibold text-white shadow-sm hover:bg-emerald-700">Run sync now</button>
    <a href="/dashboard" class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-200 dark:ring-gray-700">Open local dashboard</a>
    <a href="/games" class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-200 dark:ring-gray-700">Add a game folder</a>
    <a href="/open-log" class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-200 dark:ring-gray-700">Open log file</a>
    <a href="/help" class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-200 dark:ring-gray-700">Troubleshooting help</a>
    <a href="/" class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-200 dark:ring-gray-700">Return to setup</a>
  </div>
</main>
<script>
function triggerSync(btn) {
  btn.disabled = true;
  btn.textContent = 'Syncing...';
  fetch('/api/sync-now', {method:'POST'}).then(function() {
    btn.textContent = 'Sync requested';
    setTimeout(function() {
      btn.disabled = false;
      btn.textContent = 'Run sync now';
    }, 1600);
  }).catch(function() {
    btn.disabled = false;
    btn.textContent = 'Try sync again';
  });
}
</script>
</body>
</html>`, localNavHTML("quick-actions"), localBrandHTML("Quick actions", "Fast shortcuts for common local client tasks"))
	w.Write([]byte(page))
}

func handleHelpPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Help</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="min-h-full bg-gray-50 dark:bg-gray-900">
%s
<main class="mx-auto max-w-4xl px-4 py-8">
  %s
  <div class="space-y-3">
    <section class="rounded-xl bg-white p-5 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white">What each status means</h2>
      <ul class="mt-2 space-y-1 text-sm text-gray-600 dark:text-gray-300">
        <li><strong>Connection:</strong> login and token state for this client.</li>
        <li><strong>Watcher:</strong> file watch pipeline health on your local save folders.</li>
        <li><strong>Pending uploads:</strong> queued files still waiting to push.</li>
        <li><strong>Conflicts:</strong> manual choice needed from the tray menu.</li>
      </ul>
    </section>
    <section class="rounded-xl bg-white p-5 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white">Quick troubleshooting</h2>
      <ol class="mt-2 list-decimal space-y-1 pl-5 text-sm text-gray-600 dark:text-gray-300">
        <li>Open <a class="font-medium text-emerald-700 underline dark:text-emerald-400" href="/dashboard">Dashboard</a> and check for auth or watcher warnings.</li>
        <li>Use <a class="font-medium text-emerald-700 underline dark:text-emerald-400" href="/open-log">Open log</a> to inspect recent sync errors.</li>
        <li>If a game is missing, add the save folder from <a class="font-medium text-emerald-700 underline dark:text-emerald-400" href="/games">Add game</a>.</li>
        <li>Run a manual sync from <a class="font-medium text-emerald-700 underline dark:text-emerald-400" href="/quick-actions">Quick actions</a>.</li>
      </ol>
    </section>
  </div>
</main>
</body>
</html>`, localNavHTML("help"), localBrandHTML("Client help", "Understand status cards and recover quickly when sync fails"))
	w.Write([]byte(page))
}

func handleAboutPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — About this client</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="min-h-full bg-gray-50 dark:bg-gray-900">
%s
<main class="mx-auto max-w-3xl px-4 py-8">
  %s
  <div class="rounded-xl bg-white p-6 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
    <dl class="grid grid-cols-1 gap-4 text-sm sm:grid-cols-2">
      <div>
        <dt class="text-gray-500 dark:text-gray-400">Client version</dt>
        <dd class="mt-1 font-semibold text-gray-900 dark:text-white">%s</dd>
      </div>
      <div>
        <dt class="text-gray-500 dark:text-gray-400">Runtime</dt>
        <dd class="mt-1 font-semibold text-gray-900 dark:text-white">%s</dd>
      </div>
      <div class="sm:col-span-2">
        <dt class="text-gray-500 dark:text-gray-400">What this client does</dt>
        <dd class="mt-1 text-gray-700 dark:text-gray-300">Watches local save folders, uploads changes, and pulls fresh saves from your GSBS server.</dd>
      </div>
    </dl>
  </div>
</main>
</body>
</html>`, localNavHTML("about"), localBrandHTML("About this client", "Build and runtime details for your local GSBS app"), htmlEsc(Version), htmlEsc(runtime.GOOS+"/"+runtime.GOARCH))
	w.Write([]byte(page))
}

const dashboardPageHTML = `<!DOCTYPE html>
<html lang="en" class="h-full">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GSBS — Local Status</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>tailwind.config={darkMode:'media'}</script>
</head>
<body class="bg-gray-50 dark:bg-gray-900 min-h-full">
%s
<div class="mx-auto max-w-2xl px-4 py-8">
  <div class="mb-6">
    %s
    <p id="refreshLabel" class="text-xs text-gray-400 dark:text-gray-500">Loading…</p>
  </div>
  <div class="mb-6 flex flex-wrap items-center gap-2">
    <div class="flex flex-wrap gap-2">
      <button id="syncNowBtn" onclick="triggerSync()"
        class="rounded-lg bg-emerald-600 px-3 py-2 text-sm font-semibold text-white shadow-sm hover:bg-emerald-700 focus:outline-none focus:ring-2 focus:ring-emerald-500 disabled:opacity-50">
        Sync now
      </button>
      <a id="serverDashLink" href="#" target="_blank" rel="noopener"
        class="rounded-lg bg-white px-3 py-2 text-sm font-semibold text-gray-700 shadow-sm ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:ring-gray-700 dark:hover:bg-gray-750">
        Server dashboard →
      </a>
    </div>
  </div>

  <div class="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
    <div class="rounded-xl bg-white p-4 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <p class="text-xs font-medium text-gray-500 dark:text-gray-400">Connection</p>
      <div id="connStatus" class="mt-1 flex items-center gap-1.5">
        <span class="h-2 w-2 rounded-full bg-gray-300 dark:bg-gray-600"></span>
        <span class="text-sm font-semibold text-gray-900 dark:text-white">—</span>
      </div>
      <p id="serverURLLabel" class="mt-1 truncate text-xs text-gray-400 dark:text-gray-500"></p>
      <p id="authFailedLabel" class="mt-1 hidden text-xs font-medium text-red-600 dark:text-red-400">⚠ Re-login required</p>
    </div>
    <div class="rounded-xl bg-white p-4 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <p class="text-xs font-medium text-gray-500 dark:text-gray-400">Last sync</p>
      <div id="lastSyncStatus" class="mt-1 flex items-center gap-1.5">
        <span class="h-2 w-2 rounded-full bg-gray-300 dark:bg-gray-600"></span>
        <span class="text-sm font-semibold text-gray-900 dark:text-white">—</span>
      </div>
      <p id="lastSyncErr" class="mt-1 truncate text-xs text-red-500 dark:text-red-400"></p>
    </div>
    <div class="rounded-xl bg-white p-4 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <p class="text-xs font-medium text-gray-500 dark:text-gray-400">Watcher</p>
      <div id="watcherStatus" class="mt-1 flex items-center gap-1.5">
        <span class="h-2 w-2 rounded-full bg-gray-300 dark:bg-gray-600"></span>
        <span class="text-sm font-semibold text-gray-900 dark:text-white">—</span>
      </div>
    </div>
    <div class="rounded-xl bg-white p-4 shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
      <p class="text-xs font-medium text-gray-500 dark:text-gray-400">Discovered</p>
      <p id="discoveredCount" class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">—</p>
      <p class="mt-0.5 text-xs text-gray-400 dark:text-gray-500">games matched</p>
    </div>
  </div>

  <div id="updateCard" class="mb-4 hidden rounded-xl bg-amber-50 p-4 shadow-sm ring-1 ring-amber-200 dark:bg-amber-900/20 dark:ring-amber-800">
    <div class="flex items-center justify-between gap-2">
      <div>
        <p class="text-xs font-medium text-amber-700 dark:text-amber-400">Update available</p>
        <p id="updateTag" class="mt-0.5 text-sm font-semibold text-amber-800 dark:text-amber-300">—</p>
      </div>
      <svg class="h-5 w-5 shrink-0 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>
      </svg>
    </div>
    <p class="mt-1 text-xs text-amber-600 dark:text-amber-500">Open the tray menu → Advanced → Install update</p>
  </div>

  <div id="updateCheckedRow" class="mb-3 hidden text-xs text-gray-400 dark:text-gray-500">
    <span>Update check: </span><span id="updateCheckedAgo">—</span>
    <span id="updateUpToDateBadge" class="ml-1 inline-flex items-center gap-0.5 rounded-full bg-emerald-100 px-1.5 py-0.5 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400">
      <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/></svg>
      up to date
    </span>
  </div>

  <div class="mb-4 grid grid-cols-2 gap-3">
    <div id="pendingCard" class="hidden rounded-xl bg-amber-50 p-4 shadow-sm ring-1 ring-amber-200 dark:bg-amber-900/20 dark:ring-amber-800">
      <p class="text-xs font-medium text-amber-600 dark:text-amber-400">Pending uploads</p>
      <p id="pendingCount" class="mt-1 text-2xl font-bold text-amber-700 dark:text-amber-300">0</p>
    </div>
    <div id="conflictCard" class="hidden rounded-xl bg-red-50 p-4 shadow-sm ring-1 ring-red-200 dark:bg-red-900/20 dark:ring-red-800">
      <p class="text-xs font-medium text-red-600 dark:text-red-400">Conflicts</p>
      <p id="conflictCount" class="mt-1 text-2xl font-bold text-red-700 dark:text-red-300">0</p>
      <p class="mt-0.5 text-xs text-red-500 dark:text-red-400">Use tray menu to resolve</p>
    </div>
  </div>

  <div class="rounded-xl bg-white shadow-sm ring-1 ring-gray-200 dark:bg-gray-800 dark:ring-gray-700">
    <div class="border-b border-gray-100 px-4 py-3 dark:border-gray-700">
      <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Matched games</h2>
    </div>
    <ul id="gameList" class="divide-y divide-gray-100 dark:divide-gray-700">
      <li class="px-4 py-3 text-sm text-gray-400 dark:text-gray-500">Loading…</li>
    </ul>
  </div>

  <p class="mt-4 text-center text-xs text-gray-400 dark:text-gray-500">
    <a href="/quick-actions" class="hover:underline">Quick actions</a>
    &nbsp;·&nbsp;
    <a href="/help" class="hover:underline">Help</a>
    &nbsp;·&nbsp;
    <a href="/open-log" class="hover:underline">Open log</a>
  </p>

</div>
<script>
function timeAgo(iso) {
  if (!iso) return '—';
  var d = new Date(iso);
  var sec = Math.floor((Date.now() - d) / 1000);
  if (sec < 5) return 'just now';
  if (sec < 60) return sec + 's ago';
  if (sec < 3600) return Math.floor(sec/60) + 'm ago';
  if (sec < 86400) return (sec/3600).toFixed(1) + 'h ago';
  return Math.floor(sec/86400) + 'd ago';
}

function setDot(el, ok) {
  var dot = el.querySelector('span:first-child');
  var label = el.querySelector('span:last-child');
  if (ok === true) {
    dot.className = 'h-2 w-2 rounded-full bg-emerald-500';
  } else if (ok === false) {
    dot.className = 'h-2 w-2 rounded-full bg-red-500';
  } else {
    dot.className = 'h-2 w-2 rounded-full bg-gray-300 dark:bg-gray-600';
  }
  return label;
}

function refresh() {
  fetch('/status').then(function(r){return r.json();}).then(function(s) {
    document.getElementById('refreshLabel').textContent = 'Updated just now';

    var connOK = s.logged_in && !s.auth_failed;
    var connLabel = setDot(document.getElementById('connStatus'), s.logged_in ? (s.auth_failed ? false : true) : false);
    connLabel.textContent = s.auth_failed ? 'Re-login required' : (s.logged_in ? 'Logged in' : 'Not connected');
    document.getElementById('serverURLLabel').textContent = s.server_url || '';
    var authFailed = document.getElementById('authFailedLabel');
    if (authFailed) authFailed.classList.toggle('hidden', !s.auth_failed);
    if (s.server_url) {
      document.getElementById('serverDashLink').href = s.server_url.replace(/\/$/, '') + '/dashboard';
    }

    var syncOK = s.last_sync_at ? s.last_sync_ok : null;
    var syncLabel = setDot(document.getElementById('lastSyncStatus'), syncOK);
    syncLabel.textContent = s.last_sync_at ? timeAgo(s.last_sync_at) : 'Never';
    var errEl = document.getElementById('lastSyncErr');
    errEl.textContent = s.last_sync_err || '';

    var watchLabel = setDot(document.getElementById('watcherStatus'), s.watcher_healthy);
    watchLabel.textContent = s.watcher_healthy ? 'Healthy' : 'Recovering';

    document.getElementById('discoveredCount').textContent = s.matched_games;

    var pending = document.getElementById('pendingCard');
    document.getElementById('pendingCount').textContent = s.pending_uploads;
    pending.classList.toggle('hidden', s.pending_uploads === 0);

    var conflict = document.getElementById('conflictCard');
    document.getElementById('conflictCount').textContent = s.conflict_count;
    conflict.classList.toggle('hidden', s.conflict_count === 0);

    // Updater status
    var updateCard = document.getElementById('updateCard');
    var updateCheckedRow = document.getElementById('updateCheckedRow');
    var updateUpToDateBadge = document.getElementById('updateUpToDateBadge');
    if (s.update_status === 'available' && s.update_available_tag) {
      document.getElementById('updateTag').textContent = s.update_available_tag;
      updateCard.classList.remove('hidden');
    } else {
      updateCard.classList.add('hidden');
    }
    if (s.update_last_checked_ago) {
      document.getElementById('updateCheckedAgo').textContent = s.update_last_checked_ago;
      updateCheckedRow.classList.remove('hidden');
      if (updateUpToDateBadge) updateUpToDateBadge.classList.toggle('hidden', s.update_status !== 'up_to_date');
    } else if (s.update_status === 'checking') {
      document.getElementById('updateCheckedAgo').textContent = 'checking…';
      updateCheckedRow.classList.remove('hidden');
      if (updateUpToDateBadge) updateUpToDateBadge.classList.add('hidden');
    } else {
      updateCheckedRow.classList.add('hidden');
    }

    var ul = document.getElementById('gameList');
    var titles = s.game_titles || [];
    if (titles.length === 0) {
      ul.innerHTML = '<li class="px-4 py-3 text-sm text-gray-400 dark:text-gray-500">No games matched yet — discovery runs automatically after login.</li>';
    } else {
      ul.innerHTML = titles.slice(0, 20).map(function(t) {
        return '<li class="flex items-center gap-2 px-4 py-2.5 text-sm">' +
          '<svg class="h-4 w-4 text-emerald-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg>' +
          '<span class="text-gray-800 dark:text-gray-200">' + t.replace(/</g,'&lt;') + '</span>' +
          '</li>';
      }).join('');
      if (titles.length > 20) {
        ul.innerHTML += '<li class="px-4 py-2.5 text-xs text-gray-400 dark:text-gray-500">… and ' + (titles.length - 20) + ' more</li>';
      }
    }
  }).catch(function() {
    document.getElementById('refreshLabel').textContent = 'Could not reach status endpoint';
  });
}

function triggerSync() {
  var btn = document.getElementById('syncNowBtn');
  btn.disabled = true;
  btn.textContent = 'Syncing…';
  fetch('/api/sync-now', {method:'POST'}).then(function() {
    setTimeout(function() {
      btn.disabled = false;
      btn.textContent = 'Sync now';
      refresh();
    }, 2000);
  }).catch(function() {
    btn.disabled = false;
    btn.textContent = 'Sync now';
  });
}

refresh();
setInterval(refresh, 5000);
</script>
</body>
</html>`

func htmlEsc(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
