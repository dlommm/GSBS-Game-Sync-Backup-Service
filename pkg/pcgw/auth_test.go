package pcgw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeWiki is a minimal MediaWiki: a login token, action=login against one bot
// password, and action=cargoquery that answers only a logged-in session.
type fakeWiki struct {
	mu       sync.Mutex
	sessions map[string]bool
	logins   int
	queries  int
	next     int
}

const (
	fakeBotUser = "Tester@gsbs"
	fakeBotPass = "s3cret-bot-pass"
)

func newFakeWiki(t *testing.T) (*fakeWiki, *httptest.Server) {
	t.Helper()
	w := &fakeWiki{sessions: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(srv.Close)
	return w, srv
}

func (w *fakeWiki) serve(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = r.ParseForm()
	session := ""
	if ck, err := r.Cookie("wikisession"); err == nil {
		session = ck.Value
	}
	switch {
	case r.Form.Get("meta") == "tokens":
		if session == "" {
			w.next++
			session = "s" + string(rune('0'+w.next))
			http.SetCookie(rw, &http.Cookie{Name: "wikisession", Value: session, Path: "/"})
		}
		_, _ = rw.Write([]byte(`{"query":{"tokens":{"logintoken":"tok+\\"}}}`))
	case r.Form.Get("action") == "login":
		if r.Method != http.MethodPost || r.URL.Query().Get("lgpassword") != "" {
			http.Error(rw, "password must be POSTed in the body", http.StatusBadRequest)
			return
		}
		w.logins++
		if r.PostForm.Get("lgname") == fakeBotUser && r.PostForm.Get("lgpassword") == fakeBotPass &&
			r.PostForm.Get("lgtoken") == `tok+\` && session != "" {
			w.sessions[session] = true
			_, _ = rw.Write([]byte(`{"login":{"result":"Success","lgusername":"Tester"}}`))
			return
		}
		_, _ = rw.Write([]byte(`{"login":{"result":"Failed","reason":"Incorrect username or password entered."}}`))
	case r.Form.Get("action") == "cargoquery":
		w.queries++
		if !w.sessions[session] {
			_, _ = rw.Write([]byte(`{"error":{"code":"permissiondenied","info":"You don't have permission to run arbitrary Cargo queries."}}`))
			return
		}
		_, _ = rw.Write([]byte(`{"cargoquery":[{"title":{"PageID":"5","Title":"Titan Quest"}}]}`))
	default:
		http.Error(rw, "unexpected request", http.StatusNotFound)
	}
}

// expireAll drops every session, as a wiki does when a login times out.
func (w *fakeWiki) expireAll() {
	w.mu.Lock()
	w.sessions = map[string]bool{}
	w.mu.Unlock()
}

func newTestClient(srv *httptest.Server, user, pass string) *Client {
	c := NewClient()
	c.BaseURL = srv.URL
	c.BotUser = user
	c.BotPassword = pass
	return c
}

func TestCargoBackendPrefersAPIWithBotLogin(t *testing.T) {
	t.Setenv("GSBS_PCGW_CARGO_BACKEND", "")
	t.Setenv("GSBS_PCGW_BOT_USER", "")
	t.Setenv("GSBS_PCGW_BOT_PASSWORD", "")

	if got := (&Client{}).cargoBackendFor().name(); got != CargoBackendExport {
		t.Errorf("no login: backend = %q, want %q", got, CargoBackendExport)
	}
	if got := (&Client{BotUser: "a@b", BotPassword: "p"}).cargoBackendFor().name(); got != CargoBackendAPI {
		t.Errorf("with login: backend = %q, want %q", got, CargoBackendAPI)
	}
	// An explicit choice still wins over the credentials.
	withExport := &Client{BotUser: "a@b", BotPassword: "p", CargoBackend: CargoBackendExport}
	if got := withExport.cargoBackendFor().name(); got != CargoBackendExport {
		t.Errorf("explicit export: backend = %q, want %q", got, CargoBackendExport)
	}

	t.Setenv("GSBS_PCGW_BOT_USER", "a@b")
	t.Setenv("GSBS_PCGW_BOT_PASSWORD", "p")
	if got := (&Client{}).cargoBackendFor().name(); got != CargoBackendAPI {
		t.Errorf("login from env: backend = %q, want %q", got, CargoBackendAPI)
	}
}

func TestCargoQueryLogsInWithBotPassword(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	t.Setenv("GSBS_PCGW_CARGO_BACKEND", "")
	wiki, srv := newFakeWiki(t)
	c := newTestClient(srv, fakeBotUser, fakeBotPass)

	for i := 0; i < 3; i++ {
		rows, err := c.runCargo(context.Background(), cargoRequest{Tables: "Game", Fields: "Game._pageID=PageID", Limit: 1})
		if err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
		if len(rows) != 1 || rows[0]["Title"] != "Titan Quest" {
			t.Fatalf("query %d: rows = %v", i, rows)
		}
	}
	if wiki.logins != 1 {
		t.Errorf("logins = %d, want 1 (the session should be reused)", wiki.logins)
	}
}

func TestCargoQueryLogsInAgainAfterSessionExpires(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	t.Setenv("GSBS_PCGW_CARGO_BACKEND", "")
	wiki, srv := newFakeWiki(t)
	c := newTestClient(srv, fakeBotUser, fakeBotPass)
	q := cargoRequest{Tables: "Game", Fields: "Game._pageID=PageID", Limit: 1}

	if _, err := c.runCargo(context.Background(), q); err != nil {
		t.Fatalf("first query: %v", err)
	}
	wiki.expireAll()
	if _, err := c.runCargo(context.Background(), q); err != nil {
		t.Fatalf("query after expiry: %v", err)
	}
	if wiki.logins != 2 {
		t.Errorf("logins = %d, want 2", wiki.logins)
	}
}

func TestCargoQueryBadBotPasswordFailsWithoutLeakingIt(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	t.Setenv("GSBS_PCGW_CARGO_BACKEND", "")
	wiki, srv := newFakeWiki(t)
	c := newTestClient(srv, fakeBotUser, "wrong-password-value")

	_, err := c.runCargo(context.Background(), cargoRequest{Tables: "Game", Fields: "Game._pageID=PageID", Limit: 1})
	if err == nil {
		t.Fatal("want an error for a rejected login")
	}
	if !strings.Contains(err.Error(), "Incorrect username or password") || !strings.Contains(err.Error(), fakeBotUser) {
		t.Errorf("error should name the user and the wiki's reason: %v", err)
	}
	if strings.Contains(err.Error(), "wrong-password-value") {
		t.Errorf("error leaks the password: %v", err)
	}
	if wiki.queries != 0 {
		t.Errorf("queries = %d, want 0: nothing should run after a failed login", wiki.queries)
	}
}

func TestCargoQueryPermissionDeniedIsReportedWithoutLogin(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	t.Setenv("GSBS_PCGW_CARGO_BACKEND", CargoBackendAPI)
	t.Setenv("GSBS_PCGW_BOT_USER", "")
	t.Setenv("GSBS_PCGW_BOT_PASSWORD", "")
	_, srv := newFakeWiki(t)
	c := newTestClient(srv, "", "")

	_, err := c.runCargo(context.Background(), cargoRequest{Tables: "Game", Fields: "Game._pageID=PageID", Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("want a permission error for anonymous cargoquery, got %v", err)
	}
}
