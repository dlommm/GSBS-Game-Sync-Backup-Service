package netutil

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// ClientIP returns the client IP for rate limiting and logging.
// When GSBS_TRUST_PROXY is set, X-Forwarded-For (first hop) or X-Real-IP is used.
func ClientIP(r *http.Request) string {
	if os.Getenv("GSBS_TRUST_PROXY") != "" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
