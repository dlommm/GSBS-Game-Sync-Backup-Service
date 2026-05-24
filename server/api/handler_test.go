package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
)

func TestHandler_Health(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := NewHandler(st, auth.NewService(st), true, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("body: %v", body)
	}
}

func TestHandler_Register_Login_Pull(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	authSvc := auth.NewService(st)
	h := NewHandler(st, authSvc, true, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	// Register
	regBody, _ := json.Marshal(map[string]string{"username": "testuser", "password": "password1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("register status: %d body: %s", rec.Code, rec.Body.String())
	}

	// Login
	loginBody, _ := json.Marshal(map[string]string{"username": "testuser", "password": "password1234", "client_name": "test", "client_os": "linux"})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("login status: %d body: %s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatal(err)
	}
	if loginResp.Token == "" {
		t.Fatal("empty token")
	}

	// Pull (authenticated)
	req = httptest.NewRequest(http.MethodGet, "/api/saves", nil)
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("pull status: %d body: %s", rec.Code, rec.Body.String())
	}
	var pullResp struct {
		Saves []interface{} `json:"saves"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&pullResp); err != nil {
		t.Fatal(err)
	}
	if len(pullResp.Saves) != 0 {
		t.Errorf("expected 0 saves, got %d", len(pullResp.Saves))
	}
}

func TestHandler_RegisterDisabled(t *testing.T) {
	st, _ := store.NewSQLite(":memory:")
	defer st.Close()
	h := NewHandler(st, auth.NewService(st), false, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	regBody, _ := json.Marshal(map[string]string{"username": "u", "password": "password1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when registration disabled, got %d", rec.Code)
	}
}

func TestHandler_PullUnauthorized(t *testing.T) {
	st, _ := store.NewSQLite(":memory:")
	defer st.Close()
	h := NewHandler(st, auth.NewService(st), true, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/saves", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", rec.Code)
	}
}
