package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Set seals the secret at rest; Get decrypts it; the sealed value survives a
// store reopen because the key file persists beside the database.
func TestTOTPSecret_EncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gsbs.db")

	st, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}
	const seed = "JBSWY3DPEHPK3PXP"
	if err := st.SetTOTPSecret(ctx, userID, seed); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := st.(*sqliteStore).db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, encPrefixV1) {
		t.Fatalf("stored secret is not sealed: %q", stored)
	}
	if strings.Contains(stored, seed) {
		t.Fatal("plaintext seed leaked into the database column")
	}

	got, err := st.GetTOTPSecret(ctx, userID)
	if err != nil || got != seed {
		t.Fatalf("Get = %q err=%v, want %q", got, err, seed)
	}
	_ = st.Close()

	// Reopen: the key file in gsbs-keys/ must decrypt the same value.
	st2, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	got, err = st2.GetTOTPSecret(ctx, userID)
	if err != nil || got != seed {
		t.Fatalf("Get after reopen = %q err=%v, want %q", got, err, seed)
	}
}

// Legacy plaintext rows (pre-4.0) read through unchanged until migrated.
func TestTOTPSecret_LegacyPlaintextPassthrough(t *testing.T) {
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
	s := st.(*sqliteStore)
	if _, err := s.db.Exec(`UPDATE users SET totp_secret = 'LEGACYPLAINTEXT' WHERE id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTOTPSecret(ctx, userID)
	if err != nil || got != "LEGACYPLAINTEXT" {
		t.Fatalf("legacy passthrough = %q err=%v", got, err)
	}
}

// Migration step 23 seals legacy rows and is idempotent on sealed ones.
func TestStepEncryptTOTPSecrets(t *testing.T) {
	dir := t.TempDir()
	st, err := NewSQLite(filepath.Join(dir, "gsbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	s := st.(*sqliteStore)

	u1, _ := st.CreateUser(ctx, "legacy", "h")
	u2, _ := st.CreateUser(ctx, "sealed", "h")
	if _, err := s.db.Exec(`UPDATE users SET totp_secret = 'PLAINSEED' WHERE id = ?`, u1); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTOTPSecret(ctx, u2, "ALREADYSEALED"); err != nil {
		t.Fatal(err)
	}
	var sealedBefore string
	_ = s.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, u2).Scan(&sealedBefore)

	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.stepEncryptTOTPSecrets(tx); err != nil {
		t.Fatalf("step: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var v1, v2 string
	_ = s.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, u1).Scan(&v1)
	_ = s.db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, u2).Scan(&v2)
	if !strings.HasPrefix(v1, encPrefixV1) {
		t.Fatalf("legacy row not sealed: %q", v1)
	}
	if v2 != sealedBefore {
		t.Fatal("already-sealed row must be untouched")
	}
	if got, err := st.GetTOTPSecret(ctx, u1); err != nil || got != "PLAINSEED" {
		t.Fatalf("migrated secret = %q err=%v", got, err)
	}
}
