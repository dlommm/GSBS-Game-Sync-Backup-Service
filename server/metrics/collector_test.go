package metrics

import (
	"strconv"
	"testing"
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
