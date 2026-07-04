package retry

import (
	"math/rand"
	"sync"
	"time"
)

// Backoff implements exponential backoff with optional jitter.
type Backoff struct {
	mu         sync.Mutex
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Jitter     float64 // fraction of delay to randomize (0-1)
	current    time.Duration
}

// DefaultBackoff returns a backoff suitable for inline sync retries (2s start, 30s cap).
func DefaultBackoff() *Backoff {
	return &Backoff{
		Initial:    2 * time.Second,
		Max:        30 * time.Second,
		Multiplier: 2,
		Jitter:     0.1,
	}
}

// OutboxBackoff returns a backoff suitable for outbox retries (2m start, 30m cap).
func OutboxBackoff() *Backoff {
	return &Backoff{
		Initial:    2 * time.Minute,
		Max:        30 * time.Minute,
		Multiplier: 2,
		Jitter:     0.1,
	}
}

// SSEBackoff returns a backoff suitable for SSE reconnect (2s start, 60s cap).
func SSEBackoff() *Backoff {
	return &Backoff{
		Initial:    2 * time.Second,
		Max:        60 * time.Second,
		Multiplier: 2,
		Jitter:     0.05,
	}
}

// Reset clears the backoff to its initial value.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = 0
}

// Next returns the next delay and advances internal state.
func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == 0 {
		b.current = b.Initial
	} else {
		next := time.Duration(float64(b.current) * b.Multiplier)
		if next > b.Max {
			next = b.Max
		}
		b.current = next
	}
	d := b.current
	if b.Jitter > 0 {
		j := float64(d) * b.Jitter
		d = time.Duration(float64(d) - j + rand.Float64()*2*j) //nolint:gosec // G404: non-cryptographic retry jitter
	}
	return d
}

// Current returns the current delay without advancing.
func (b *Backoff) Current() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == 0 {
		return b.Initial
	}
	return b.current
}
