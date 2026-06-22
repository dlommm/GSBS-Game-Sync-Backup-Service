package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestExportDiagnosticsStripsSecrets(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GSBS_TOKEN_STORE", "file") // keep secrets in the file to prove they're stripped from the zip
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg"))

	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A log file and a config with secrets.
	if err := os.WriteFile(filepath.Join(dir, "gsbs.log"), []byte("hello log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := blankConfig()
	cfg.ServerURL = "https://example.test"
	cfg.Token = "SECRET-TOKEN"
	cfg.EncryptionPassphrase = "SECRET-PASS"
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	zipPath, err := ExportDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	found := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		found[f.Name] = string(data)
	}

	if _, ok := found["gsbs.log"]; !ok {
		t.Error("zip missing gsbs.log")
	}
	cfgJSON, ok := found["config.sanitized.json"]
	if !ok {
		t.Fatal("zip missing config.sanitized.json")
	}
	if strings.Contains(cfgJSON, "SECRET-TOKEN") || strings.Contains(cfgJSON, "SECRET-PASS") {
		t.Errorf("secrets leaked into diagnostics bundle:\n%s", cfgJSON)
	}
	if !strings.Contains(cfgJSON, "example.test") {
		t.Error("sanitized config should still contain non-secret fields")
	}
	if _, ok := found["about.txt"]; !ok {
		t.Error("zip missing about.txt")
	}
}
