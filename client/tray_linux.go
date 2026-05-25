//go:build linux

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/getlantern/systray"
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
	}
}

func openDataFolderLinux() {
	path := ClientDataDir()
	_ = os.MkdirAll(path, 0755)
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: open data: %v", err)
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
	editor := os.Getenv("EDITOR")
	if editor != "" {
		cmd := exec.Command(editor, path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("tray: open config: %v", err)
		}
		return
	}
	if err := exec.Command("xdg-open", path).Run(); err != nil {
		log.Printf("tray: xdg-open config: %v", err)
	}
}

func runLoginDialogProcess() {
	runLogin()
}
