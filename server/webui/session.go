package webui

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "gsbs_session"
const sessionDuration = 7 * 24 * time.Hour

const csrfCookieName = "gsbs_csrf"
const csrfDuration = 1 * time.Hour

const totpStepCookieName = "gsbs_totp_step"
const totpStepDuration = 5 * time.Minute

const totpPendingCookieName = "gsbs_totp_pending"
const totpPendingDuration = 10 * time.Minute

// SetSession sets a signed session cookie with the given sessionID (from store.CreateSession).
// The Secure flag is set when the request arrives over TLS or via a reverse proxy
// that sets X-Forwarded-Proto: https, so cookies are only sent over HTTPS in production.
func SetSession(w http.ResponseWriter, r *http.Request, secret, sessionID string) {
	expiry := time.Now().Add(sessionDuration).Unix()
	payload := sessionID + "|" + strconv.FormatInt(expiry, 10)
	sig := signSession(secret, payload)
	value := base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// ClearSession removes the session cookie.
func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GetSessionID returns the session ID from the cookie if the signature and expiry are valid, else "".
// The caller must validate the session ID against the store (e.g. store.GetSessionByID) to get userID.
func GetSessionID(r *http.Request, secret string) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(signSession(secret, payload)), []byte(parts[1])) {
		return ""
	}
	idx := strings.LastIndex(payload, "|")
	if idx < 0 {
		return ""
	}
	expiry, err := strconv.ParseInt(payload[idx+1:], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return ""
	}
	return payload[:idx]
}

func signSession(secret, payload string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// SetTOTPStepCookie sets a short-lived signed cookie with userID for the TOTP verification step after password login.
func SetTOTPStepCookie(w http.ResponseWriter, r *http.Request, secret, userID string) {
	expiry := time.Now().Add(totpStepDuration).Unix()
	payload := userID + "|" + strconv.FormatInt(expiry, 10)
	sig := signSession(secret, payload)
	value := base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     totpStepCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(totpStepDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// GetTOTPStepUserID returns userID from the TOTP step cookie if valid, else "".
func GetTOTPStepUserID(r *http.Request, secret string) string {
	c, err := r.Cookie(totpStepCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(signSession(secret, payload)), []byte(parts[1])) {
		return ""
	}
	idx := strings.LastIndex(payload, "|")
	if idx < 0 {
		return ""
	}
	expiry, err := strconv.ParseInt(payload[idx+1:], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return ""
	}
	return payload[:idx]
}

// ClearTOTPStepCookie removes the TOTP step cookie.
func ClearTOTPStepCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   totpStepCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// SetTOTPPendingCookie sets a short-lived cookie with userID and TOTP secret for the "enable 2FA" confirmation step.
func SetTOTPPendingCookie(w http.ResponseWriter, r *http.Request, secret, userID, totpSecret string) {
	expiry := time.Now().Add(totpPendingDuration).Unix()
	payload := userID + "|" + totpSecret + "|" + strconv.FormatInt(expiry, 10)
	sig := signSession(secret, payload)
	value := base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     totpPendingCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(totpPendingDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// GetTOTPPendingCookie returns (userID, totpSecret) from the pending 2FA cookie if valid, else ("", "").
func GetTOTPPendingCookie(r *http.Request, secret string) (userID, totpSecret string) {
	c, err := r.Cookie(totpPendingCookieName)
	if err != nil || c.Value == "" {
		return "", ""
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ""
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(signSession(secret, payload)), []byte(parts[1])) {
		return "", ""
	}
	// payload = userID|totpSecret|expiry
	last := strings.LastIndex(payload, "|")
	if last < 0 {
		return "", ""
	}
	expiry, err := strconv.ParseInt(payload[last+1:], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", ""
	}
	mid := strings.LastIndex(payload[:last], "|")
	if mid < 0 {
		return "", ""
	}
	return payload[:mid], payload[mid+1 : last]
}

// ClearTOTPPendingCookie removes the pending 2FA cookie.
func ClearTOTPPendingCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   totpPendingCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// Redirect redirects to path with status 302.
func Redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusFound)
}

// SetCSRFToken generates a CSRF token, sets it in a signed cookie, and returns the token
// for embedding in forms. Call on GET of any page that shows a form (login, register, dashboard, admin).
func SetCSRFToken(w http.ResponseWriter, r *http.Request, secret string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	token := hex.EncodeToString(b)
	expiry := time.Now().Add(csrfDuration).Unix()
	payload := token + "|" + strconv.FormatInt(expiry, 10)
	sig := signSession(secret, payload)
	value := base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(csrfDuration.Seconds()),
		HttpOnly: false, // not needed in JS; form submits send it in body
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return token
}

// ValidateCSRF returns true if the request has a valid CSRF token in the form that
// matches the signed cookie. Call before processing any state-changing POST.
func ValidateCSRF(r *http.Request, secret string) bool {
	if err := r.ParseForm(); err != nil {
		return false
	}
	formToken := r.FormValue("csrf")
	if formToken == "" {
		return false
	}
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(payloadBytes)
	if !hmac.Equal([]byte(signSession(secret, payload)), []byte(parts[1])) {
		return false
	}
	idx := strings.LastIndex(payload, "|")
	if idx < 0 {
		return false
	}
	expiry, err := strconv.ParseInt(payload[idx+1:], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}
	token := payload[:idx]
	return hmac.Equal([]byte(token), []byte(formToken))
}
