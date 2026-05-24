//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const autostartDesktopName = "gsbs-client.desktop"

func autostartDesktopPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", autostartDesktopName), nil
}

// RunAtStartupEnabled reports whether the XDG autostart entry exists.
func RunAtStartupEnabled() bool {
	path, err := autostartDesktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// SetRunAtStartup creates or removes the XDG autostart desktop entry.
func SetRunAtStartup(enabled bool) error {
	path, err := autostartDesktopPath()
	if err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("abs path: %w", err)
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=GSBS
Comment=Game Sync and Backup Service
Exec=%q --minimized
Icon=gsbs-client
Terminal=false
Categories=Utility;
StartupNotify=false
Hidden=false
X-GNOME-Autostart-enabled=true
`, exe)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
