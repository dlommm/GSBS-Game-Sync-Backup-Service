package pcgw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoGetRespectsContextCancelDuringRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cargoquery":[]}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	c.mu.Lock()
	c.lastRequest = time.Now()
	c.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.doGet(ctx, srv.URL+"/w/api.php?action=cargoquery&format=json&tables=Infobox_game")
	if err == nil {
		t.Fatal("expected context error")
	}
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
