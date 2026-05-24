package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_AllowWithinLimit(t *testing.T) {
	l := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatal("4th request should be denied")
	}
}

func TestLimiter_SeparateKeys(t *testing.T) {
	l := New(1, time.Minute)
	if !l.Allow("a") {
		t.Fatal("first key should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("different key should be allowed")
	}
}

func TestLimiter_NilOrZeroLimit(t *testing.T) {
	var l *Limiter
	if !l.Allow("k") {
		t.Fatal("nil limiter should allow")
	}
	l2 := New(0, time.Minute)
	if !l2.Allow("k") {
		t.Fatal("zero limit should allow")
	}
}

func TestLimiter_PruneIdleKeys(t *testing.T) {
	l := New(2, 50*time.Millisecond)
	if !l.Allow("idle") {
		t.Fatal("expected allow")
	}
	time.Sleep(120 * time.Millisecond)
	if !l.Allow("fresh") {
		t.Fatal("expected allow for fresh key")
	}
	l.mu.Lock()
	if _, ok := l.times["idle"]; ok {
		t.Fatal("idle key should be evicted after 2x window")
	}
	l.mu.Unlock()
}
