package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// LockoutTracker applies per-account brute-force backoff on top of the IP-keyed
// rate limiter: after several failed attempts within a window an account is
// briefly locked, with progressive backoff on further failures. State is
// in-memory (the server is a single process; a restart simply clears locks) and
// keyed by a hash so a username is never retained in cleartext.
//
// Locks are always short (minutes, never permanent) so a malicious actor cannot
// permanently deny a victim by guessing their password — the IP limiter plus a
// brief per-account lock is the intended balance.
type LockoutTracker struct {
	mu        sync.Mutex
	states    map[string]*lockState
	threshold int           // failures before the first lock
	window    time.Duration // failures older than this stop counting
	maxLock   time.Duration // cap on progressive backoff
}

type lockState struct {
	failures    int
	firstFail   time.Time
	lockedUntil time.Time
}

// NewLockoutTracker creates a tracker. A threshold <= 0 disables locking.
func NewLockoutTracker(threshold int, window, maxLock time.Duration) *LockoutTracker {
	return &LockoutTracker{
		states:    map[string]*lockState{},
		threshold: threshold,
		window:    window,
		maxLock:   maxLock,
	}
}

// AccountKey derives a stable, non-reversible key from a username so unknown and
// known accounts are tracked identically (no account-existence oracle).
func AccountKey(username string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	return hex.EncodeToString(sum[:])
}

// Locked reports whether key is currently locked and, if so, the time remaining.
func (l *LockoutTracker) Locked(key string) (bool, time.Duration) {
	if l == nil || l.threshold <= 0 {
		return false, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.states[key]
	if st == nil {
		return false, 0
	}
	if now := time.Now(); now.Before(st.lockedUntil) {
		return true, st.lockedUntil.Sub(now)
	}
	return false, 0
}

// Fail records a failed attempt and locks the account once the threshold is
// reached, lengthening the lock on each further failure (1m, 2m, 4m … capped).
func (l *LockoutTracker) Fail(key string) {
	if l == nil || l.threshold <= 0 {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(now)
	st := l.states[key]
	if st == nil {
		st = &lockState{firstFail: now}
		l.states[key] = st
	}
	// Start a fresh counting window if the last burst aged out and we are not
	// currently locked.
	if now.Sub(st.firstFail) > l.window && now.After(st.lockedUntil) {
		st.failures = 0
		st.firstFail = now
	}
	st.failures++
	if st.failures >= l.threshold {
		exp := st.failures - l.threshold
		if exp > 20 {
			exp = 20
		}
		d := time.Duration(1<<uint(exp)) * time.Minute
		if d <= 0 || d > l.maxLock {
			d = l.maxLock
		}
		st.lockedUntil = now.Add(d)
	}
}

// Reset clears failure state for key (call on any successful authentication).
func (l *LockoutTracker) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.states, key)
	l.mu.Unlock()
}

func (l *LockoutTracker) pruneLocked(now time.Time) {
	for k, st := range l.states {
		if now.After(st.lockedUntil) && now.Sub(st.firstFail) > l.window {
			delete(l.states, k)
		}
	}
}
