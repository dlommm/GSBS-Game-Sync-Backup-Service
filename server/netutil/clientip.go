package netutil

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// trustedProxyHops reports how many trusted reverse proxies sit in front of
// this server, or 0 when forwarding headers must not be trusted at all.
//
// GSBS_TRUST_PROXY unset (or "0") disables header trust entirely. An integer
// N declares N trusted proxies in the chain; any other non-empty value means
// one, which is the common Caddy/nginx-in-front deployment.
func trustedProxyHops() int {
	v := strings.TrimSpace(os.Getenv("GSBS_TRUST_PROXY"))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return n
	}
	return 1
}

// hostOnly strips a port and IPv6 brackets from an address, leaving the host.
func hostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

// forwardedChain flattens every X-Forwarded-For header into an ordered,
// left-to-right list of hosts, dropping empty entries.
func forwardedChain(r *http.Request) []string {
	var out []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(header, ",") {
			if h := hostOnly(part); h != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

// ClientIP returns the client IP for rate limiting and logging.
//
// When GSBS_TRUST_PROXY is set, the address is taken from X-Forwarded-For
// counting from the RIGHT, not the left. A proxy appends the peer it actually
// saw to the end of the chain, so only the rightmost entries are ones we can
// vouch for; everything to their left was written by the client and is
// attacker-controlled. Keying the rate limiter on the leftmost entry let a
// caller mint a fresh limiter bucket per request — unthrottled password
// spraying against register/login/TOTP — and poisoned logged IPs.
func ClientIP(r *http.Request) string {
	if hops := trustedProxyHops(); hops > 0 {
		if chain := forwardedChain(r); len(chain) > 0 {
			// Skip the hops our own proxies appended; clamp to the leftmost
			// entry when the chain is shorter than the configured hop count.
			idx := len(chain) - hops
			if idx < 0 {
				idx = 0
			}
			return chain[idx]
		}
		// X-Real-IP is a single value the proxy overwrites rather than appends,
		// so there is no chain to walk.
		if xri := hostOnly(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	return hostOnly(r.RemoteAddr)
}
