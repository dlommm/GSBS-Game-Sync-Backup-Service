package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func isolateConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)                                  // darwin UserConfigDir base
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg")) // linux UserConfigDir
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "gsbs", "config.json")
}

// Keyring available: token is stored in the keyring and kept out of the file.
func TestSaveConfigKeyringStripsTokenFromFile(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GSBS_TOKEN_STORE", "")
	path := isolateConfig(t)

	c := blankConfig()
	c.ServerURL = "https://example.test"
	c.Token = "tok-abc"
	c.EncryptionPassphrase = "pass-xyz"
	if err := saveConfig(c); err != nil {
		t.Fatal(err)
	}
	if c.Token != "tok-abc" || c.EncryptionPassphrase != "pass-xyz" {
		t.Fatalf("saveConfig mutated in-memory secrets: token=%q pass=%q", c.Token, c.EncryptionPassphrase)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tok-abc") || strings.Contains(string(data), "pass-xyz") {
		t.Fatalf("secrets leaked to config file in keyring mode:\n%s", data)
	}
	if v, ok := secretGet(secretToken); !ok || v != "tok-abc" {
		t.Fatalf("keyring token = %q ok=%v", v, ok)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "tok-abc" || got.EncryptionPassphrase != "pass-xyz" {
		t.Fatalf("loadConfig secrets: token=%q pass=%q", got.Token, got.EncryptionPassphrase)
	}
}

// Keyring disabled: secrets stay in the (0600) file as a graceful fallback.
func TestSaveConfigFileFallback(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GSBS_TOKEN_STORE", "file")
	path := isolateConfig(t)

	c := blankConfig()
	c.ServerURL = "https://example.test"
	c.Token = "tok-file"
	if err := saveConfig(c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tok-file") {
		t.Fatalf("fallback should keep token in file:\n%s", data)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "tok-file" {
		t.Fatalf("loadConfig token = %q", got.Token)
	}
}

// Legacy on-disk token is migrated into the keyring and removed from the file.
func TestLoadConfigMigratesLegacyToken(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GSBS_TOKEN_STORE", "")
	path := isolateConfig(t)

	// Simulate an old config.json with the token written in plaintext.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"server_url":"https://example.test","token":"legacy-tok","watch_paths":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "legacy-tok" {
		t.Fatalf("loadConfig token = %q", got.Token)
	}
	if v, ok := secretGet(secretToken); !ok || v != "legacy-tok" {
		t.Fatalf("token not migrated to keyring: %q ok=%v", v, ok)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "legacy-tok") {
		t.Fatalf("legacy token not removed from file after migration:\n%s", data)
	}
}
