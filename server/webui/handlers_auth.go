package webui

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/pquerna/otp/totp"
)

func (h *WebHandler) serveLogin(w http.ResponseWriter, r *http.Request) {
	if userID, _ := h.getSessionUser(r); userID != "" {
		Redirect(w, r, "/dashboard")
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.render(w, "login.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
		"CSRFToken":     csrfToken,
	})
}

func (h *WebHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token. Please try again.", http.StatusBadRequest)
		return
	}
	if h.loginLimiter != nil && !h.loginLimiter.Allow(clientIP(r)) {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
			"Error":         "Too many login attempts. Please wait and try again.",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if len(username) > 128 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
			"Error":         "Username too long",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if len(password) < 8 || len(password) > 72 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
			"Error":         "Invalid password length",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	userID, err := h.auth.Authenticate(r.Context(), username, password)
	if err != nil {
		logx.Logger().Warn().Str("username", username).Msg("webui login failed")
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
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
		logx.Logger().Error().Err(err).Msg("webui login create session failed")
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "login.html", map[string]interface{}{
			"Error":         "Login failed. Please try again.",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	logx.Logger().Info().Str("username", username).Msg("webui login ok")
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
	h.render(w, "login_totp.html", map[string]interface{}{
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
		h.render(w, "login_totp.html", map[string]interface{}{
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
		h.render(w, "login_totp.html", map[string]interface{}{
			"Error":     "Invalid code. Please try again.",
			"CSRFToken": csrfToken,
		})
		return
	}
	ClearTOTPStepCookie(w)
	sessionID, err := h.store.CreateSession(r.Context(), userID, r.UserAgent())
	if err != nil {
		logx.Logger().Error().Err(err).Msg("webui login totp create session failed")
		http.Error(w, "Login failed.", http.StatusInternalServerError)
		return
	}
	SetSession(w, r, h.secret, sessionID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveRegister(w http.ResponseWriter, r *http.Request) {
	csrfToken := SetCSRFToken(w, r, h.secret)
	h.render(w, "register.html", map[string]interface{}{
		"AllowRegister": h.allowRegister,
		"CSRFToken":     csrfToken,
	})
}

func (h *WebHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegister {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
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
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if username == "" || password == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
			"Error":         "Username and password required",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if len(username) > 128 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
			"Error":         "Username too long",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if len(password) < 8 || len(password) > 72 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
			"Error":         "Password must be 8–72 characters",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	if password != confirm {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
			"Error":         "Passwords do not match",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), username, password)
	if err != nil {
		logx.Logger().Warn().Str("username", username).Err(err).Msg("webui register failed")
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "register.html", map[string]interface{}{
			"Error":         "Username already taken",
			"AllowRegister": h.allowRegister,
			"CSRFToken":     csrfToken,
		})
		return
	}
	logx.Logger().Info().Str("username", username).Msg("webui register ok")
	Redirect(w, r, "/login")
}

func dashboardErrorMsg(code string) string {
	switch code {
	case "delete_failed":
		return "Failed to delete save."
	case "delete_missing_params":
		return "Missing game or path for delete."
	case "missing_game_or_path":
		return "Missing game_id or path_key for versions."
	case "restore_missing_params", "restore_invalid_version":
		return "Invalid restore request."
	case "read_only":
		return "Server is in read-only mode. Push, delete, and restore are disabled."
	case "revoke_failed":
		return "Failed to revoke client."
	case "missing_client":
		return "Missing client ID."
	default:
		return code
	}
}

func adminQuerySuccess(r *http.Request) string {
	q := r.URL.Query()
	if q.Get("revoked") == "1" {
		return "Client token revoked. The client must run gsbs-client login to reconnect."
	}
	if q.Get("pushed") == "1" {
		if sent := q.Get("sent"); sent != "" {
			return fmt.Sprintf("Manifest push event sent to %s connected client(s).", sent)
		}
		return "Manifest push event sent."
	}
	if q.Get("job_started") == "1" {
		return "PCGW sync job started in background."
	}
	if q.Get("user_disabled") == "1" {
		return "User disabled. They cannot log in until re-enabled."
	}
	if q.Get("user_enabled") == "1" {
		return "User re-enabled."
	}
	if q.Get("user_deleted") == "1" {
		return "User deleted."
	}
	if q.Get("quota_set") == "1" {
		return "Storage quota updated."
	}
	return ""
}

func adminQueryError(r *http.Request) string {
	switch r.URL.Query().Get("error") {
	case "cannot_disable_self":
		return "You cannot disable your own account."
	case "cannot_delete_self":
		return "You cannot delete your own account."
	case "disable_failed", "enable_failed", "delete_user_failed", "quota_failed":
		return "Action failed. See server log."
	case "missing_user_id":
		return "Missing user ID."
	case "invalid_quota":
		return "Invalid quota value."
	case "missing_client":
		return "Missing client ID."
	case "revoke_failed":
		return "Failed to revoke client."
	default:
		return r.URL.Query().Get("error")
	}
}

func (h *WebHandler) appendAuditBroadcast(ctx context.Context, actorUserID, actorUsername, action, targetID, details string) {
	_ = h.store.AppendAudit(ctx, actorUserID, actorUsername, action, targetID, details)
	if h.hub != nil {
		h.hub.Broadcast(sse.Event{Type: "audit-updated", Data: "{}"})
	}
}
