package api

import (
	"sync"
	"time"
)

// lastSeenThrottle coalesces per-client last_seen writes. Persisting last_seen
// on every authenticated request turned read-heavy traffic (pulls, summaries,
// SSE connects) into serialized writes under SQLite's single-writer WAL. A
// coarse timestamp is all the stale-device cron and crypto-v2 window need, so
// the row is rewritten at most once per ttl per client — except when the
// reported app version changes, which must land immediately.
type lastSeenThrottle struct {
	mu   sync.Mutex
	seen map[string]lastSeenRec
	ttl  time.Duration
}

type lastSeenRec struct {
	at      time.Time
	version string
}

func newLastSeenThrottle(ttl time.Duration) *lastSeenThrottle {
	return &lastSeenThrottle{seen: make(map[string]lastSeenRec), ttl: ttl}
}

// shouldWrite reports whether last_seen should be persisted now: first sighting,
// once per ttl, or on any app-version change.
func (t *lastSeenThrottle) shouldWrite(clientID, version string) bool {
	if t == nil {
		return true
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if rec, ok := t.seen[clientID]; ok && rec.version == version && now.Sub(rec.at) < t.ttl {
		return false
	}
	t.pruneLocked(now)
	t.seen[clientID] = lastSeenRec{at: now, version: version}
	return true
}

// pruneLocked drops entries older than the throttle window. Without it the map
// only ever grew: one permanent entry per client id ever seen, so client churn
// (reinstalls, ephemeral containers) leaked memory for the process lifetime.
// An entry past its ttl can no longer suppress a write, so dropping it is free.
func (t *lastSeenThrottle) pruneLocked(now time.Time) {
	for id, rec := range t.seen {
		if now.Sub(rec.at) >= t.ttl {
			delete(t.seen, id)
		}
	}
}
