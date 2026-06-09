package logview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClientLineSlogSyncOp(t *testing.T) {
	line := `time=2026-06-09T14:30:00.123Z level=INFO msg="sync" component=sync op=push_ok game_id=730 path_key=steamuserdata`
	entry := ParseClientLine(line)
	if entry.Level != "info" {
		t.Fatalf("level=%q want info", entry.Level)
	}
	if entry.Event != "push_ok" {
		t.Fatalf("event=%q want push_ok", entry.Event)
	}
	if entry.GameID != "730" || entry.PathKey != "steamuserdata" {
		t.Fatalf("game/path_key: %+v", entry)
	}
	want := "push_ok game=730 path_key=steamuserdata"
	if entry.Summary != want {
		t.Fatalf("summary=%q want %q", entry.Summary, want)
	}
	if !strings.Contains(entry.Context, "component=sync") {
		t.Fatalf("context=%q missing component", entry.Context)
	}
}

func TestParseClientLineSlogError(t *testing.T) {
	line := `time=2026-06-09T14:31:00Z level=ERROR msg="sync" component=sync op=push_fail game_id=440 error="connection refused"`
	entry := ParseClientLine(line)
	if entry.Level != "error" {
		t.Fatalf("level=%q want error", entry.Level)
	}
	if entry.Error != "connection refused" {
		t.Fatalf("error=%q", entry.Error)
	}
	if !strings.Contains(entry.Summary, "connection refused") {
		t.Fatalf("summary=%q should include error", entry.Summary)
	}
}

func TestParseClientLinePlainLogPrintf(t *testing.T) {
	line := `2026/06/09 14:30:00 client sync: starting server=https://gsbs.example`
	entry := ParseClientLine(line)
	if entry.Level != "info" {
		t.Fatalf("level=%q want info", entry.Level)
	}
	if entry.Event != "client.sync" {
		t.Fatalf("event=%q want client.sync", entry.Event)
	}
	if entry.Timestamp != "2026/06/09 14:30:00" {
		t.Fatalf("timestamp=%q", entry.Timestamp)
	}
}

func TestParseClientLinePlainWarning(t *testing.T) {
	line := `2026/06/09 14:30:00 WARNING: server_url uses plain HTTP on a non-local host`
	entry := ParseClientLine(line)
	if entry.Level != "warn" {
		t.Fatalf("level=%q want warn", entry.Level)
	}
}

func TestParseClientLinePlainFailed(t *testing.T) {
	line := `2026/06/09 14:30:00 client login: failed server=https://x username="u": token invalid`
	entry := ParseClientLine(line)
	if entry.Level != "error" {
		t.Fatalf("level=%q want error", entry.Level)
	}
	if entry.Event != "client.login" {
		t.Fatalf("event=%q want client.login", entry.Event)
	}
}

func TestLoadEntriesClientLog(t *testing.T) {
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

	byGame, err := LoadEntries(path, "all", "730", 10, ParseClientLine)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(byGame) != 1 || byGame[0].GameID != "730" {
		t.Fatalf("game search: %+v", byGame)
	}

	errOnly, err := LoadEntries(path, "error", "", 10, ParseClientLine)
	if err != nil {
		t.Fatalf("load error level: %v", err)
	}
	if len(errOnly) != 1 || errOnly[0].Level != "error" {
		t.Fatalf("error filter: %+v", errOnly)
	}
}
