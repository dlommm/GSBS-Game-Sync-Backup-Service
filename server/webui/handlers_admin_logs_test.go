package webui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/logview"
)

func TestParseAdminLogQueryDefaultsAndBounds(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/logs", nil)
	q := logview.ParseQuery(r)
	if q.Level != "all" {
		t.Fatalf("level=%q want all", q.Level)
	}
	if q.Text != "" {
		t.Fatalf("text=%q want empty", q.Text)
	}
	if q.Component != "all" {
		t.Fatalf("component=%q want all", q.Component)
	}
	if !q.HideHTTPNoise {
		t.Fatalf("HideHTTPNoise should default true")
	}
	if q.Limit != logview.DefaultLimit {
		t.Fatalf("limit=%d want %d", q.Limit, logview.DefaultLimit)
	}
	if q.AutoRefresh {
		t.Fatalf("auto should be false by default")
	}
	if q.RefreshSeconds != logview.DefaultRefresh {
		t.Fatalf("refresh=%d want %d", q.RefreshSeconds, logview.DefaultRefresh)
	}

	r = httptest.NewRequest("GET", "/admin/logs?level=error&limit=9000&auto=1&refresh=1&q=panic&component=pcgw&show_http=1", nil)
	q = logview.ParseQuery(r)
	if q.Level != "error" || q.Text != "panic" || q.Component != "pcgw" {
		t.Fatalf("parsed unexpected filters: %+v", q)
	}
	if q.HideHTTPNoise {
		t.Fatalf("show_http=1 should disable HideHTTPNoise")
	}
	if q.Limit != logview.MaxLimit {
		t.Fatalf("limit=%d want %d", q.Limit, logview.MaxLimit)
	}
	if !q.AutoRefresh {
		t.Fatalf("auto should be true")
	}
	if q.RefreshSeconds != logview.MinRefresh {
		t.Fatalf("refresh=%d want %d", q.RefreshSeconds, logview.MinRefresh)
	}
}

func TestParseAdminLogLineJSONAndRaw(t *testing.T) {
	entry := logview.ParseZerologLine(`{"level":"warning","time":"2026-01-01T00:00:00Z","message":"slow request"}`)
	if entry.Level != "warn" {
		t.Fatalf("level=%q want warn", entry.Level)
	}
	if entry.Component != "server" {
		t.Fatalf("component=%q want server", entry.Component)
	}
	if entry.Timestamp == "" || entry.Summary != "slow request" || entry.Message != "slow request" {
		t.Fatalf("unexpected parsed entry: %+v", entry)
	}

	raw := logview.ParseZerologLine("plain log line")
	if raw.Level != "raw" || raw.Message != "plain log line" || raw.Summary != "plain log line" {
		t.Fatalf("unexpected raw entry: %+v", raw)
	}
}

func TestParseAdminLogLineHTTPRequest(t *testing.T) {
	line := `{"level":"info","time":"2026-06-09T12:00:00Z","event":"http.request","component":"http","message":"GET /api/manifest 200","request_id":"req-42","method":"GET","path":"/api/manifest","status":200,"ip":"10.0.0.5","duration":12,"user_id":"user-9"}`
	entry := logview.ParseZerologLine(line)
	if entry.Event != "http.request" {
		t.Fatalf("event=%q want http.request", entry.Event)
	}
	if entry.Component != "http" {
		t.Fatalf("component=%q want http", entry.Component)
	}
	wantSummary := "GET /api/manifest → 200 in 12ms from 10.0.0.5"
	if entry.Summary != wantSummary {
		t.Fatalf("summary=%q want %q", entry.Summary, wantSummary)
	}
	if entry.Message != wantSummary {
		t.Fatalf("message=%q want summary", entry.Message)
	}
	for _, pair := range []struct{ got, want string }{
		{entry.RequestID, "req-42"},
		{entry.Method, "GET"},
		{entry.Path, "/api/manifest"},
		{entry.Status, "200"},
		{entry.Duration, "12ms"},
		{entry.IP, "10.0.0.5"},
		{entry.UserID, "user-9"},
	} {
		if pair.got != pair.want {
			t.Fatalf("field got %q want %q", pair.got, pair.want)
		}
	}
	if !strings.Contains(entry.Context, "component=http") || !strings.Contains(entry.Context, "request_id=req-42") {
		t.Fatalf("context=%q missing expected fields", entry.Context)
	}
}

func TestParseAdminLogLinePCGWComponent(t *testing.T) {
	line := `{"level":"info","component":"pcgw","message":"pcgw sync: phase2 batch","run_id":"run-1","queue":120,"ok":10,"partial":2,"failed":1}`
	entry := logview.ParseZerologLine(line)
	if entry.Component != "pcgw" {
		t.Fatalf("component=%q want pcgw", entry.Component)
	}
	if entry.Event != "pcgw.sync" {
		t.Fatalf("event=%q want pcgw.sync", entry.Event)
	}
	if !strings.Contains(entry.Summary, "queue=120") || !strings.Contains(entry.Summary, "ok=10") {
		t.Fatalf("summary=%q missing domain fields", entry.Summary)
	}
}

func TestParseAdminLogLineRateLimit(t *testing.T) {
	entry := logview.ParseZerologLine(`{"level":"info","message":"rate limit: default","key":"GSBS_AUTH_RATE_LIMIT","limit":30,"window":60000}`)
	want := "rate limit: default (key=GSBS_AUTH_RATE_LIMIT, limit=30, window=60000)"
	if entry.Summary != want {
		t.Fatalf("summary=%q want %q", entry.Summary, want)
	}
	if entry.Event != "rate.limit.default" {
		t.Fatalf("event=%q want rate.limit.default", entry.Event)
	}
}

func TestParseAdminLogLineErrorField(t *testing.T) {
	entry := logview.ParseZerologLine(`{"level":"error","message":"api list clients failed","user_id":"u1","error":"database locked"}`)
	want := "api list clients failed: database locked"
	if entry.Summary != want {
		t.Fatalf("summary=%q want %q", entry.Summary, want)
	}
	if entry.Error != "database locked" {
		t.Fatalf("error=%q want database locked", entry.Error)
	}
	if !strings.Contains(entry.Context, "error=database locked") {
		t.Fatalf("context=%q missing error", entry.Context)
	}
}

func TestLoadAdminLogEntriesSearchAndFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	lines := []string{
		`{"level":"info","event":"http.request","component":"http","message":"GET /api/manifest 200","method":"GET","path":"/api/manifest","status":200,"duration":8,"ip":"127.0.0.1","request_id":"hidden-id"}`,
		`{"level":"info","event":"http.request","component":"http","message":"GET /api/health 200","method":"GET","path":"/api/health","status":200,"duration":0,"ip":"127.0.0.1"}`,
		`{"level":"info","component":"pcgw","message":"pcgw sync: started","run_id":"run-9"}`,
		`{"level":"warn","message":"rate limit exceeded","key":"10.0.0.1","limit":"auth","path":"/api/login"}`,
		`{"level":"error","message":"store open failed","error":"permission denied"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	byPath, err := loadAdminLogEntries(path, logview.Query{Level: "all", Text: "/api/manifest", Limit: 10, Component: "all", HideHTTPNoise: false})
	if err != nil {
		t.Fatalf("load by path: %v", err)
	}
	if len(byPath) != 1 {
		t.Fatalf("path search len=%d want 1", len(byPath))
	}
	if byPath[0].Summary != "GET /api/manifest → 200 in 8ms from 127.0.0.1" {
		t.Fatalf("unexpected summary: %q", byPath[0].Summary)
	}

	hidden, err := loadAdminLogEntries(path, logview.Query{Level: "all", Text: "hidden-id", Limit: 10, Component: "all", HideHTTPNoise: true})
	if err != nil {
		t.Fatalf("load by request_id: %v", err)
	}
	if len(hidden) != 1 {
		t.Fatalf("request_id search len=%d want 1", len(hidden))
	}

	pcgwOnly, err := loadAdminLogEntries(path, logview.Query{Level: "all", Limit: 10, Component: "pcgw", HideHTTPNoise: true})
	if err != nil {
		t.Fatalf("load pcgw: %v", err)
	}
	if len(pcgwOnly) != 1 || pcgwOnly[0].Component != "pcgw" {
		t.Fatalf("pcgw filter unexpected: %+v", pcgwOnly)
	}

	noNoise, err := loadAdminLogEntries(path, logview.Query{Level: "all", Limit: 10, Component: "all", HideHTTPNoise: true})
	if err != nil {
		t.Fatalf("load no noise: %v", err)
	}
	for _, e := range noNoise {
		if e.Path == "/api/health" {
			t.Fatalf("health check should be filtered: %+v", e)
		}
	}
}

func TestResolveAdminLogSourceForPrecedenceAndFallback(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "GSBS_SERVICE_LOG_PATH":
			return "/tmp/service.log"
		case "GSBS_LOG_FILE":
			return "/tmp/legacy.log"
		case "ProgramData":
			return `C:\ProgramData`
		default:
			return ""
		}
	}
	statFn := func(path string) (os.FileInfo, error) {
		if filepath.Clean(path) == filepath.Clean("/tmp/legacy.log") {
			return fakeFileInfo{name: "legacy.log"}, nil
		}
		return nil, os.ErrNotExist
	}
	path, info, present := resolveAdminLogSourceFor("windows", getenv, statFn)
	if !present {
		t.Fatalf("present=%v want true", present)
	}
	if filepath.Clean(path) != filepath.Clean("/tmp/legacy.log") {
		t.Fatalf("path=%q want %q", path, "/tmp/legacy.log")
	}
	if !strings.Contains(info, "GSBS_LOG_FILE") {
		t.Fatalf("info=%q want GSBS_LOG_FILE source", info)
	}
}

func TestResolveAdminLogSourceForMissingIncludesAttemptedPaths(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "GSBS_SERVICE_LOG_PATH":
			return "/tmp/service.log"
		case "GSBS_LOG_FILE":
			return "/tmp/legacy.log"
		default:
			return ""
		}
	}
	statFn := func(path string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	path, info, present := resolveAdminLogSourceFor("linux", getenv, statFn)
	if present {
		t.Fatalf("present=%v want false", present)
	}
	if filepath.Clean(path) != filepath.Clean("/tmp/service.log") {
		t.Fatalf("path=%q want first attempted path", path)
	}
	infoNorm := strings.ReplaceAll(info, "\\", "/")
	if !strings.Contains(infoNorm, "/tmp/service.log") || !strings.Contains(infoNorm, "/tmp/legacy.log") {
		t.Fatalf("info=%q must include attempted paths", info)
	}
	if !strings.Contains(info, "GSBS_SERVICE_LOG_PATH") || !strings.Contains(info, "GSBS_LOG_FILE") {
		t.Fatalf("info=%q must include env guidance", info)
	}
}

type fakeFileInfo struct {
	name string
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() interface{}   { return nil }
