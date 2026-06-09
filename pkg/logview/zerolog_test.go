package logview

import (
	"strings"
	"testing"
)

func TestParseZerologLineJSONAndRaw(t *testing.T) {
	entry := ParseZerologLine(`{"level":"warning","time":"2026-01-01T00:00:00Z","message":"slow request"}`)
	if entry.Level != "warn" {
		t.Fatalf("level=%q want warn", entry.Level)
	}
	if entry.Timestamp == "" || entry.Summary != "slow request" || entry.Message != "slow request" {
		t.Fatalf("unexpected parsed entry: %+v", entry)
	}

	raw := ParseZerologLine("plain log line")
	if raw.Level != "raw" || raw.Message != "plain log line" || raw.Summary != "plain log line" {
		t.Fatalf("unexpected raw entry: %+v", raw)
	}
}

func TestParseZerologLineHTTPRequest(t *testing.T) {
	line := `{"level":"info","time":"2026-06-09T12:00:00Z","event":"http.request","message":"GET /api/manifest 200","request_id":"req-42","method":"GET","path":"/api/manifest","status":200,"ip":"10.0.0.5","duration":12,"user_id":"user-9"}`
	entry := ParseZerologLine(line)
	if entry.Event != "http.request" {
		t.Fatalf("event=%q want http.request", entry.Event)
	}
	wantSummary := "GET /api/manifest → 200 in 12ms from 10.0.0.5"
	if entry.Summary != wantSummary {
		t.Fatalf("summary=%q want %q", entry.Summary, wantSummary)
	}
	if !strings.Contains(entry.Context, "request_id=req-42") || !strings.Contains(entry.Context, "user_id=user-9") {
		t.Fatalf("context=%q missing expected fields", entry.Context)
	}
}

func TestParseZerologLineRateLimit(t *testing.T) {
	entry := ParseZerologLine(`{"level":"info","message":"rate limit: default","key":"GSBS_AUTH_RATE_LIMIT","limit":30,"window":60000}`)
	want := "rate limit: default (key=GSBS_AUTH_RATE_LIMIT, limit=30, window=60000)"
	if entry.Summary != want {
		t.Fatalf("summary=%q want %q", entry.Summary, want)
	}
}

func TestParseZerologLineErrorField(t *testing.T) {
	entry := ParseZerologLine(`{"level":"error","message":"api list clients failed","user_id":"u1","error":"database locked"}`)
	want := "api list clients failed: database locked"
	if entry.Summary != want {
		t.Fatalf("summary=%q want %q", entry.Summary, want)
	}
}
