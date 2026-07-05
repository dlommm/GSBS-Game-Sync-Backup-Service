package pcgw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecentChangesSincePagination(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("list") != "recentchanges" {
			http.NotFound(w, r)
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		if q.Get("rccontinue") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"continue": map[string]any{"rccontinue": "next-page"},
				"query": map[string]any{"recentchanges": []map[string]any{
					{"type": "edit", "pageid": 101, "title": "Game A", "revid": 5001, "timestamp": "2026-07-01T00:00:00Z"},
					{"type": "new", "pageid": 102, "title": "Game B", "revid": 5002, "timestamp": "2026-07-02T00:00:00Z"},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{"recentchanges": []map[string]any{
				{"type": "log", "pageid": 103, "title": "Game C", "logtype": "delete", "logaction": "delete", "timestamp": "2026-07-03T00:00:00Z"},
			}},
		})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	changes, err := c.RecentChangesSince(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 paginated requests, got %d", requests)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 entries across pages, got %d", len(changes))
	}
	if changes[2].LogType != "delete" || changes[2].PageID != 103 {
		t.Fatalf("unexpected final entry: %+v", changes[2])
	}
}

func TestGetPageRevisionsBatchChunks(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("prop") != "revisions" {
			http.NotFound(w, r)
			return
		}
		requests++
		ids := strings.Split(q.Get("pageids"), "|")
		if len(ids) > 50 {
			t.Errorf("batch exceeded MediaWiki limit: %d ids", len(ids))
		}
		pages := map[string]any{}
		for _, id := range ids {
			if id == "7" { // simulate a page deleted on the wiki
				pages[id] = map[string]any{"missing": ""}
				continue
			}
			pages[id] = map[string]any{
				"pageid":    jsonNumber(id),
				"revisions": []map[string]any{{"revid": 1000, "timestamp": "2026-07-01T00:00:00Z"}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": pages}})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	ids := make([]int64, 0, 120)
	for i := int64(1); i <= 120; i++ {
		ids = append(ids, i)
	}
	revs, err := c.GetPageRevisionsBatch(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("expected 3 requests for 120 ids (50/batch), got %d", requests)
	}
	if len(revs) != 119 {
		t.Fatalf("expected 119 revisions (one missing page), got %d", len(revs))
	}
	if _, ok := revs[7]; ok {
		t.Fatal("missing page must be absent from result")
	}
	if revs[1].RevID != 1000 {
		t.Fatalf("unexpected revision: %+v", revs[1])
	}
}

func jsonNumber(id string) json.Number { return json.Number(fmt.Sprint(id)) }
