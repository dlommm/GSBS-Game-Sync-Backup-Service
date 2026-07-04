package webui

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// setupState caches whether the first-run wizard is still needed so the check
// isn't a DB hit on every request. Once a user exists it latches to false.
var (
	setupMu       sync.Mutex
	setupDone     bool      // latched true once any user has been observed
	setupChecked  time.Time // last DB check (re-checked at most every few seconds while empty)
	setupStartedT time.Time // first time the wizard was observed empty (claim-safety window)
)

// setupClaimWindow is how long after first boot the wizard accepts a
// submission. Past this, it locks until restart so a late visitor to an
// exposed-but-unconfigured instance can't claim the admin account (Portainer
// pattern). Restarting the server resets the window.
const setupClaimWindow = 60 * time.Minute

// setupNeeded reports whether the setup wizard should be active (no users yet).
func (h *WebHandler) setupNeeded(r *http.Request) bool {
	setupMu.Lock()
	defer setupMu.Unlock()
	if setupDone {
		return false
	}
	if time.Since(setupChecked) < 3*time.Second {
		return true
	}
	setupChecked = time.Now()
	n, err := h.store.CountUsers(r.Context())
	if err != nil {
		return true // fail toward showing setup rather than a broken redirect loop
	}
	if n > 0 {
		setupDone = true
		return false
	}
	if setupStartedT.IsZero() {
		setupStartedT = time.Now()
	}
	return true
}

func (h *WebHandler) serveSetup(w http.ResponseWriter, r *http.Request) {
	if !h.setupNeeded(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.handleSetupSubmit(w, r)
		return
	}
	csrf := SetCSRFToken(w, r, h.secret)
	h.render(w, "setup.html", map[string]interface{}{
		"CSRFToken": csrf,
		"Error":     setupErrorMessage(r.URL.Query().Get("error")),
		"Locked":    h.setupLocked(),
	})
}

func (h *WebHandler) setupLocked() bool {
	setupMu.Lock()
	defer setupMu.Unlock()
	return !setupStartedT.IsZero() && time.Since(setupStartedT) > setupClaimWindow
}

func setupErrorMessage(code string) string {
	switch code {
	case "username":
		return "Choose a username (3+ characters)."
	case "password":
		return "Password must be at least 8 characters and match the confirmation."
	case "locked":
		return "Setup timed out for security. Restart the server, then complete setup promptly."
	case "failed":
		return "Setup failed. Please try again."
	case "":
		return ""
	default:
		return "Setup could not be completed."
	}
}

func (h *WebHandler) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	if h.setupLocked() {
		Redirect(w, r, "/setup?error=locked")
		return
	}
	ctx := r.Context()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if len(username) < 3 {
		Redirect(w, r, "/setup?error=username")
		return
	}
	if len(password) < 8 || password != confirm {
		Redirect(w, r, "/setup?error=password")
		return
	}

	// Re-guard against a race: only the request that finds zero users wins.
	if n, err := h.store.CountUsers(ctx); err != nil || n > 0 {
		Redirect(w, r, "/login")
		return
	}
	userID, err := h.auth.RegisterUser(ctx, username, password)
	if err != nil {
		logx.Logger().Error().Err(err).Msg("setup: create admin user")
		Redirect(w, r, "/setup?error=failed")
		return
	}
	// First user is the admin.
	if err := h.store.SetUserRole(ctx, userID, "admin"); err != nil {
		logx.Logger().Error().Err(err).Msg("setup: promote admin")
	}

	// Persist the chosen operational settings (DB-backed; env still overrides).
	allowRegister := r.FormValue("allow_register") == "1"
	settings := map[string]string{
		store.AdminSettingAllowRegister:    boolStr(allowRegister),
		store.AdminSettingSetupCompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if q := strings.TrimSpace(r.FormValue("storage_quota_gb")); q != "" {
		if gb, convErr := strconv.ParseFloat(q, 64); convErr == nil && gb > 0 {
			settings[store.AdminSettingMaxStorageBytes] = strconv.FormatInt(int64(gb*1024*1024*1024), 10)
		}
	}
	if r.FormValue("enable_backups") == "1" {
		settings[job.SettingBackupEnabled] = "true"
	}
	if url := strings.TrimSpace(r.FormValue("notify_webhook_url")); url != "" {
		settings[store.AdminSettingNotifyWebhookURL] = url
	}
	for k, v := range settings {
		if err := h.store.SetAdminSetting(ctx, k, v); err != nil {
			logx.Logger().Warn().Str("key", k).Err(err).Msg("setup: save setting")
		}
	}

	setupMu.Lock()
	setupDone = true
	setupMu.Unlock()

	// Log the new admin straight in.
	sessionID, err := h.store.CreateSession(ctx, userID, r.UserAgent())
	if err == nil {
		SetSession(w, r, h.secret, sessionID)
		Redirect(w, r, "/dashboard")
		return
	}
	Redirect(w, r, "/login")
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
