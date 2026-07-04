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

	tests := []struct {
		name string
		xff  string
		xri  string
		want string
	}{
		{"single hop", "198.51.100.7", "", "198.51.100.7"},
		{"first hop of chain", "198.51.100.7, 10.0.0.1, 10.0.0.2", "", "198.51.100.7"},
		{"whitespace trimmed", "  198.51.100.7 , 10.0.0.1", "", "198.51.100.7"},
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
