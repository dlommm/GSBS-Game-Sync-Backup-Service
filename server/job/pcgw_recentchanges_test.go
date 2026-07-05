package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/pkg/types"
	"github.com/gsbs/gsbs/server/store"
)

// TestSeededGateBlocksEmptyMirror: the absolute gate — an API sync against an
// empty mirror must refuse before making a single API call.
func TestSeededGateBlocksEmptyMirror(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	apiCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	_, err = PCGWSyncEx(context.Background(), st, client, nil, nil, PCGWSyncOptions{})
	if !errors.Is(err, ErrPCGWMirrorNotSeeded) {
		t.Fatalf("expected ErrPCGWMirrorNotSeeded, got %v", err)
	}
	if apiCalls != 0 {
		t.Fatalf("gate must fire before any API call; saw %d", apiCalls)
	}

	// Full mode is gated too — no override exists.
	_, err = PCGWSyncEx(context.Background(), st, client, nil, nil, PCGWSyncOptions{Full: true, ForceFull: true})
	if !errors.Is(err, ErrPCGWMirrorNotSeeded) {
		t.Fatalf("full sync must also be gated, got %v", err)
	}

	// RebuildManifestOnly makes no API calls and stays allowed.
	if _, err := PCGWSyncEx(context.Background(), st, client, nil, nil, PCGWSyncOptions{RebuildManifestOnly: true}); err != nil {
		t.Fatalf("rebuild-manifest-only should pass the gate: %v", err)
	}
}

func seedGameWithCatalog(t *testing.T, ctx context.Context, st store.Store, pageID int64, title string, revID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.UpsertPCGWCatalogBatch(ctx, []types.PCGWCatalogEntry{{
		PageID: pageID, Title: title, FirstSeenAt: now, LastSeenAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertPCGWGame(ctx, &types.PCGWGame{
		PageID: pageID, PageName: fmt.Sprint(pageID), Title: title,
		ParseStatus: "ok", LastRevID: revID,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestBuildChangedQueueRecentChanges drives the cheap path end to end against
// a mock wiki: changed pages queue with revision hints, already-current pages
// are skipped, new pages gain catalog rows, and wiki deletions cascade.
func TestBuildChangedQueueRecentChanges(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	seedGameWithCatalog(t, ctx, st, 201, "Changed Game", 100)     // edited: rev 100 -> 150
	seedGameWithCatalog(t, ctx, st, 202, "Unchanged Game", 200)   // edited but rev still 200
	seedGameWithCatalog(t, ctx, st, 203, "Deleted Game", 300)     // deleted (with pageid)
	seedGameWithCatalog(t, ctx, st, 204, "Deleted By Title", 400) // deleted (title only)
	// Filler so 2 deletions stay under the 25% safety valve.
	for i := int64(1); i <= 6; i++ {
		seedGameWithCatalog(t, ctx, st, 210+i, fmt.Sprintf("Filler %d", i), 100)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list") != "recentchanges" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{"recentchanges": []map[string]any{
				{"type": "edit", "pageid": 201, "title": "Changed Game", "revid": 150, "timestamp": "2026-07-04T00:00:00Z"},
				{"type": "edit", "pageid": 202, "title": "Unchanged Game", "revid": 200, "timestamp": "2026-07-04T01:00:00Z"},
				{"type": "new", "pageid": 205, "title": "Brand New Game", "revid": 500, "timestamp": "2026-07-04T02:00:00Z"},
				{"type": "log", "pageid": 203, "title": "Deleted Game", "logtype": "delete", "logaction": "delete", "timestamp": "2026-07-04T03:00:00Z"},
				{"type": "log", "pageid": 0, "title": "Deleted By Title", "logtype": "delete", "logaction": "delete", "timestamp": "2026-07-04T04:00:00Z"},
			}},
		})
	}))
	defer srv.Close()
	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	lastCheck := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	res, err := buildChangedQueue(ctx, st, client, "run1", PCGWFilters{}, lastCheck)
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != "recentchanges" {
		t.Fatalf("expected recentchanges method, got %s", res.Method)
	}

	want := map[int64]bool{201: true, 205: true}
	got := map[int64]bool{}
	for _, id := range res.PageIDs {
		got[id] = true
	}
	if len(got) != len(want) || !got[201] || !got[205] {
		t.Fatalf("queue mismatch: got %v want changed=201 + new=205", res.PageIDs)
	}
	if res.RevHints[201].RevID != 150 || res.RevHints[205].RevID != 500 {
		t.Fatalf("revision hints missing: %+v", res.RevHints)
	}

	// Deletions cascaded (both by pageid and by title).
	if res.Deleted != 2 {
		t.Fatalf("expected 2 deletions, got %d", res.Deleted)
	}
	for _, title := range []string{"Deleted Game", "Deleted By Title"} {
		if id, _ := st.GetPCGWCatalogPageIDByTitle(ctx, title); id != 0 {
			t.Fatalf("%s still in catalog after deletion", title)
		}
	}

	// New page got a catalog row so the exported catalog includes it.
	if id, _ := st.GetPCGWCatalogPageIDByTitle(ctx, "Brand New Game"); id != 205 {
		t.Fatalf("new page missing catalog row, got id %d", id)
	}
}

// TestBuildChangedQueueFallsBackToSweep: a stale window must use the batched
// revision sweep, 50 pages per request.
func TestBuildChangedQueueFallsBackToSweep(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	seedGameWithCatalog(t, ctx, st, 301, "Sweep Changed", 100)
	seedGameWithCatalog(t, ctx, st, 302, "Sweep Same", 200)

	rcCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("list") == "recentchanges" {
			rcCalls++
			http.NotFound(w, r)
			return
		}
		if q.Get("prop") == "revisions" {
			pages := map[string]any{}
			for _, id := range strings.Split(q.Get("pageids"), "|") {
				rev := 200 // 302 unchanged
				if id == "301" {
					rev = 999 // changed
				}
				pages[id] = map[string]any{
					"pageid":    json.Number(id),
					"revisions": []map[string]any{{"revid": rev, "timestamp": "2026-07-04T00:00:00Z"}},
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": pages}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	stale := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339)
	res, err := buildChangedQueue(ctx, st, client, "run1", PCGWFilters{}, stale)
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != "sweep" {
		t.Fatalf("stale window must use sweep, got %s", res.Method)
	}
	if rcCalls != 0 {
		t.Fatal("stale window must not query recentchanges")
	}
	if len(res.PageIDs) != 1 || res.PageIDs[0] != 301 {
		t.Fatalf("sweep should flag only 301, got %v", res.PageIDs)
	}
	if res.RevHints[301].RevID != 999 {
		t.Fatalf("sweep must carry revision hints, got %+v", res.RevHints)
	}

	// Never-checked (empty window) also sweeps.
	res, err = buildChangedQueue(ctx, st, client, "run1", PCGWFilters{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != "sweep" {
		t.Fatalf("empty window must use sweep, got %s", res.Method)
	}
}

// TestRecentChangesDeletionSafetyValve: a deletion burst larger than 25% of
// the catalog must be ignored (suspect feed), mirroring the import guard.
func TestRecentChangesDeletionSafetyValve(t *testing.T) {
	t.Setenv("GSBS_PCGW_RATE_LIMIT", "1ms")
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	for i := int64(1); i <= 4; i++ {
		seedGameWithCatalog(t, ctx, st, 400+i, fmt.Sprintf("Valve Game %d", i), 100)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": map[string]any{"recentchanges": []map[string]any{
				{"type": "log", "pageid": 401, "title": "Valve Game 1", "logtype": "delete", "logaction": "delete"},
				{"type": "log", "pageid": 402, "title": "Valve Game 2", "logtype": "delete", "logaction": "delete"},
				{"type": "log", "pageid": 403, "title": "Valve Game 3", "logtype": "delete", "logaction": "delete"},
			}},
		})
	}))
	defer srv.Close()
	client := pcgw.NewClient()
	client.BaseURL = srv.URL

	lastCheck := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	res, err := buildChangedQueue(ctx, st, client, "run1", PCGWFilters{}, lastCheck)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 0 {
		t.Fatalf("safety valve should skip mass deletion, deleted %d", res.Deleted)
	}
	if id, _ := st.GetPCGWCatalogPageIDByTitle(ctx, "Valve Game 1"); id == 0 {
		t.Fatal("game was deleted despite safety valve")
	}
}
