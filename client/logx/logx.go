package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	mu     sync.RWMutex
	logger *slog.Logger
)

// Init configures structured logging from GSBS_LOG_LEVEL (debug, info, warn, error).
// Output goes to w (typically the rotated client log file).
func Init(w io.Writer) {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("GSBS_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	mu.Lock()
	logger = slog.New(h)
	mu.Unlock()
}

// Logger returns the global slog logger, or a default if Init was not called.
func Logger() *slog.Logger {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		return l
	}
	return slog.Default()
}
