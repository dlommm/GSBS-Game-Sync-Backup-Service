package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gen2brain/beeep"
)

// ExportDiagnostics writes a zip of the client log(s), a secret-free copy of the
// config, and a build/runtime summary into the data folder, returning its path.
// Token and encryption passphrase are always stripped so the bundle is safe to
// share in a bug report.
func ExportDiagnostics() (string, error) {
	dir := ClientDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, fmt.Sprintf("gsbs-diagnostics-%s.zip", time.Now().Format("20060102-150405")))
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	addBytes := func(name string, data []byte) {
		if w, werr := zw.Create(name); werr == nil {
			_, _ = w.Write(data)
		}
	}

	// Logs (current + rotated), if present.
	for _, name := range []string{"gsbs.log", "gsbs.log.old"} {
		if data, rerr := os.ReadFile(filepath.Join(dir, name)); rerr == nil {
			addBytes(name, data)
		}
	}

	// Secret-free config snapshot.
	if cfg, lerr := loadConfig(); lerr == nil && cfg != nil {
		sanitized := *cfg
		sanitized.Token = ""
		sanitized.EncryptionPassphrase = ""
		if data, merr := json.MarshalIndent(&sanitized, "", "  "); merr == nil {
			addBytes("config.sanitized.json", data)
		}
	}

	// Build/runtime summary.
	summary := fmt.Sprintf("gsbs-client %s\nbuild: %s\ncommit: %s\nos: %s/%s\nflatpak: %v\n",
		Version, BuildDate, Commit, runtime.GOOS, runtime.GOARCH, isFlatpak())
	addBytes("about.txt", []byte(summary))

	return zipPath, nil
}

func notifyDiagnosticsSaved(path string) {
	_ = beeep.Notify("GSBS — diagnostics", "Saved to "+path, "")
}

func notifyDiagnosticsError(err error) {
	_ = beeep.Alert("GSBS — diagnostics", "Could not export diagnostics: "+err.Error(), "")
}
