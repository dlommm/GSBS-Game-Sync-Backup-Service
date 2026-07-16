//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
)

const autostartDesktopName = "gsbs-client.desktop"

func autostartDesktopPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", autostartDesktopName), nil
}

// portalAutostartPath is where the Background portal writes its autostart
// entry (named after the Flatpak app ID, not our legacy file name).
func portalAutostartPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", flatpakAppID+".desktop"), nil
}

// RunAtStartupEnabled reports whether an autostart entry exists (either the
// portal-written app-id file or our legacy direct-write file).
func RunAtStartupEnabled() bool {
	if isFlatpak() {
		if p, err := portalAutostartPath(); err == nil {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	path, err := autostartDesktopPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// setAutostartViaPortal asks the org.freedesktop.portal.Background portal to
// manage the autostart entry — the supported mechanism for sandboxed apps
// (some desktops filter autostart files authored directly by a sandbox).
func setAutostartViaPortal(enabled bool) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	// Shared connection — do not Close.
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	defer conn.RemoveSignal(ch)
	matchOpts := []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	}
	if err := conn.AddMatchSignal(matchOpts...); err != nil {
		return fmt.Errorf("match signal: %w", err)
	}
	defer func() { _ = conn.RemoveMatchSignal(matchOpts...) }()

	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(fmt.Sprintf("gsbs%d", time.Now().UnixNano())),
		"autostart":    dbus.MakeVariant(enabled),
		"commandline":  dbus.MakeVariant([]string{"gsbs-client", "--minimized"}),
	}
	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	var handle dbus.ObjectPath
	if err := obj.Call("org.freedesktop.portal.Background.RequestBackground", 0, "", options).Store(&handle); err != nil {
		return fmt.Errorf("RequestBackground: %w", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case sig := <-ch:
			if sig == nil || sig.Path != handle || len(sig.Body) < 1 {
				continue
			}
			if code, ok := sig.Body[0].(uint32); ok && code != 0 {
				return fmt.Errorf("background portal declined the request (response %d)", code)
			}
			return nil
		case <-deadline:
			return fmt.Errorf("background portal did not respond within 5s")
		}
	}
}

// SetRunAtStartup creates or removes the XDG autostart desktop entry. Under
// Flatpak it goes through the Background portal first (falling back to the
// legacy direct write, which still works on Deck/most desktops via the home
// filesystem grant).
func SetRunAtStartup(enabled bool) error {
	if isFlatpak() {
		if err := setAutostartViaPortal(enabled); err == nil {
			// Portal owns the app-id-named entry now; drop our legacy file so
			// disable/enable state can't split across two entries.
			if legacy, perr := autostartDesktopPath(); perr == nil {
				_ = os.Remove(legacy)
			}
			return nil
		} else {
			log.Printf("autostart: background portal unavailable (%v); falling back to direct autostart entry", err)
		}
	}
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
	// In a Flatpak sandbox the bare executable path won't relaunch the app on
	// the host session; the host must run `flatpak run <app-id>`. Outside the
	// sandbox, use the absolute executable path.
	var execLine string
	if isFlatpak() {
		execLine = "flatpak run " + flatpakAppID + " --minimized"
	} else if ap := os.Getenv("APPIMAGE"); ap != "" {
		// Inside an AppImage os.Executable() is the ephemeral FUSE mount
		// (/tmp/.mount_XXXX/...), gone after exit — autostart entries written
		// from it pointed at nothing on the next boot. $APPIMAGE is the real
		// outer file.
		execLine = fmt.Sprintf("%q --minimized", ap)
	} else {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("executable path: %w", err)
		}
		exe, err = filepath.Abs(exe)
		if err != nil {
			return fmt.Errorf("abs path: %w", err)
		}
		execLine = fmt.Sprintf("%q --minimized", exe)
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=GSBS
Comment=Game Sync and Backup Service
Exec=%s
Icon=%s
Terminal=false
Categories=Game;Utility;
StartupNotify=false
Hidden=false
X-GNOME-Autostart-enabled=true
`, execLine, flatpakAppID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
