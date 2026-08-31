package netutil

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPWithoutTrustProxy(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:4567"
	// Spoofed headers must be ignored when the proxy is not trusted.
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	r.Header.Set("X-Real-IP", "10.0.0.2")

	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("got %q, want RemoteAddr host 203.0.113.9", got)
	}
}

func TestClientIPTrustProxy(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "1")

	// One trusted proxy: the address it appended is the LAST entry. Anything to
	// its left is client-written and must never be used as the limiter key.
	tests := []struct {
		name string
		xff  string
		xri  string
		want string
	}{
		{"single hop", "198.51.100.7", "", "198.51.100.7"},
		{"spoofed prefix is ignored", "1.2.3.4, 198.51.100.7", "", "198.51.100.7"},
		{"only the proxy-appended hop is trusted", "10.0.0.1, 10.0.0.2, 198.51.100.7", "", "198.51.100.7"},
		{"whitespace trimmed", "  10.0.0.1 , 198.51.100.7 ", "", "198.51.100.7"},
		{"port stripped", "10.0.0.1, 198.51.100.7:51234", "", "198.51.100.7"},
		{"bracketed ipv6", "10.0.0.1, [2001:db8::1]:443", "", "2001:db8::1"},
		{"x-real-ip fallback", "", "198.51.100.8", "198.51.100.8"},
		{"no headers falls back to remote addr", "", "", "203.0.113.9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = "203.0.113.9:4567"
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				r.Header.Set("X-Real-IP", tc.xri)
			}
			if got := ClientIP(r); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPMalformedRemoteAddr(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "not-a-hostport"
	if got := ClientIP(r); got != "not-a-hostport" {
		t.Fatalf("got %q, want raw RemoteAddr", got)
	}
}

// A caller that varies X-Forwarded-For per request must not be able to vary the
// rate-limit key with it — that made the IP-keyed limiter on register, login,
// and TOTP fully bypassable.
func TestClientIPSpoofedXFFCannotVaryKey(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "1")

	for _, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3, 4.4.4.4"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.5:4567"
		r.Header.Set("X-Forwarded-For", spoof+", 198.51.100.7")
		if got := ClientIP(r); got != "198.51.100.7" {
			t.Fatalf("spoof %q: got %q, want 198.51.100.7", spoof, got)
		}
	}
}

// Multiple X-Forwarded-For headers are one logical chain; a client that splits
// its spoofed prefix across headers must not shift the trusted hop.
func TestClientIPMultipleXFFHeaders(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "1")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:4567"
	r.Header.Add("X-Forwarded-For", "1.1.1.1")
	r.Header.Add("X-Forwarded-For", "2.2.2.2, 198.51.100.7")
	if got := ClientIP(r); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}
}

// GSBS_TRUST_PROXY=N declares N trusted proxies, so the Nth entry from the
// right is the first hop we can vouch for.
func TestClientIPTrustProxyHopCount(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "2")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:4567"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 198.51.100.7, 10.0.0.9")
	if got := ClientIP(r); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}

	// Chain shorter than the configured hop count clamps to the leftmost entry
	// rather than indexing out of range.
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.RemoteAddr = "10.0.0.5:4567"
	r2.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := ClientIP(r2); got != "198.51.100.7" {
		t.Fatalf("short chain: got %q, want 198.51.100.7", got)
	}
}

// "0" means no trusted proxy, so forwarding headers are ignored.
func TestClientIPTrustProxyZero(t *testing.T) {
	t.Setenv("GSBS_TRUST_PROXY", "0")
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:4567"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("got %q, want 203.0.113.9", got)
	}
}
