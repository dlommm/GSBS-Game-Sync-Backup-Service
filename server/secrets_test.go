package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The auto-generated session secret is strong, persisted, and stable across
// restarts (so sessions/CSRF tokens survive a container restart).
func TestLoadOrCreateSessionSecret(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gsbs.db")

	s1, err := loadOrCreateSessionSecret(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(s1) < 32 {
		t.Fatalf("secret too short: %d chars", len(s1))
	}
	if err := checkSessionSecretStrength(s1); err != nil {
		t.Fatalf("generated secret fails the strength check: %v", err)
	}

	// Second call returns the SAME secret (persisted).
	s2, err := loadOrCreateSessionSecret(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatal("secret changed between calls; must be stable")
	}

	// Stored 0600 in gsbs-keys/ (Unix permission bits; Windows doesn't model them).
	keyPath := filepath.Join(dir, "gsbs-keys", "session.secret")
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file perms = %v, want 0600", fi.Mode().Perm())
	}

	// A too-short hand-edited file is regenerated, not trusted.
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	s3, err := loadOrCreateSessionSecret(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(s3) < 32 || s3 == "short" {
		t.Fatalf("weak stored secret not regenerated: %q", s3)
	}
}
