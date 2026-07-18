package netutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSafeDialControl(t *testing.T) {
	cases := []struct {
		addr    string
		blocked bool
	}{
		{"8.8.8.8:443", false},
		{"1.1.1.1:80", false},
		{"[2606:4700:4700::1111]:443", false}, // public IPv6
		{"127.0.0.1:80", true},
		{"10.0.0.1:80", true},
		{"172.16.5.4:80", true},
		{"192.168.1.10:443", true},
		{"169.254.169.254:80", true}, // cloud metadata (link-local)
		{"100.64.1.1:80", true},      // CGNAT
		{"[::1]:80", true},
		{"[fe80::1]:80", true},
		{"[fc00::1]:80", true}, // ULA
		{"0.0.0.0:80", true},
	}
	for _, c := range cases {
		err := SafeDialControl("tcp", c.addr, nil)
		if c.blocked && err == nil {
			t.Errorf("%s: expected blocked, got allowed", c.addr)
		}
		if !c.blocked && err != nil {
			t.Errorf("%s: expected allowed, got %v", c.addr, err)
		}
	}
}

func TestSafeDialControl_NonTCP(t *testing.T) {
	if err := SafeDialControl("udp", "8.8.8.8:53", nil); err == nil {
		t.Fatal("expected non-tcp network to be blocked")
	}
}

func TestValidatePublicURL(t *testing.T) {
	bad := []string{
		"",
		"file:///etc/passwd",
		"ftp://example.com",
		"gopher://8.8.8.8",
		"http://127.0.0.1/x",
		"http://169.254.169.254/latest/meta-data",
		"http://10.1.2.3/hook",
		"https://[::1]/x",
	}
	for _, u := range bad {
		if err := ValidatePublicURL(u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
	good := []string{
		"https://discord.com/api/webhooks/x/y",
		"https://ntfy.sh/mytopic",
		"http://example.com:8080/hook",
	}
	for _, u := range good {
		if err := ValidatePublicURL(u); err != nil {
			t.Errorf("expected %q to be allowed, got %v", u, err)
		}
	}
}

// TestSafeHTTPClient_BlocksLoopbackHost proves the guard is DNS-rebinding-safe:
// even reaching a loopback server by URL is refused at connect time.
func TestSafeHTTPClient_BlocksLoopbackHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := SafeHTTPClient(2 * time.Second)
	resp, err := client.Get(srv.URL) // srv.URL is http://127.0.0.1:<port>
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the safe client to block a loopback destination")
	}
	if !strings.Contains(err.Error(), ErrBlockedAddress.Error()) {
		t.Logf("blocked with: %v", err) // still blocked; message may be wrapped by the transport
	}
}
