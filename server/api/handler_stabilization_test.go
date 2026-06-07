package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// failStore wraps a real store and overrides specific methods to return errors,
// allowing failure-path testing without needing a real DB error condition.
type failStore struct {
	store.Store
	listSaveSummariesErr      error
	listSaveSummariesPagedErr error
	totalStorageBytesErr      error
	userStorageBytesErr       error
}

func (f *failStore) ListSaveSummaries(ctx context.Context, userID string) ([]store.SaveSummary, error) {
	if f.listSaveSummariesErr != nil {
		return nil, f.listSaveSummariesErr
	}
	return f.Store.ListSaveSummaries(ctx, userID)
}

func (f *failStore) ListSaveSummariesPaginated(ctx context.Context, userID string, limit, offset int) ([]store.SaveSummary, int, error) {
	if f.listSaveSummariesPagedErr != nil {
		return nil, 0, f.listSaveSummariesPagedErr
	}
	return f.Store.ListSaveSummariesPaginated(ctx, userID, limit, offset)
}

func (f *failStore) TotalStorageBytes(ctx context.Context) (int64, error) {
	if f.totalStorageBytesErr != nil {
		return 0, f.totalStorageBytesErr
	}
	return f.Store.TotalStorageBytes(ctx)
}

func (f *failStore) UserStorageBytes(ctx context.Context, userID string) (int64, error) {
	if f.userStorageBytesErr != nil {
		return 0, f.userStorageBytesErr
	}
	return f.Store.UserStorageBytes(ctx, userID)
}

// captureLog redirects the global zerolog logger to a buffer for the duration of
// the test, restoring it in t.Cleanup.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Logger
	log.Logger = zerolog.New(buf)
	t.Cleanup(func() { log.Logger = orig })
	return buf
}

// TestHandleSaveSummaries_DBLockedReturns503 verifies that a "database is locked"
// store error is classified correctly, logged with the right structured fields,
// and returns HTTP 503.
func TestHandleSaveSummaries_DBLockedReturns503(t *testing.T) {
	realSt, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer realSt.Close()

	svc := auth.NewService(realSt)
	ctx := context.Background()
	_, _ = svc.RegisterUser(ctx, "sumuser", "password123")
	_, token, _ := svc.Login(ctx, "sumuser", "password123", "test", "linux")

	dbLockedErr := errors.New("database is locked")
	fs := &failStore{Store: realSt, listSaveSummariesErr: dbLockedErr}
	h := NewHandler(fs, svc, false, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	logBuf := captureLog(t)

	req := httptest.NewRequest(http.MethodGet, "/api/saves?summaries=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", "test-req-id-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d — body: %s", rec.Code, rec.Body.String())
	}
	logOut := logBuf.String()
	for _, field := range []string{"user_id", "limit", "offset", "request_id", "error_class", "db_locked"} {
		if !strings.Contains(logOut, field) {
			t.Errorf("expected log field %q in output: %s", field, logOut)
		}
	}
}

// TestHandleSaveSummaries_UnknownErrorReturns500 verifies that an unclassified
// store error still returns HTTP 500 (not 503).
func TestHandleSaveSummaries_UnknownErrorReturns500(t *testing.T) {
	realSt, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer realSt.Close()

	svc := auth.NewService(realSt)
	ctx := context.Background()
	_, _ = svc.RegisterUser(ctx, "sum500user", "password123")
	_, token, _ := svc.Login(ctx, "sum500user", "password123", "test", "linux")

	fs := &failStore{Store: realSt, listSaveSummariesErr: errors.New("unexpected store failure")}
	h := NewHandler(fs, svc, false, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/saves?summaries=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// TestHandlePush_UserStorageBytesError_FailsClosed verifies that when
// UserStorageBytes returns an error the push is rejected with 503 and not
// silently allowed through (quota bypass protection).
func TestHandlePush_UserStorageBytesError_FailsClosed(t *testing.T) {
	realSt, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer realSt.Close()

	svc := auth.NewService(realSt)
	ctx := context.Background()
	userID, _ := svc.RegisterUser(ctx, "pushfail", "password123")
	_, token, _ := svc.Login(ctx, "pushfail", "password123", "test", "linux")

	// Set a quota so the per-user check runs.
	if err := realSt.SetUserQuota(ctx, userID, 1<<20); err != nil {
		t.Fatal(err)
	}

	fs := &failStore{Store: realSt, userStorageBytesErr: errors.New("database is locked")}
	h := NewHandler(fs, svc, false, sse.NewHub(), nil, nil, nil, nil, nil, 0, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/saves", bytes.NewReader([]byte("save-content")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Game-ID", "g1")
	req.Header.Set("X-Path-Key", "pk1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when UserStorageBytes fails, got %d — body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("storage check failed")) {
		t.Errorf("expected 'storage check failed' error message, got: %s", rec.Body.String())
	}
}

// TestHandlePush_TotalStorageBytesError_FailsClosed verifies that when
// TotalStorageBytes returns an error the push is rejected with 503.
func TestHandlePush_TotalStorageBytesError_FailsClosed(t *testing.T) {
	realSt, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer realSt.Close()

	svc := auth.NewService(realSt)
	ctx := context.Background()
	_, _ = svc.RegisterUser(ctx, "pushglobalfail", "password123")
	_, token, _ := svc.Login(ctx, "pushglobalfail", "password123", "test", "linux")

	fs := &failStore{Store: realSt, totalStorageBytesErr: errors.New("database is locked")}
	// maxStorageBytes > 0 activates the global check.
	h := NewHandler(fs, svc, false, sse.NewHub(), nil, nil, nil, nil, nil, 1<<30, false, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/saves", bytes.NewReader([]byte("save-content")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Game-ID", "g2")
	req.Header.Set("X-Path-Key", "pk2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when TotalStorageBytes fails, got %d — body: %s", rec.Code, rec.Body.String())
	}
}

// TestClassifyDBError tests the error classifier directly.
func TestClassifyDBError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("database is locked"), "db_locked"},
		{errors.New("SQLITE_BUSY: try again"), "db_locked"},
		{errors.New("context canceled"), "context_canceled"},
		{errors.New("context deadline exceeded"), "context_canceled"},
		{errors.New("no such column: foo"), "schema_error"},
		{errors.New("no such table: saves"), "schema_error"},
		{errors.New("random io error"), "unknown"},
	}
	for _, tc := range cases {
		got := classifyDBError(tc.err)
		if got != tc.want {
			t.Errorf("classifyDBError(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// Ensure failStore fully satisfies the store.Store interface at compile time.
var _ store.Store = (*failStore)(nil)
