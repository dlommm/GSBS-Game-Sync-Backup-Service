package store

import (
	"context"
	"testing"

	"github.com/gsbs/gsbs/pkg/types"
)

func TestSQLite_CreateUser_UserByUsername(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	id, err := st.CreateUser(ctx, "user1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Error("expected non-empty user ID")
	}

	uid, hash, err := st.UserByUsername(ctx, "user1")
	if err != nil {
		t.Fatal(err)
	}
	if uid != id || hash != "hash" {
		t.Errorf("UserByUsername: uid=%s hash=%s", uid, hash)
	}

	_, _, err = st.UserByUsername(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

func TestSQLite_RegisterClient_ClientByToken_ListClientsByUserID(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	token, err := st.RegisterClient(ctx, userID, "my-laptop", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	uid, cid, name, os, err := st.ClientByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if uid != userID || name != "my-laptop" || os != "linux" {
		t.Errorf("ClientByToken: uid=%s cid=%s name=%s os=%s", uid, cid, name, os)
	}

	clients, err := st.ListClientsByUserID(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].Name != "my-laptop" {
		t.Errorf("ListClientsByUserID: %+v", clients)
	}
}

func TestSQLite_UpsertSave_ListSaves_GetSave(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	userID, _ := st.CreateUser(ctx, "u", "h")
	content := []byte("save data here")
	err = st.UpsertSave(ctx, userID, "game1", "pathkey1", content)
	if err != nil {
		t.Fatal(err)
	}

	saves, err := st.ListSaves(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0].GameID != "game1" || string(saves[0].Content) != string(content) {
		t.Errorf("ListSaves: %+v", saves)
	}

	blob, err := st.GetSave(ctx, userID, "game1", "pathkey1")
	if err != nil {
		t.Fatal(err)
	}
	if blob == nil || string(blob.Content) != string(content) {
		t.Errorf("GetSave: %+v", blob)
	}

	// Upsert same key again
	err = st.UpsertSave(ctx, userID, "game1", "pathkey1", []byte("updated"))
	if err != nil {
		t.Fatal(err)
	}
	saves, _ = st.ListSaves(ctx, userID)
	if len(saves) != 1 || string(saves[0].Content) != "updated" {
		t.Errorf("ListSaves after upsert: %+v", saves)
	}
}

func TestSQLite_GameSaveLocations(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	entries := []types.GameSaveLocation{
		{GameID: "1", PCGWPageID: 1, GameTitle: "Game One", Platform: "windows", PathTemplate: "%APPDATA%\\Game1", IsConfig: false, Source: "pcgw"},
	}
	err = st.UpsertGameSaveLocations(ctx, entries)
	if err != nil {
		t.Fatal(err)
	}

	list, err := st.ListGameSaveLocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].GameID != "1" || list[0].PathTemplate != "%APPDATA%\\Game1" {
		t.Errorf("ListGameSaveLocations: %+v", list)
	}
}

func TestSQLite_SearchGameSaveLocations(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	entries := []types.GameSaveLocation{
		{GameID: "1", PCGWPageID: 1, GameTitle: "Alpha Game", Platform: "windows", PathTemplate: "a", Source: "pcgw"},
		{GameID: "2", PCGWPageID: 2, GameTitle: "Beta Game", Platform: "linux", PathTemplate: "b", Source: "pcgw"},
	}
	if err := st.UpsertGameSaveLocations(ctx, entries); err != nil {
		t.Fatal(err)
	}
	found, total, err := st.SearchGameSaveLocations(ctx, "Alpha", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(found) != 1 || found[0].GameTitle != "Alpha Game" {
		t.Errorf("SearchGameSaveLocations Alpha: total=%d found=%+v", total, found)
	}
	all, totalAll, err := st.SearchGameSaveLocations(ctx, "", 10, 0)
	if err != nil || totalAll != 2 || len(all) != 2 {
		t.Errorf("SearchGameSaveLocations empty: total=%d len=%d err=%v", totalAll, len(all), err)
	}
}

func TestSQLite_ListAuditLogByUser(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u1, _ := st.CreateUser(ctx, "alice", "h")
	u2, _ := st.CreateUser(ctx, "bob", "h")
	_ = st.AppendAudit(ctx, u1, "alice", "login", "", "")
	_ = st.AppendAudit(ctx, u2, "bob", "login", "", "")
	rows, err := st.ListAuditLogByUser(ctx, u1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ActorUsername != "alice" {
		t.Errorf("ListAuditLogByUser: %+v", rows)
	}
}

func TestSQLite_ClientUserID(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	userID, _ := st.CreateUser(ctx, "u", "h")
	token, err := st.RegisterClient(ctx, userID, "pc", "linux")
	if err != nil || token == "" {
		t.Fatal(err)
	}
	_, clientID, _, _, err := st.ClientByToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := st.ClientUserID(ctx, clientID)
	if err != nil || owner != userID {
		t.Errorf("ClientUserID: owner=%q err=%v", owner, err)
	}
}

func TestSQLite_ListSaveSummariesFiltered(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	userID, _ := st.CreateUser(ctx, "u", "h")
	_ = st.UpsertGameSaveLocations(ctx, []types.GameSaveLocation{
		{GameID: "g1", PCGWPageID: 1, GameTitle: "Zelda", Platform: "windows", PathTemplate: "p", Source: "pcgw"},
	})
	_ = st.UpsertSave(ctx, userID, "g1", "key1", []byte("data"))
	_ = st.UpsertSave(ctx, userID, "g2", "key2", []byte("data"))
	all, err := st.ListSaveSummaries(ctx, userID)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListSaveSummaries: %d err=%v", len(all), err)
	}
	filtered, err := st.ListSaveSummariesFiltered(ctx, userID, "Zelda")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].GameTitle != "Zelda" {
		t.Errorf("ListSaveSummariesFiltered: %+v", filtered)
	}
}

func TestSQLite_TokenHashLookup(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	userID, _ := st.CreateUser(ctx, "u", "h")
	plaintext, err := st.RegisterClient(ctx, userID, "pc", "linux")
	if err != nil || plaintext == "" {
		t.Fatal(err)
	}
	uid, _, _, _, err := st.ClientByToken(ctx, plaintext)
	if err != nil || uid != userID {
		t.Fatalf("ClientByToken with plaintext: uid=%s err=%v", uid, err)
	}
	_, _, _, _, err = st.ClientByToken(ctx, "wrong-token")
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
}

func TestSQLite_EncryptionFlag(t *testing.T) {
	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	userID, _ := st.CreateUser(ctx, "u", "h")

	enabled, err := st.IsEncryptionEnabled(ctx, userID)
	if err != nil || enabled {
		t.Fatalf("expected encryption disabled by default, got %v err=%v", enabled, err)
	}
	if err := st.SetEncryptionEnabled(ctx, userID, true); err != nil {
		t.Fatal(err)
	}
	enabled, err = st.IsEncryptionEnabled(ctx, userID)
	if err != nil || !enabled {
		t.Fatalf("expected encryption enabled, got %v err=%v", enabled, err)
	}

	_, err = st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("cipher"), &SaveMeta{Encrypted: true})
	if err != nil {
		t.Fatal(err)
	}
	blob, err := st.GetSave(ctx, userID, "g1", "pk1")
	if err != nil || blob == nil || !blob.Encrypted {
		t.Fatalf("GetSave encrypted: %+v err=%v", blob, err)
	}
	summaries, err := st.ListSaveSummaries(ctx, userID)
	if err != nil || len(summaries) != 1 || !summaries[0].Encrypted {
		t.Fatalf("ListSaveSummaries encrypted: %+v err=%v", summaries, err)
	}
}
