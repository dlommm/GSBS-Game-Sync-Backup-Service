package logx

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultLogMaxBytes   = 20 << 20 // 20 MiB per file before rotation
	defaultLogMaxBackups = 3        // path.1 .. path.N kept after rotation
)

// rotatingWriter is a size-based rotating file writer: when a write would
// cross maxBytes the current file is closed, existing backups shift
// (path.1 -> path.2, ...), the live file becomes path.1, and a fresh file is
// opened at path. Writes are serialized by a mutex; rotation errors fall back
// to continuing on the current handle so logging never stops the server.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	f        *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64, backups int) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	size := int64(0)
	if fi, statErr := f.Stat(); statErr == nil {
		size = fi.Size()
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, backups: backups, f: f, size: size}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.maxBytes && w.size > 0 {
		w.rotateLocked()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotateLocked() {
	_ = w.f.Close()
	// Shift path.(N-1) -> path.N, ..., path -> path.1. Oldest falls off.
	for i := w.backups - 1; i >= 1; i-- {
		_ = os.Rename(backupName(w.path, i), backupName(w.path, i+1))
	}
	if w.backups >= 1 {
		_ = os.Rename(w.path, backupName(w.path, 1))
	} else {
		_ = os.Remove(w.path)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		// Last resort: reopen the (renamed or original) path in append mode
		// so log output continues somewhere rather than being dropped.
		f, err = os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
	}
	w.f = f
	w.size = 0
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

func backupName(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

// rotationConfigFromEnv returns (maxBytes, backups). GSBS_LOG_MAX_BYTES=0
// disables rotation (legacy unbounded append); invalid values use defaults.
func rotationConfigFromEnv() (int64, int, bool) {
	maxBytes := int64(defaultLogMaxBytes)
	backups := defaultLogMaxBackups
	if v := strings.TrimSpace(os.Getenv("GSBS_LOG_MAX_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			if n == 0 {
				return 0, 0, false
			}
			maxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GSBS_LOG_MAX_BACKUPS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			backups = n
		}
	}
	return maxBytes, backups, true
}
