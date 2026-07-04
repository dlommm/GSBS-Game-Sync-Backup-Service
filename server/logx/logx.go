package logx

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	closeMu   sync.Mutex
	logCloser io.Closer
)

const (
	// ServiceLogPathEnv is the preferred log file path override.
	ServiceLogPathEnv = "GSBS_SERVICE_LOG_PATH"
	// LegacyLogFileEnv is the legacy file logging override retained for compatibility.
	LegacyLogFileEnv = "GSBS_LOG_FILE"
)

// Init configures global structured logging from GSBS_LOG_LEVEL (debug, info, warn, error).
func Init() {
	initWithWriter(os.Stdout, nil)
}

// ConfiguredLogFilePath returns the configured log file path from environment
// variables in precedence order.
func ConfiguredLogFilePath() (path, sourceEnv string) {
	if v := strings.TrimSpace(os.Getenv(ServiceLogPathEnv)); v != "" {
		return filepath.Clean(v), ServiceLogPathEnv
	}
	if v := strings.TrimSpace(os.Getenv(LegacyLogFileEnv)); v != "" {
		return filepath.Clean(v), LegacyLogFileEnv
	}
	return "", ""
}

// InitFile configures logging to write JSON logs to the given file path with
// size-based rotation (defaults 20 MiB / 3 backups; GSBS_LOG_MAX_BYTES and
// GSBS_LOG_MAX_BACKUPS override, GSBS_LOG_MAX_BYTES=0 restores the legacy
// unbounded append).
func InitFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return os.ErrInvalid
	}
	clean := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	if maxBytes, backups, rotate := rotationConfigFromEnv(); rotate {
		w, err := newRotatingWriter(clean, maxBytes, backups)
		if err != nil {
			return err
		}
		initWithWriter(w, w)
		return nil
	}
	f, err := os.OpenFile(clean, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	initWithWriter(f, f)
	return nil
}

// Close releases file handles used by InitFile.
func Close() error {
	closeMu.Lock()
	defer closeMu.Unlock()
	if logCloser == nil {
		return nil
	}
	err := logCloser.Close()
	logCloser = nil
	return err
}

func initWithWriter(w io.Writer, closer io.Closer) {
	level := zerolog.InfoLevel
	switch strings.ToLower(os.Getenv("GSBS_LOG_LEVEL")) {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(w).With().Timestamp().Logger().Level(level)
	closeMu.Lock()
	defer closeMu.Unlock()
	if logCloser != nil {
		_ = logCloser.Close()
	}
	logCloser = closer
}

// Logger returns a pointer to the global zerolog logger (required for chained methods).
func Logger() *zerolog.Logger {
	return &log.Logger
}
