//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"fyne.io/systray"
	"github.com/skratchdot/open-golang/open"
)

var trayCtrl *TrayController

func runTray() {
	release := acquireSingleInstance()
	if release == nil {
		notifyAlreadyRunning()
		os.Exit(0)
	}
	defer release()
	systray.Run(onReadyWindows, onExitWindows)
}

func onReadyWindows() {
	systray.SetIcon(IconIdle())
	systray.SetTitle("GSBS")
	systray.SetTooltip(lastSyncTooltip())

	trayCtrl = NewTrayController(TrayPlatform{
		OpenConfig:      openConfigWindows,
		OpenDataFolder:  openDataFolderWindows,
		OpenLog:         openLogWindows,
		NativeLogin:     showLoginDialog,
		LoginConsole:    runLoginConsoleWindows,
		HasNativeLogin:  true,
		HasConsoleLogin: true,
	})
	SetupTraySyncCallbacks(trayCtrl)
	trayCtrl.Run()
}

func onExitWindows() {
	FlushActivityNow()
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		syncCancel()
	}
}

func runFirstTimeSetupIfNeeded() {
	cfg, _ := loadConfig()
	if cfg != nil && cfg.ServerURL != "" && cfg.Token != "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "login")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x10}
	cmd.Run()
}

func runLoginDialogProcess() {
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	fmt.Println("Opening login window...")
	newCfg, err := showLoginDialog(cfg.ServerURL, cfg.ClientName)
	if err != nil || newCfg == nil {
		fmt.Println("Login cancelled or failed.")
		os.Exit(1)
	}
	fmt.Println("Login successful.")
	os.Exit(0)
}

func openConfigWindows() {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs", "config.json")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig(path)
		_ = saveConfig(cfg)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor != "" {
		parts := strings.Fields(editor)
		if len(parts) > 0 {
			cmd := exec.Command(parts[0], append(parts[1:], path)...)
			_ = cmd.Start()
			return
		}
	}
	if codePath, err := exec.LookPath("code"); err == nil {
		_ = exec.Command(codePath, path).Start()
		return
	}
	_ = open.Run(path)
}

func openDataFolderWindows() {
	dir := ClientDataDir()
	if dir != "" {
		_ = exec.Command("explorer", dir).Start()
	}
}

func openLogWindows() {
	path := ClientLogPath()
	if path != "" {
		_ = open.Run(path)
	}
}

func runLoginConsoleWindows() {
	exe, err := os.Executable()
	if err != nil {
		log.Println("login (console):", err)
		return
	}
	cmd := exec.Command(exe, "login")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x10}
	_ = cmd.Run()
}
