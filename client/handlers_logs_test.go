package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/pkg/logview"
)

func TestLoadClientLogEntriesFromTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gsbs.log")
	lines := []string{
		`2026/06/09 14:30:00 client sync: starting server=https://gsbs.example`,
		`time=2026-06-09T14:30:01Z level=INFO msg="sync" component=sync op=push_ok game_id=730 path_key=save1`,
		`time=2026-06-09T14:30:02Z level=ERROR msg="sync" component=sync op=push_fail game_id=440 error="timeout"`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	entries, err := loadClientLogEntries(path, "all", "push_ok", 10)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != "push_ok" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	errEntries, err := loadClientLogEntries(path, "error", "", 10)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(errEntries) != 1 {
		t.Fatalf("error entries len=%d want 1", len(errEntries))
	}
}

func TestClientLogQueryParsing(t *testing.T) {
	r := httptest.NewRequest("GET", "/logs?level=warn&limit=500&q=tray&auto=1&refresh=10", nil)
	level, query, limit, auto, refresh := logview.ParseQuery(r)
	if level != "warn" || query != "tray" || limit != 500 || !auto || refresh != 10 {
		t.Fatalf("parsed %q %q %d %v %d", level, query, limit, auto, refresh)
	}
}
