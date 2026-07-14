package sync

import "time"

// PullDecision is the result of conflict resolution logic for a pull.
type PullDecision int

const (
	PullApply PullDecision = iota
	PullSkip
	PullConflict
)

// DefaultSkewTolerance is the window within which the local file mtime and
// the server's updated_at count as "simultaneous". The two timestamps come
// from different clocks (this machine's filesystem vs the server), so
// ordering inside the window is meaningless — deciding a winner from it
// would let whichever clock runs fast silently clobber the other side.
const DefaultSkewTolerance = 2 * time.Minute

// DecidePull determines whether to apply, skip, or record a conflict for a
// save pull, using the default clock-skew tolerance.
func DecidePull(localExists bool, localHash string, localMtime time.Time, serverHash string, serverTime time.Time, policy string) PullDecision {
	return DecidePullSkew(localExists, localHash, localMtime, serverHash, serverTime, policy, DefaultSkewTolerance)
}

// DecidePullSkew is DecidePull with an explicit skew tolerance. Inside the
// tolerance window (content differs, timestamps ambiguous):
//   - last_write_wins surfaces a conflict — it must never silently pick a
//     side based on clock noise;
//   - keep_local skips (no local data loss; the server copy still exists);
//   - keep_server applies (the user explicitly chose the server as truth).
//
// Outside the window keep_server and last_write_wins apply the classic time
// comparison. keep_local NEVER overwrites an existing local file — a
// definitively newer server copy surfaces as a conflict instead, keeping both
// versions alive (local on disk, server's retained server-side) until the
// user picks one.
func DecidePullSkew(localExists bool, localHash string, localMtime time.Time, serverHash string, serverTime time.Time, policy string, skewTolerance time.Duration) PullDecision {
	if !localExists {
		return PullApply
	}
	if serverHash != "" && localHash == serverHash {
		return PullSkip
	}
	// An empty serverHash (legacy row without a hash) must not blind-apply
	// over an existing local file; fall through to the policy comparison.
	delta := localMtime.Sub(serverTime)
	if delta < 0 {
		delta = -delta
	}
	withinWindow := skewTolerance > 0 && delta <= skewTolerance
	switch policy {
	case "keep_local":
		if withinWindow || localMtime.After(serverTime) {
			return PullSkip
		}
		return PullConflict
	case "keep_server":
		if withinWindow || serverTime.After(localMtime) || serverTime.Equal(localMtime) {
			return PullApply
		}
		return PullConflict
	default: // last_write_wins
		if withinWindow {
			return PullConflict
		}
		if localMtime.After(serverTime) {
			return PullSkip
		}
		return PullApply
	}
}
