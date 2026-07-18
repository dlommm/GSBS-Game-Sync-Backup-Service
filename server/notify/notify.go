// Package notify delivers server events to user-configured HTTP sinks:
// generic webhooks (JSON POST), Discord webhooks, and ntfy topics. Everything
// is plain net/http — no third-party services are required or contacted
// unless the operator configures a URL.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/gsbs/gsbs/pkg/retry"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/netutil"
)

// Event types. A user-scoped event (UserID != "") goes to that user's sinks
// AND the admin sinks; global events go to the admin sinks only.
const (
	EventConflict         = "conflict"
	EventQuota            = "quota"
	EventDeviceRegistered = "device_registered"
	EventLogin            = "login"
	EventBackup           = "backup"
	EventStaleDevice      = "stale_device"
	EventTest             = "test"
)

// AllEvents lists every event type (used by the settings UI).
var AllEvents = []string{EventConflict, EventQuota, EventDeviceRegistered, EventLogin, EventBackup, EventStaleDevice}

// Event is one notification.
type Event struct {
	Type   string
	Title  string
	Body   string
	UserID string // "" = admin/global
	At     time.Time
}

// Sinks is one recipient's delivery configuration.
type Sinks struct {
	WebhookURL string
	DiscordURL string
	NtfyURL    string
	// Events filters delivery; nil or empty = all events.
	Events map[string]bool
}

func (s Sinks) empty() bool {
	return s.WebhookURL == "" && s.DiscordURL == "" && s.NtfyURL == ""
}

func (s Sinks) wants(eventType string) bool {
	if len(s.Events) == 0 {
		return true
	}
	return s.Events[eventType]
}

// ParseEventFilter decodes the stored JSON array of enabled event types
// (empty input = all events enabled).
func ParseEventFilter(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, e := range list {
		out[e] = true
	}
	return out
}

// Notifier queues events and delivers them asynchronously so notification
// latency never blocks request handling. The queue drops oldest on overflow.
type Notifier struct {
	queue chan Event
	// AdminSinks / UserSinks resolve current delivery config per event (so
	// settings changes apply without restarts). Either may be nil.
	AdminSinks func(ctx context.Context) Sinks
	UserSinks  func(ctx context.Context, userID string) Sinks

	// safeClient refuses non-public destinations (SSRF guard) and is always
	// used for user-configured sinks. adminClient is the same safe client
	// unless GSBS_NOTIFY_ALLOW_PRIVATE is set, in which case operator-configured
	// admin sinks may reach LAN hosts (e.g. a self-hosted ntfy).
	safeClient  *http.Client
	adminClient *http.Client
}

// New creates a Notifier and starts its delivery worker (stops with ctx).
func New(ctx context.Context, adminSinks func(context.Context) Sinks, userSinks func(context.Context, string) Sinks) *Notifier {
	safe := netutil.SafeHTTPClient(10 * time.Second)
	admin := safe
	if allowPrivateSinks() {
		admin = &http.Client{Timeout: 10 * time.Second}
	}
	n := &Notifier{
		queue:       make(chan Event, 64),
		AdminSinks:  adminSinks,
		UserSinks:   userSinks,
		safeClient:  safe,
		adminClient: admin,
	}
	go n.worker(ctx)
	return n
}

// allowPrivateSinks reports whether operator-configured admin sinks may target
// private/loopback addresses (opt-in for self-hosters with a LAN webhook).
func allowPrivateSinks() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GSBS_NOTIFY_ALLOW_PRIVATE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Notify enqueues an event; on a full queue the oldest event is dropped so
// fresh information wins.
func (n *Notifier) Notify(ev Event) {
	if n == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	for {
		select {
		case n.queue <- ev:
			return
		default:
			select {
			case dropped := <-n.queue:
				logx.Logger().Warn().Str("component", "notify").Str("dropped", dropped.Type).
					Msg("notification queue full; dropped oldest")
			default:
			}
		}
	}
}

func (n *Notifier) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-n.queue:
			n.deliver(ctx, ev)
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, ev Event) {
	seen := map[string]bool{}
	sendAll := func(s Sinks, client *http.Client) {
		if s.empty() || !s.wants(ev.Type) {
			return
		}
		if s.WebhookURL != "" && !seen["w"+s.WebhookURL] {
			seen["w"+s.WebhookURL] = true
			n.send(ctx, client, "webhook", s.WebhookURL, ev, n.sendWebhook)
		}
		if s.DiscordURL != "" && !seen["d"+s.DiscordURL] {
			seen["d"+s.DiscordURL] = true
			n.send(ctx, client, "discord", s.DiscordURL, ev, n.sendDiscord)
		}
		if s.NtfyURL != "" && !seen["n"+s.NtfyURL] {
			seen["n"+s.NtfyURL] = true
			n.send(ctx, client, "ntfy", s.NtfyURL, ev, n.sendNtfy)
		}
	}
	// Admin sinks may use the private-allowed client (opt-in); user sinks are
	// always delivered through the SSRF-guarded safe client.
	if n.AdminSinks != nil {
		sendAll(n.AdminSinks(ctx), n.adminClient)
	}
	if ev.UserID != "" && n.UserSinks != nil {
		sendAll(n.UserSinks(ctx, ev.UserID), n.safeClient)
	}
}

func (n *Notifier) send(ctx context.Context, client *http.Client, kind, url string, ev Event, fn func(context.Context, *http.Client, string, Event) error) {
	err := retry.Do(ctx, retry.DefaultBackoff(), 3, func() error {
		return fn(ctx, client, url, ev)
	})
	if err != nil {
		logx.Logger().Warn().Str("component", "notify").Str("sink", kind).Str("event", ev.Type).Err(err).
			Msg("notification delivery failed")
	}
}

func (n *Notifier) sendWebhook(ctx context.Context, client *http.Client, url string, ev Event) error {
	payload, err := json.Marshal(map[string]string{
		"type":  ev.Type,
		"title": ev.Title,
		"body":  ev.Body,
		"at":    ev.At.Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return n.post(ctx, client, url, "application/json", payload, nil)
}

func (n *Notifier) sendDiscord(ctx context.Context, client *http.Client, url string, ev Event) error {
	content := "**" + ev.Title + "**"
	if ev.Body != "" {
		content += "\n" + ev.Body
	}
	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	return n.post(ctx, client, url, "application/json", payload, nil)
}

func (n *Notifier) sendNtfy(ctx context.Context, client *http.Client, url string, ev Event) error {
	return n.post(ctx, client, url, "text/plain", []byte(ev.Body), map[string]string{
		"X-Title": ev.Title,
		"X-Tags":  "video_game",
	})
}

func (n *Notifier) post(ctx context.Context, client *http.Client, url, contentType string, body []byte, headers map[string]string) error {
	// Belt-and-suspenders scheme guard; the safe client's dialer is the
	// authoritative SSRF defense at connect time.
	if u, perr := neturl.Parse(url); perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("notify: refusing non-http(s) sink URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("sink returned %d", resp.StatusCode)
	}
	return nil
}
