package sync

import "time"

// PullDecision is the result of conflict resolution logic for a pull.
type PullDecision int

const (
	PullApply PullDecision = iota
	PullSkip
	PullConflict
)

// DecidePull determines whether to apply, skip, or record a conflict for a save pull.
func DecidePull(localExists bool, localHash string, localMtime time.Time, serverHash string, serverTime time.Time, policy string) PullDecision {
	if !localExists {
		return PullApply
	}
	if serverHash != "" && localHash == serverHash {
		return PullSkip
	}
	// An empty serverHash (legacy row without a hash) must not blind-apply
	// over an existing local file; fall through to the policy comparison.
	switch policy {
	case "keep_local":
		if localMtime.After(serverTime) {
			return PullSkip
		}
		return PullApply
	case "keep_server":
		if serverTime.After(localMtime) || serverTime.Equal(localMtime) {
			return PullApply
		}
		return PullConflict
	default:
		if localMtime.After(serverTime) {
			return PullSkip
		}
		if localMtime.Before(serverTime) {
			return PullApply
		}
		return PullConflict
	}
}
