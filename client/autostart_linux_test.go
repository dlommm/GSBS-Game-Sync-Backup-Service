//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An AppImage's os.Executable() is the ephemeral FUSE mount, gone after exit;
// the autostart Exec= line must use the persistent $APPIMAGE path instead or
// "Run at startup" silently breaks on the next boot.
func TestSetRunAtStartupUsesAppImagePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	appimage := filepath.Join(home, "Apps", "GSBS.AppImage")
	t.Setenv("APPIMAGE", appimage)

	if err := SetRunAtStartup(true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "autostart", autostartDesktopName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), appimage) {
		t.Fatalf("autostart Exec must reference $APPIMAGE, got:\n%s", data)
	}
	if strings.Contains(string(data), "/tmp/.mount") {
		t.Fatal("autostart must not reference the FUSE mount path")
	}
}
