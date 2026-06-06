package sync

import (
	"log/slog"

	clientlogx "github.com/gsbs/gsbs/client/logx"
)

func syncLog() *slog.Logger {
	return clientlogx.Sync()
}

func logSync(op string, level slog.Level, attrs ...any) {
	args := append([]any{"op", op}, attrs...)
	switch level {
	case slog.LevelDebug:
		syncLog().Debug("sync", args...)
	case slog.LevelWarn:
		syncLog().Warn("sync", args...)
	case slog.LevelError:
		syncLog().Error("sync", args...)
	default:
		syncLog().Info("sync", args...)
	}
}

func logSyncInfo(op string, attrs ...any) {
	logSync(op, slog.LevelInfo, attrs...)
}

func logSyncDebug(op string, attrs ...any) {
	logSync(op, slog.LevelDebug, attrs...)
}

func logSyncWarn(op string, attrs ...any) {
	logSync(op, slog.LevelWarn, attrs...)
}

func logSyncError(op string, attrs ...any) {
	logSync(op, slog.LevelError, attrs...)
}
