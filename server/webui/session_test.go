package webui

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-0123456789abcdef0123456789abcdef"

// signedCookieValue builds a cookie value the same way the Set* helpers do,
// allowing expired/tampered variants to be constructed for negative tests.
func signedCookieValue(secret, payload string) string {
	return base64.StdEncoding.EncodeToString([]byte(payload)) + "." + signSession(secret, payload)
}

func requestWithCookie(name, value string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: name, Value: value})
	return r
}

func TestSessionCookieRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSession(rec, httptest.NewRequest("GET", "/", nil), testSecret, "sess-123")
	c := rec.Result().Cookies()[0]
	if c.Name != sessionCookieName || !c.HttpOnly {
		t.Fatalf("unexpected cookie: %+v", c)
	}

	if got := GetSessionID(requestWithCookie(c.Name, c.Value), testSecret); got != "sess-123" {
		t.Fatalf("round trip got %q", got)
	}
	if got := GetSessionID(requestWithCookie(c.Name, c.Value), "wrong-secret"); got != "" {
		t.Fatalf("wrong secret accepted, got %q", got)
	}
}

func TestSessionCookieTampered(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSession(rec, httptest.NewRequest("GET", "/", nil), testSecret, "sess-123")
	c := rec.Result().Cookies()[0]

	// Swap the payload for a different session ID, keeping the old signature.
	parts := strings.SplitN(c.Value, ".", 2)
	forged := base64.StdEncoding.EncodeToString([]byte("sess-456|"+strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))) + "." + parts[1]
	if got := GetSessionID(requestWithCookie(c.Name, forged), testSecret); got != "" {
		t.Fatalf("tampered cookie accepted, got %q", got)
	}
	if got := GetSessionID(requestWithCookie(c.Name, "garbage"), testSecret); got != "" {
		t.Fatalf("garbage cookie accepted, got %q", got)
	}
}

func TestSessionCookieExpired(t *testing.T) {
	past := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	value := signedCookieValue(testSecret, "sess-123|"+past)
	if got := GetSessionID(requestWithCookie(sessionCookieName, value), testSecret); got != "" {
		t.Fatalf("expired cookie accepted, got %q", got)
	}
}

func TestTOTPStepCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetTOTPStepCookie(rec, httptest.NewRequest("GET", "/", nil), testSecret, "user-1")
	c := rec.Result().Cookies()[0]

	if got := GetTOTPStepUserID(requestWithCookie(c.Name, c.Value), testSecret); got != "user-1" {
		t.Fatalf("round trip got %q", got)
	}
	if got := GetTOTPStepUserID(requestWithCookie(c.Name, c.Value), "wrong-secret"); got != "" {
		t.Fatalf("wrong secret accepted, got %q", got)
	}

	past := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expired := signedCookieValue(testSecret, "user-1|"+past)
	if got := GetTOTPStepUserID(requestWithCookie(totpStepCookieName, expired), testSecret); got != "" {
		t.Fatalf("expired step cookie accepted, got %q", got)
	}
}

func TestTOTPPendingCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetTOTPPendingCookie(rec, httptest.NewRequest("GET", "/", nil), testSecret, "user-1", "BASE32SECRET")
	c := rec.Result().Cookies()[0]

	uid, sec := GetTOTPPendingCookie(requestWithCookie(c.Name, c.Value), testSecret)
	if uid != "user-1" || sec != "BASE32SECRET" {
		t.Fatalf("round trip got (%q, %q)", uid, sec)
	}
	if uid, _ := GetTOTPPendingCookie(requestWithCookie(c.Name, c.Value), "wrong-secret"); uid != "" {
		t.Fatalf("wrong secret accepted, got %q", uid)
	}
}

// csrfRequest builds a POST carrying both the signed CSRF cookie and the
// matching (or given) form token.
func csrfRequest(t *testing.T, formToken string, cookie *http.Cookie) *http.Request {
	t.Helper()
	form := url.Values{"csrf": {formToken}}
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestValidateCSRF(t *testing.T) {
	rec := httptest.NewRecorder()
	token := SetCSRFToken(rec, httptest.NewRequest("GET", "/", nil), testSecret)
	if token == "" {
		t.Fatal("empty CSRF token")
	}
	cookie := rec.Result().Cookies()[0]

	if !ValidateCSRF(csrfRequest(t, token, cookie), testSecret) {
		t.Fatal("valid token rejected")
	}
	if ValidateCSRF(csrfRequest(t, "not-the-token", cookie), testSecret) {
		t.Fatal("mismatched form token accepted")
	}
	if ValidateCSRF(csrfRequest(t, token, nil), testSecret) {
		t.Fatal("missing cookie accepted")
	}
	if ValidateCSRF(csrfRequest(t, token, cookie), "wrong-secret") {
		t.Fatal("wrong secret accepted")
	}

	past := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expired := &http.Cookie{Name: csrfCookieName, Value: signedCookieValue(testSecret, token+"|"+past)}
	if ValidateCSRF(csrfRequest(t, token, expired), testSecret) {
		t.Fatal("expired CSRF cookie accepted")
	}
}
