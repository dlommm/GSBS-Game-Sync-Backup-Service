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
	allowRegister bool
	templates     *template.Template
}

// NewWebHandler creates a WebHandler. secret is used to sign session cookies; if empty, a default is used (insecure for production).
// adminUsername is the username allowed to access /admin; if empty, admin UI is disabled.
func NewWebHandler(st store.Store, authSvc *auth.Service, secret, adminUsername string, allowRegister bool) *WebHandler {
	if secret == "" {
		secret = "gsbs-default-secret-change-me" // fallback so WebUI works out-of-box; main logs a warning
	}
	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"formatTime":  formatTime,
		"formatBytes": formatBytes,
		"truncate":    truncate,
	}).ParseFS(templatesFS, "templates/*.html"))
	return &WebHandler{store: st, auth: authSvc, secret: secret, adminUsername: adminUsername, allowRegister: allowRegister, templates: tmpl}
}

// formatTime formats an RFC3339 timestamp for display: "just now", "5 mins ago", or "Jan 2, 2006" for older dates.
func formatTime(s string) string {
	if s == "" {
		return "\u2014"
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
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	d := float64(n)
	i := 0
	for d >= 1024 && i < len(units)-1 {
		d /= 1024
		i++
	}
	if d >= 10 || d == float64(int64(d)) {
		return fmt.Sprintf("%d %s", int64(d), units[i])
	}
	return fmt.Sprintf("%.1f %s", d, units[i])
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
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
	h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
	})
}

func (h *WebHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
		})
		return
	}
	userID, err := h.auth.Authenticate(r.Context(), username, password)
	if err != nil {
		h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error":         "Invalid username or password",
			"AllowRegister": h.allowRegister,
		})
		return
	}
	SetSession(w, h.secret, userID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveRegister(w http.ResponseWriter, r *http.Request) {
	h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
	})
}

func (h *WebHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegister {
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Registration is currently disabled by the server administrator.",
			"AllowRegister": false,
		})
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
		})
		return
	}
	if password != confirm {
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Passwords do not match",
			"AllowRegister": h.allowRegister,
		})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), username, password)
	if err != nil {
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Username already taken",
			"AllowRegister": h.allowRegister,
		})
		return
	}
	Redirect(w, r, "/login")
}

// dashboardData is passed to the dashboard template.
type dashboardData struct {
	Username  string
	IsAdmin   bool
	Stats     dashboardStats
	Clients   []store.ClientInfo
	Saves     []store.SaveSummary
	Error     string
}

// dashboardStats holds aggregate counts for the dashboard stat cards.
type dashboardStats struct {
	ClientCount int
	SaveCount   int
	GameCount   int
	TotalBytes  int64
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
	saves, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Username: username, Clients: clients, Error: "Failed to load saves"})
		return
	}
	totalBytes, _ := h.store.UserStorageBytes(r.Context(), userID)
	gameCount, _ := h.store.DistinctGameCount(r.Context(), userID)
	stats := dashboardStats{
		ClientCount: len(clients),
		SaveCount:   len(saves),
		GameCount:   gameCount,
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

// adminData is passed to the admin template.
type adminData struct {
	Username      string
	Stats         adminStats
	Users         []store.UserStatRow
	Clients       []store.ClientInfoWithUser
	Error         string
	Revoked       bool
	AllowRegister bool
}

// adminStats holds global counts shown on the admin page.
type adminStats struct {
	UserCount     int
	ClientCount   int
	SaveCount     int
	ManifestCount int
	TotalBytes    int64
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
	totalBytes, _ := h.store.TotalStorageBytes(r.Context())
	users, _ := h.store.ListUserStats(r.Context())
	clients, _ := h.store.ListAllClients(r.Context())
	revoked := r.URL.Query().Get("revoked") == "1"
	h.templates.ExecuteTemplate(w, "admin.html", adminData{
		Username: username,
		Stats: adminStats{
			UserCount:     userCount,
			ClientCount:   clientCount,
			SaveCount:     saveCount,
			ManifestCount: manifestCount,
			TotalBytes:    totalBytes,
		},
		Users:         users,
		Clients:       clients,
		Revoked:       revoked,
		AllowRegister: h.allowRegister,
	})
}

// handleRevokeClient revokes a client's token (POST /admin/revoke). Admin-only.
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
