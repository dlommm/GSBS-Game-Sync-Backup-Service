package auth

import (
	"testing"
	"time"
)

func TestLockoutTracker_LocksAfterThreshold(t *testing.T) {
	l := NewLockoutTracker(3, time.Minute, 15*time.Minute)
	k := AccountKey("Alice")

	if locked, _ := l.Locked(k); locked {
		t.Fatal("should not start locked")
	}
	l.Fail(k)
	l.Fail(k)
	if locked, _ := l.Locked(k); locked {
		t.Fatal("2 failures should not lock (threshold 3)")
	}
	l.Fail(k) // 3rd failure → locked
	locked, d := l.Locked(k)
	if !locked || d <= 0 {
		t.Fatalf("expected lock after threshold, locked=%v d=%v", locked, d)
	}

	l.Reset(k)
	if locked, _ := l.Locked(k); locked {
		t.Fatal("Reset should clear the lock")
	}
}

func TestLockoutTracker_KeyNormalizes(t *testing.T) {
	// Same account despite case/whitespace so an attacker can't distinguish a
	// real account from a made-up one by lockout behavior.
	if AccountKey("Bob") != AccountKey("  bob ") {
		t.Fatal("account key should normalize case/whitespace")
	}
	if AccountKey("bob") == AccountKey("alice") {
		t.Fatal("different usernames must yield different keys")
	}
}

func TestLockoutTracker_Disabled(t *testing.T) {
	l := NewLockoutTracker(0, time.Minute, time.Minute)
	k := AccountKey("x")
	for i := 0; i < 10; i++ {
		l.Fail(k)
	}
	if locked, _ := l.Locked(k); locked {
		t.Fatal("threshold 0 must disable locking")
	}
}

func TestLockoutTracker_ProgressiveBackoff(t *testing.T) {
	l := NewLockoutTracker(1, time.Minute, 15*time.Minute)
	k := AccountKey("victim")
	l.Fail(k)
	_, d1 := l.Locked(k)
	l.Fail(k)
	_, d2 := l.Locked(k)
	if d2 <= d1 {
		t.Fatalf("expected backoff to grow: d1=%v d2=%v", d1, d2)
	}
}
