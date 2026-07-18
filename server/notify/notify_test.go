package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type capture struct {
	mu     sync.Mutex
	bodies []string
	titles []string
	ctypes []string
}

func (c *capture) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, string(b))
		c.titles = append(c.titles, r.Header.Get("X-Title"))
		c.ctypes = append(c.ctypes, r.Header.Get("Content-Type"))
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// allowLoopbackForTest swaps in plain clients so the delivery-logic tests can
// target loopback httptest servers; production always uses the SSRF-guarded
// client (exercised separately by TestNotifier_UserSinkToLoopbackBlocked and
// the netutil package tests).
func allowLoopbackForTest(n *Notifier) {
	c := &http.Client{Timeout: 3 * time.Second}
	n.safeClient = c
	n.adminClient = c
}

func TestNotifier_DeliversToAdminAndUserSinks(t *testing.T) {
	adminHook := &capture{}
	userNtfy := &capture{}
	adminSrv := httptest.NewServer(adminHook.handler())
	defer adminSrv.Close()
	userSrv := httptest.NewServer(userNtfy.handler())
	defer userSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := New(ctx,
		func(context.Context) Sinks { return Sinks{WebhookURL: adminSrv.URL} },
		func(_ context.Context, userID string) Sinks {
			if userID == "u1" {
				return Sinks{NtfyURL: userSrv.URL}
			}
			return Sinks{}
		},
	)
	allowLoopbackForTest(n)

	n.Notify(Event{Type: EventConflict, Title: "Conflict", Body: "details", UserID: "u1"})
	waitFor(t, func() bool { return adminHook.count() == 1 && userNtfy.count() == 1 })

	// Webhook payload is JSON with the event fields.
	var payload map[string]string
	if err := json.Unmarshal([]byte(adminHook.bodies[0]), &payload); err != nil {
		t.Fatalf("webhook payload not JSON: %v", err)
	}
	if payload["type"] != EventConflict || payload["title"] != "Conflict" {
		t.Fatalf("webhook payload = %v", payload)
	}
	// ntfy carries the title as a header and the body as plain text.
	if userNtfy.titles[0] != "Conflict" || userNtfy.bodies[0] != "details" {
		t.Fatalf("ntfy delivery = title %q body %q", userNtfy.titles[0], userNtfy.bodies[0])
	}

	// Global event (no user): only admin sink fires.
	n.Notify(Event{Type: EventBackup, Title: "Backup done"})
	waitFor(t, func() bool { return adminHook.count() == 2 })
	if userNtfy.count() != 1 {
		t.Fatalf("user sink received a global event")
	}
}

func TestNotifier_EventFilter(t *testing.T) {
	hook := &capture{}
	srv := httptest.NewServer(hook.handler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := New(ctx,
		func(context.Context) Sinks {
			return Sinks{WebhookURL: srv.URL, Events: map[string]bool{EventBackup: true}}
		}, nil)
	allowLoopbackForTest(n)

	n.Notify(Event{Type: EventLogin, Title: "filtered out"})
	n.Notify(Event{Type: EventBackup, Title: "kept"})
	waitFor(t, func() bool { return hook.count() == 1 })
	if hook.count() != 1 {
		t.Fatalf("filter failed: %d deliveries", hook.count())
	}
}

// TestNotifier_UserSinkToLoopbackBlocked: a user-configured sink pointing at a
// loopback/internal address is refused by the SSRF guard and never delivered.
func TestNotifier_UserSinkToLoopbackBlocked(t *testing.T) {
	hook := &capture{}
	srv := httptest.NewServer(hook.handler()) // listens on 127.0.0.1
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Note: NO allowLoopbackForTest — the production safe client is in effect.
	n := New(ctx, nil, func(_ context.Context, _ string) Sinks {
		return Sinks{WebhookURL: srv.URL}
	})

	n.Notify(Event{Type: EventConflict, Title: "x", UserID: "u1"})
	time.Sleep(600 * time.Millisecond) // dial is refused instantly; give the worker time
	if hook.count() != 0 {
		t.Fatalf("user sink to loopback should be blocked, got %d deliveries", hook.count())
	}
}

func TestParseEventFilter(t *testing.T) {
	if ParseEventFilter("") != nil || ParseEventFilter("[]") != nil || ParseEventFilter("garbage") != nil {
		t.Fatal("empty/invalid filters must mean all-events (nil)")
	}
	f := ParseEventFilter(`["backup","quota"]`)
	if !f["backup"] || !f["quota"] || f["login"] {
		t.Fatalf("filter = %v", f)
	}
}

func TestDiscordPayload(t *testing.T) {
	hook := &capture{}
	srv := httptest.NewServer(hook.handler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := New(ctx, func(context.Context) Sinks { return Sinks{DiscordURL: srv.URL} }, nil)
	allowLoopbackForTest(n)
	n.Notify(Event{Type: EventTest, Title: "T", Body: "B"})
	waitFor(t, func() bool { return hook.count() == 1 })
	var payload map[string]string
	if err := json.Unmarshal([]byte(hook.bodies[0]), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["content"] != "**T**\nB" {
		t.Fatalf("discord content = %q", payload["content"])
	}
}
