package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/server/job"
	"github.com/gsbs/gsbs/server/store"
)

// Full disaster-recovery drill: back up a live store, restore the archive
// into a fresh directory, reopen the restored database, and verify the user,
// the save content, and the working TOTP key file all survived.
func TestRestore_DisasterRecoveryDrill(t *testing.T) {
	srcDir := t.TempDir()
	t.Setenv("GSBS_SAVE_ROOT", filepath.Join(srcDir, "gamesaves"))
	st, err := store.NewSQLite(filepath.Join(srcDir, "gsbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "survivor", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", []byte("save-me"), &store.SaveMeta{RelativePath: "saves/slot.dat"}); err != nil {
		t.Fatal(err)
	}
	const seed = "JBSWY3DPEHPK3PXP"
	if err := st.SetTOTPSecret(ctx, userID, seed); err != nil {
		t.Fatal(err)
	}

	res, err := job.RunBackup(ctx, st, job.BackupConfig{Dir: filepath.Join(srcDir, "backups"), Keep: 3})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	// Restore into a brand-new directory (the "new server").
	destDir := t.TempDir()
	if err := runRestore([]string{"--data-dir", destDir, res.Path}); err != nil {
		t.Fatalf("runRestore: %v", err)
	}
	// Refusal without --force on a second run.
	if err := runRestore([]string{"--data-dir", destDir, res.Path}); err == nil {
		t.Fatal("second restore without --force must refuse")
	}
	if err := runRestore([]string{"--data-dir", destDir, "--force", res.Path}); err != nil {
		t.Fatalf("restore --force: %v", err)
	}

	// Reopen the restored world: save root + key file must work.
	t.Setenv("GSBS_SAVE_ROOT", filepath.Join(destDir, "gamesaves"))
	t.Setenv("GSBS_TOTP_KEY_FILE", filepath.Join(destDir, "gsbs-keys", "totp.key"))
	restored, err := store.NewSQLite(filepath.Join(destDir, "gsbs.db"))
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	t.Cleanup(func() { _ = restored.Close() })

	gotID, _, err := restored.UserByUsername(ctx, "survivor")
	if err != nil || gotID != userID {
		t.Fatalf("restored user: id=%q err=%v", gotID, err)
	}
	blob, err := restored.GetSave(ctx, userID, "g1", "pk1")
	if err != nil || string(blob.Content) != "save-me" {
		t.Fatalf("restored save: %v err=%v", blob, err)
	}
	gotSeed, err := restored.GetTOTPSecret(ctx, userID)
	if err != nil || gotSeed != seed {
		t.Fatalf("restored TOTP secret: %q err=%v (key file missing?)", gotSeed, err)
	}
	if fi, err := os.Stat(filepath.Join(destDir, "gsbs-keys")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("gsbs-keys perms: %v err=%v", fi, err)
	}
}
