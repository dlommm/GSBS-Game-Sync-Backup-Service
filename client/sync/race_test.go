package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
)

// TestClientTokenRace spawns concurrent Push goroutines while tryReloadToken fires
// concurrently. Running with -race should report no data races.
func TestClientTokenRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	ResetPushHashCacheForTest()
	resolver := paths.NewResolver()
	client, err := NewClient(srv.URL, "initial-token", resolver, paths.CurrentOS(), 0, false, false)
	if err != nil {
		t.Fatal(err)
	}

	// TokenReload returns new tokens to exercise concurrent writes to c.token / c.authRetried.
	var mu sync.Mutex
	counter := 0
	client.TokenReload = func() string {
		mu.Lock()
		defer mu.Unlock()
		counter++
		// Return a new token each time so tryReloadToken actually writes.
		return "reloaded-token-" + string(rune('A'+counter%26))
	}

	content := []byte("save-data-payload")
	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			// Push reads c.token and c.authRetried concurrently with tryReloadToken writes.
			_ = client.Push(context.Background(), "game1", "pk1", "/tmp/save.dat", "slot/save.dat", content)
		}()
	}

	// One goroutine explicitly fires tryReloadToken concurrently with Push goroutines.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			client.tryReloadToken()
		}
	}()

	wg.Wait()
}
