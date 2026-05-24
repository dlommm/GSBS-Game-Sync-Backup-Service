package sync

import (
	"github.com/gsbs/gsbs/pkg/paths"
)

// PullOptions configures pull behavior including conflict policy and install-aware eligibility.
type PullOptions struct {
	BackupBeforeOverwrite bool
	ConflictPolicy        string // last_write_wins, keep_local, keep_server
	PullContext           paths.PullContext
}

// DefaultPullOptions returns sensible defaults.
func DefaultPullOptions() PullOptions {
	return PullOptions{
		ConflictPolicy: "last_write_wins",
		PullContext:    paths.PullContext{LegacyMode: true},
	}
}

// OnConflictDetected is called when both local and server changed. Tray may notify.
var OnConflictDetected func(gameID, pathKey, filePath string)
