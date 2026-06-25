package webui

import (
	"context"
	"html/template"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/api"
	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/netutil"
	"github.com/gsbs/gsbs/server/ratelimit"
	"github.com/gsbs/gsbs/server/schedule"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

// WebHandler serves the WebUI (login, register, dashboard, admin).
type WebHandler struct {
	store           store.Store
	auth            *auth.Service
	secret          string
	adminUsername   string
	allowRegister   bool
	templates       *template.Template
	hub             *sse.Hub
	apiHandler      *api.Handler
	jobRunner       *job.Runner
	pcgwCron        *schedule.PCGWCron
	gsbsVersion     string
	maxStorageBytes int64
	readOnly        bool
	loginLimiter    *ratelimit.Limiter
}

// NewWebHandler creates a WebHandler. loginLimiter may be nil (no rate limit on WebUI login).
func NewWebHandler(st store.Store, authSvc *auth.Service, secret, adminUsername string, allowRegister bool, hub *sse.Hub, apiHandler *api.Handler, jobRunner *job.Runner, pcgwCron *schedule.PCGWCron, gsbsVersion string, maxStorageBytes int64, readOnly bool, loginLimiter *ratelimit.Limiter) *WebHandler {
	return &WebHandler{
		store: st, auth: authSvc, secret: secret, adminUsername: adminUsername,
		allowRegister: allowRegister, templates: parseTemplates(), hub: hub,
		apiHandler: apiHandler, jobRunner: jobRunner, pcgwCron: pcgwCron, gsbsVersion: gsbsVersion,
		maxStorageBytes: maxStorageBytes,
		readOnly:        readOnly, loginLimiter: loginLimiter,
	}
}

func (h *WebHandler) isAdminUser(ctx context.Context, userID, username string) bool {
	role, err := h.store.UserRole(ctx, userID)
	if err == nil && role == "admin" {
		return true
	}
	return h.adminUsername != "" && username == h.adminUsername
}

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

func (h *WebHandler) requireSession(w http.ResponseWriter, r *http.Request) (userID, username string, ok bool) {
	var sessionID string
	userID, sessionID = h.getSessionUser(r)
	if userID == "" {
		Redirect(w, r, "/login")
		return "", "", false
	}
	// Cut off disabled users: revoke their session and redirect to login.
	if disabled, err := h.store.IsUserDisabled(r.Context(), userID); err == nil && disabled {
		_ = h.store.DeleteSession(r.Context(), sessionID)
		ClearSession(w)
		Redirect(w, r, "/login")
		return "", "", false
	}
	username, _ = h.store.UsernameByID(r.Context(), userID)
	return userID, username, true
}

func (h *WebHandler) requireAdmin(w http.ResponseWriter, r *http.Request) (userID, username string, ok bool) {
	userID, username, ok = h.requireSession(w, r)
	if !ok {
		return "", "", false
	}
	if !h.isAdminUser(r.Context(), userID, username) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", "", false
	}
	return userID, username, true
}

func clientIP(r *http.Request) string {
	return netutil.ClientIP(r)
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
	case path == "/dashboard" && r.Method == http.MethodGet:
		h.serveDashboard(w, r)
	case path == "/dashboard/events" && r.Method == http.MethodGet:
		h.serveDashboardEvents(w, r)
	case path == "/dashboard/partial/stats" && r.Method == http.MethodGet:
		h.serveDashboardStatsPartial(w, r)
	case path == "/dashboard/partial/clients" && r.Method == http.MethodGet:
		h.serveDashboardClientsPartial(w, r)
	case path == "/dashboard/partial/saves" && r.Method == http.MethodGet:
		h.serveDashboardSavesPartial(w, r)
	case path == "/dashboard/partial/activity" && r.Method == http.MethodGet:
		h.serveDashboardActivityPartial(w, r)
	case path == "/dashboard/games" && r.Method == http.MethodGet:
		h.serveDashboardGames(w, r)
	case path == "/dashboard/partial/games" && r.Method == http.MethodGet:
		h.serveDashboardGamesPartial(w, r)
	case path == "/dashboard/analytics" && r.Method == http.MethodGet:
		h.serveDashboardAnalytics(w, r)
	case path == "/dashboard/partial/search" && r.Method == http.MethodGet:
		h.serveGlobalSearch(w, r)
	case path == "/dashboard/export/saves.csv" && r.Method == http.MethodGet:
		h.handleExportSaves(w, r, "csv")
	case path == "/dashboard/export/saves.json" && r.Method == http.MethodGet:
		h.handleExportSaves(w, r, "json")
	case path == "/dashboard/games/bulk-delete" && r.Method == http.MethodPost:
		h.handleBulkDeleteGames(w, r)
	case path == "/dashboard/clients" && r.Method == http.MethodGet:
		h.serveDashboardClientsPage(w, r)
	case path == "/dashboard/partial/clients-list" && r.Method == http.MethodGet:
		h.serveDashboardClientsListPartial(w, r)
	case path == "/dashboard/clients/rename" && r.Method == http.MethodPost:
		h.handleDashboardRenameClient(w, r)
	case path == "/dashboard/clients/revoke" && r.Method == http.MethodPost:
		h.handleDashboardRevokeClient(w, r)
	case path == "/logout":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !ValidateCSRF(r, h.secret) {
			http.Error(w, "Invalid security token.", http.StatusBadRequest)
			return
		}
		if sessionID := GetSessionID(r, h.secret); sessionID != "" {
			_ = h.store.DeleteSession(r.Context(), sessionID)
		}
		ClearSession(w)
		Redirect(w, r, "/login")
	case path == "/admin" && r.Method == http.MethodGet:
		h.serveAdminOverview(w, r)
	case path == "/admin/users" && r.Method == http.MethodGet:
		h.serveAdminUsers(w, r)
	case path == "/admin/users/view" && r.Method == http.MethodGet:
		h.serveAdminUserDetail(w, r)
	case path == "/admin/manifest" && r.Method == http.MethodGet:
		h.serveAdminManifest(w, r)
	case path == "/admin/activity" && r.Method == http.MethodGet:
		h.serveAdminActivity(w, r)
	case path == "/admin/logs" && r.Method == http.MethodGet:
		h.serveAdminLogs(w, r)
	case path == "/admin/settings" && r.Method == http.MethodGet:
		h.serveAdminSettings(w, r)
	case path == "/admin/settings/save" && r.Method == http.MethodPost:
		h.handleAdminSettingsSave(w, r)
	case path == "/admin/pcgw/source" && r.Method == http.MethodPost:
		h.handleAdminChooseSource(w, r)
	case path == "/admin/analytics" && r.Method == http.MethodGet:
		h.serveAdminAnalytics(w, r)
	case path == "/admin/partial/analytics-pcgw" && r.Method == http.MethodGet:
		h.serveAdminAnalyticsPCGWPartial(w, r)
	case path == "/admin/partial/manifest" && r.Method == http.MethodGet:
		h.serveAdminManifestPartial(w, r)
	case path == "/admin/partial/jobs" && r.Method == http.MethodGet:
		h.serveAdminJobsPartial(w, r)
	case path == "/admin/partial/logs" && r.Method == http.MethodGet:
		h.serveAdminLogsPartial(w, r)
	case path == "/admin/logs/export.csv" && r.Method == http.MethodGet:
		h.serveAdminLogsCSV(w, r)
	case path == "/admin/revoke" && r.Method == http.MethodPost:
		h.handleRevokeClient(w, r)
	case path == "/admin/push-manifest" && r.Method == http.MethodPost:
		h.handlePushManifest(w, r)
	case path == "/admin/run-job" && r.Method == http.MethodPost:
		h.handleRunJob(w, r)
	case path == "/admin/jobs/pcgw/cancel" && r.Method == http.MethodPost:
		h.handleCancelPCGWJob(w, r)
	case path == "/admin/user/create" && r.Method == http.MethodPost:
		h.handleCreateUser(w, r)
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
	case path == "/dashboard/save/versions/preview" && r.Method == http.MethodGet:
		h.serveSaveVersionPreview(w, r)
	case path == "/dashboard/save/delete" && r.Method == http.MethodPost:
		h.handleDeleteSave(w, r)
	case path == "/dashboard/game/delete" && r.Method == http.MethodPost:
		h.handleDeleteGameSaves(w, r)
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
	case path == "/dashboard/settings/encryption" && r.Method == http.MethodPost:
		h.handleEncryptionSettings(w, r)
	case path == "/admin/manifest.csv" && r.Method == http.MethodGet:
		h.serveManifestCSV(w, r)
	case strings.HasPrefix(r.URL.EscapedPath(), "/dashboard/games/") && r.Method == http.MethodGet:
		h.serveGameDetail(w, r)
	default:
		if h.routeAdminPCGW(w, r) {
			return
		}
		http.NotFound(w, r)
	}
}
