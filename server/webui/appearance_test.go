package webui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

// newAppearanceTestHandler builds a WebHandler over an in-memory store with
// one signed-in user, returning the handler, the user id, and a request
// factory that attaches a valid session cookie.
func newAppearanceTestHandler(t *testing.T) (*WebHandler, string, func(method, path string) *httptest.ResponseRecorder) {
	t.Helper()
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	uid, err := st.CreateUser(ctx, "appearanceuser", "hash")
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := st.CreateSession(ctx, uid, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	h := &WebHandler{secret: testSecret, templates: parseTemplates(), store: st}

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		cookieRec := httptest.NewRecorder()
		SetSession(cookieRec, httptest.NewRequest("GET", "/", nil), testSecret, sessionID)
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(cookieRec.Result().Cookies()[0])
		h.ServeHTTP(rec, r)
		return rec
	}
	return h, uid, do
}

func TestDashboardRendersAppearanceAttrs(t *testing.T) {
	h, uid, do := newAppearanceTestHandler(t)
	ctx := context.Background()
	if err := h.store.SetUserPref(ctx, uid, "appearance.design", "hud"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetUserPref(ctx, uid, "appearance.layout", "topnav"); err != nil {
		t.Fatal(err)
	}

	rec := do("GET", "/dashboard")
	if rec.Code != 200 {
		t.Fatalf("dashboard status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-design="hud"`) {
		t.Error("dashboard missing data-design attr from user pref")
	}
	if !strings.Contains(body, `data-layout="topnav"`) {
		t.Error("dashboard missing data-layout attr from user pref")
	}
}

func TestDashboardIgnoresInvalidStoredAppearance(t *testing.T) {
	h, uid, do := newAppearanceTestHandler(t)
	ctx := context.Background()
	if err := h.store.SetUserPref(ctx, uid, "appearance.design", "evil"); err != nil {
		t.Fatal(err)
	}
	rec := do("GET", "/dashboard")
	if rec.Code != 200 {
		t.Fatalf("dashboard status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-design=`) {
		t.Error("invalid stored design must render as default (no attr)")
	}
}
