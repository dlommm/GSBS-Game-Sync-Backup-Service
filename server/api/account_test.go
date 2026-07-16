package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
)

// GET /api/account carries storage usage + quota since 5.4 so the client's
// local dashboard can show quota headroom (previously only visible as a 413
// error toast at push time).
func TestAccountIncludesUsageAndQuota(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := auth.NewService(st)
	ctx := context.Background()
	uid, _ := svc.RegisterUser(ctx, "u1", "password123")
	_, token, _ := svc.Login(ctx, "u1", "password123", "test-client", "linux")
	if err := st.SetUserQuota(ctx, uid, 1<<20); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(st, svc, false, nil, nil, nil, nil, nil, nil, 0, false, "", "test")

	// Store one save so usage is non-zero.
	body := []byte("some save bytes")
	push := httptest.NewRequest(http.MethodPost, "/api/saves", bytes.NewReader(body))
	push.Header.Set("Authorization", "Bearer "+token)
	push.Header.Set("X-Game-ID", "game1")
	push.Header.Set("X-Path-Key", "pk1")
	push.Header.Set("X-Content-Hash", sha256Hex(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, push)
	if rec.Code != http.StatusOK {
		t.Fatalf("push: %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/account", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		UsageBytes int64 `json:"usage_bytes"`
		QuotaBytes int64 `json:"quota_bytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.UsageBytes <= 0 {
		t.Fatalf("usage_bytes = %d, want > 0", out.UsageBytes)
	}
	if out.QuotaBytes != 1<<20 {
		t.Fatalf("quota_bytes = %d, want %d", out.QuotaBytes, 1<<20)
	}
}
