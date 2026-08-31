package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/covers/730":            "/covers/:id",
		"/covers/abc-def":        "/covers/:id",
		"/dashboard/games/12345": "/dashboard/games/:id",
		"/dashboard/games":       "/dashboard/games",
		"/api/saves":             "/api/saves",
		"/dashboard":             "/dashboard",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollectorRecordBounded(t *testing.T) {
	c := NewCollector(nil, nil)
	for i := 0; i < 100; i++ {
		c.Record(NormalizePath("/covers/"+strconv.Itoa(i)), 200)
	}
	// 100 distinct cover paths collapse to a single "/covers/:id|200" series.
	n := 0
	c.counts.Range(func(_, _ any) bool { n++; return true })
	if n != 1 {
		t.Fatalf("expected 1 bounded series, got %d", n)
	}
}

// The server is internet-facing and every unmatched path used to get its own
// counter, so scanner traffic grew the map and the Prometheus series set
// without limit.
func TestCollectorFoldsNotFoundPathsIntoOneSeries(t *testing.T) {
	c := NewCollector(nil, nil)
	for i := 0; i < 1000; i++ {
		c.Record(fmt.Sprintf("/wp-admin/%d.php", i), http.StatusNotFound)
	}
	if got := c.countKeys.Load(); got != 1 {
		t.Errorf("404 flood produced %d series, want 1", got)
	}
}

// Real routes still get their own series, and the hard cap backstops anything
// that slips past the 404 fold (e.g. a scanner that gets 200s or 500s).
func TestCollectorCapsDistinctSeries(t *testing.T) {
	c := NewCollector(nil, nil)
	for i := 0; i < maxDistinctSeries*3; i++ {
		c.Record(fmt.Sprintf("/api/thing/%d", i), http.StatusOK)
		c.RecordDuration(fmt.Sprintf("/api/thing/%d", i), time.Millisecond)
	}
	if got := c.countKeys.Load(); got > maxDistinctSeries+1 {
		t.Errorf("count series %d exceeds cap %d", got, maxDistinctSeries)
	}
	if got := c.durationKeys.Load(); got > maxDistinctSeries+1 {
		t.Errorf("duration series %d exceeds cap %d", got, maxDistinctSeries)
	}
}
