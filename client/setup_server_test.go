package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestSetupServerRoutes boots the real loopback WebUI and sweeps every page:
// HTTP 200, the strict CSP header present, and no template-execution errors.
func TestSetupServerRoutes(t *testing.T) {
	base := StartSetupServer()
	if base == "" {
		t.Skip("no loopback port available")
	}
	routes := []string{
		"/", "/dashboard", "/games", "/insights", "/quick-actions",
		"/settings", "/logs", "/help", "/about", "/status",
	}
	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(base + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d", path, resp.StatusCode)
			}
			csp := resp.Header.Get("Content-Security-Policy")
			if csp == "" || strings.Contains(csp, "unsafe-inline") {
				t.Fatalf("GET %s: weak or missing CSP: %q", path, csp)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "Template error") {
				t.Fatalf("GET %s: template error in body", path)
			}
		})
	}
}
