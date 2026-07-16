package retry

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestBackoffNextAndReset(t *testing.T) {
	b := &Backoff{Initial: time.Second, Max: 10 * time.Second, Multiplier: 2}
	d1 := b.Next()
	if d1 < time.Second || d1 > 1200*time.Millisecond {
		t.Fatalf("first delay %v want ~1s", d1)
	}
	d2 := b.Next()
	if d2 < 2*time.Second || d2 > 2500*time.Millisecond {
		t.Fatalf("second delay %v want ~2s", d2)
	}
	b.Reset()
	d3 := b.Next()
	if d3 < time.Second || d3 > 1200*time.Millisecond {
		t.Fatalf("after reset delay %v want ~1s", d3)
	}
}

func TestBackoffCap(t *testing.T) {
	b := &Backoff{Initial: time.Second, Max: 3 * time.Second, Multiplier: 2, Jitter: 0}
	for i := 0; i < 5; i++ {
		b.Next()
	}
	if b.Current() != 3*time.Second {
		t.Fatalf("current %v want 3s cap", b.Current())
	}
}

func TestIsRetryableError(t *testing.T) {
	if !IsRetryableError(errStatus(503)) {
		t.Fatal("503 should be retryable")
	}
	if IsRetryableError(errStatus(401)) {
		t.Fatal("401 should not be retryable")
	}
	if IsRetryableError(errStatus(413)) {
		t.Fatal("413 should not be retryable")
	}
}

type statusErr string

func (e statusErr) Error() string { return string(e) }

func errStatus(code int) error {
	return statusErr("status " + itoa(code))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Permanent local failures must not burn retry attempts: the outcome cannot
// change on a second try.
func TestIsRetryableError_PermanentLocalErrors(t *testing.T) {
	if IsRetryableError(os.ErrNotExist) {
		t.Fatal("os.ErrNotExist should not be retryable")
	}
	if IsRetryableError(fmt.Errorf("read save: %w", os.ErrPermission)) {
		t.Fatal("wrapped os.ErrPermission should not be retryable")
	}
	if IsRetryableError(base64.CorruptInputError(7)) {
		t.Fatal("corrupt base64 should not be retryable")
	}
	if !IsRetryableError(errors.New("connection reset by peer")) {
		t.Fatal("transport errors stay retryable")
	}
}

// HTTPError must classify through multi-%w wrap chains regardless of the
// surrounding message text (the string parser can't see "push: 413 …").
func TestHTTPErrorThroughWrapChains(t *testing.T) {
	base := &HTTPError{Status: 413, Msg: "quota exceeded"}
	wrapped := fmt.Errorf("push: %w: %s (%w)", errors.New("quota sentinel"), "quota exceeded", base)
	if got := HTTPStatusFromError(wrapped); got != 413 {
		t.Fatalf("HTTPStatusFromError(wrapped) = %d, want 413", got)
	}
	if IsRetryableError(wrapped) {
		t.Fatal("413 through wrap chain must be non-retryable")
	}
	retryable := fmt.Errorf("pull: %w", &HTTPError{Status: 503})
	if !IsRetryableError(retryable) {
		t.Fatal("503 through wrap chain must stay retryable")
	}
	// Legacy string format still parses (fallback path).
	if got := HTTPStatusFromError(errors.New("push: status 502")); got != 502 {
		t.Fatalf("legacy parse = %d, want 502", got)
	}
}
