package logview

import (
	"net/http/httptest"
	"testing"
)

func TestParseQueryHideHTTPDefault(t *testing.T) {
	q := ParseQuery(httptest.NewRequest("GET", "/admin/logs", nil))
	if !q.HideHTTPNoise {
		t.Fatalf("HideHTTPNoise should default true")
	}
	q = ParseQuery(httptest.NewRequest("GET", "/admin/logs?show_http=1", nil))
	if q.HideHTTPNoise {
		t.Fatalf("show_http=1 should show routine HTTP")
	}
}

func TestMatchComponent(t *testing.T) {
	entry := Entry{Component: "pcgw"}
	if !MatchComponent(entry, "pcgw") {
		t.Fatal("expected pcgw match")
	}
	if MatchComponent(entry, "job") {
		t.Fatal("expected no match for job")
	}
	if !MatchComponent(entry, "all") {
		t.Fatal("all should match everything")
	}
}

func TestIsRoutineHTTPNoise(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/health", true},
		{"/admin/partial/logs", true},
		{"/dashboard/events", true},
		{"/static/app.css", true},
		{"/admin/pcgw/sync", false},
		{"/api/manifest", false},
	}
	for _, tc := range cases {
		entry := Entry{Component: "http", Event: "http.request", Path: tc.path}
		if got := IsRoutineHTTPNoise(entry); got != tc.want {
			t.Fatalf("path %q: got %v want %v", tc.path, got, tc.want)
		}
	}
}
