package webui

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/pquerna/otp/totp"
)

func (h *WebHandler) serveSettings(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	csrfToken := SetCSRFToken(w, r, h.secret)
	success := adminQuerySuccess(r)
	if r.URL.Query().Get("updated") == "1" {
		success = "Password updated successfully."
	} else if r.URL.Query().Get("revoked") == "1" {
		success = "Session revoked."
	} else if r.URL.Query().Get("2fa_enabled") == "1" {
		success = "Two-factor authentication enabled."
	} else if r.URL.Query().Get("2fa_disabled") == "1" {
		success = "Two-factor authentication disabled."
	} else if r.URL.Query().Get("encryption_updated") == "1" {
		success = "Encryption setting updated."
	}
	errorMsg := r.URL.Query().Get("error")
	switch errorMsg {
	case "missing_session":
		errorMsg = "No session specified."
	case "invalid_session":
		errorMsg = "That session does not belong to you."
	case "revoke_failed":
		errorMsg = "Failed to revoke session."
	case "2fa_wrong_password":
		errorMsg = "Wrong password. 2FA was not disabled."
	case "2fa_wrong_code":
		errorMsg = "Wrong authenticator code. 2FA was not disabled."
	case "2fa_not_enabled":
		errorMsg = "2FA is not enabled."
	case "2fa_disable_failed":
		errorMsg = "Failed to disable 2FA."
	default:
		if errorMsg != "" && !strings.HasPrefix(errorMsg, "2fa_") {
			// keep
		} else if errorMsg == "" {
			errorMsg = ""
		}
	}
	sessions, _ := h.store.ListSessionsByUser(r.Context(), userID)
	totpEnabled, _ := h.store.IsTOTPEnabled(r.Context(), userID)
	encryptionEnabled, _ := h.store.IsEncryptionEnabled(r.Context(), userID)
	h.render(w, "settings.html", settingsData{
		PageData: PageData{
			PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "settings", Success: success, Error: errorMsg,
		},
		Sessions: sessions, CurrentSessionID: GetSessionID(r, h.secret), TOTPEnabled: totpEnabled,
		EncryptionEnabled: encryptionEnabled,
	})
}

func (h *WebHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")
	if newPassword != confirmPassword {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "settings.html", settingsData{
			PageData: PageData{PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "New passwords do not match", CSRFToken: csrfToken, NavActive: "settings"},
		})
		return
	}
	if len(newPassword) < 8 {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "settings.html", settingsData{
			PageData: PageData{PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "New password must be at least 8 characters", CSRFToken: csrfToken, NavActive: "settings"},
		})
		return
	}
	hash, err := h.store.UserPasswordHash(r.Context(), userID)
	if err != nil || hash == "" {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "settings.html", settingsData{
			PageData: PageData{PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Could not verify current password", CSRFToken: csrfToken, NavActive: "settings"},
		})
		return
	}
	if err := auth.CheckPassword(r.FormValue("current_password"), hash); err != nil {
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "settings.html", settingsData{
			PageData: PageData{PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Current password is wrong", CSRFToken: csrfToken, NavActive: "settings"},
		})
		return
	}
	if err := h.auth.ChangePassword(r.Context(), userID, newPassword); err != nil {
		logx.Logger().Error().Err(err).Msg("webui change password failed")
		csrfToken := SetCSRFToken(w, r, h.secret)
		h.render(w, "settings.html", settingsData{
			PageData: PageData{PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username), Error: "Failed to update password", CSRFToken: csrfToken, NavActive: "settings"},
		})
		return
	}
	logx.Logger().Info().Str("username", username).Msg("webui password changed")
	Redirect(w, r, "/dashboard/settings?updated=1")
}

func (h *WebHandler) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
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
	h.appendAuditBroadcast(r.Context(), userID, username, "revoke_session", targetID, "")
	logx.Logger().Info().Str("session_id", targetID).Str("username", username).Msg("webui session revoked")
	Redirect(w, r, "/dashboard/settings?revoked=1")
}

func (h *WebHandler) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteSessionsByUser(r.Context(), userID); err != nil {
		Redirect(w, r, "/dashboard/settings?error=revoke_all_failed")
		return
	}
	ClearSession(w)
	logx.Logger().Info().Str("user_id", userID).Msg("webui all sessions revoked")
	Redirect(w, r, "/login")
}

func (h *WebHandler) serveEnable2FA(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "GSBS", AccountName: username})
	if err != nil {
		logx.Logger().Error().Err(err).Msg("webui 2fa enable generate failed")
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
	h.render(w, "enable_2fa.html", map[string]interface{}{
		"PageName": "enable_2fa", "Username": username, "IsAdmin": h.isAdminUser(r.Context(), userID, username),
		"CSRFToken": csrfToken, "QRDataURL": qrDataURL, "Secret": secret, "Error": enableError,
		"NavActive": "settings",
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
	h.appendAuditBroadcast(r.Context(), userID, username, "enable_2fa", "", "")
	logx.Logger().Info().Str("username", username).Msg("webui 2FA enabled")
	Redirect(w, r, "/dashboard/settings?2fa_enabled=1")
}

func (h *WebHandler) handleDisable2FA(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
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
	h.appendAuditBroadcast(r.Context(), userID, username, "disable_2fa", "", "")
	logx.Logger().Info().Str("username", username).Msg("webui 2FA disabled")
	Redirect(w, r, "/dashboard/settings?2fa_disabled=1")
}

func (h *WebHandler) handleEncryptionSettings(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	enabled := r.FormValue("encryption_enabled") == "1"
	if err := h.store.SetEncryptionEnabled(r.Context(), userID, enabled); err != nil {
		logx.Logger().Error().Err(err).Msg("webui encryption setting failed")
		Redirect(w, r, "/dashboard/settings?error=encryption_update_failed")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "encryption_setting", "", map[bool]string{true: "enabled", false: "disabled"}[enabled])
	logx.Logger().Info().Str("username", username).Bool("enabled", enabled).Msg("webui encryption setting updated")
	Redirect(w, r, "/dashboard/settings?encryption_updated=1")
}
