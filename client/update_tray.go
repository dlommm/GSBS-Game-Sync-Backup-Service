package main

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/gen2brain/beeep"
)

var (
	updateMu          sync.Mutex
	pendingUpdate     *UpdateInfo
	updateInProgress  bool
	lastUpdateCheck   time.Time
	updateCheckPeriod = 24 * time.Hour
)

func (c *TrayController) wireUpdateMenu(parent *systray.MenuItem) {
	addItem := func(title, tooltip string) *systray.MenuItem {
		if parent != nil {
			return parent.AddSubMenuItem(title, tooltip)
		}
		return systray.AddMenuItem(title, tooltip)
	}
	c.mVersion = addItem(versionMenuTitle(), "Installed client version")
	c.mVersion.Disable()
	if isFlatpak() {
		// The store / `flatpak update` owns updates in the sandbox.
		c.mCheckUpdate = addItem("Updates managed by your software center", "Run 'flatpak update' or use your app store")
		c.mCheckUpdate.Disable()
		c.mApplyUpdate = addItem("", "")
		c.mApplyUpdate.Hide()
		c.mApplyUpdate.Disable()
		return
	}
	c.mCheckUpdate = addItem("Check for updates...", "Check GitHub for a newer client")
	c.mApplyUpdate = addItem("", "Download and install update")
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
	if isFlatpak() {
		return // updates are managed by the software center
	}
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
		if manual {
			_ = beeep.Notify("GSBS", "Update check already in progress.", "")
		}
		return
	}
	if !manual && !lastUpdateCheck.IsZero() && time.Since(lastUpdateCheck) < updateCheckPeriod {
		updateMu.Unlock()
		return
	}
	updateInProgress = true
	updateMu.Unlock()

	if manual {
		c.mCheckUpdate.SetTitle("Checking for updates…")
		c.mCheckUpdate.Disable()
	}

	defer func() {
		updateMu.Lock()
		updateInProgress = false
		lastUpdateCheck = time.Now()
		updateMu.Unlock()
		if manual {
			c.mCheckUpdate.SetTitle("Check for updates...")
			c.mCheckUpdate.Enable()
		}
	}()

	cfg := c.cfg()
	repo := ""
	if cfg != nil {
		repo = strings.TrimSpace(cfg.UpdateRepo)
	}
	result := CheckForUpdate(repo, manual)

	updateMu.Lock()
	pendingUpdate = result.Info
	updateMu.Unlock()

	switch result.Status {
	case "available":
		log.Printf("update: available %s", result.Info.Tag)
		c.setUpdateMenuVisible(result.Info)
		if manual {
			_ = beeep.Notify("GSBS", fmt.Sprintf("Update available: %s", result.Info.Tag), "")
		} else {
			_ = beeep.Notify("GSBS", fmt.Sprintf("Update available: %s — open tray to install", result.Info.Tag), "")
		}
	case "manual_download":
		log.Printf("update: available %s (manual download only: %s)", result.Info.Tag, result.Message)
		c.setUpdateMenuVisible(result.Info)
		_ = beeep.Notify("GSBS", fmt.Sprintf("Update %s available — open tray to get it from GitHub", result.Info.Tag), "")
	case "up_to_date":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "You are running the latest version.", "")
		}
	case "disabled":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "Update checks are disabled.", "")
		}
	case "metered_skip":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "Update check skipped on metered connection.", "")
		}
	case "flatpak":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "Updates are managed by your software center (flatpak update).", "")
		}
	case "network_error", "api_error":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", fmt.Sprintf("Update check failed: %s. See gsbs.log for details.", result.Message), "")
		}
	case "manifest_mismatch":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "No update found for this platform. See gsbs.log.", "")
		}
	case "unsupported_arch":
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "Auto-update not supported on this architecture.", "")
		}
	default:
		c.setUpdateMenuVisible(nil)
		if manual {
			_ = beeep.Notify("GSBS", "You are running the latest version.", "")
		}
	}
}

func (c *TrayController) runUpdateApply() {
	updateMu.Lock()
	info := pendingUpdate
	updateMu.Unlock()
	if info == nil {
		return
	}
	if info.Manual {
		c.openReleasePage()
		return
	}
	c.mApplyUpdate.SetTitle("Downloading update...")
	c.mApplyUpdate.Disable()
	path, err := DownloadUpdate(info)
	if err != nil {
		log.Printf("update: download failed: %v", err)
		_ = beeep.Alert("GSBS", fmt.Sprintf("Update download failed: %v", err), "")
		c.setUpdateMenuVisible(info)
		c.offerManualFallback()
		return
	}
	if err := ApplyUpdate(path); err != nil {
		log.Printf("update: apply failed: %v", err)
		_ = beeep.Alert("GSBS", fmt.Sprintf("Update failed: %v", err), "")
		c.setUpdateMenuVisible(info)
		c.offerManualFallback()
		return
	}
	systray.Quit()
}

// openReleasePage opens the GitHub releases page in the default browser.
func (c *TrayController) openReleasePage() {
	repo := ""
	if cfg := c.cfg(); cfg != nil {
		repo = strings.TrimSpace(cfg.UpdateRepo)
	}
	url := ReleasePageURL(repo)
	if err := openNative(url); err != nil {
		log.Printf("update: open release page: %v", err)
		_ = beeep.Notify("GSBS", "Get the latest release at "+url, "")
	}
}

// offerManualFallback opens the releases page on macOS when a self-update
// attempt fails, so the user can drag in the fresh DMG instead.
func (c *TrayController) offerManualFallback() {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = beeep.Notify("GSBS", "Opening the GitHub releases page so you can update manually.", "")
	c.openReleasePage()
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
	if info.Manual {
		c.mApplyUpdate.SetTitle(fmt.Sprintf("Get update %s from GitHub...", info.Tag))
	} else {
		c.mApplyUpdate.SetTitle(fmt.Sprintf("Install update %s...", info.Tag))
	}
	c.mApplyUpdate.Show()
	c.mApplyUpdate.Enable()
}
