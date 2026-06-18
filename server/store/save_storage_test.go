package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLite_FilesystemSave(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GSBS_SAVE_ROOT", root)

	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, userID)); err != nil {
		t.Fatalf("user storage dir: %v", err)
	}

	content := []byte("save-bytes")
	rel := "game1/saves/slot.dat"
	_, err = st.UpsertSaveWithMeta(ctx, userID, "game1", "pk1", content, &SaveMeta{RelativePath: rel})
	if err != nil {
		t.Fatalf("UpsertSaveWithMeta: %v", err)
	}

	blob, err := st.GetSave(ctx, userID, "game1", "pk1")
	if err != nil || blob == nil {
		t.Fatalf("GetSave: %+v err=%v", blob, err)
	}
	if string(blob.Content) != string(content) {
		t.Fatalf("content = %q", blob.Content)
	}

	s := st.(*sqliteStore)
	var dbContent []byte
	var storagePath string
	err = s.db.QueryRow(
		`SELECT content, storage_path FROM saves WHERE user_id = ? AND game_id = ? AND path_key = ?`,
		userID, "game1", "pk1",
	).Scan(&dbContent, &storagePath)
	if err != nil {
		t.Fatalf("query save row: %v", err)
	}
	if len(dbContent) != 0 {
		t.Error("expected NULL content in DB when using filesystem")
	}
	if storagePath == "" {
		t.Fatal("expected storage_path in DB")
	}
	if _, err := os.Stat(storagePath); err != nil {
		t.Fatalf("save file on disk: %v", err)
	}

	if err := st.DeleteSave(ctx, userID, "game1", "pk1"); err != nil {
		t.Fatalf("DeleteSave: %v", err)
	}
	if _, err := os.Stat(storagePath); !os.IsNotExist(err) {
		t.Errorf("save file should be removed, stat err=%v", err)
	}
}

func TestAtomicWriteFileSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slot.dat")

	// Fresh write.
	if err := atomicWriteFileSync(path, []byte("v1"), 0o640); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "v1" {
		t.Fatalf("read v1 = %q err=%v", got, err)
	}
	if fi, _ := os.Stat(path); fi != nil && fi.Mode().Perm() != 0o640 {
		t.Errorf("perm = %v, want 0640", fi.Mode().Perm())
	}

	// Overwrite is atomic: the destination ends up as the complete new bytes.
	if err := atomicWriteFileSync(path, []byte("version-two-longer"), 0o640); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "version-two-longer" {
		t.Fatalf("read v2 = %q", got)
	}

	// No temp files are left behind in the directory after success.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestSQLite_MultiUserIsolation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GSBS_SAVE_ROOT", root)

	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	u1, err := st.CreateUser(ctx, "alice", "h1")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := st.CreateUser(ctx, "bob", "h2")
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.UpsertSaveWithMeta(ctx, u1, "game1", "pk1", []byte("alice-save"), &SaveMeta{RelativePath: "saves/a.sav"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertSaveWithMeta(ctx, u2, "game1", "pk1", []byte("bob-save"), &SaveMeta{RelativePath: "saves/a.sav"})
	if err != nil {
		t.Fatal(err)
	}

	b1, err := st.GetSave(ctx, u1, "game1", "pk1")
	if err != nil || string(b1.Content) != "alice-save" {
		t.Fatalf("user1 save: %+v err=%v", b1, err)
	}
	b2, err := st.GetSave(ctx, u2, "game1", "pk1")
	if err != nil || string(b2.Content) != "bob-save" {
		t.Fatalf("user2 save: %+v err=%v", b2, err)
	}
	if filepath.Join(root, u1) == filepath.Join(root, u2) {
		t.Fatal("user storage dirs must differ")
	}
}
