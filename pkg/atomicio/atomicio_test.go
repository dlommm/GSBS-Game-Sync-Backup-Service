package atomicio

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The crash-safety primitive had no test file at all.
func TestWriteFileCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Fatalf("got %q", got)
	}

	if err := WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q", got)
	}
}

// perm is applied exactly, not filtered through the process umask.
func TestWriteFileAppliesExactPerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	old := syscallUmask(0o077) // a umask that would strip group/other bits
	defer syscallUmask(old)

	dir := t.TempDir()
	path := filepath.Join(dir, "shared.dat")
	if err := WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644 (umask must not apply)", fi.Mode().Perm())
	}
}

// No temp file may survive a successful write.
func TestWriteFileLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "a.json"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if IsTempName(e.Name()) {
			t.Errorf("temp file survived a successful write: %s", e.Name())
		}
	}
}

// Concurrent writers to one destination must not tear each other's temp file;
// the result must be one of the writers' payloads, never a mixture.
func TestWriteFileConcurrentWritersDoNotTear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	payloads := []string{strings.Repeat("a", 4096), strings.Repeat("b", 4096)}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = WriteFile(path, []byte(payloads[i%2]), 0o600)
		}(i)
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payloads[0] && string(got) != payloads[1] {
		t.Fatalf("torn write: got %d bytes that match neither payload", len(got))
	}
}

// A temp name must be recognizable, so the sync client's artifact filter can
// keep a half-written temp file from being uploaded as a save.
func TestIsTempName(t *testing.T) {
	if !IsTempName("config.json-12345" + TempSuffix) {
		t.Error("WriteFile's own temp name must be recognized")
	}
	if !strings.HasSuffix(TempSuffix, ".tmp") {
		t.Errorf("TempSuffix %q should still end in .tmp", TempSuffix)
	}
	for _, notTemp := range []string{"save.dat", "save.tmp", "notes.txt"} {
		if IsTempName(notTemp) {
			t.Errorf("%q must not be treated as a GSBS temp file", notTemp)
		}
	}
}

// A crash between CreateTemp and Rename leaves a temp file behind forever.
func TestSweepOrphans(t *testing.T) {
	dir := t.TempDir()

	stale := filepath.Join(dir, "config.json-999"+TempSuffix)
	if err := os.WriteFile(stale, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(dir, "config.json-1000"+TempSuffix)
	if err := os.WriteFile(fresh, []byte("in flight"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "config.json")
	if err := os.WriteFile(keep, []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}

	if n := SweepOrphans(dir, time.Hour); n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale temp file should have been removed")
	}
	for _, p := range []string{fresh, keep} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should have been left alone: %v", filepath.Base(p), err)
		}
	}

	// A missing directory is not an error.
	if n := SweepOrphans(filepath.Join(dir, "nope"), time.Hour); n != 0 {
		t.Errorf("missing dir returned %d", n)
	}
}
