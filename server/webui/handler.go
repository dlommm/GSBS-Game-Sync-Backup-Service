package webui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"image/png"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/api"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/pquerna/otp/totp"
)

// WebHandler serves the WebUI (login, register, dashboard, admin).
type WebHandler struct {
	store            store.Store
	auth             *auth.Service
	secret           string
	adminUsername    string
	allowRegister    bool
	templates        *template.Template
	hub              *sse.Hub
	apiHandler       *api.Handler
	jobRunner        *job.Runner
	maxStorageBytes  int64
	readOnly         bool
}

// NewWebHandler creates a WebHandler. secret is used to sign session cookies; if empty, a default is used (insecure for production).
// adminUsername is the username allowed to access /admin; if empty, admin UI is disabled.
func NewWebHandler(st store.Store, authSvc *auth.Service, secret, adminUsername string, allowRegister bool, hub *sse.Hub, apiHandler *api.Handler, jobRunner *job.Runner, maxStorageBytes int64, readOnly bool) *WebHandler {
	if secret == "" {
		secret = "gsbs-default-secret-change-me" // fallback so WebUI works out-of-box; main logs a warning
	}
	tmpl := template.Must(template.New("").Funcs(template.FuncMap{
		"formatTime":     formatTime,
		"formatBytes":    formatBytes,
		"truncate":       truncate,
		"formatDuration": formatDuration,
		"urlquery":       url.QueryEscape,
	}).ParseFS(templatesFS, "templates/*.html"))
	return &WebHandler{store: st, auth: authSvc, secret: secret, adminUsername: adminUsername, allowRegister: allowRegister, templates: tmpl, hub: hub, apiHandler: apiHandler, jobRunner: jobRunner, maxStorageBytes: maxStorageBytes, readOnly: readOnly}
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

// isAdminUser returns true if the user has admin access (DB role or GSBS_ADMIN_USERNAME match).
func (h *WebHandler) isAdminUser(ctx context.Context, userID, username string) bool {
	role, err := h.store.UserRole(ctx, userID)
	if err == nil && role == "admin" {
		return true
	}
	return h.adminUsername != "" && username == h.adminUsername
}

// formatDuration computes human-readable duration between two RFC3339 timestamps.
func formatDuration(start, end string) string {
	if start == "" || end == "" {
		return "—"
	}
	t1, err1 := time.Parse(time.RFC3339, start)
	t2, err2 := time.Parse(time.RFC3339, end)
	if err1 != nil || err2 != nil {
		return "—"
	}
	d := t2.Sub(t1)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func (h *WebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/login/totp":
		if r.Method == http.MethodGet {
			h.serveLoginTOTP(w, r)
		} else if r.Method == http.MethodPost {
			h.handleLoginTOTP(w, r)
		} else {
			http.NotFound(w, r)
		}
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
	case path == "/dashboard/events" && r.Method == http.MethodGet:
		h.serveDashboardEvents(w, r)
	case path == "/dashboard/partial/saves" && r.Method == http.MethodGet:
		h.serveDashboardSavesPartial(w, r)
	case path == "/dashboard/partial/activity" && r.Method == http.MethodGet:
		h.serveDashboardActivityPartial(w, r)
	case path == "/logout":
		if r.Method == http.MethodPost {
			if !ValidateCSRF(r, h.secret) {
				http.Error(w, "Invalid security token.", http.StatusBadRequest)
				return
			}
			if sessionID := GetSessionID(r, h.secret); sessionID != "" {
				_ = h.store.DeleteSession(r.Context(), sessionID)
			}
			ClearSession(w)
			Redirect(w, r, "/login")
		} else if r.Method == http.MethodGet {
			if sessionID := GetSessionID(r, h.secret); sessionID != "" {
				_ = h.store.DeleteSession(r.Context(), sessionID)
			}
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
	case path == "/admin/push-manifest":
		if r.Method == http.MethodPost {
			h.handlePushManifest(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/admin/run-job":
		if r.Method == http.MethodPost {
			h.handleRunJob(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/admin/user/disable" && r.Method == http.MethodPost:
		h.handleDisableUser(w, r)
	case path == "/admin/user/enable" && r.Method == http.MethodPost:
		h.handleEnableUser(w, r)
	case path == "/admin/user/delete" && r.Method == http.MethodPost:
		h.handleDeleteUser(w, r)
	case path == "/admin/user/quota" && r.Method == http.MethodPost:
		h.handleSetUserQuota(w, r)
	case path == "/dashboard/save/versions" && r.Method == http.MethodGet:
		h.serveSaveVersions(w, r)
	case path == "/dashboard/save/versions/restore" && r.Method == http.MethodPost:
		h.handleRestoreVersion(w, r)
	case path == "/dashboard/save/versions/download" && r.Method == http.MethodGet:
		h.serveSaveVersionDownload(w, r)
	case path == "/dashboard/save/delete" && r.Method == http.MethodPost:
		h.handleDeleteSave(w, r)
	case path == "/dashboard/settings" && r.Method == http.MethodGet:
		h.serveSettings(w, r)
	case path == "/dashboard/settings" && r.Method == http.MethodPost:
		h.handleSettings(w, r)
	case path == "/dashboard/settings/sessions/revoke" && r.Method == http.MethodPost:
		h.handleRevokeSession(w, r)
	case path == "/dashboard/settings/sessions/revoke-all" && r.Method == http.MethodPost:
		h.handleRevokeAllSessions(w, r)
	case path == "/dashboard/settings/2fa/enable" && r.Method == http.MethodGet:
		h.serveEnable2FA(w, r)
	case path == "/dashboard/settings/2fa/confirm" && r.Method == http.MethodPost:
		h.handleConfirm2FA(w, r)
	case path == "/dashboard/settings/2fa/disable" && r.Method == http.MethodPost:
		h.handleDisable2FA(w, r)
	case path == "/admin/manifest.csv" && r.Method == http.MethodGet:
		h.serveManifestCSV(w, r)
	default:
		http.NotFound(w, r)
	}
}

// getSessionUser returns (userID, sessionID) from the request's session cookie and DB. Returns ("", "") if invalid.
func (h *WebHandler) getSessionUser(r *http.Request) (userID, sessionID string) {
	sessionID = GetSessionID(r, h.secret)
	if sessionID == "" {
		return "", ""
	}
	userID, err := h.store.GetSessionByID(r.Context(), sessionID)
	if err != nil || userID == "" {
		return "", ""
	}
	return userID, sessionID
}

func (h *WebHandler) serveDashboardEvents(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := h.hub.Subscribe(userID)
	defer unsub()

	fmt.Fprint(w, ": heartbeat\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, evt.Format())
			flusher.Flush()
		}
	}
}

func (h *WebHandler) serveLogin(w http.ResponseWriter, r *http.Request) {
	if userID, _ := h.getSessionUser(r); userID != "" {
		Redirect(w, r, "/dashboard")
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
		"CSRFToken":     csrfToken,
	})
}

func (h *WebHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token. Please try again.", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	userID, err := h.auth.Authenticate(r.Context(), username, password)
	if err != nil {
		log.Printf("webui login: failed username=%q", username)
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error":         "Invalid username or password",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	enabled, _ := h.store.IsTOTPEnabled(r.Context(), userID)
	if enabled {
		SetTOTPStepCookie(w, r, h.secret, userID)
		Redirect(w, r, "/login/totp")
		return
	}
	sessionID, err := h.store.CreateSession(r.Context(), userID, r.UserAgent())
	if err != nil {
		log.Printf("webui login: create session failed: %v", err)
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error":         "Login failed. Please try again.",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	log.Printf("webui login: ok username=%q", username)
	SetSession(w, r, h.secret, sessionID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveLoginTOTP(w http.ResponseWriter, r *http.Request) {
	userID := GetTOTPStepUserID(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "login_totp.html", map[string]interface{}{
		"CSRFToken": csrfToken,
	})
}

func (h *WebHandler) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID := GetTOTPStepUserID(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if code == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "login_totp.html", map[string]interface{}{
			"Error":     "Enter the code from your authenticator app.",
			"CSRFToken": csrfToken,
		})
		return
	}
	secret, err := h.store.GetTOTPSecret(r.Context(), userID)
	if err != nil || secret == "" {
		ClearTOTPStepCookie(w)
		Redirect(w, r, "/login")
		return
	}
	if !totp.Validate(code, secret) {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "login_totp.html", map[string]interface{}{
			"Error":     "Invalid code. Please try again.",
			"CSRFToken": csrfToken,
		})
		return
	}
	ClearTOTPStepCookie(w)
	sessionID, err := h.store.CreateSession(r.Context(), userID, r.UserAgent())
	if err != nil {
		log.Printf("webui login totp: create session failed: %v", err)
		http.Error(w, "Login failed.", http.StatusInternalServerError)
		return
	}
	SetSession(w, r, h.secret, sessionID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveRegister(w http.ResponseWriter, r *http.Request) {
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
		"CSRFToken":     csrfToken,
	})
}

func (h *WebHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegister {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Registration is currently disabled by the server administrator.",
			"AllowRegister": false,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token. Please try again.", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if username == "" || password == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if password != confirm {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Passwords do not match",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), username, password)
	if err != nil {
		log.Printf("webui register: failed username=%q: %v", username, err)
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "register.html", map[string]interface{}{
			"Error":         "Username already taken",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	log.Printf("webui register: ok username=%q", username)
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
	Restored  bool
	Deleted   bool
	CSRFToken string
}

// dashboardStats holds aggregate counts for the dashboard stat cards.
type dashboardStats struct {
	ClientCount int
	SaveCount   int
	GameCount   int
	TotalBytes  int64
}

func (h *WebHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	csrfToken := SetCSRFToken(w, r, h.secret)
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Username: username, Error: "Failed to load clients", CSRFToken: csrfToken})
		return
	}
	saves, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Username: username, Clients: clients, Error: "Failed to load saves", CSRFToken: csrfToken})
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
	isAdmin := h.isAdminUser(r.Context(), userID, username)
	restored := r.URL.Query().Get("restored") == "1"
	deleted := r.URL.Query().Get("deleted") == "1"
	errorMsg := r.URL.Query().Get("error")
	if errorMsg == "delete_failed" {
		errorMsg = "Failed to delete save."
	} else if errorMsg == "delete_missing_params" {
		errorMsg = "Missing game or path for delete."
	} else if errorMsg == "missing_game_or_path" {
		errorMsg = "Missing game_id or path_key for versions."
	} else if errorMsg == "restore_missing_params" || errorMsg == "restore_invalid_version" {
		errorMsg = "Invalid restore request."
	} else if errorMsg == "read_only" {
		errorMsg = "Server is in read-only mode. Push, delete, and restore are disabled."
	}
	h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{
		Username:  username,
		IsAdmin:   isAdmin,
		Stats:     stats,
		Clients:   clients,
		Saves:     saves,
		Error:     errorMsg,
		Restored:  restored,
		Deleted:   deleted,
		CSRFToken: csrfToken,
	})
}

func (h *WebHandler) serveDashboardSavesPartial(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	saves, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load saves", http.StatusInternalServerError)
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "dashboard_saves_partial.html", map[string]interface{}{
		"Saves": saves, "CSRFToken": csrfToken,
	})
}

func (h *WebHandler) serveDashboardActivityPartial(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	entries, _ := h.store.ListAuditLog(r.Context(), 20, "")
	var userEntries []store.AuditRow
	for _, e := range entries {
		if e.ActorUsername == username {
			userEntries = append(userEntries, e)
		}
	}
	h.templates.ExecuteTemplate(w, "dashboard_activity_partial.html", map[string]interface{}{
		"Entries": userEntries,
	})
}

// saveVersionsData is passed to the save versions template.
type saveVersionsData struct {
	Username   string
	IsAdmin    bool
	GameID     string
	PathKey    string
	GameTitle  string
	Versions   []store.SaveVersionInfo
	Error      string
	CSRFToken  string
}

func (h *WebHandler) serveSaveVersions(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	if gameID == "" || pathKey == "" {
		Redirect(w, r, "/dashboard?error=missing_game_or_path")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	versions, err := h.store.ListSaveVersions(r.Context(), userID, gameID, pathKey, 20)
	if err != nil {
		log.Printf("webui save versions: list failed: %v", err)
		h.templates.ExecuteTemplate(w, "save_versions.html", saveVersionsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), GameID: gameID, PathKey: pathKey, Error: "Failed to load versions", CSRFToken: SetCSRFToken(w, r, h.secret)})
		return
	}
	// Get game title from save summaries for display
	gameTitle := gameID
	if summaries, err := h.store.ListSaveSummaries(r.Context(), userID); err == nil {
		for _, s := range summaries {
			if s.GameID == gameID && s.PathKey == pathKey {
				gameTitle = s.GameTitle
				break
			}
		}
	}
	errorMsg := r.URL.Query().Get("error")
	if errorMsg == "restore_failed" {
		errorMsg = "Restore failed. Version may not exist."
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "save_versions.html", saveVersionsData{
		Username:  username,
		IsAdmin:   h.isAdminUser(r.Context(), userID, username),
		GameID:    gameID,
		PathKey:   pathKey,
		GameTitle: gameTitle,
		Versions:  versions,
		Error:     errorMsg,
		CSRFToken: csrfToken,
	})
}

func (h *WebHandler) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard?error=read_only")
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	gameID := strings.TrimSpace(r.FormValue("game_id"))
	pathKey := strings.TrimSpace(r.FormValue("path_key"))
	versionStr := strings.TrimSpace(r.FormValue("version"))
	if gameID == "" || pathKey == "" || versionStr == "" {
		Redirect(w, r, "/dashboard?error=restore_missing_params")
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		Redirect(w, r, "/dashboard?error=restore_invalid_version")
		return
	}
	if err := h.store.RestoreSaveVersion(r.Context(), userID, gameID, pathKey, version); err != nil {
		log.Printf("webui restore version: %v", err)
		Redirect(w, r, "/dashboard/save/versions?game_id="+url.QueryEscape(gameID)+"&path_key="+url.QueryEscape(pathKey)+"&error=restore_failed")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	_ = h.store.AppendAudit(r.Context(), userID, username, "restore_version", "", fmt.Sprintf("game_id=%s path_key=%s version=%d", gameID, pathKey, version))
	log.Printf("webui restore version: ok user=%s game_id=%s path_key=%s version=%d", userID, gameID, pathKey, version)
	Redirect(w, r, "/dashboard?restored=1")
}

func (h *WebHandler) serveSaveVersionDownload(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	pathKey := strings.TrimSpace(r.URL.Query().Get("path_key"))
	versionStr := strings.TrimSpace(r.URL.Query().Get("version"))
	if gameID == "" || pathKey == "" || versionStr == "" {
		http.Error(w, "game_id, path_key and version required", http.StatusBadRequest)
		return
	}
	var version int
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil || version < 1 {
		http.Error(w, "invalid version", http.StatusBadRequest)
		return
	}
	blob, err := h.store.GetSaveVersion(r.Context(), userID, gameID, pathKey, version)
	if err != nil || blob == nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}
	// Suggest a filename for download (game_id and version; path_key is often a hash)
	filename := fmt.Sprintf("save-%s-v%d.bin", strings.ReplaceAll(gameID, "/", "_"), version)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(blob.Content)
}

func (h *WebHandler) handleDeleteSave(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard?error=read_only")
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	gameID := strings.TrimSpace(r.FormValue("game_id"))
	pathKey := strings.TrimSpace(r.FormValue("path_key"))
	if gameID == "" || pathKey == "" {
		Redirect(w, r, "/dashboard?error=delete_missing_params")
		return
	}
	if err := h.store.DeleteSave(r.Context(), userID, gameID, pathKey); err != nil {
		log.Printf("webui delete save: %v", err)
		Redirect(w, r, "/dashboard?error=delete_failed")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	_ = h.store.AppendAudit(r.Context(), userID, username, "delete_save", "", fmt.Sprintf("game_id=%s path_key=%s", gameID, pathKey))
	log.Printf("webui delete save: ok user=%s game_id=%s path_key=%s", userID, gameID, pathKey)
	Redirect(w, r, "/dashboard?deleted=1")
}

// adminData is passed to the admin template.
type adminData struct {
	Username      string
	CurrentUserID string // to hide Disable/Delete for self
	Stats         adminStats
	Users         []store.UserStatRow
	Clients       []store.ClientInfoWithUser
	Manifest      []types.GameSaveLocation
	Fetches       []store.ManifestFetchRow
	AuditLog      []store.AuditRow
	StatsSnapshots []store.StatsSnapshotRow
	LatestJob       *store.JobRun
	RecentJobs      []store.JobRun
	JobRunning      bool
	JobProgressPages int
	SSEClients      int
	Error             string
	Revoked           bool
	Pushed            bool
	ManifestPushSent  int  // SSE count at time of broadcast (for "sent to N" message)
	JobStarted        bool
	UserDisabled      bool
	UserEnabled       bool
	UserDeleted       bool
	QuotaSet          bool
	AllowRegister     bool
	MaxStorageBytes   int64  // 0 = unlimited
	ReadOnly          bool
	CSRFToken         string
	// Manifest pagination (admin manifest viewer)
	ManifestPage       int   // 1-based current page
	ManifestPerPage    int   // page size (10, 20, 40, 60, 100)
	ManifestTotal      int   // total entries
	ManifestTotalPages int   // total pages
	ManifestStart      int   // 1-based start index for "X–Y of Z"
	ManifestEnd        int   // 1-based end index
	ManifestPrevPage   int   // ManifestPage - 1 for Prev link
	ManifestNextPage   int   // ManifestPage + 1 for Next link
}

// adminStats holds global counts shown on the admin page.
type adminStats struct {
	UserCount     int
	ClientCount   int
	SaveCount     int
	ManifestCount int
	TotalBytes    int64
}

// serveAdmin renders the admin page. Only users with admin role (or GSBS_ADMIN_USERNAME match) may access it.
func (h *WebHandler) serveAdmin(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		log.Printf("webui admin: forbidden user=%q", username)
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	log.Printf("webui admin: view username=%q", username)
	ctx := r.Context()
	userCount, _ := h.store.CountUsers(ctx)
	clientCount, _ := h.store.CountClients(ctx)
	saveCount, _ := h.store.CountSaves(ctx)
	manifestCount, _ := h.store.CountGameSaveLocations(ctx)
	totalBytes, _ := h.store.TotalStorageBytes(ctx)
	users, _ := h.store.ListUserStats(ctx)
	clients, _ := h.store.ListAllClients(ctx)

	// Manifest pagination: count in [10, 20, 40, 60, 100], page >= 1
	manifestPerPage := 20
	if n := r.URL.Query().Get("count"); n != "" {
		switch n {
		case "10", "20", "40", "60", "100":
			fmt.Sscanf(n, "%d", &manifestPerPage)
		}
	}
	manifestPage := 1
	if p := r.URL.Query().Get("page"); p != "" {
		var v int
		if _, err := fmt.Sscanf(p, "%d", &v); err == nil && v >= 1 {
			manifestPage = v
		}
	}
	manifestTotalPages := (manifestCount + manifestPerPage - 1) / manifestPerPage
	if manifestTotalPages < 1 {
		manifestTotalPages = 1
	}
	if manifestPage > manifestTotalPages {
		manifestPage = manifestTotalPages
	}
	manifestOffset := (manifestPage - 1) * manifestPerPage
	manifest, _ := h.store.ListGameSaveLocationsPaginated(ctx, manifestPerPage, manifestOffset)

	manifestStart := 0
	manifestEnd := 0
	if manifestCount > 0 {
		manifestStart = manifestOffset + 1
		manifestEnd = manifestOffset + len(manifest)
		if manifestEnd > manifestCount {
			manifestEnd = manifestCount
		}
	}

	fetches, _ := h.store.ListManifestFetches(ctx, 50)
	auditLog, _ := h.store.ListAuditLog(ctx, 50, "")
	statsSnapshots, _ := h.store.ListStatsSnapshots(ctx, 30)
	latestJob, _ := h.store.GetLatestJobRun(ctx, "pcgw_sync")
	recentJobs, _ := h.store.ListJobRuns(ctx, "pcgw_sync", 10)

	sseClients := 0
	if h.hub != nil {
		sseClients = h.hub.Count()
	}
	jobRunning := false
	jobProgressPages := 0
	if h.jobRunner != nil {
		jobRunning = h.jobRunner.IsRunning("pcgw_sync")
		jobProgressPages = h.jobRunner.ProgressPages("pcgw_sync")
	}

	revoked := r.URL.Query().Get("revoked") == "1"
	pushed := r.URL.Query().Get("pushed") == "1"
	manifestPushSent := 0
	if s := r.URL.Query().Get("sent"); s != "" {
		fmt.Sscanf(s, "%d", &manifestPushSent)
	}
	jobStarted := r.URL.Query().Get("job_started") == "1"
	userDisabled := r.URL.Query().Get("user_disabled") == "1"
	userEnabled := r.URL.Query().Get("user_enabled") == "1"
	userDeleted := r.URL.Query().Get("user_deleted") == "1"
	quotaSet := r.URL.Query().Get("quota_set") == "1"
	adminError := r.URL.Query().Get("error")
	if adminError == "cannot_disable_self" {
		adminError = "You cannot disable your own account."
	} else if adminError == "cannot_delete_self" {
		adminError = "You cannot delete your own account."
	} else if adminError == "disable_failed" || adminError == "enable_failed" || adminError == "delete_user_failed" || adminError == "quota_failed" {
		adminError = "Action failed. See server log."
	} else if adminError == "missing_user_id" {
		adminError = "Missing user ID."
	} else if adminError == "invalid_quota" {
		adminError = "Invalid quota value."
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.templates.ExecuteTemplate(w, "admin.html", adminData{
		Username:        username,
		CurrentUserID:   userID,
		Stats: adminStats{
			UserCount:     userCount,
			ClientCount:   clientCount,
			SaveCount:     saveCount,
			ManifestCount: manifestCount,
			TotalBytes:    totalBytes,
		},
		Users:            users,
		Clients:          clients,
		Manifest:         manifest,
		Fetches:          fetches,
		AuditLog:         auditLog,
		StatsSnapshots:   statsSnapshots,
		LatestJob:        latestJob,
		RecentJobs:       recentJobs,
		Error:            adminError,
		UserDisabled:     userDisabled,
		UserEnabled:      userEnabled,
		UserDeleted:      userDeleted,
		QuotaSet:         quotaSet,
		JobRunning:       jobRunning,
		JobProgressPages: jobProgressPages,
		SSEClients:       sseClients,
		Revoked:          revoked,
		Pushed:           pushed,
		ManifestPushSent: manifestPushSent,
		JobStarted:       jobStarted,
		ManifestPage:       manifestPage,
		ManifestPerPage:    manifestPerPage,
		ManifestTotal:      manifestCount,
		ManifestTotalPages: manifestTotalPages,
		ManifestStart:      manifestStart,
		ManifestEnd:        manifestEnd,
		ManifestPrevPage:   manifestPage - 1,
		ManifestNextPage:   manifestPage + 1,
		AllowRegister:     h.allowRegister,
		MaxStorageBytes:  h.maxStorageBytes,
		ReadOnly:         h.readOnly,
		CSRFToken:        csrfToken,
	})
}

// handleRevokeClient revokes a client's token (POST /admin/revoke). Admin-only.
func (h *WebHandler) handleRevokeClient(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	// Form already parsed by ValidateCSRF
	clientID := r.FormValue("client_id")
	if clientID == "" {
		Redirect(w, r, "/admin?error=missing_client")
		return
	}
	if err := h.store.RegenerateClientToken(r.Context(), clientID); err != nil {
		log.Printf("webui admin revoke: failed client_id=%s: %v", clientID, err)
		Redirect(w, r, "/admin?error=revoke_failed")
		return
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "revoke_client", clientID, "")
	log.Printf("webui admin revoke: ok client_id=%s by username=%q", clientID, username)
	Redirect(w, r, "/admin?revoked=1")
}

// handlePushManifest broadcasts a manifest-updated SSE event to all connected clients.
func (h *WebHandler) handlePushManifest(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if h.apiHandler != nil {
		h.apiHandler.InvalidateManifestCache()
	}
	sent := 0
	if h.hub != nil {
		sent = h.hub.Count()
		h.hub.Broadcast(sse.Event{Type: "manifest-updated", Data: "{}"})
		log.Printf("webui admin push-manifest: broadcast to %d client(s)", sent)
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "push_manifest", "", fmt.Sprintf("sent=%d", sent))
	log.Printf("webui admin push-manifest: ok by username=%q", username)
	Redirect(w, r, fmt.Sprintf("/admin?pushed=1&sent=%d", sent))
}

// handleRunJob manually triggers the PCGW sync job.
func (h *WebHandler) handleRunJob(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if h.jobRunner != nil {
		h.jobRunner.RunPCGWSync(context.Background())
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "run_job", "pcgw_sync", "")
	log.Printf("webui admin run-job: pcgw_sync triggered by username=%q", username)
	Redirect(w, r, "/admin?job_started=1")
}

func (h *WebHandler) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin?error=missing_user_id")
		return
	}
	if targetID == userID {
		Redirect(w, r, "/admin?error=cannot_disable_self")
		return
	}
	if err := h.store.DisableUser(r.Context(), targetID); err != nil {
		log.Printf("webui admin disable user: %v", err)
		Redirect(w, r, "/admin?error=disable_failed")
		return
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "disable_user", targetID, "")
	log.Printf("webui admin: user %s disabled by %q", targetID, username)
	Redirect(w, r, "/admin?user_disabled=1")
}

func (h *WebHandler) handleEnableUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin?error=missing_user_id")
		return
	}
	if err := h.store.EnableUser(r.Context(), targetID); err != nil {
		log.Printf("webui admin enable user: %v", err)
		Redirect(w, r, "/admin?error=enable_failed")
		return
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "enable_user", targetID, "")
	log.Printf("webui admin: user %s enabled by %q", targetID, username)
	Redirect(w, r, "/admin?user_enabled=1")
}

func (h *WebHandler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	if targetID == "" {
		Redirect(w, r, "/admin?error=missing_user_id")
		return
	}
	if targetID == userID {
		Redirect(w, r, "/admin?error=cannot_delete_self")
		return
	}
	if err := h.store.DeleteUser(r.Context(), targetID); err != nil {
		log.Printf("webui admin delete user: %v", err)
		Redirect(w, r, "/admin?error=delete_user_failed")
		return
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "delete_user", targetID, "")
	log.Printf("webui admin: user %s deleted by %q", targetID, username)
	Redirect(w, r, "/admin?user_deleted=1")
}

func (h *WebHandler) handleSetUserQuota(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	targetID := strings.TrimSpace(r.FormValue("user_id"))
	quotaStr := strings.TrimSpace(r.FormValue("quota_bytes"))
	if targetID == "" {
		Redirect(w, r, "/admin?error=missing_user_id")
		return
	}
	var quotaBytes int64
	if quotaStr == "" || quotaStr == "0" {
		quotaBytes = 0
	} else {
		if _, err := fmt.Sscanf(quotaStr, "%d", &quotaBytes); err != nil || quotaBytes < 0 {
			Redirect(w, r, "/admin?error=invalid_quota")
			return
		}
	}
	if err := h.store.SetUserQuota(r.Context(), targetID, quotaBytes); err != nil {
		log.Printf("webui admin set quota: %v", err)
		Redirect(w, r, "/admin?error=quota_failed")
		return
	}
	_ = h.store.AppendAudit(r.Context(), userID, username, "set_quota", targetID, fmt.Sprintf("quota=%d", quotaBytes))
	log.Printf("webui admin: quota for user %s set to %d by %q", targetID, quotaBytes, username)
	Redirect(w, r, "/admin?quota_set=1")
}

// settingsData is passed to the settings template.
type settingsData struct {
	Username         string
	IsAdmin          bool
	Error            string
	Success          string
	CSRFToken        string
	Sessions         []store.SessionRow
	CurrentSessionID string
	TOTPEnabled      bool
}

func (h *WebHandler) serveSettings(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	csrfToken := SetCSRFToken(w, r, h.secret)
	success := ""
	if r.URL.Query().Get("updated") == "1" {
		success = "Password updated successfully."
	} else if r.URL.Query().Get("revoked") == "1" {
		success = "Session revoked."
	}
	errorMsg := r.URL.Query().Get("error")
	if errorMsg == "missing_session" {
		errorMsg = "No session specified."
	} else if errorMsg == "invalid_session" {
		errorMsg = "That session does not belong to you."
	} else if errorMsg == "revoke_failed" {
		errorMsg = "Failed to revoke session."
	} else if errorMsg == "2fa_wrong_password" {
		errorMsg = "Wrong password. 2FA was not disabled."
	} else if errorMsg == "2fa_wrong_code" {
		errorMsg = "Wrong authenticator code. 2FA was not disabled."
	} else if errorMsg == "2fa_not_enabled" {
		errorMsg = "2FA is not enabled."
	} else if errorMsg == "2fa_disable_failed" {
		errorMsg = "Failed to disable 2FA."
	} else {
		errorMsg = "" // use existing .Error from handleSettings for password errors
	}
	sessions, _ := h.store.ListSessionsByUser(r.Context(), userID)
	currentSessionID := GetSessionID(r, h.secret)
	totpEnabled, _ := h.store.IsTOTPEnabled(r.Context(), userID)
	if r.URL.Query().Get("2fa_enabled") == "1" {
		success = "Two-factor authentication enabled."
	} else if r.URL.Query().Get("2fa_disabled") == "1" {
		success = "Two-factor authentication disabled."
	}
	h.templates.ExecuteTemplate(w, "settings.html", settingsData{
		Username:         username,
		IsAdmin:          h.isAdminUser(r.Context(), userID, username),
		Success:          success,
		Error:            errorMsg,
		CSRFToken:        csrfToken,
		Sessions:         sessions,
		CurrentSessionID: currentSessionID,
		TOTPEnabled:      totpEnabled,
	})
}

func (h *WebHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")
	if newPassword != confirmPassword {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "settings.html", settingsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "New passwords do not match", CSRFToken: csrfToken})
		return
	}
	if len(newPassword) < 8 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "settings.html", settingsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "New password must be at least 8 characters", CSRFToken: csrfToken})
		return
	}
	hash, err := h.store.UserPasswordHash(r.Context(), userID)
	if err != nil || hash == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "settings.html", settingsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Could not verify current password", CSRFToken: csrfToken})
		return
	}
	if err := auth.CheckPassword(currentPassword, hash); err != nil {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "settings.html", settingsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Current password is wrong", CSRFToken: csrfToken})
		return
	}
	if err := h.auth.ChangePassword(r.Context(), userID, newPassword); err != nil {
		log.Printf("webui change password: %v", err)
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.templates.ExecuteTemplate(w, "settings.html", settingsData{Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Failed to update password", CSRFToken: csrfToken})
		return
	}
	log.Printf("webui: password changed for user %q", username)
	Redirect(w, r, "/dashboard/settings?updated=1")
}

func (h *WebHandler) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	targetID := strings.TrimSpace(r.FormValue("session_id"))
	if targetID == "" {
		Redirect(w, r, "/dashboard/settings?error=missing_session")
		return
	}
	targetUserID, err := h.store.GetSessionByID(r.Context(), targetID)
	if err != nil || targetUserID != userID {
		Redirect(w, r, "/dashboard/settings?error=invalid_session")
		return
	}
	if err := h.store.DeleteSession(r.Context(), targetID); err != nil {
		Redirect(w, r, "/dashboard/settings?error=revoke_failed")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	_ = h.store.AppendAudit(r.Context(), userID, username, "revoke_session", targetID, "")
	log.Printf("webui: session %s revoked by user %q", targetID, username)
	Redirect(w, r, "/dashboard/settings?revoked=1")
}

func (h *WebHandler) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	if err := h.store.DeleteSessionsByUser(r.Context(), userID); err != nil {
		Redirect(w, r, "/dashboard/settings?error=revoke_all_failed")
		return
	}
	ClearSession(w)
	log.Printf("webui: all sessions revoked for user %s", userID)
	Redirect(w, r, "/login")
}

func (h *WebHandler) serveEnable2FA(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "GSBS",
		AccountName: username,
	})
	if err != nil {
		log.Printf("webui 2fa enable: generate failed: %v", err)
		Redirect(w, r, "/dashboard/settings?error=2fa_generate_failed")
		return
	}
	secret := key.Secret()
	SetTOTPPendingCookie(w, r, h.secret, userID, secret)
	img, err := key.Image(200, 200)
	if err != nil {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings?error=2fa_generate_failed")
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings?error=2fa_generate_failed")
		return
	}
	qrDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	csrfToken := SetCSRFToken(w, r, h.secret)
	enableError := ""
	if r.URL.Query().Get("error") == "invalid_code" {
		enableError = "Invalid code. Please try again. A new QR code is shown below."
	}
	h.templates.ExecuteTemplate(w, "enable_2fa.html", map[string]interface{}{
		"Username":  username,
		"IsAdmin":   h.isAdminUser(r.Context(), userID, username),
		"CSRFToken": csrfToken,
		"QRDataURL": qrDataURL,
		"Secret":    secret,
		"Error":     enableError,
	})
}

func (h *WebHandler) handleConfirm2FA(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, totpSecret := GetTOTPPendingCookie(r, h.secret)
	if userID == "" || totpSecret == "" {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings?error=2fa_session_expired")
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if code == "" || !totp.Validate(code, totpSecret) {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings/2fa/enable?error=invalid_code")
		return
	}
	if err := h.store.SetTOTPSecret(r.Context(), userID, totpSecret); err != nil {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings?error=2fa_save_failed")
		return
	}
	if err := h.store.SetTOTPEnabled(r.Context(), userID, true); err != nil {
		ClearTOTPPendingCookie(w)
		Redirect(w, r, "/dashboard/settings?error=2fa_save_failed")
		return
	}
	ClearTOTPPendingCookie(w)
	username, _ := h.store.UsernameByID(r.Context(), userID)
	_ = h.store.AppendAudit(r.Context(), userID, username, "enable_2fa", "", "")
	log.Printf("webui: 2FA enabled for user %q", username)
	Redirect(w, r, "/dashboard/settings?2fa_enabled=1")
}

func (h *WebHandler) handleDisable2FA(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	password := r.FormValue("password")
	code := strings.TrimSpace(r.FormValue("code"))
	hash, err := h.store.UserPasswordHash(r.Context(), userID)
	if err != nil || hash == "" {
		Redirect(w, r, "/dashboard/settings?error=2fa_disable_failed")
		return
	}
	if err := auth.CheckPassword(password, hash); err != nil {
		Redirect(w, r, "/dashboard/settings?error=2fa_wrong_password")
		return
	}
	secret, err := h.store.GetTOTPSecret(r.Context(), userID)
	if err != nil || secret == "" {
		Redirect(w, r, "/dashboard/settings?error=2fa_not_enabled")
		return
	}
	if !totp.Validate(code, secret) {
		Redirect(w, r, "/dashboard/settings?error=2fa_wrong_code")
		return
	}
	if err := h.store.SetTOTPSecret(r.Context(), userID, ""); err != nil {
		Redirect(w, r, "/dashboard/settings?error=2fa_disable_failed")
		return
	}
	if err := h.store.SetTOTPEnabled(r.Context(), userID, false); err != nil {
		Redirect(w, r, "/dashboard/settings?error=2fa_disable_failed")
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	_ = h.store.AppendAudit(r.Context(), userID, username, "disable_2fa", "", "")
	log.Printf("webui: 2FA disabled for user %q", username)
	Redirect(w, r, "/dashboard/settings?2fa_disabled=1")
}

func (h *WebHandler) serveManifestCSV(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getSessionUser(r)
	if userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	entries, err := h.store.ListGameSaveLocations(r.Context())
	if err != nil {
		http.Error(w, "Failed to load manifest", http.StatusInternalServerError)
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if platform != "" || source != "" {
		var filtered []types.GameSaveLocation
		for _, e := range entries {
			if platform != "" && e.Platform != platform {
				continue
			}
			if source != "" && e.Source != source {
				continue
			}
			filtered = append(filtered, e)
		}
		entries = filtered
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"manifest.csv\"")
	wr := csv.NewWriter(w)
	_ = wr.Write([]string{"game_id", "game_title", "platform", "path_template", "is_config", "source", "updated_at"})
	for _, e := range entries {
		configVal := "false"
		if e.IsConfig {
			configVal = "true"
		}
		_ = wr.Write([]string{e.GameID, e.GameTitle, e.Platform, e.PathTemplate, configVal, e.Source, e.UpdatedAt})
	}
	wr.Flush()
}
