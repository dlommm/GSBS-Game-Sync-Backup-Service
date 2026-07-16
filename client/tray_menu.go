package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/gen2brain/beeep"
)

const (
	maxSyncedGameSlots = 12
	maxDiscoveredSlots = 8
)

// TrayPlatform provides OS-specific tray actions.
type TrayPlatform struct {
	OpenConfig      func()
	OpenDataFolder  func()
	OpenLog         func()
	NativeLogin     func(serverURL, clientName string) (*config, error)
	LoginConsole    func()
	HasNativeLogin  bool
	HasConsoleLogin bool
}

// TrayController owns the systray menu and refresh loop.
type TrayController struct {
	platform TrayPlatform

	mu         sync.Mutex
	currentCfg *config

	// refreshMu serializes refreshFromSnapshot (called from the debounce loop,
	// the 60s ticker, and OnSyncResult) and guards the slot-ID arrays below,
	// which the per-slot click handlers read concurrently.
	refreshMu sync.Mutex

	// Status
	mStatus    *systray.MenuItem
	mProgress  *systray.MenuItem
	mSyncNow   *systray.MenuItem
	mPause     *systray.MenuItem
	mDashboard *systray.MenuItem

	// Synced games submenu
	mGamesMenu   *systray.MenuItem
	gameSlots    []*systray.MenuItem
	mGamesFooter *systray.MenuItem
	gameIDs      [maxSyncedGameSlots]string

	// Discovered submenu
	mDiscoveredMenu *systray.MenuItem
	discoveredSlots []*systray.MenuItem
	discoveredIDs   [maxDiscoveredSlots]string
	mAddGame        *systray.MenuItem
	mRescan         *systray.MenuItem

	// Issues — the conflicts entry is a submenu shown only when conflicts exist.
	mConflicts       *systray.MenuItem
	mConflictsLocal  *systray.MenuItem
	mConflictsSrv    *systray.MenuItem
	mConflictsReview *systray.MenuItem
	mOutbox          *systray.MenuItem
	mLastError       *systray.MenuItem

	// Settings
	mAccountMenu  *systray.MenuItem
	mAdvancedMenu *systray.MenuItem
	mServer       *systray.MenuItem
	mInterval     *systray.MenuItem
	mLogin        *systray.MenuItem
	mLoginBrowser *systray.MenuItem
	mLoginConsole *systray.MenuItem
	mDetect       *systray.MenuItem
	mRefresh      *systray.MenuItem
	mEditConfig   *systray.MenuItem
	mAutostart    *systray.MenuItem
	mViewLog      *systray.MenuItem
	mDataFolder   *systray.MenuItem
	mLocalStatus  *systray.MenuItem
	mLocalSet     *systray.MenuItem
	mLocalIns     *systray.MenuItem
	mAbout        *systray.MenuItem
	mDiagnostics  *systray.MenuItem
	mVersion      *systray.MenuItem
	mCheckUpdate  *systray.MenuItem
	mApplyUpdate  *systray.MenuItem
	mQuit         *systray.MenuItem
}

// NewTrayController creates a controller with the given platform hooks.
func NewTrayController(p TrayPlatform) *TrayController {
	return &TrayController{platform: p}
}

// Run builds the menu and starts handlers. Call from systray onReady after SetIcon.
func (c *TrayController) Run() {
	initTrayState()
	setupTrayCallbacks()

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	c.mu.Lock()
	c.currentCfg = cfg
	c.mu.Unlock()

	if cfg.ServerURL == "" || strings.TrimSpace(cfg.Token) == "" {
		UpdateTraySetup(true)
	} else {
		UpdateTraySetup(false)
	}
	UpdateTrayPaused(cfg.SyncPaused)
	UpdateTrayMetered(IsMeteredConnection())

	c.buildMenu(cfg)
	setupURL := StartSetupServer()
	restartSync(cfg)

	c.startRefreshLoop()
	c.startClickHandlers()
	c.startUpdateHandlers()
	go c.runSetupFlow(setupURL, cfg)
	go c.runConfigValidation(cfg)

	CleanupOldUpdateBinary()
	if applyErr := ConsumeUpdateApplyError(); applyErr != "" {
		log.Printf("tray: previous update apply failed: %s", applyErr)
		_ = beeep.Notify("GSBS", "Update could not be applied — running the previous version. See gsbs.log for details.", "")
	}
}

func (c *TrayController) buildMenu(cfg *config) {
	c.mStatus = systray.AddMenuItem("GSBS", "Current status")
	c.mStatus.Disable()
	c.mProgress = systray.AddMenuItem("", "Sync progress")
	c.mProgress.Disable()
	c.mProgress.Hide()

	systray.AddSeparator()
	c.mSyncNow = systray.AddMenuItem("Sync now", "Run sync immediately")
	c.mPause = systray.AddMenuItem(pauseResumeMenuTitle(cfg.SyncPaused), "Pause or resume syncing")

	systray.AddSeparator()
	c.mGamesMenu = systray.AddMenuItem("Synced games", "Recently synced games")
	for i := 0; i < maxSyncedGameSlots; i++ {
		slot := c.mGamesMenu.AddSubMenuItem("", "Open save versions")
		slot.Disable()
		slot.Hide()
		c.gameSlots = append(c.gameSlots, slot)
	}
	c.mGamesFooter = c.mGamesMenu.AddSubMenuItem("View all in dashboard →", "Open dashboard")
	if len(c.gameSlots) == 0 {
		_ = c.mGamesMenu.AddSubMenuItem("(no synced games yet)", "Games appear after first sync")
	}

	c.mDiscoveredMenu = systray.AddMenuItem("Discovered games", "Installed games matched to manifest")
	for i := 0; i < maxDiscoveredSlots; i++ {
		slot := c.mDiscoveredMenu.AddSubMenuItem("", "Discovered game")
		slot.Disable()
		slot.Hide()
		c.discoveredSlots = append(c.discoveredSlots, slot)
	}
	c.mAddGame = c.mDiscoveredMenu.AddSubMenuItem("Add a game manually…", "Search by name or add a save folder by path")
	c.mRescan = c.mDiscoveredMenu.AddSubMenuItem("Rescan installed games", "Re-scan launchers and refresh manifest")
	c.mDashboard = systray.AddMenuItem("Open dashboard ↗", "Open the server dashboard in your browser")

	systray.AddSeparator()
	// Conflicts, pending uploads, and errors all live here and stay hidden
	// until they actually apply, so the healthy menu shows none of them. The
	// conflict controls are grouped in one submenu that only appears when a
	// game changed on two devices.
	c.mConflicts = systray.AddMenuItem("Resolve conflicts", "Games changed on two devices")
	c.mConflictsLocal = c.mConflicts.AddSubMenuItem("Keep all local files (overwrite server)", "Push the local copy for every conflict")
	c.mConflictsSrv = c.mConflicts.AddSubMenuItem("Use all server versions (overwrite local)", "Pull the server copy for every conflict")
	c.mConflictsReview = c.mConflicts.AddSubMenuItem("Review each in browser…", "Open the conflicts view to decide per file")
	c.mConflicts.Hide()
	c.mOutbox = systray.AddMenuItem("Pending uploads: 0", "Saves queued while offline — retried automatically")
	c.mOutbox.Disable()
	c.mOutbox.Hide()
	c.mLastError = systray.AddMenuItem("", "Last sync error")
	c.mLastError.Disable()
	c.mLastError.Hide()

	// Account & Setup submenu — connection and login.
	c.mAccountMenu = systray.AddMenuItem("Account & Setup", "Server connection and login")
	c.mServer = c.mAccountMenu.AddSubMenuItem("Server: (not set)", "Current server URL")
	c.mServer.Disable()
	c.mInterval = c.mAccountMenu.AddSubMenuItem("Sync every 6h", "Current sync interval")
	c.mInterval.Disable()
	c.mLogin = c.mAccountMenu.AddSubMenuItem("Login...", "Connect to server")
	if c.platform.HasNativeLogin {
		c.mLogin.SetTitle("Login...")
	} else {
		c.mLogin.SetTitle("Login / Setup...")
	}
	if c.platform.HasNativeLogin {
		c.mLoginBrowser = c.mAccountMenu.AddSubMenuItem("Login (browser)...", "Open setup page in browser")
	}
	if c.platform.HasConsoleLogin {
		c.mLoginConsole = c.mAccountMenu.AddSubMenuItem("Login (console)...", "Open console to log in")
	}
	c.mDetect = c.mAccountMenu.AddSubMenuItem("Detect launcher paths", "Auto-detect launcher install paths")
	c.mRefresh = c.mAccountMenu.AddSubMenuItem("Refresh manifest", "Re-fetch save locations from server")

	// Advanced submenu — config, logs, updates.
	c.mAdvancedMenu = systray.AddMenuItem("Advanced", "Config, logs, and updates")
	c.mEditConfig = c.mAdvancedMenu.AddSubMenuItem("Edit config file", "Open config in editor")
	c.mViewLog = c.mAdvancedMenu.AddSubMenuItem("View log", "Open client log file")
	c.mDataFolder = c.mAdvancedMenu.AddSubMenuItem("Open data folder", "Open GSBS data folder")
	c.mLocalStatus = c.mAdvancedMenu.AddSubMenuItem("Local status page", "Open local sync status in browser")
	c.mLocalSet = c.mAdvancedMenu.AddSubMenuItem("Settings page", "Open client settings in browser")
	c.mLocalIns = c.mAdvancedMenu.AddSubMenuItem("Sync insights", "Open local sync history and conflicts in browser")
	c.mAbout = c.mAdvancedMenu.AddSubMenuItem("About GSBS", "Version, links, and credits")
	c.mDiagnostics = c.mAdvancedMenu.AddSubMenuItem("Copy diagnostics", "Save logs + sanitized config to a zip for bug reports")
	c.mAutostart = c.mAdvancedMenu.AddSubMenuItemCheckbox("Run at startup", "Start GSBS when the system starts", RunAtStartupEnabled())
	c.wireUpdateMenu(c.mAdvancedMenu)

	systray.AddSeparator()
	c.mQuit = systray.AddMenuItem("Quit", "Exit GSBS")

	c.updateStaticLabels(cfg)
	c.refreshFromSnapshot()
}

func (c *TrayController) updateStaticLabels(cfg *config) {
	updateServerLabel(c.mServer, cfg.ServerURL)
	updateSyncIntervalLabel(c.mInterval, cfg.SyncInterval)
}

func (c *TrayController) cfg() *config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentCfg
}

func (c *TrayController) setCfg(cfg *config) {
	c.mu.Lock()
	c.currentCfg = cfg
	c.mu.Unlock()
}

func (c *TrayController) reloadConfig() *config {
	reload, _ := loadConfig()
	if reload != nil && reload.Token != "" {
		c.setCfg(reload)
		c.updateStaticLabels(reload)
		UpdateTraySetup(false)
		UpdateTrayPaused(reload.SyncPaused)
		c.mPause.SetTitle(pauseResumeMenuTitle(reload.SyncPaused))
		if c.mAutostart != nil {
			if RunAtStartupEnabled() {
				c.mAutostart.Check()
			} else {
				c.mAutostart.Uncheck()
			}
		}
		log.Printf("tray: config reloaded, sync restarted")
		restartSync(reload)
		return reload
	}
	return nil
}

func (c *TrayController) startRefreshLoop() {
	sub := subscribeTrayState()
	go func() {
		debounce := time.NewTimer(0)
		if !debounce.Stop() {
			<-debounce.C
		}
		for {
			select {
			case <-sub:
				debounce.Reset(500 * time.Millisecond)
			case <-debounce.C:
				c.refreshFromSnapshot()
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.refreshFromSnapshot()
			systray.SetTooltip(lastSyncTooltip())
		}
	}()
}

// gameIDAt / discoveredIDAt return a slot's game ID under refreshMu so the
// per-slot click handlers never read a string being rewritten by a refresh.
func (c *TrayController) gameIDAt(idx int) string {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	return c.gameIDs[idx]
}

func (c *TrayController) discoveredIDAt(idx int) string {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	return c.discoveredIDs[idx]
}

func (c *TrayController) refreshFromSnapshot() {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	snap := GetTraySnapshot()
	c.applyIcon(snap)
	systray.SetTooltip(formatTrayTooltip(snap))

	c.mStatus.SetTitle(formatStatusHeader(snap))
	if snap.Status == TrayStatusSyncing && snap.Progress.Total > 0 {
		c.mProgress.SetTitle(fmt.Sprintf("%s %d/%d", snap.Progress.Phase, snap.Progress.Current, snap.Progress.Total))
		c.mProgress.Show()
	} else if snap.Status == TrayStatusSyncing {
		c.mProgress.SetTitle(snap.Progress.Phase + "…")
		c.mProgress.Show()
	} else {
		c.mProgress.Hide()
	}

	for i := 0; i < maxSyncedGameSlots; i++ {
		slot := c.gameSlots[i]
		if i < len(snap.Games) {
			g := snap.Games[i]
			c.gameIDs[i] = g.GameID
			slot.SetTitle(formatGameRow(g))
			slot.SetTooltip(g.GameID)
			slot.Enable()
			slot.Show()
		} else {
			c.gameIDs[i] = ""
			slot.Hide()
			slot.Disable()
		}
	}

	for i := 0; i < maxDiscoveredSlots; i++ {
		slot := c.discoveredSlots[i]
		if i < len(snap.Discovered) {
			g := snap.Discovered[i]
			c.discoveredIDs[i] = g.GameID
			slot.SetTitle(formatDiscoveredRow(g))
			tip := g.GameID
			if g.MatchReason != "" {
				tip += " (" + g.MatchReason + ")"
			}
			if g.Disabled {
				tip += " — disabled"
			} else if g.SyncReason != "" {
				tip += " — " + SyncReason(g.SyncReason).Friendly()
			}
			slot.SetTooltip(tip + " — click to toggle sync")
			slot.Enable()
			slot.Show()
			if g.Disabled {
				slot.Uncheck()
			} else {
				slot.Check()
			}
		} else {
			c.discoveredIDs[i] = ""
			slot.Hide()
			slot.Disable()
		}
	}

	if snap.ConflictCount > 0 {
		suffix := ""
		if snap.ConflictCount != 1 {
			suffix = "s"
		}
		c.mConflicts.SetTitle(fmt.Sprintf("⚠ Resolve %d conflict%s", snap.ConflictCount, suffix))
		c.mConflicts.Show()
	} else {
		c.mConflicts.Hide()
	}

	if snap.PendingUploads > 0 {
		c.mOutbox.SetTitle(fmt.Sprintf("%d upload%s pending", snap.PendingUploads, plural(snap.PendingUploads)))
		c.mOutbox.Show()
	} else {
		c.mOutbox.Hide()
	}

	if snap.LastSyncErr != "" && snap.Status == TrayStatusError {
		c.mLastError.SetTitle("Error: " + truncateMsg(snap.LastSyncErr, 60))
		c.mLastError.SetTooltip(snap.LastSyncErr)
		c.mLastError.Show()
	} else {
		c.mLastError.Hide()
	}
}

func (c *TrayController) applyIcon(snap TraySnapshot) {
	switch snap.Status {
	case TrayStatusSyncing:
		systray.SetIcon(IconSyncing())
	case TrayStatusPaused:
		systray.SetIcon(IconPaused())
	case TrayStatusIdle, TrayStatusOffline:
		if !snap.WatcherHealthy {
			systray.SetIcon(IconRecovering())
			return
		}
		systray.SetIcon(IconIdle())
	case TrayStatusError:
		systray.SetIcon(IconError())
	case TrayStatusSetup:
		systray.SetIcon(IconSetup())
	default:
		systray.SetIcon(IconIdle())
	}
}

func formatStatusHeader(snap TraySnapshot) string {
	// Game-aware deferral wins over the idle line (but not over active
	// syncing/paused/error states, which the user should still see).
	if snap.GamesRunning > 0 && snap.Status == TrayStatusIdle {
		if snap.GamesRunning == 1 {
			return "GSBS — In game: sync deferred"
		}
		return fmt.Sprintf("GSBS — In game (%d): sync deferred", snap.GamesRunning)
	}
	switch snap.Status {
	case TrayStatusSyncing:
		return "GSBS — Syncing…"
	case TrayStatusPaused:
		return "GSBS — Paused"
	case TrayStatusSetup:
		return "GSBS — Setup required"
	case TrayStatusError:
		if snap.LastSyncErr != "" {
			return "GSBS — Error: " + truncateMsg(snap.LastSyncErr, 40)
		}
		return "GSBS — Sync error"
	case TrayStatusOffline:
		return "GSBS — Offline"
	default:
		if snap.LastSyncAt.IsZero() {
			return "GSBS — Ready"
		}
		status := "ok"
		if snap.LastSyncErr != "" {
			status = "failed"
		}
		return fmt.Sprintf("GSBS — Last sync: %s (%s)", formatAgo(snap.LastSyncAt), status)
	}
}

func formatDiscoveredRow(g GameRow) string {
	notReady := g.SyncReason != "" && g.SyncReason != string(SyncReasonReady)
	prefix := "○ "
	switch {
	case g.Disabled:
		prefix = "⊘ "
	case g.SyncReason == string(SyncReasonReady):
		prefix = "✓ "
	case notReady:
		prefix = "⚠ "
	}
	title := g.Title
	if title == "" {
		title = g.GameID
	}
	if len(title) > 22 {
		title = title[:19] + "..."
	}
	// For games that won't sync, show the reason inline so the user knows why.
	if notReady && !g.Disabled {
		return prefix + title + " — " + SyncReason(g.SyncReason).Friendly()
	}
	sub := g.Launcher
	if sub == "" && g.MatchReason != "" {
		sub = g.MatchReason
	}
	if sub != "" {
		if len(sub) > 12 {
			sub = sub[:10] + ".."
		}
		return prefix + title + " · " + sub
	}
	return prefix + title
}

func formatGameRow(g GameRow) string {
	prefix := "✓ "
	switch g.Status {
	case GameStatusConflict:
		prefix = "⚠ "
	case GameStatusPending:
		prefix = "⏳ "
	case GameStatusError:
		prefix = "✗ "
	case GameStatusSyncing:
		prefix = "↻ "
	}
	if g.LastDirection == SaveDirPush && g.Status == GameStatusOK {
		prefix = "↑ "
	}
	title := g.Title
	if title == "" {
		title = g.GameID
	}
	if len(title) > 28 {
		title = title[:25] + "..."
	}
	if g.LastSyncAt.IsZero() {
		return prefix + title
	}
	if g.Status == GameStatusConflict {
		return prefix + title + " — conflict"
	}
	return prefix + title + " — " + formatAgo(g.LastSyncAt)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func formatAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	ago := time.Since(t)
	switch {
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		return fmt.Sprintf("%.0fm ago", ago.Minutes())
	case ago < 24*time.Hour:
		return fmt.Sprintf("%.1fh ago", ago.Hours())
	default:
		return fmt.Sprintf("%.0fd ago", ago.Hours()/24)
	}
}

func formatTrayTooltip(snap TraySnapshot) string {
	base := formatStatusHeader(snap)
	var parts []string
	if snap.AuthFailed {
		parts = append(parts, "re-login required")
	}
	if !snap.WatcherHealthy {
		parts = append(parts, "watcher: recovering")
	}
	if snap.ManifestAge > 0 {
		parts = append(parts, "manifest: "+formatAgo(time.Now().Add(-snap.ManifestAge)))
	}
	if d := snap.NextRetryIn; d > 0 {
		sec := int(d.Round(time.Second).Seconds())
		parts = append(parts, fmt.Sprintf("retry in %ds", sec))
	}
	if n := len(snap.Games); n > 0 && snap.Status == TrayStatusIdle {
		parts = append(parts, fmt.Sprintf("%d game(s) tracked", n))
	}
	if snap.PendingUploads > 0 {
		parts = append(parts, fmt.Sprintf("%d upload(s) pending", snap.PendingUploads))
	}
	if len(parts) == 0 {
		return base
	}
	return base + " · " + strings.Join(parts, " · ")
}

func (c *TrayController) startClickHandlers() {
	go func() {
		for range c.mSyncNow.ClickedCh {
			log.Printf("tray: sync now triggered")
			triggerSyncNow()
		}
	}()
	go func() {
		for range c.mPause.ClickedCh {
			paused := !SyncPaused.Load()
			SyncPaused.Store(paused)
			if cfg, _ := loadConfig(); cfg != nil {
				cfg.SyncPaused = paused
				_ = saveConfig(cfg)
				c.setCfg(cfg)
			}
			c.mPause.SetTitle(pauseResumeMenuTitle(paused))
			UpdateTrayPaused(paused)
			if !paused {
				triggerSyncNow()
			}
		}
	}()
	go func() {
		for range c.mDashboard.ClickedCh {
			openDashboard(c.cfg())
		}
	}()
	go func() {
		for range c.mGamesFooter.ClickedCh {
			openDashboard(c.cfg())
		}
	}()
	go func() {
		for range c.mRescan.ClickedCh {
			triggerManifestRefresh()
		}
	}()
	go func() {
		for range c.mAddGame.ClickedCh {
			c.openAddGamePage()
		}
	}()
	go func() {
		for range c.mConflictsLocal.ClickedCh {
			resolveAllConflictsKeepLocal()
			refreshTrayCounts()
			c.refreshFromSnapshot()
		}
	}()
	go func() {
		for range c.mConflictsSrv.ClickedCh {
			resolveAllConflictsUseServer()
			refreshTrayCounts()
			c.refreshFromSnapshot()
		}
	}()
	go func() {
		for range c.mConflictsReview.ClickedCh {
			if url := GetSetupURL(); url != "" {
				_ = openURL(url + "/insights")
			} else {
				openDashboard(c.cfg())
			}
		}
	}()
	go func() {
		for range c.mRefresh.ClickedCh {
			log.Printf("tray: refresh manifest triggered")
			triggerManifestRefresh()
		}
	}()
	go func() {
		for range c.mDetect.ClickedCh {
			c.handleDetectLaunchers()
		}
	}()
	go func() {
		for range c.mEditConfig.ClickedCh {
			if c.platform.OpenConfig != nil {
				c.platform.OpenConfig()
			}
		}
	}()
	go func() {
		for range c.mViewLog.ClickedCh {
			if c.platform.OpenLog != nil {
				c.platform.OpenLog()
			}
		}
	}()
	go func() {
		for range c.mDataFolder.ClickedCh {
			if c.platform.OpenDataFolder != nil {
				c.platform.OpenDataFolder()
			}
		}
	}()
	if c.mLocalStatus != nil {
		go func() {
			for range c.mLocalStatus.ClickedCh {
				if url := GetSetupURL(); url != "" {
					_ = openURL(url + "/dashboard")
				}
			}
		}()
	}
	if c.mLocalSet != nil {
		go func() {
			for range c.mLocalSet.ClickedCh {
				if url := GetSetupURL(); url != "" {
					_ = openURL(url + "/settings")
				}
			}
		}()
	}
	if c.mLocalIns != nil {
		go func() {
			for range c.mLocalIns.ClickedCh {
				if url := GetSetupURL(); url != "" {
					_ = openURL(url + "/insights")
				}
			}
		}()
	}
	if c.mAbout != nil {
		go func() {
			for range c.mAbout.ClickedCh {
				if url := GetSetupURL(); url != "" {
					_ = openURL(url + "/about")
				} else {
					_ = openURL(projectURL)
				}
			}
		}()
	}
	if c.mDiagnostics != nil {
		go func() {
			for range c.mDiagnostics.ClickedCh {
				path, err := ExportDiagnostics()
				if err != nil {
					notifyDiagnosticsError(err)
					continue
				}
				notifyDiagnosticsSaved(path)
				if c.platform.OpenDataFolder != nil {
					c.platform.OpenDataFolder()
				}
			}
		}()
	}
	go func() {
		for range c.mQuit.ClickedCh {
			systray.Quit()
		}
	}()
	go func() {
		for range c.mLogin.ClickedCh {
			c.handleLogin()
		}
	}()
	if c.mLoginBrowser != nil {
		go func() {
			for range c.mLoginBrowser.ClickedCh {
				if url := GetSetupURL(); url != "" {
					_ = openURL(url)
				}
				go func() {
					time.Sleep(3 * time.Second)
					c.reloadConfig()
				}()
			}
		}()
	}
	if c.mLoginConsole != nil && c.platform.LoginConsole != nil {
		go func() {
			for range c.mLoginConsole.ClickedCh {
				c.platform.LoginConsole()
				c.reloadConfig()
			}
		}()
	}
	if c.mAutostart != nil {
		go func() {
			for range c.mAutostart.ClickedCh {
				enabled := !RunAtStartupEnabled()
				if err := SetRunAtStartup(enabled); err != nil {
					log.Printf("tray: run at startup: %v", err)
					notifyActionError("Run at startup", err)
				} else if enabled {
					c.mAutostart.Check()
				} else {
					c.mAutostart.Uncheck()
				}
			}
		}()
	}
	for i, slot := range c.gameSlots {
		idx := i
		go func() {
			for range slot.ClickedCh {
				gameID := c.gameIDAt(idx)
				cfg := c.cfg()
				if gameID == "" || cfg == nil {
					continue
				}
				pathKey := ""
				for _, g := range GetTraySnapshot().Games {
					if g.GameID == gameID {
						pathKey = g.FirstPathKey
						break
					}
				}
				if pathKey == "" {
					pathKey = "default"
				}
				openSaveVersions(cfg, gameID, pathKey)
			}
		}()
	}
	for i, slot := range c.discoveredSlots {
		idx := i
		go func() {
			for range slot.ClickedCh {
				gameID := c.discoveredIDAt(idx)
				if gameID == "" {
					continue
				}
				enabled := isGameDisabled(gameID)
				if err := toggleDiscoveredGame(gameID, !enabled); err != nil {
					log.Printf("tray: toggle discovered game: %v", err)
					continue
				}
				triggerManifestRefresh()
				c.refreshFromSnapshot()
			}
		}()
	}
}

// openAddGamePage opens the local browser page for manually adding a game.
func (c *TrayController) openAddGamePage() {
	url := GetSetupURL()
	if url == "" {
		notifyAddGameUnavailable()
		log.Printf("tray: add game page unavailable (setup server not running)")
		return
	}
	if err := openURL(url + "/games"); err != nil {
		log.Printf("tray: open add-game page: %v", err)
	}
}

func (c *TrayController) handleLogin() {
	cfg := c.cfg()
	if cfg == nil {
		cfg = blankConfig()
	}
	// Prefer browser-based login (better UX, consistent with server WebUI).
	// Fall back to native Walk dialog only if setup server isn't running.
	if url := GetSetupURL(); url != "" {
		_ = openURL(url)
		go func() {
			time.Sleep(3 * time.Second)
			c.reloadConfig()
		}()
		return
	}
	// Fallback: native dialog (Windows only, when setup server couldn't bind a port)
	if c.platform.HasNativeLogin && c.platform.NativeLogin != nil {
		newCfg, err := c.platform.NativeLogin(cfg.ServerURL, cfg.ClientName)
		if err == nil && newCfg != nil {
			c.setCfg(newCfg)
			c.updateStaticLabels(newCfg)
			UpdateTraySetup(false)
			restartSync(newCfg)
		}
		return
	}
}

func (c *TrayController) handleDetectLaunchers() {
	detected := DetectLauncherPaths()
	cfg := c.cfg()
	if cfg == nil {
		cfg, _ = loadConfig()
	}
	if cfg == nil {
		cfg = blankConfig()
	}
	if mergeDetectedIntoConfig(cfg, detected) {
		_ = saveConfig(cfg)
		c.setCfg(cfg)
		log.Printf("tray: detected launcher paths merged into config")
		restartSync(cfg)
	}
	if c.platform.OpenConfig != nil {
		c.platform.OpenConfig()
	}
}

func (c *TrayController) runSetupFlow(setupURL string, cfg *config) {
	if cfg == nil || (cfg.ServerURL != "" && cfg.Token != "") {
		return
	}
	if setupURL == "" {
		notifySetupRequired()
		return
	}
	time.Sleep(800 * time.Millisecond)
	_ = openURL(setupURL)
	notifySetupRequired()
	log.Printf("tray: setup page opened in browser")
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)
		if c.reloadConfig() != nil {
			return
		}
	}
}

func (c *TrayController) runConfigValidation(cfg *config) {
	if cfg == nil || (cfg.ServerURL == "" && cfg.Token == "") {
		return
	}
	warnings := ValidateConfig(cfg)
	if len(warnings) > 0 {
		for _, w := range warnings {
			log.Printf("config validation: %s", w)
		}
		notifyConfigWarnings(warnings)
	}
}

// SetupTraySyncCallbacks wires icon and notification handlers for sync events.
func SetupTraySyncCallbacks(ctrl *TrayController) {
	OnSyncStart = func() {
		if ctrl != nil {
			ctrl.applyIcon(GetTraySnapshot())
		} else {
			systray.SetIcon(IconSyncing())
		}
	}
	OnSyncResult = func(success bool, errMsg string) {
		if ctrl != nil {
			snap := GetTraySnapshot()
			ctrl.applyIcon(snap)
			ctrl.refreshFromSnapshot()
		}
		notifySyncComplete(success, errMsg)
	}
	OnDiscoveryResult = notifyDiscoveryNew
}

func openURL(url string) error {
	if url == "" {
		return nil
	}
	return osExecOpen(url)
}
