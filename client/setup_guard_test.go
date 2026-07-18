package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLocalGuard(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	guard := clientLocalGuard(next)

	cases := []struct {
		name       string
		method     string
		host       string
		origin     string
		secFetch   string
		wantStatus int
	}{
		{"GET loopback ok", "GET", "127.0.0.1:41234", "", "", http.StatusOK},
		{"GET localhost ok", "GET", "localhost:41234", "", "", http.StatusOK},
		{"GET rebound host blocked", "GET", "evil.com", "", "", http.StatusMisdirectedRequest},
		{"POST same-origin fetch ok", "POST", "127.0.0.1:41234", "http://127.0.0.1:41234", "same-origin", http.StatusOK},
		{"POST loopback origin ok", "POST", "127.0.0.1:41234", "http://127.0.0.1:41234", "", http.StatusOK},
		{"POST cross-origin blocked", "POST", "127.0.0.1:41234", "http://evil.com", "", http.StatusForbidden},
		{"POST cross-site secfetch blocked", "POST", "127.0.0.1:41234", "", "cross-site", http.StatusForbidden},
		{"POST no origin blocked", "POST", "127.0.0.1:41234", "", "", http.StatusForbidden},
		{"POST rebound host blocked before csrf", "POST", "attacker.example", "http://127.0.0.1:41234", "same-origin", http.StatusMisdirectedRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "http://x/settings/save", nil)
			req.Host = c.host
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			if c.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", c.secFetch)
			}
			rec := httptest.NewRecorder()
			guard.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	ok := []string{"127.0.0.1", "127.0.0.1:41234", "localhost", "localhost:8080", "::1", "[::1]:41234", "127.5.5.5"}
	for _, h := range ok {
		if !isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = false, want true", h)
		}
	}
	bad := []string{"", "evil.com", "evil.com:41234", "10.0.0.1", "192.168.1.1:80", "example.org"}
	for _, h := range bad {
		if isLoopbackHost(h) {
			t.Errorf("isLoopbackHost(%q) = true, want false", h)
		}
	}
}
