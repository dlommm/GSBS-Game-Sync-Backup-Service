package netutil

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned when a dial or URL targets a non-public address.
var ErrBlockedAddress = errors.New("connection to non-public address blocked")

// isBlockedIP reports whether an outbound request from the server must never
// reach ip: loopback, RFC1918 private, link-local (incl. the cloud metadata
// endpoint 169.254.169.254), unique-local IPv6 (fc00::/7, covered by IsPrivate),
// unspecified, multicast, or CGNAT (100.64.0.0/10).
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true // 100.64.0.0/10 CGNAT
	}
	return false
}

// SafeDialControl is a net.Dialer.Control hook that rejects connections to
// non-public addresses. It runs AFTER DNS resolution, on the concrete IP being
// dialed, so it is safe against DNS-rebinding: a hostname that resolves to a
// private IP is blocked at connect time, not merely at URL-parse time.
func SafeDialControl(network, address string, _ syscall.RawConn) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("%w: network %q", ErrBlockedAddress, network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || isBlockedIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	return nil
}

// SafeHTTPClient returns an *http.Client whose dialer refuses non-public
// destinations (see SafeDialControl) and whose redirects are capped and
// re-validated for a safe scheme. Use it for any outbound request whose URL a
// user can influence (webhooks, ntfy, Discord) to prevent SSRF against the
// server's own network position.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   SafeDialControl,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: redirect scheme %q", ErrBlockedAddress, req.URL.Scheme)
			}
			return nil
		},
	}
}

// ValidatePublicURL fails fast (e.g. at save time) for a URL that is obviously
// unsafe: a non-http(s) scheme, or an IP-literal host that is non-public.
// Hostnames pass here — the authoritative, rebinding-safe check happens at dial
// time in SafeDialControl.
func ValidatePublicURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q", ErrBlockedAddress, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedAddress)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	return nil
}
