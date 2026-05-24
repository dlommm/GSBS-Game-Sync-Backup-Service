package logx

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init configures global structured logging from GSBS_LOG_LEVEL (debug, info, warn, error).
func Init() {
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
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger().Level(level)
}

// Logger returns a pointer to the global zerolog logger (required for chained methods).
func Logger() *zerolog.Logger {
	return &log.Logger
}
