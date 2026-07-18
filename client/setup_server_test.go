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
		"/settings", "/logs", "/logs/export.csv", "/help", "/about", "/status",
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
			if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("GET %s: dynamic route must be Cache-Control: no-store, got %q", path, cc)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "Template error") {
				t.Fatalf("GET %s: template error in body", path)
			}
		})
	}

	// Static assets keep their long-lived cache policy — the no-store
	// middleware must not leak onto them.
	t.Run("/static/app.css", func(t *testing.T) {
		resp, err := http.Get(base + "/static/app.css")
		if err != nil {
			t.Fatalf("GET /static/app.css: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age") {
			t.Fatalf("static asset Cache-Control = %q, want a max-age policy", cc)
		}
	})
}
