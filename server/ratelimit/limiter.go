package ratelimit

import (
	"sync"
	"time"
)

// Limiter is an in-memory per-key rate limiter (sliding window of recent timestamps).
type Limiter struct {
	mu        sync.Mutex
	times     map[string][]time.Time
	limit     int
	window    time.Duration
	lastPrune time.Time
}

// New creates a limiter that allows up to limit requests per key within window.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{times: make(map[string][]time.Time), limit: limit, window: window}
}

// Allow reports whether the request for key is allowed. It prunes old entries and appends the current time.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	list := l.times[key]
	i := 0
	for i < len(list) && list[i].Before(cutoff) {
		i++
	}
	list = list[i:]
	allowed := len(list) < l.limit
	if allowed {
		list = append(list, now)
	}
	l.times[key] = list
	l.maybePruneLocked(now)
	return allowed
}

// maybePruneLocked evicts keys with no timestamps within 2x the window, but at
// most once per window. Pruning is a full O(keys) map scan; running it on every
// Allow made the limiter degrade under exactly the many-distinct-key floods it
// exists to absorb, so it is amortized here — steady-state Allow stays O(1) on
// the per-key slice, and idle keys are still evicted within one extra window.
func (l *Limiter) maybePruneLocked(now time.Time) {
	if now.Sub(l.lastPrune) < l.window {
		return
	}
	l.lastPrune = now
	idleCutoff := now.Add(-2 * l.window)
	for k, ts := range l.times {
		if len(ts) == 0 {
			delete(l.times, k)
			continue
		}
		last := ts[len(ts)-1]
		if last.Before(idleCutoff) {
			delete(l.times, k)
		}
	}
}
