//go:build linux

package gamewatch

import (
	"os"
	"path/filepath"
	"testing"
)

// The linux detector parses a /proc-shaped tree: numeric dirs with exe
// symlinks; non-numeric entries and unreadable processes are skipped.
func TestProcDetector_FakeProcTree(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "games", "mygame", "bin", "game")
	if err := os.MkdirAll(filepath.Dir(game), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(game, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	mk := func(pid, target string) {
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if target != "" {
			if err := os.Symlink(target, filepath.Join(dir, "exe")); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("123", game)
	mk("456", "") // kernel thread / unreadable: no exe link
	if err := os.MkdirAll(filepath.Join(root, "not-a-pid"), 0o755); err != nil {
		t.Fatal(err)
	}

	d := &procDetector{root: root}
	procs, err := d.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 || procs[0].PID != 123 || procs[0].ExePath != game {
		t.Fatalf("procs = %+v, want single PID 123 -> %s", procs, game)
	}

	// The real /proc must also parse without error on linux.
	real := &procDetector{root: "/proc"}
	if _, err := real.Snapshot(); err != nil {
		t.Fatalf("real /proc snapshot: %v", err)
	}
}
