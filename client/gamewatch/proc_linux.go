//go:build linux

package gamewatch

import (
	"os"
	"path/filepath"
	"strconv"
)

// NewDetector returns the /proc-based process detector.
func NewDetector() Detector {
	return &procDetector{root: "/proc"}
}

// procDetector reads /proc: numeric directories are PIDs, and the exe
// symlink resolves to the running executable. Readlink fails with EACCES for
// other users' processes — irrelevant here, games run as the same user.
type procDetector struct {
	root string // parameterized for tests
}

func (d *procDetector) Snapshot() ([]ProcessInfo, error) {
	entries, err := os.ReadDir(d.root)
	if err != nil {
		return nil, err
	}
	out := make([]ProcessInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(filepath.Join(d.root, e.Name(), "exe"))
		if err != nil || exe == "" {
			continue // kernel thread, permission denied, or exited
		}
		// A deleted-but-running executable reads as "/path (deleted)".
		if n := len(exe); n > 10 && exe[n-10:] == " (deleted)" {
			exe = exe[:n-10]
		}
		out = append(out, ProcessInfo{PID: pid, ExePath: exe})
	}
	return out, nil
}
