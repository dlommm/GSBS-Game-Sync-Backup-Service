package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gsbs/gsbs/server/store"
)

// SSECounter returns the current number of SSE connections (e.g. hub.Count()). May be nil.
type SSECounter interface {
	Count() int
}

// Collector holds optional Prometheus-format metrics (request counts, storage, SSE, job).
type Collector struct {
	store store.Store
	sse   SSECounter
	// key: "path|code" -> count
	counts sync.Map
	// key: path -> *durationSumCount (for request duration summary)
	durations sync.Map
}

type durationSumCount struct {
	sumSeconds float64
	count      int64
	mu         sync.Mutex
}

// NewCollector creates a metrics collector. Store may be nil (storage metric skipped). sse may be nil.
func NewCollector(st store.Store, sse SSECounter) *Collector {
	return &Collector{store: st, sse: sse}
}

// Record increments the counter for the given path and status code.
func (c *Collector) Record(path string, statusCode int) {
	if c == nil {
		return
	}
	key := fmt.Sprintf("%s|%d", path, statusCode)
	v, _ := c.counts.LoadOrStore(key, new(int64))
	atomic.AddInt64(v.(*int64), 1)
}

// RecordDuration records request duration for the given path (for summary metrics).
func (c *Collector) RecordDuration(path string, d time.Duration) {
	if c == nil {
		return
	}
	v, _ := c.durations.LoadOrStore(path, &durationSumCount{})
	dc := v.(*durationSumCount)
	dc.mu.Lock()
	dc.sumSeconds += d.Seconds()
	dc.count++
	dc.mu.Unlock()
}

// ServeHTTP writes Prometheus text exposition format to w.
func (c *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Request counts: gsbs_http_requests_total{path="...",code="..."} N
	var keys []string
	c.counts.Range(func(k, v interface{}) bool {
		keys = append(keys, k.(string))
		return true
	})
	sort.Strings(keys)
	for _, key := range keys {
		v, _ := c.counts.Load(key)
		n := atomic.LoadInt64(v.(*int64))
		parts := strings.SplitN(key, "|", 2)
		path, code := parts[0], "0"
		if len(parts) == 2 {
			code = parts[1]
		}
		fmt.Fprintf(w, "gsbs_http_requests_total{path=%q,code=%q} %d\n", path, code, n)
	}

	if c.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		total, err := c.store.TotalStorageBytes(ctx)
		if err == nil {
			fmt.Fprintf(w, "gsbs_storage_bytes_total %d\n", total)
		}
		users, _ := c.store.CountUsers(ctx)
		clients, _ := c.store.CountClients(ctx)
		saves, _ := c.store.CountSaves(ctx)
		fmt.Fprintf(w, "gsbs_users_total %d\n", users)
		fmt.Fprintf(w, "gsbs_clients_total %d\n", clients)
		fmt.Fprintf(w, "gsbs_saves_total %d\n", saves)
		latest, err := c.store.GetLatestJobRun(ctx, "pcgw_sync")
		if err == nil && latest != nil {
			t, _ := time.Parse(time.RFC3339, latest.StartedAt)
			fmt.Fprintf(w, "gsbs_job_last_run_timestamp_seconds %d\n", t.Unix())
			success := 0
			if latest.Status == "success" {
				success = 1
			}
			fmt.Fprintf(w, "gsbs_job_last_success %d\n", success)
		}
	}
	if c.sse != nil {
		fmt.Fprintf(w, "gsbs_sse_connections_total %d\n", c.sse.Count())
	}
	c.durations.Range(func(k, v interface{}) bool {
		path := k.(string)
		dc := v.(*durationSumCount)
		dc.mu.Lock()
		sum, count := dc.sumSeconds, dc.count
		dc.mu.Unlock()
		if count > 0 {
			fmt.Fprintf(w, "gsbs_http_request_duration_seconds_sum{path=%q} %g\n", path, sum)
			fmt.Fprintf(w, "gsbs_http_request_duration_seconds_count{path=%q} %d\n", path, count)
		}
		return true
	})
}
