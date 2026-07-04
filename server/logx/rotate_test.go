package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter_RotatesAndKeepsBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gsbs.log")

	w, err := newRotatingWriter(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 9; i++ {               // 360 bytes total -> 3 rotations
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Live file plus at most 2 backups; backup 3 must have fallen off.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("live log: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("backup .1: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatalf("backup .2: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("backup .3 should not exist, err=%v", err)
	}

	// Every retained file stays within the size cap.
	for _, p := range []string{path, path + ".1", path + ".2"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() > 100 {
			t.Errorf("%s size %d exceeds cap", p, fi.Size())
		}
	}
}

func TestRotatingWriter_ResumesExistingSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gsbs.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("y", 90)), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := newRotatingWriter(path, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// 90 existing + 20 new crosses the cap -> rotation happens first.
	if _, err := w.Write([]byte(strings.Repeat("z", 20))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.Repeat("z", 20) {
		t.Fatalf("live file = %q, want fresh writes only", got)
	}
	old, err := os.ReadFile(path + ".1")
	if err != nil || len(old) != 90 {
		t.Fatalf("backup should hold the previous 90 bytes, len=%d err=%v", len(old), err)
	}
}
