package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileSecretRoundTripEncrypted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))

	if err := fileSecretSet("token", "supersecret-abc123"); err != nil {
		t.Fatal(err)
	}
	if v, ok := fileSecretGet("token"); !ok || v != "supersecret-abc123" {
		t.Fatalf("get = %q %v, want the secret and true", v, ok)
	}

	// The secret must be encrypted at rest — never present verbatim on disk.
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "gsbs", "secrets.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("supersecret-abc123")) {
		t.Fatal("secrets.enc contains the plaintext secret")
	}
	// The wrap key lives in a separate 0600 file, so secrets.enc alone is useless.
	// Windows has no POSIX permission bits (Stat reports 0666); the file is
	// protected there by the profile directory's ACL instead.
	if info, err := os.Stat(filepath.Join(dir, "gsbs", "secret.key")); err != nil {
		t.Fatalf("secret.key missing: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key mode = %v, want 0600", info.Mode().Perm())
	}

	fileSecretDelete("token")
	if _, ok := fileSecretGet("token"); ok {
		t.Fatal("delete did not remove the secret")
	}
}
