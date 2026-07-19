package webui

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
)

// appearanceHarness is a WebHandler over an in-memory store with one
// signed-in user and helpers for authed GET/POST requests.
type appearanceHarness struct {
	h         *WebHandler
	userID    string
	sessionID string
}

func newAppearanceHarness(t *testing.T) *appearanceHarness {
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
	return &appearanceHarness{
		h:         &WebHandler{secret: testSecret, templates: parseTemplates(), store: st},
		userID:    uid,
		sessionID: sessionID,
	}
}

func (a *appearanceHarness) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	cookieRec := httptest.NewRecorder()
	SetSession(cookieRec, httptest.NewRequest("GET", "/", nil), testSecret, a.sessionID)
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookieRec.Result().Cookies()[0])
	a.h.ServeHTTP(rec, r)
	return rec
}

func (a *appearanceHarness) post(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sessRec := httptest.NewRecorder()
	SetSession(sessRec, httptest.NewRequest("GET", "/", nil), testSecret, a.sessionID)
	csrfRec := httptest.NewRecorder()
	token := SetCSRFToken(csrfRec, httptest.NewRequest("GET", "/", nil), testSecret)
	form.Set("csrf", token)
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sessRec.Result().Cookies()[0])
	r.AddCookie(csrfRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, r)
	return rec
}

func TestDashboardRendersAppearanceAttrs(t *testing.T) {
	a := newAppearanceHarness(t)
	ctx := context.Background()
	if err := a.h.store.SetUserPref(ctx, a.userID, "appearance.design", "hud"); err != nil {
		t.Fatal(err)
	}
	if err := a.h.store.SetUserPref(ctx, a.userID, "appearance.layout", "topnav"); err != nil {
		t.Fatal(err)
	}

	rec := a.get(t, "/dashboard")
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
	a := newAppearanceHarness(t)
	if err := a.h.store.SetUserPref(context.Background(), a.userID, "appearance.design", "evil"); err != nil {
		t.Fatal(err)
	}
	rec := a.get(t, "/dashboard")
	if rec.Code != 200 {
		t.Fatalf("dashboard status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `data-design=`) {
		t.Error("invalid stored design must render as default (no attr)")
	}
}

func TestAppearanceSaveRoundTrip(t *testing.T) {
	a := newAppearanceHarness(t)
	ctx := context.Background()

	rec := a.post(t, "/dashboard/settings/appearance", url.Values{"design": {"synth"}, "layout": {"dense"}})
	if rec.Code != 302 {
		t.Fatalf("save status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "appearance_saved=1") {
		t.Fatalf("redirect = %q, want appearance_saved flash", loc)
	}
	if v, _ := a.h.store.GetUserPref(ctx, a.userID, "appearance.design"); v != "synth" {
		t.Fatalf("stored design = %q, want synth", v)
	}
	if v, _ := a.h.store.GetUserPref(ctx, a.userID, "appearance.layout"); v != "dense" {
		t.Fatalf("stored layout = %q, want dense", v)
	}

	// The explicit default keys clear the stored prefs.
	rec = a.post(t, "/dashboard/settings/appearance", url.Values{"design": {"default"}, "layout": {"sidebar"}})
	if rec.Code != 302 {
		t.Fatalf("reset status = %d", rec.Code)
	}
	if v, _ := a.h.store.GetUserPref(ctx, a.userID, "appearance.design"); v != "" {
		t.Fatalf("design after reset = %q, want \"\"", v)
	}

	// Invalid values are rejected and leave prefs untouched.
	_ = a.post(t, "/dashboard/settings/appearance", url.Values{"design": {"synth"}, "layout": {"dense"}})
	rec = a.post(t, "/dashboard/settings/appearance", url.Values{"design": {"bogus"}, "layout": {"dense"}})
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=invalid_appearance") {
		t.Fatalf("redirect = %q, want invalid_appearance", loc)
	}
	if v, _ := a.h.store.GetUserPref(ctx, a.userID, "appearance.design"); v != "synth" {
		t.Fatalf("design after invalid save = %q, want synth (unchanged)", v)
	}
}
