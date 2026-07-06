package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestTokenRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/token/refresh" {
			http.Error(w, "wrong route", http.StatusNotFound)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer good-token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"token": "rotated-token", "expires_in": 7776000})
		case "Bearer empty-token":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"token": ""})
		default:
			http.Error(w, `{"error":"refresh failed"}`, http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	got, err := requestTokenRefresh(context.Background(), srv.URL, "good-token")
	if err != nil || got != "rotated-token" {
		t.Fatalf("refresh ok case: got %q, %v", got, err)
	}
	if _, err := requestTokenRefresh(context.Background(), srv.URL, "expired-token"); err == nil {
		t.Fatal("expected error for 401 response")
	}
	if _, err := requestTokenRefresh(context.Background(), srv.URL, "empty-token"); err == nil {
		t.Fatal("expected error for empty token in response")
	}
}
