package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
)

var (
	updateMu          sync.Mutex
	pendingUpdate     *UpdateInfo
	updateInProgress  bool
	lastUpdateCheck   time.Time
	updateCheckPeriod = 24 * time.Hour
)

func (c *TrayController) wireUpdateMenu() {
	c.mVersion = systray.AddMenuItem(versionMenuTitle(), "Installed client version")
	c.mVersion.Disable()
	c.mCheckUpdate = systray.AddMenuItem("Check for updates...", "Check GitHub for a newer client")
	c.mApplyUpdate = systray.AddMenuItem("", "Download and install update")
	c.mApplyUpdate.Hide()
	c.mApplyUpdate.Disable()
}

func versionMenuTitle() string {
	title := fmt.Sprintf("Version: %s", Version)
	if len(BuildDate) >= 10 {
		title += fmt.Sprintf(" (%s)", BuildDate[:10])
	}
	return title
}

func (c *TrayController) startUpdateHandlers() {
	go func() {
		for range c.mCheckUpdate.ClickedCh {
			c.runUpdateCheck(true)
		}
	}()
	go func() {
		for range c.mApplyUpdate.ClickedCh {
			c.runUpdateApply()
		}
	}()
	go c.startUpdateLoop()
}

func (c *TrayController) startUpdateLoop() {
	time.Sleep(30 * time.Second)
	c.runUpdateCheck(false)
	ticker := time.NewTicker(updateCheckPeriod)
	defer ticker.Stop()
	for range ticker.C {
		c.runUpdateCheck(false)
	}
}

func (c *TrayController) runUpdateCheck(manual bool) {
	if c.mCheckUpdate == nil {
		return
	}
	updateMu.Lock()
	if updateInProgress {
		updateMu.Unlock()
		return
	}
	if !manual && !lastUpdateCheck.IsZero() && time.Since(lastUpdateCheck) < updateCheckPeriod {
		updateMu.Unlock()
		return
	}
	updateInProgress = true
	updateMu.Unlock()

	defer func() {
		updateMu.Lock()
		updateInProgress = false
		lastUpdateCheck = time.Now()
		updateMu.Unlock()
	}()

	cfg := c.cfg()
	repo := ""
	if cfg != nil {
		repo = strings.TrimSpace(cfg.UpdateRepo)
	}
	info := CheckForUpdate(repo)
	updateMu.Lock()
	pendingUpdate = info
	updateMu.Unlock()

	if info == nil {
		if manual {
			_ = beeep.Notify("GSBS", "You are running the latest version.", "")
		}
		c.setUpdateMenuVisible(nil)
		return
	}
	log.Printf("update: available %s", info.Tag)
	c.setUpdateMenuVisible(info)
	if manual {
		_ = beeep.Notify("GSBS", fmt.Sprintf("Update available: %s", info.Tag), "")
	} else {
		_ = beeep.Notify("GSBS", fmt.Sprintf("Update available: %s — open tray to install", info.Tag), "")
	}
}

func (c *TrayController) runUpdateApply() {
	updateMu.Lock()
	info := pendingUpdate
	updateMu.Unlock()
	if info == nil {
		return
	}
	c.mApplyUpdate.SetTitle("Downloading update...")
	c.mApplyUpdate.Disable()
	path, err := DownloadUpdate(info)
	if err != nil {
		log.Printf("update: download failed: %v", err)
		_ = beeep.Alert("GSBS", fmt.Sprintf("Update download failed: %v", err), "")
		c.setUpdateMenuVisible(info)
		return
	}
	if err := ApplyUpdate(path); err != nil {
		log.Printf("update: apply failed: %v", err)
		_ = beeep.Alert("GSBS", fmt.Sprintf("Update failed: %v", err), "")
		c.setUpdateMenuVisible(info)
		return
	}
	systray.Quit()
}

func (c *TrayController) setUpdateMenuVisible(info *UpdateInfo) {
	if c.mApplyUpdate == nil {
		return
	}
	if info == nil {
		c.mApplyUpdate.Hide()
		c.mApplyUpdate.Disable()
		return
	}
	c.mApplyUpdate.SetTitle(fmt.Sprintf("Install update %s...", info.Tag))
	c.mApplyUpdate.Show()
	c.mApplyUpdate.Enable()
}
