package webui

import (
	"crypto/hmac"
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

// SetSession sets a signed session cookie with userID.
func SetSession(w http.ResponseWriter, secret, userID string) {
	expiry := time.Now().Add(sessionDuration).Unix()
	payload := userID + "|" + strconv.FormatInt(expiry, 10)
	sig := signSession(secret, payload)
	value := base64.StdEncoding.EncodeToString([]byte(payload)) + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
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

// GetSession returns userID if the session cookie is valid, else "".
func GetSession(r *http.Request, secret string) string {
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

// Redirect redirects to path with status 302.
func Redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusFound)
}
