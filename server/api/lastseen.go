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
	t.seen[clientID] = lastSeenRec{at: now, version: version}
	return true
}
