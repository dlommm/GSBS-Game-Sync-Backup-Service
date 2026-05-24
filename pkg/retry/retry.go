package retry

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// IsRetryableHTTP reports whether an HTTP status code is transient.
func IsRetryableHTTP(status int) bool {
	return status >= 500 && status < 600 || status == http.StatusTooManyRequests
}

// IsNonRetryableHTTP reports auth/quota errors that should not be retried.
func IsNonRetryableHTTP(status int) bool {
	return status == http.StatusUnauthorized ||
		status == http.StatusForbidden ||
		status == http.StatusRequestEntityTooLarge
}

// HTTPStatusFromError extracts an HTTP status from error messages like "status 503" or "push: status 503".
func HTTPStatusFromError(err error) int {
	if err == nil {
		return 0
	}
	s := err.Error()
	for _, prefix := range []string{"status ", "push: status ", "pull single: status ", "summaries: status "} {
		if idx := strings.Index(s, prefix); idx >= 0 {
			var code int
			if _, scanErr := parseStatus(s[idx+len(prefix):], &code); scanErr == nil {
				return code
			}
		}
	}
	return 0
}

func parseStatus(s string, code *int) (int, error) {
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	if n == 0 {
		return 0, errors.New("no status")
	}
	*code = n
	return n, nil
}

// IsRetryableError reports whether err represents a transient failure worth retrying.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	code := HTTPStatusFromError(err)
	if code != 0 {
		if IsNonRetryableHTTP(code) {
			return false
		}
		return IsRetryableHTTP(code)
	}
	// Transport errors (timeout, connection reset) are retryable.
	return true
}

// Do runs fn up to maxAttempts times with backoff between failures.
func Do(ctx context.Context, b *Backoff, maxAttempts int, fn func() error) error {
	if b == nil {
		b = DefaultBackoff()
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !IsRetryableError(lastErr) {
			return lastErr
		}
		if attempt >= maxAttempts {
			break
		}
		delay := b.Next()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}
