package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
)

func TestPushGzipAndHashDedup(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := auth.NewService(st)
	ctx := context.Background()
	_, _ = svc.RegisterUser(ctx, "u1", "password123")
	_, token, _ := svc.Login(ctx, "u1", "password123", "test-client", "linux")

	h := NewHandler(st, svc, false, nil, nil, nil, nil, 0, false, "", "test")

	body := []byte("save content v1")
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write(body)
	_ = gw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/saves", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Game-ID", "game1")
	req.Header.Set("X-Path-Key", "pk1")
	req.Header.Set("X-Content-Hash", "abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first push: %d %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/saves", bytes.NewReader(body))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("X-Game-ID", "game1")
	req2.Header.Set("X-Path-Key", "pk1")
	req2.Header.Set("X-Content-Hash", "abc")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("dedup push: %d", rec2.Code)
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("unchanged")) {
		t.Fatalf("expected unchanged, got %s", rec2.Body.String())
	}
}

func TestPullSingleSave(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := auth.NewService(st)
	ctx := context.Background()
	userID, _ := svc.RegisterUser(ctx, "u2", "password123")
	_, token, _ := svc.Login(ctx, "u2", "password123", "c", "linux")
	_ = st.UpsertSave(ctx, userID, "g1", "pk1", []byte("data"))

	h := NewHandler(st, svc, false, nil, nil, nil, nil, 0, false, "", "test")
	req := httptest.NewRequest(http.MethodGet, "/api/saves?game_id=g1&path_key=pk1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull single: %d %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("g1")) {
		t.Fatalf("expected game in response")
	}
}
