package store

import (
	"context"
	"os"
	"testing"
)

func TestRunIntegrityCheck(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GSBS_SAVE_ROOT", root)

	st, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}

	good := []byte("healthy-save-bytes")
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", good, &SaveMeta{RelativePath: "saves/a.dat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g2", "pk2", []byte("ciphertext"), &SaveMeta{RelativePath: "saves/b.dat", Encrypted: true}); err != nil {
		t.Fatal(err)
	}

	res, err := st.RunIntegrityCheck(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Checked != 1 || res.SkippedEncrypted != 1 || res.Problems() != 0 {
		t.Fatalf("clean run: %+v", res)
	}

	// Corrupt the blob file behind the store's back.
	s := st.(*sqliteStore)
	var storagePath string
	if err := s.db.QueryRow(`SELECT storage_path FROM saves WHERE game_id='g1'`).Scan(&storagePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storagePath, []byte("bit-rotted!"), 0o640); err != nil {
		t.Fatal(err)
	}

	res, err = st.RunIntegrityCheck(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mismatched != 1 {
		t.Fatalf("corruption not detected: %+v", res)
	}
	count, err := st.CountIntegrityFindings(ctx)
	if err != nil || count != 1 {
		t.Fatalf("findings count = %d err=%v", count, err)
	}
	findings, err := st.ListIntegrityFindings(ctx, 10)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings = %v err=%v", findings, err)
	}
	f := findings[0]
	if f.Kind != IntegrityKindHashMismatch || f.GameID != "g1" || f.Username != "u" || f.ExpectedHash == "" || f.ActualHash == "" {
		t.Fatalf("unexpected finding: %+v", f)
	}

	// Delete the file: the finding for the slot is replaced with missing_file.
	if err := os.Remove(storagePath); err != nil {
		t.Fatal(err)
	}
	if res, err = st.RunIntegrityCheck(ctx); err != nil || res.MissingFile != 1 {
		t.Fatalf("missing file: %+v err=%v", res, err)
	}
	findings, _ = st.ListIntegrityFindings(ctx, 10)
	if len(findings) != 1 || findings[0].Kind != IntegrityKindMissingFile {
		t.Fatalf("finding should be replaced, got %+v", findings)
	}

	// Heal: rewrite correct bytes; the finding clears on the next run.
	if err := os.WriteFile(storagePath, good, 0o640); err != nil {
		t.Fatal(err)
	}
	if res, err = st.RunIntegrityCheck(ctx); err != nil || res.Problems() != 0 {
		t.Fatalf("healed run: %+v err=%v", res, err)
	}
	if count, _ = st.CountIntegrityFindings(ctx); count != 0 {
		t.Fatalf("findings should clear after clean re-check, count=%d", count)
	}
}
