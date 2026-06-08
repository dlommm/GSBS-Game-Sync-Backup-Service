package webui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if !strings.Contains(info, "/tmp/service.log") || !strings.Contains(info, "/tmp/legacy.log") {
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
