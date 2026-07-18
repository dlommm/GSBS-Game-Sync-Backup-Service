//go:build linux

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
	systray.Run(onReadyLinux, onExitLinux)
}

func onExitLinux() {
	FlushActivityNow()
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		syncCancel()
	}
}

func onReadyLinux() {
	systray.SetIcon(IconIdle())
	systray.SetTitle("GSBS")
	systray.SetTooltip(lastSyncTooltip())

	trayCtrl = NewTrayController(TrayPlatform{
		OpenConfig:     openConfigLinux,
		OpenDataFolder: openDataFolderLinux,
		OpenLog:        openLogLinux,
		HasNativeLogin: false,
	})
	SetupTraySyncCallbacks(trayCtrl)
	trayCtrl.Run()
}

func openLogLinux() {
	path := ClientLogPath()
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: open log: %v", err)
		notifyActionError("Open log", err)
	}
}

func openDataFolderLinux() {
	path := ClientDataDir()
	_ = os.MkdirAll(path, 0755)
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: open data: %v", err)
		notifyActionError("Open data folder", err)
	}
}

func openConfigLinux() {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs", "config.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig(path)
		_ = saveConfig(cfg)
	}
	// Non-blocking desktop handler, like the log/data-folder helpers and the
	// other platforms. The old $EDITOR path inherited the tray's stdio: a tray
	// launched from autostart has no TTY, so a terminal editor (vim/nano — the
	// common $EDITOR values) opened nowhere and the menu click blocked forever.
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: xdg-open config: %v", err)
		notifyActionError("Open config", err)
	}
}

func runLoginDialogProcess() {
	runLogin()
}
