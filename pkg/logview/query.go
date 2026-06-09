package logview

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	DefaultLimit    = 200
	MaxLimit        = 1000
	MaxReadBytes    = 4 << 20
	DefaultRefresh  = 5
	MinRefresh      = 2
	MaxRefresh      = 30
)

// ParseQuery reads log viewer filter parameters from an HTTP request.
func ParseQuery(r *http.Request) (level, query string, limit int, autoRefresh bool, refreshSeconds int) {
	level = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	switch level {
	case "", "all", "debug", "info", "warn", "error":
		if level == "" {
			level = "all"
		}
	default:
		level = "all"
	}
	query = strings.TrimSpace(r.URL.Query().Get("q"))
	limit = DefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > MaxLimit {
				n = MaxLimit
			}
			limit = n
		}
	}
	autoRefresh = r.URL.Query().Get("auto") == "1"
	refreshSeconds = DefaultRefresh
	if raw := strings.TrimSpace(r.URL.Query().Get("refresh")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < MinRefresh {
				n = MinRefresh
			}
			if n > MaxRefresh {
				n = MaxRefresh
			}
			refreshSeconds = n
		}
	}
	return level, query, limit, autoRefresh, refreshSeconds
}

// MatchQuery returns true when q matches searchable fields on entry (case-insensitive).
func MatchQuery(entry Entry, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		entry.Raw,
		entry.Summary,
		entry.Context,
		entry.Event,
		entry.Method,
		entry.Path,
		entry.Error,
		entry.RequestID,
		entry.GameID,
		entry.PathKey,
	}, " "))
	return strings.Contains(haystack, q)
}
