package webui

import (
	"net/http/httptest"
	"testing"
)

func TestParseAdminLogQueryDefaultsAndBounds(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/logs", nil)
	level, query, limit, auto, refresh := parseAdminLogQuery(r)
	if level != "all" {
		t.Fatalf("level=%q want all", level)
	}
	if query != "" {
		t.Fatalf("query=%q want empty", query)
	}
	if limit != defaultAdminLogLimit {
		t.Fatalf("limit=%d want %d", limit, defaultAdminLogLimit)
	}
	if auto {
		t.Fatalf("auto should be false by default")
	}
	if refresh != defaultAdminRefreshSecond {
		t.Fatalf("refresh=%d want %d", refresh, defaultAdminRefreshSecond)
	}

	r = httptest.NewRequest("GET", "/admin/logs?level=error&limit=9000&auto=1&refresh=1&q=panic", nil)
	level, query, limit, auto, refresh = parseAdminLogQuery(r)
	if level != "error" || query != "panic" {
		t.Fatalf("parsed unexpected level/query: %q / %q", level, query)
	}
	if limit != maxAdminLogLimit {
		t.Fatalf("limit=%d want %d", limit, maxAdminLogLimit)
	}
	if !auto {
		t.Fatalf("auto should be true")
	}
	if refresh != 2 {
		t.Fatalf("refresh=%d want 2", refresh)
	}
}

func TestParseAdminLogLineJSONAndRaw(t *testing.T) {
	entry := parseAdminLogLine(`{"level":"warning","time":"2026-01-01T00:00:00Z","message":"slow request"}`)
	if entry.Level != "warn" {
		t.Fatalf("level=%q want warn", entry.Level)
	}
	if entry.Timestamp == "" || entry.Message != "slow request" {
		t.Fatalf("unexpected parsed entry: %+v", entry)
	}

	raw := parseAdminLogLine("plain log line")
	if raw.Level != "raw" || raw.Message != "plain log line" {
		t.Fatalf("unexpected raw entry: %+v", raw)
	}
}
