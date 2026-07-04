//go:build darwin

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"fyne.io/systray"
)

var trayCtrl *TrayController

func runTray() {
	release := acquireSingleInstance()
	if release == nil {
		notifyAlreadyRunning()
		os.Exit(0)
	}
	defer release()
	systray.Run(onReadyDarwin, onExitDarwin)
}

func onExitDarwin() {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		syncCancel()
	}
}

func onReadyDarwin() {
	systray.SetIcon(IconIdle())
	systray.SetTitle("")
	systray.SetTooltip(lastSyncTooltip())

	trayCtrl = NewTrayController(TrayPlatform{
		OpenConfig:     openConfigDarwin,
		OpenDataFolder: openDataFolderDarwin,
		OpenLog:        openLogDarwin,
		HasNativeLogin: false, // browser-based login, same as Linux
	})
	SetupTraySyncCallbacks(trayCtrl)
	trayCtrl.Run()
}

// openWithDefault opens a file or folder with the macOS default handler.
func openWithDefault(args ...string) error {
	return exec.Command("open", args...).Start() //nolint:gosec // G204: fixed OS utility on our own config/log/data paths
}

func openLogDarwin() {
	if err := openWithDefault(ClientLogPath()); err != nil {
		log.Printf("tray: open log: %v", err)
	}
}

func openDataFolderDarwin() {
	path := ClientDataDir()
	_ = os.MkdirAll(path, 0o755)
	if err := openWithDefault(path); err != nil {
		log.Printf("tray: open data: %v", err)
	}
}

func openConfigDarwin() {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs", "config.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig(path)
		_ = saveConfig(cfg)
	}
	// -t opens in the default text editor rather than a JSON-associated app.
	if err := openWithDefault("-t", path); err != nil {
		log.Printf("tray: open config: %v", err)
	}
}

// runLoginDialogProcess is a no-op on macOS: login is browser-based (there is
// no native Walk dialog). The tray's Login item opens the setup page.
func runLoginDialogProcess() {}

// runFirstTimeSetupIfNeeded is a no-op on macOS (browser setup flow).
func runFirstTimeSetupIfNeeded() {}
