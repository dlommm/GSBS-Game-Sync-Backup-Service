package sync

import (
	"github.com/gsbs/gsbs/pkg/paths"
)

// PullOptions configures pull behavior including conflict policy and install-aware eligibility.
type PullOptions struct {
	BackupBeforeOverwrite bool
	ConflictPolicy        string // last_write_wins, keep_local, keep_server
	PullContext           paths.PullContext
	// WatchRoot returns the resolved watch directory for a save slot (path safety on pull).
	WatchRoot func(gameID, pathKey string) string
	// SkipGame, when set, defers pulls for a game (it is currently running —
	// overwriting a live save file mid-session would corrupt the play session).
	SkipGame func(gameID string) bool
	// PolicyFor, when set, returns a per-game conflict policy; empty falls
	// back to ConflictPolicy.
	PolicyFor func(gameID string) string
}

// policyFor resolves the effective conflict policy for one game.
func (o PullOptions) policyFor(gameID string) string {
	if o.PolicyFor != nil {
		if p := o.PolicyFor(gameID); p != "" {
			return p
		}
	}
	return o.ConflictPolicy
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
