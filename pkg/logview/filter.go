package logview

import (
	"net/http"
	"strconv"
	"strings"
)

// Query holds log viewer filter parameters from the UI.
type Query struct {
	Level          string
	Text           string
	Component      string
	HideHTTPNoise  bool
	Limit          int
	AutoRefresh    bool
	RefreshSeconds int
}

// ParseQuery reads log viewer filter parameters from an HTTP request.
func ParseQuery(r *http.Request) Query {
	q := Query{
		Level:          "all",
		Limit:          DefaultLimit,
		RefreshSeconds: DefaultRefresh,
	}
	if r == nil {
		return q
	}
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	switch level {
	case "", "all", "debug", "info", "warn", "error":
		if level != "" {
			q.Level = level
		}
	default:
		q.Level = "all"
	}
	q.Text = strings.TrimSpace(r.URL.Query().Get("q"))

	component := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("component")))
	switch component {
	case "", "all", "http", "pcgw", "job", "sse", "store", "cron", "migration", "webui", "api", "server", "sync", "auth", "tray", "client", "setup":
		if component == "" {
			q.Component = "all"
		} else {
			q.Component = component
		}
	default:
		q.Component = "all"
	}

	// Default: hide routine polling/static/health HTTP unless show_http=1.
	q.HideHTTPNoise = r.URL.Query().Get("show_http") != "1"

	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > MaxLimit {
				n = MaxLimit
			}
			q.Limit = n
		}
	}
	q.AutoRefresh = r.URL.Query().Get("auto") == "1"
	if raw := strings.TrimSpace(r.URL.Query().Get("refresh")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < MinRefresh {
				n = MinRefresh
			}
			if n > MaxRefresh {
				n = MaxRefresh
			}
			q.RefreshSeconds = n
		}
	}
	return q
}

// MatchComponent returns true when entry passes the component filter.
func MatchComponent(entry Entry, component string) bool {
	component = strings.ToLower(strings.TrimSpace(component))
	if component == "" || component == "all" {
		return true
	}
	return strings.EqualFold(entry.Component, component)
}

// IsRoutineHTTPNoise returns true for high-volume low-signal HTTP access lines.
func IsRoutineHTTPNoise(entry Entry) bool {
	if entry.Component != "http" && entry.Event != "http.request" {
		return false
	}
	path := entry.Path
	if path == "" {
		return false
	}
	switch {
	case path == "/api/health":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	case path == "/admin/partial/logs":
		return true
	case path == "/admin/partial/jobs":
		return true
	case path == "/dashboard/events":
		return true
	case strings.HasPrefix(path, "/favicon"):
		return true
	case strings.HasPrefix(path, "/client-logo"):
		return true
	}
	return false
}

// MatchQuery returns true when entry matches a free-text search (case-insensitive).
func MatchQuery(entry Entry, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	fields := []string{
		entry.Timestamp, entry.Level, entry.Message, entry.Event, entry.Component,
		entry.Summary, entry.Context, entry.Method, entry.Path, entry.Status,
		entry.Duration, entry.RequestID, entry.IP, entry.UserID, entry.Username,
		entry.GameID, entry.PathKey, entry.ClientID, entry.Error, entry.Raw,
	}
	for _, f := range fields {
		if f != "" && strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}
