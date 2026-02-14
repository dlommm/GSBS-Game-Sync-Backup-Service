package webui

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
)

// WebHandler serves the WebUI (login, register, dashboard, admin).
type WebHandler struct {
	store         store.Store
	auth          *auth.Service
	secret        string
	adminUsername string
	templates     *template.Template
}

// NewWebHandler creates a WebHandler. secret is used to sign session cookies; if empty, a default is used (insecure for production).
// adminUsername is the username allowed to access /admin; if empty, admin UI is disabled.
func NewWebHandler(st store.Store, authSvc *auth.Service, secret, adminUsername string) *WebHandler {
	if secret == "" {
		secret = "gsbs-default-secret-change-me"
	}
	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"formatTime": formatTime,
		"formatBytes": formatBytes,
	}).ParseFS(templatesFS, "templates/*.html"))
	return &WebHandler{store: st, auth: authSvc, secret: secret, adminUsername: adminUsername, templates: tmpl}
}

// formatTime formats an RFC3339 timestamp for display: "just now", "5 mins ago", or "Jan 2, 2006" for older dates.
func formatTime(s string) string {
	if s == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	}
	if d < 7*24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("Jan 2, 2006")
}

// formatBytes formats a byte count as "0 B", "512 B", "1.2 MB", etc. (1024-based units).
func formatBytes(n int64) string {
	const unit = 1024
	if n < 0 {
		n = 0
	}
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	if exp >= len(units) {
		exp = len(units) - 1
		div = 1
		for i := 0; i < exp; i++ {
			div *= unit
		}
	}
	v := float64(n) / float64(div)
	if v >= 10 || v == float64(int64(v)) {
		return fmt.Sprintf("%d %s", int64(v), units[exp])
	}
	return fmt.Sprintf("%.1f %s", v, units[exp])
}

func (h *WebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/" || path == "/login":
		if r.Method == http.MethodGet {
			h.serveLogin(w, r)
		} else if r.Method == http.MethodPost {
			h.handleLogin(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/register":
		if r.Method == http.MethodGet {
			h.serveRegister(w, r)
		} else if r.Method == http.MethodPost {
			h.handleRegister(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/dashboard":
		if r.Method == http.MethodGet {
			h.serveDashboard(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/logout":
		if r.Method == http.MethodPost || r.Method == http.MethodGet {
			ClearSession(w)
			Redirect(w, r, "/login")
		} else {
			http.NotFound(w, r)
		}
	case path == "/admin":
		if r.Method == http.MethodGet {
			h.serveAdmin(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/admin/revoke":
		if r.Method == http.MethodPost {
			h.handleRevokeClient(w, r)
		} else {
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (h *WebHandler) serveLogin(w http.ResponseWriter, r *http.Request) {
	if GetSession(r, h.secret) != "" {
		Redirect(w, r, "/dashboard")
		return
	}
	h.templates.ExecuteTemplate(w, "login.html", nil)
}

func (h *WebHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Username and password required"})
		return
	}
	userID, _, err := h.auth.Login(r.Context(), username, password, "web", "web")
	if err != nil {
		h.templates.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid username or password"})
		return
	}
	SetSession(w, h.secret, userID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveRegister(w http.ResponseWriter, r *http.Request) {
	h.templates.ExecuteTemplate(w, "register.html", nil)
}

func (h *WebHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Username and password required"})
		return
	}
	if password != confirm {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Passwords do not match"})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), username, password)
	if err != nil {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Username already taken"})
		return
	}
	Redirect(w, r, "/login")
}

// dashboardData is passed to the dashboard template (clients, saves, stats, and optional error).
type dashboardData struct {
	Username  string
	IsAdmin   bool
	Stats     dashboardStats
	Clients   []store.ClientInfo
	Saves     []saveSummary
	Error     string
}

// dashboardStats holds aggregate counts for the dashboard stat cards.
type dashboardStats struct {
	ClientCount int
	SaveCount   int
	TotalBytes  int64
}

// saveSummary is a row for the synced-saves table (game_id, path_key, updated_at).
type saveSummary struct {
	GameID    string
	PathKey   string
	UpdatedAt string
}

func (h *WebHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	userID := GetSession(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Username: username, Error: "Failed to load clients"})
		return
	}
	saveBlobs, err := h.store.ListSaves(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Username: username, Clients: clients, Error: "Failed to load saves"})
		return
	}
	var totalBytes int64
	saves := make([]saveSummary, len(saveBlobs))
	for i := range saveBlobs {
		saves[i] = saveSummary{
			GameID:    saveBlobs[i].GameID,
			PathKey:   saveBlobs[i].PathKey,
			UpdatedAt: saveBlobs[i].UpdatedAt,
		}
		totalBytes += int64(len(saveBlobs[i].Content))
	}
	stats := dashboardStats{
		ClientCount: len(clients),
		SaveCount:   len(saves),
		TotalBytes:  totalBytes,
	}
	isAdmin := h.adminUsername != "" && username == h.adminUsername
	h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{
		Username: username,
		IsAdmin:  isAdmin,
		Stats:    stats,
		Clients:  clients,
		Saves:    saves,
	})
}

// adminData is passed to the admin template (global stats, users list, clients list, revoke feedback).
type adminData struct {
	Username     string
	Stats        adminStats
	Users        []store.UserInfo
	Clients      []store.ClientInfoWithUser
	Error        string
	Revoked      bool
}

// adminStats holds global counts shown on the admin page (users, clients, saves, manifest entries).
type adminStats struct {
	UserCount     int
	ClientCount   int
	SaveCount     int
	ManifestCount int
}

// serveAdmin renders the admin page. Only the user whose username equals adminUsername may access it.
func (h *WebHandler) serveAdmin(w http.ResponseWriter, r *http.Request) {
	userID := GetSession(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if h.adminUsername == "" || username != h.adminUsername {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	userCount, _ := h.store.CountUsers(r.Context())
	clientCount, _ := h.store.CountClients(r.Context())
	saveCount, _ := h.store.CountSaves(r.Context())
	manifestCount, _ := h.store.CountGameSaveLocations(r.Context())
	users, _ := h.store.ListUsers(r.Context())
	clients, _ := h.store.ListAllClients(r.Context())
	revoked := r.URL.Query().Get("revoked") == "1"
	h.templates.ExecuteTemplate(w, "admin.html", adminData{
		Username: username,
		Stats: adminStats{
			UserCount:     userCount,
			ClientCount:   clientCount,
			SaveCount:     saveCount,
			ManifestCount: manifestCount,
		},
		Users:   users,
		Clients: clients,
		Revoked: revoked,
	})
}

// handleRevokeClient revokes a client's token (POST /admin/revoke). Admin-only; client must run gsbs-client login again.
func (h *WebHandler) handleRevokeClient(w http.ResponseWriter, r *http.Request) {
	userID := GetSession(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if h.adminUsername == "" || username != h.adminUsername {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		Redirect(w, r, "/admin?error=bad_request")
		return
	}
	clientID := r.FormValue("client_id")
	if clientID == "" {
		Redirect(w, r, "/admin?error=missing_client")
		return
	}
	if err := h.store.RegenerateClientToken(r.Context(), clientID); err != nil {
		Redirect(w, r, "/admin?error=revoke_failed")
		return
	}
	Redirect(w, r, "/admin?revoked=1")
}
