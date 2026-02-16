package ratelimit

import (
	"sync"
	"time"
)

// Limiter is an in-memory per-key rate limiter (sliding window of recent timestamps).
type Limiter struct {
	mu     sync.Mutex
	times  map[string][]time.Time
	limit  int
	window time.Duration
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
	// Prune old
	i := 0
	for i < len(list) && list[i].Before(cutoff) {
		i++
	}
	list = list[i:]
	if len(list) >= l.limit {
		return false
	}
	l.times[key] = append(list, now)
	return true
}
