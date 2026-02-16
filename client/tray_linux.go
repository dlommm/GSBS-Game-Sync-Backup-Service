//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/skratchdot/open-golang/open"
)

func runTray() {
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
	systray.SetTitle("GSBS")
	systray.SetTooltip(lastSyncTooltip())
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			systray.SetTooltip(lastSyncTooltip())
		}
	}()

	mServer := systray.AddMenuItem("Server: (not set)", "Current server URL")
	mServer.Disable()
	mOpenServer := systray.AddMenuItem("Open server in browser", "Open server URL in default browser")
	mLogin := systray.AddMenuItem("Login / Setup...", "Open setup page in browser")
	mEditConfig := systray.AddMenuItem("Edit config file", "Open config in default editor")
	mSyncNow := systray.AddMenuItem("Sync now", "Run a sync immediately")
	mRefreshManifest := systray.AddMenuItem("Refresh manifest", "Re-fetch game save locations from server")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit GSBS")

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	updateServerLabelLinux(mServer, cfg.ServerURL)
	var currentCfg *config
	currentCfg = cfg
	setupURL := StartSetupServer()
	restartSync(currentCfg)

	go func() {
		for {
			select {
			case <-mOpenServer.ClickedCh:
				syncMu.Lock()
				url := currentCfg.ServerURL
				syncMu.Unlock()
				if url != "" {
					open.Run(url)
				}
			case <-mLogin.ClickedCh:
				if url := GetSetupURL(); url != "" {
					open.Run(url)
				}
				go func() {
					time.Sleep(3 * time.Second)
					if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
						syncMu.Lock()
						currentCfg = reload
						syncMu.Unlock()
						updateServerLabelLinux(mServer, reload.ServerURL)
						restartSync(reload)
					}
				}()
			case <-mEditConfig.ClickedCh:
				openConfigInEditorLinux()
			case <-mSyncNow.ClickedCh:
				triggerSyncNow()
			case <-mRefreshManifest.ClickedCh:
				triggerManifestRefresh()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	if setupURL != "" && (currentCfg.ServerURL == "" || currentCfg.Token == "") {
		go func() {
			time.Sleep(800 * time.Millisecond)
			open.Run(setupURL)
		}()
	}
}

func updateServerLabelLinux(m *systray.MenuItem, url string) {
	if url == "" {
		m.SetTitle("Server: (not set) — click Login to connect")
		return
	}
	label := url
	if len(label) > 40 {
		label = label[:37] + "..."
	}
	m.SetTitle("Server: " + label)
}

func openConfigInEditorLinux() {
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
		fmt.Fprintf(os.Stderr, "Set EDITOR or install xdg-utils to open config.\n")
	}
}
