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
