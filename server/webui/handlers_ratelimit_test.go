package webui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gsbs/gsbs/server/ratelimit"
)

// newRateLimitTestHandler builds a WebHandler with a limiter that allows a
// single request per window. The store is nil: the tests only exercise the
// deny branch, which must return before any store access.
func newRateLimitTestHandler(t *testing.T) *WebHandler {
	t.Helper()
	return &WebHandler{
		secret:        testSecret,
		templates:     parseTemplates(),
		allowRegister: true,
		loginLimiter:  ratelimit.New(1, time.Minute),
	}
}

// authedPost builds a POST with a valid CSRF cookie+token pair and any extra
// cookies, from the given client address.
func authedPost(t *testing.T, path, remoteAddr string, form url.Values, extra ...*http.Cookie) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	token := SetCSRFToken(rec, httptest.NewRequest("GET", "/", nil), testSecret)
	form.Set("csrf", token)
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	r.AddCookie(rec.Result().Cookies()[0])
	for _, c := range extra {
		r.AddCookie(c)
	}
	return r
}

func TestHandleLoginTOTPRateLimited(t *testing.T) {
	h := newRateLimitTestHandler(t)
	addr := "203.0.113.50:1234"

	// Exhaust the 1/minute budget for this IP.
	if !h.loginLimiter.Allow("203.0.113.50") {
		t.Fatal("first Allow should pass")
	}

	stepRec := httptest.NewRecorder()
	SetTOTPStepCookie(stepRec, httptest.NewRequest("GET", "/", nil), testSecret, "user-1")
	r := authedPost(t, "/login/totp", addr, url.Values{"code": {"123456"}}, stepRec.Result().Cookies()[0])

	rec := httptest.NewRecorder()
	h.handleLoginTOTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered form)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Too many attempts") {
		t.Fatalf("expected rate-limit error in body, got: %.200s", rec.Body.String())
	}
}

func TestHandleRegisterRateLimited(t *testing.T) {
	h := newRateLimitTestHandler(t)
	addr := "203.0.113.51:1234"

	if !h.loginLimiter.Allow("203.0.113.51") {
		t.Fatal("first Allow should pass")
	}

	r := authedPost(t, "/register", addr, url.Values{
		"username":         {"newuser"},
		"password":         {"password123"},
		"confirm_password": {"password123"},
	})

	rec := httptest.NewRecorder()
	h.handleRegister(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered form)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Too many attempts") {
		t.Fatalf("expected rate-limit error in body, got: %.200s", rec.Body.String())
	}
}
