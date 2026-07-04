package webui

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image/png"
	"net/http"
	"strings"

	"github.com/gsbs/gsbs/pkg/i18n"
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
	case "recovery_wrong_password":
		errorMsg = "Wrong password. Recovery codes were not regenerated."
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
	recoveryCount := 0
	if totpEnabled {
		recoveryCount, _ = h.store.CountRecoveryCodes(r.Context(), userID)
	}
	encryptionEnabled, _ := h.store.IsEncryptionEnabled(r.Context(), userID)
	notifySettings, _ := h.store.GetUserNotifySettings(r.Context(), userID)
	userLocale, _ := h.store.GetUserLocale(r.Context(), userID)
	h.render(w, "settings.html", settingsData{
		PageData: PageData{
			PageName: "settings", Username: username, IsAdmin: h.isAdminUser(r.Context(), userID, username),
			CSRFToken: csrfToken, NavActive: "settings", Success: success, Error: errorMsg,
		},
		Sessions: sessions, CurrentSessionID: GetSessionID(r, h.secret), TOTPEnabled: totpEnabled,
		RecoveryCount:     recoveryCount,
		EncryptionEnabled: encryptionEnabled,
		Notify:            notifySettings,
		Locale:            userLocale,
		Locales:           i18n.AvailableLocales(),
	})
}

// handleSetLocale stores the user's preferred UI language.
func (h *WebHandler) handleSetLocale(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, _, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	locale := strings.TrimSpace(r.FormValue("locale"))
	if locale != "" && !i18n.HasLocale(locale) {
		locale = "" // ignore unknown; falls back to negotiation
	}
	if err := h.store.SetUserLocale(r.Context(), userID, locale); err != nil {
		logx.Logger().Warn().Err(err).Msg("set user locale")
	}
	Redirect(w, r, "/dashboard/settings?updated=1")
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
	if err := h.store.RevokeAllClientTokens(r.Context(), userID); err != nil {
		logx.Logger().Warn().Err(err).Str("username", username).Msg("webui password change: revoke client tokens failed")
	}
	// Log out every other browser; the session that changed the password stays.
	if err := h.store.DeleteSessionsByUserExcept(r.Context(), userID, GetSessionID(r, h.secret)); err != nil {
		logx.Logger().Warn().Err(err).Str("username", username).Msg("webui password change: revoke other sessions failed")
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
	if code == "" || !auth.ValidateTOTPOnce(userID, code, totpSecret) {
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
	// Enabling 2FA revokes existing client tokens: devices that logged in
	// before 2FA must prove the second factor to come back.
	if err := h.store.RevokeAllClientTokens(r.Context(), userID); err != nil {
		logx.Logger().Warn().Err(err).Str("user_id", userID).Msg("webui 2fa enable: revoke client tokens failed")
	}
	username, _ := h.store.UsernameByID(r.Context(), userID)
	h.appendAuditBroadcast(r.Context(), userID, username, "enable_2fa", "", "")
	logx.Logger().Info().Str("username", username).Msg("webui 2FA enabled")
	// Show the one-time recovery codes immediately after enabling.
	h.issueRecoveryCodes(w, r, userID, username)
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
	if !auth.ValidateTOTPOnce(userID, code, secret) {
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
	if err := h.store.RevokeAllClientTokens(r.Context(), userID); err != nil {
		logx.Logger().Warn().Err(err).Str("username", username).Msg("webui 2fa disable: revoke client tokens failed")
	}
	if err := h.store.DeleteRecoveryCodes(r.Context(), userID); err != nil {
		logx.Logger().Warn().Err(err).Str("username", username).Msg("webui 2fa disable: delete recovery codes failed")
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

// recoveryCodeCount is how many one-time 2FA recovery codes each user gets.
const recoveryCodeCount = 10

// generateRecoveryCodes returns plaintext codes (shown to the user once) and
// their SHA-256 hex hashes (the only thing stored). The alphabet omits
// look-alike characters (I/L/O/0/1).
func generateRecoveryCodes() (codes, hashes []string, err error) {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		buf := make([]byte, 10)
		for j, b := range raw {
			buf[j] = alphabet[int(b)%len(alphabet)]
		}
		code := fmt.Sprintf("%s-%s", buf[:5], buf[5:])
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// hashRecoveryCode normalises (uppercase, no separators) then hashes, so users
// can type codes with or without the dash.
func hashRecoveryCode(code string) string {
	norm := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// issueRecoveryCodes generates + stores a fresh recovery-code set and renders
// the show-once page. Used on 2FA enable and on explicit regeneration.
func (h *WebHandler) issueRecoveryCodes(w http.ResponseWriter, r *http.Request, userID, username string) {
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		logx.Logger().Error().Err(err).Msg("webui recovery code generation failed")
		Redirect(w, r, "/dashboard/settings?error=2fa_save_failed")
		return
	}
	if err := h.store.SetRecoveryCodes(r.Context(), userID, hashes); err != nil {
		logx.Logger().Error().Err(err).Msg("webui recovery code store failed")
		Redirect(w, r, "/dashboard/settings?error=2fa_save_failed")
		return
	}
	h.render(w, "recovery_codes.html", map[string]interface{}{
		"PageName": "recovery_codes", "Username": username,
		"IsAdmin":   h.isAdminUser(r.Context(), userID, username),
		"CSRFToken": SetCSRFToken(w, r, h.secret), "NavActive": "settings",
		"Codes":        codes,
		"CodesJoined":  strings.Join(codes, "\n"),
		"DownloadData": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("GSBS two-factor recovery codes\nEach code works exactly once.\n\n"+strings.Join(codes, "\n")+"\n")),
	})
}

// handleRegenerateRecoveryCodes replaces the user's recovery codes (password
// required — this invalidates every previously saved code).
func (h *WebHandler) handleRegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	enabled, err := h.store.IsTOTPEnabled(r.Context(), userID)
	if err != nil || !enabled {
		Redirect(w, r, "/dashboard/settings?error=2fa_not_enabled")
		return
	}
	hash, err := h.store.UserPasswordHash(r.Context(), userID)
	if err != nil || hash == "" {
		Redirect(w, r, "/dashboard/settings?error=2fa_disable_failed")
		return
	}
	if err := auth.CheckPassword(r.FormValue("password"), hash); err != nil {
		Redirect(w, r, "/dashboard/settings?error=recovery_wrong_password")
		return
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "regenerate_recovery_codes", "", "")
	h.issueRecoveryCodes(w, r, userID, username)
}
