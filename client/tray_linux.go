//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/gsbs/gsbs/client/sync"
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
	OnDiscoveryResult = func(newGames int) {
		_ = beeep.Notify("GSBS", fmt.Sprintf("Discovered %d new game(s)", newGames), "")
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			systray.SetTooltip(lastSyncTooltip())
		}
	}()

	mServer := systray.AddMenuItem("Server: (not set)", "Current server URL")
	mServer.Disable()
	mLastSync := systray.AddMenuItem("Last sync: —", "Last sync time")
	mLastSync.Disable()
	mOutbox := systray.AddMenuItem("Pending uploads: 0", "Offline queue")
	mOutbox.Disable()
	mConflicts := systray.AddMenuItem("Conflicts: 0", "Sync conflicts")
	mConflicts.Disable()
	mOpenServer := systray.AddMenuItem("Open server in browser", "Open server URL in default browser")
	mLogin := systray.AddMenuItem("Login / Setup...", "Open setup wizard in browser")
	mEditConfig := systray.AddMenuItem("Edit config file", "Open config in default editor")
	mSyncNow := systray.AddMenuItem("Sync now", "Run a sync immediately")
	mRefreshManifest := systray.AddMenuItem("Refresh manifest", "Re-fetch game save locations from server")
	mPause := systray.AddMenuItem("Pause sync", "Pause automatic sync")
	mOpenLog := systray.AddMenuItem("View log", "Open client log file")
	mOpenData := systray.AddMenuItem("Open data folder", "Open GSBS config folder")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit GSBS")

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	updateServerLabelLinux(mServer, cfg.ServerURL)
	updateLastSyncMenuLinux(mLastSync)
	updateStatusMenusLinux(mOutbox, mConflicts)
	var currentCfg *config
	currentCfg = cfg
	SyncPaused.Store(cfg.SyncPaused)
	if cfg.SyncPaused {
		mPause.SetTitle("Resume sync")
	}
	setupURL := StartSetupServer()
	restartSync(currentCfg)

	go func() {
		statusTicker := time.NewTicker(15 * time.Second)
		defer statusTicker.Stop()
		for range statusTicker.C {
			updateLastSyncMenuLinux(mLastSync)
			updateStatusMenusLinux(mOutbox, mConflicts)
		}
	}()

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
			case <-mPause.ClickedCh:
				syncMu.Lock()
				paused := !SyncPaused.Load()
				SyncPaused.Store(paused)
				if currentCfg != nil {
					currentCfg.SyncPaused = paused
					_ = saveConfig(currentCfg)
				}
				syncMu.Unlock()
				if paused {
					mPause.SetTitle("Resume sync")
				} else {
					mPause.SetTitle("Pause sync")
					triggerSyncNow()
				}
			case <-mOpenLog.ClickedCh:
				openLogLinux()
			case <-mOpenData.ClickedCh:
				openDataFolderLinux()
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

func updateLastSyncMenuLinux(m *systray.MenuItem) {
	at, err := getLastSync()
	if at.IsZero() {
		m.SetTitle("Last sync: —")
		return
	}
	status := "ok"
	if err != nil {
		status = "failed"
	}
	m.SetTitle(fmt.Sprintf("Last sync: %s (%s)", at.Format("15:04"), status))
}

func updateStatusMenusLinux(outbox, conflicts *systray.MenuItem) {
	n := sync.OutboxCount()
	outbox.SetTitle(fmt.Sprintf("Pending uploads: %d", n))
	c := sync.ConflictCount()
	conflicts.SetTitle(fmt.Sprintf("Conflicts: %d", c))
}

func openLogLinux() {
	path := ClientLogPath()
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: open log: %v", err)
	}
}

func openDataFolderLinux() {
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "gsbs")
	_ = os.MkdirAll(path, 0755)
	if err := exec.Command("xdg-open", path).Start(); err != nil {
		log.Printf("tray: open data: %v", err)
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
