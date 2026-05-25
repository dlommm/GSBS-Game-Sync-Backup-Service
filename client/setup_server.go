// Package main: local HTTP server for setup/login in the browser (works reliably on Windows).

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
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
	mux.HandleFunc("/", handleSetupPage)
	mux.HandleFunc("/login", handleSetupLogin)
	mux.HandleFunc("/open-log", handleOpenLog)
	mux.HandleFunc("/status", handleSetupStatus)

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
	LoggedIn     bool     `json:"logged_in"`
	LastScanAt   string   `json:"last_scan_at,omitempty"`
	MatchedGames int      `json:"matched_games"`
	GameTitles   []string `json:"game_titles,omitempty"`
	ServerURL    string   `json:"server_url,omitempty"`
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
		for _, id := range cache.MatchedGameIDs {
			titles = append(titles, id)
		}
	}
	resp := setupStatusResponse{
		LoggedIn:     loggedIn,
		LastScanAt:   cache.LastScanAt,
		MatchedGames: len(cache.MatchedGameIDs),
		GameTitles:   titles,
	}
	if cfg != nil {
		resp.ServerURL = cfg.ServerURL
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
	fmt.Fprintf(w, `<html><head><title>Log</title></head><body><p>Opening log file.</p><p>Path: %s</p><p><a href="/">Back to setup</a></p></body></html>`, htmlEsc(logPath))
}

func writeSetupHTML(w http.ResponseWriter, serverURL, clientName, errMsg string, success, done bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := ""
	if errMsg != "" {
		status = fmt.Sprintf("<p class=\"err\">%s</p>", htmlEsc(errMsg))
	} else if done || success {
		status = `<div id="discoveryPanel" class="ok">
  <p><strong>Step 2 — Discovery</strong></p>
  <p id="discStatus">Scanning installed games…</p>
  <ul id="gameList"></ul>
  <p class="hint">When games appear below, close this page. Sync continues from the tray icon.</p>
  <p><a id="dashLink" href="#" target="_blank" rel="noopener">Open dashboard</a></p>
</div>
<script>
(function poll() {
  fetch('/status').then(r => r.json()).then(function(s) {
    var el = document.getElementById('discStatus');
    if (!el) return;
    if (s.matched_games > 0) {
      el.textContent = 'Found ' + s.matched_games + ' game(s) ready to sync.';
      var ul = document.getElementById('gameList');
      if (ul && s.game_titles) {
        ul.innerHTML = s.game_titles.slice(0, 12).map(function(t) {
          return '<li>' + t.replace(/</g,'&lt;') + '</li>';
        }).join('');
        if (s.matched_games > 12) {
          ul.innerHTML += '<li>… and ' + (s.matched_games - 12) + ' more</li>';
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
<html>
<head>
  <meta charset="utf-8">
  <title>GSBS — Setup</title>
  <style>
    body { font-family: Segoe UI, sans-serif; max-width: 420px; margin: 40px auto; padding: 20px; }
    h1 { font-size: 1.3em; }
    label { display: block; margin-top: 10px; }
    input[type=text], input[type=password] { width: 100%%; padding: 6px; box-sizing: border-box; }
    .err { color: #c00; }
    .ok { color: #080; }
    button { margin-top: 14px; padding: 8px 16px; }
    .hint { font-size: 0.9em; color: #666; margin-top: 2px; }
  </style>
</head>
<body>
  <h1>GSBS — Setup Wizard</h1>
  <p class="hint"><strong>Step 1.</strong> Server URL &amp; login. <strong>Step 2.</strong> After login, the client auto-discovers installed games. <strong>Step 3.</strong> Close this page — sync runs from the tray.</p>
  %s
  <form method="post" action="/login" id="loginForm" onsubmit="return validateForm(this);">
    <label>Server URL</label>
    <input type="text" name="server_url" id="server_url" value="%s" placeholder="https://your-server:8080" required>
    <span class="hint">e.g. https://your-server:8080 or http://localhost:8080</span>
    <label>Username</label>
    <input type="text" name="username" id="username" required>
    <label>Password</label>
    <input type="password" name="password" id="password" required>
    <label>Client name</label>
    <input type="text" name="client_name" value="%s" placeholder="(optional, default: this PC name)">
    <button type="submit">Login</button>
  </form>
  <p style="margin-top: 20px;"><strong>Optional:</strong> <a href="/open-log">Open log file</a> (for troubleshooting). After login, use the tray menu to add launcher paths or edit config.</p>
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
</html>`, status, htmlEsc(serverURL), htmlEsc(clientName))
	w.Write([]byte(page))
}

func htmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
