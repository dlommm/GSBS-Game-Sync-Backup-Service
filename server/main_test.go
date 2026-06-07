package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func captureMainLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })
	return buf
}

// TestRecoverMiddleware_PanicAPIRoute verifies that a panic inside an /api/
// handler is caught, logged with the stack trace, and returns structured JSON
// with HTTP 500 — keeping the server process alive.
func TestRecoverMiddleware_PanicAPIRoute(t *testing.T) {
	logBuf := captureMainLog(t)

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("deliberate test panic")
	})
	handler := recoverMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/saves", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error"`) || !strings.Contains(body, `"internal error"`) {
		t.Errorf("expected JSON error body, got: %s", body)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected application/json content type, got: %s", contentType)
	}
	logOut := logBuf.String()
	if !strings.Contains(logOut, "panic recovered") {
		t.Errorf("expected 'panic recovered' in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, "deliberate test panic") {
		t.Errorf("expected panic value in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, "stack") {
		t.Errorf("expected stack field in log, got: %s", logOut)
	}
}

// TestRecoverMiddleware_PanicWebUIRoute verifies that panics on non-/api/
// routes return plain text (not JSON).
func TestRecoverMiddleware_PanicWebUIRoute(t *testing.T) {
	captureMainLog(t) // suppress log noise

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("deliberate web panic")
	})
	handler := recoverMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	contentType := rec.Header().Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		t.Errorf("WebUI route should not get JSON content-type, got: %s", contentType)
	}
}

// TestRecoverMiddleware_NoPanic verifies that a normal (non-panicking) handler
// passes through unmodified with its original status code.
func TestRecoverMiddleware_NoPanic(t *testing.T) {
	normalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	handler := recoverMiddleware(normalHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
