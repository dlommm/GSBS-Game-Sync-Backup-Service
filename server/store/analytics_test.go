package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestAnalyticsQueries(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	userID, err := st.CreateUser(ctx, "analytics-user", "hash")
	if err != nil {
		t.Fatal(err)
	}

	if err := st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "730", GameTitle: "CS2", Platform: "windows", PathTemplate: "/save"},
	}); err != nil {
		t.Fatal(err)
	}

	meta := SaveMeta{ContentSize: 100}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "main", []byte("data"), &meta); err != nil {
		t.Fatal(err)
	}
	meta.ContentSize = 50
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "730", "cfg", []byte("more"), &meta); err != nil {
		t.Fatal(err)
	}

	if n, err := st.CountTotalSaves(ctx); err != nil || n != 2 {
		t.Fatalf("CountTotalSaves: %d %v", n, err)
	}
	if n, err := st.CountDistinctSaveGames(ctx); err != nil || n != 1 {
		t.Fatalf("CountDistinctSaveGames: %d %v", n, err)
	}

	top, err := st.ListTopSaveGames(ctx, 5)
	if err != nil || len(top) != 1 {
		t.Fatalf("ListTopSaveGames: %v len=%d", err, len(top))
	}
	if top[0].GameID != "730" || top[0].SaveCount != 2 || top[0].StorageBytes != 150 {
		t.Fatalf("top game: %+v", top[0])
	}

	if err := st.InsertPCGWParseFailure(ctx, &types.PCGWParseFailure{
		PageID: 42, Section: "save", ErrorMessage: "bad wikitext",
	}); err != nil {
		t.Fatal(err)
	}
	failures, err := st.ListRecentPCGWParseFailures(ctx, 10)
	if err != nil || len(failures) != 1 {
		t.Fatalf("ListRecentPCGWParseFailures: %v len=%d", err, len(failures))
	}
	if failures[0].PageID != 42 || failures[0].Section != "save" {
		t.Fatalf("failure: %+v", failures[0])
	}
}
