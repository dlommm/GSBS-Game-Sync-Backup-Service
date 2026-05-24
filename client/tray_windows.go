//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/getlantern/systray"
	"github.com/skratchdot/open-golang/open"
)

func runTray() {
	systray.Run(onReady, onExit)
}

// runFirstTimeSetupIfNeeded: if config has no server or token, run a console login subprocess so the user can set up before the tray starts.
func runFirstTimeSetupIfNeeded() {
	cfg, _ := loadConfig()
	if cfg != nil && cfg.ServerURL != "" && cfg.Token != "" {
		return // already configured
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "login")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x10} // CREATE_NEW_CONSOLE — child gets its own console window
	cmd.Run()
}

// runLoginDialogProcess shows the login popup in this process, saves config on success, and exits. Used when tray runs "gsbs-client login-dialog".
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

// showSyncNotification displays a Windows toast and updates tray icon state.
func showSyncNotification(success bool, errMsg string) {
	// Update tray icon: idle (normal) or error (red)
	if len(iconData) > 0 {
		if success {
			systray.SetIcon(iconData)
		} else {
			systray.SetIcon(iconError)
		}
	}
	// Toast
	title := "GSBS"
	var msg string
	if success {
		msg = "Sync complete."
	} else {
		msg = "Sync failed."
		if errMsg != "" {
			if len(errMsg) > 80 {
				msg = msg + " " + strings.TrimSpace(errMsg[:77]) + "..."
			} else {
				msg = msg + " " + strings.TrimSpace(errMsg)
			}
		}
	}
	if err := beeep.Notify(title, msg, ""); err != nil {
		log.Printf("tray: notify: %v", err)
	}
}

// showSyncStart sets the tray icon to "syncing" (blue).
func showSyncStart() {
	if len(iconSyncing) > 0 {
		systray.SetIcon(iconSyncing)
	}
}

func onReady() {
	systray.SetTitle("GSBS")
	systray.SetTooltip(lastSyncTooltip())
	// Register sync start/result for icon state and toast.
	OnSyncStart = showSyncStart
	OnSyncResult = showSyncNotification
	OnDiscoveryResult = func(newGames int) {
		_ = beeep.Notify("GSBS", fmt.Sprintf("Discovered %d new game(s)", newGames), "")
	}
	// Set icon after a delay so the tray is ready (systray may log a false-positive error but the icon still shows).
	if len(iconData) > 0 {
		go func() {
			time.Sleep(500 * time.Millisecond)
			systray.SetIcon(iconData)
		}()
	}

	mServer := systray.AddMenuItem("Server: (not set)", "Current server URL")
	mServer.Disable()

	mSyncInterval := systray.AddMenuItem("Sync every 5m", "Current sync interval")
	mSyncInterval.Disable()
	mLastSync := systray.AddMenuItem("Last sync: —", "Last sync time and status")
	mLastSync.Disable()

	// Update tooltip and status menu periodically
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			systray.SetTooltip(lastSyncTooltip())
			updateLastSyncLabel(mLastSync)
		}
	}()

	// Quick actions at top: Sync now, Open server (most common).
	mSyncNow := systray.AddMenuItem("Sync now", "Run a sync immediately")
	mOpenServer := systray.AddMenuItem("Open server in browser", "Open server URL in default browser")
	mPauseResume := systray.AddMenuItem(pauseResumeMenuTitle(SyncPaused.Load()), "Pause or resume syncing")
	systray.AddSeparator()
	mLogin := systray.AddMenuItem("Login...", "Connect to server (native dialog)")
	mLoginBrowser := systray.AddMenuItem("Login (browser)...", "Open setup page in browser")
	mLoginConsole := systray.AddMenuItem("Login (console)...", "Open a console window to log in")
	mEditConfig := systray.AddMenuItem("Edit config file", "Open config in Notepad")
	mDetectLaunchers := systray.AddMenuItem("Detect launcher paths", "Auto-detect Ubisoft, GOG, Epic, Xbox paths and merge into config")
	mRefreshManifest := systray.AddMenuItem("Refresh manifest", "Re-fetch game save locations from server")
	mRunAtStartup := systray.AddMenuItemCheckbox("Run at Windows startup", "Start GSBS when Windows starts", RunAtStartupEnabled())
	mViewLog := systray.AddMenuItem("View log", "Open gsbs.log in default editor")
	mOpenDataFolder := systray.AddMenuItem("Open data folder", "Open GSBS data folder in Explorer")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit GSBS")

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	updateServerLabel(mServer, cfg.ServerURL)
	updateSyncIntervalLabel(mSyncInterval, cfg.SyncInterval)
	updateLastSyncLabel(mLastSync)

	// currentCfg is used by menu handlers; protected by syncMu.
	currentCfg := cfg

	// Start local setup server so "Login" opens the browser to a form (works reliably on Windows).
	setupURL := StartSetupServer()

	restartSync(currentCfg)

	// Optional config validation in background (log and one balloon if issues).
	go func() {
		syncMu.Lock()
		cfg := currentCfg
		syncMu.Unlock()
		if cfg == nil || (cfg.ServerURL == "" && cfg.Token == "") {
			return
		}
		warnings := ValidateConfig(cfg)
		if len(warnings) > 0 {
			for _, w := range warnings {
				log.Printf("config validation: %s", w)
			}
			msg := "Config issues: " + strings.Join(warnings, "; ")
			if len(msg) > 120 {
				msg = msg[:117] + "..."
			}
			_ = beeep.Alert("GSBS", msg, "")
		}
	}()

	// First run: if not logged in, open the setup page in the browser after a short delay.
	go func() {
		syncMu.Lock()
		needSetup := currentCfg.ServerURL == "" || currentCfg.Token == ""
		syncMu.Unlock()
				if needSetup && setupURL != "" {
			time.Sleep(800 * time.Millisecond)
			open.Run(setupURL)
			log.Printf("tray: setup page opened in browser")
			// Poll for config change so we can update tray and restart sync when user logs in via browser
			for i := 0; i < 60; i++ {
				time.Sleep(2 * time.Second)
				if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
					syncMu.Lock()
					currentCfg = reload
					syncMu.Unlock()
					updateServerLabel(mServer, reload.ServerURL)
					updateSyncIntervalLabel(mSyncInterval, reload.SyncInterval)
					log.Printf("tray: config reloaded (browser login), sync restarted")
					restartSync(reload)
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-mOpenServer.ClickedCh:
				syncMu.Lock()
				url := currentCfg.ServerURL
				syncMu.Unlock()
				if url == "" {
					continue
				}
				open.Run(url)
			case <-mLogin.ClickedCh:
				// Native login dialog (primary on Windows).
				syncMu.Lock()
				url, name := currentCfg.ServerURL, currentCfg.ClientName
				syncMu.Unlock()
				newCfg, err := showLoginDialog(url, name)
				if err == nil && newCfg != nil {
					syncMu.Lock()
					currentCfg = newCfg
					syncMu.Unlock()
					updateServerLabel(mServer, newCfg.ServerURL)
					updateSyncIntervalLabel(mSyncInterval, newCfg.SyncInterval)
					log.Printf("tray: config reloaded (native login), sync restarted")
					restartSync(newCfg)
				}
			case <-mLoginBrowser.ClickedCh:
				if url := GetSetupURL(); url != "" {
					open.Run(url)
				}
				go func() {
					time.Sleep(3 * time.Second)
					if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
						syncMu.Lock()
						currentCfg = reload
						syncMu.Unlock()
					updateServerLabel(mServer, reload.ServerURL)
					updateSyncIntervalLabel(mSyncInterval, reload.SyncInterval)
					log.Printf("tray: config reloaded (browser), sync restarted")
					restartSync(reload)
					}
				}()
			case <-mLoginConsole.ClickedCh:
				// Run gsbs-client login in a new console window (direct exec with CREATE_NEW_CONSOLE).
				exe, err := os.Executable()
				if err != nil {
					log.Println("login (console):", err)
					continue
				}
				cmd := exec.Command(exe, "login")
				cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x10} // CREATE_NEW_CONSOLE
				_ = cmd.Run()
				// Reload config and restart sync in case user logged in
				if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
					syncMu.Lock()
					currentCfg = reload
					syncMu.Unlock()
					updateServerLabel(mServer, reload.ServerURL)
					updateSyncIntervalLabel(mSyncInterval, reload.SyncInterval)
					log.Printf("tray: config reloaded (console login), sync restarted")
					restartSync(reload)
				}
			case <-mEditConfig.ClickedCh:
				openConfigInEditor()
			case <-mDetectLaunchers.ClickedCh:
				detected := DetectLauncherPaths()
				syncMu.Lock()
				cfg := currentCfg
				syncMu.Unlock()
				if cfg == nil {
					cfg, _ = loadConfig()
				}
				if cfg == nil {
					cfg = blankConfig()
				}
				merged := false
				if detected.UbisoftConnect != "" && cfg.UbisoftConnectFolder == "" {
					cfg.UbisoftConnectFolder = detected.UbisoftConnect
					merged = true
				}
				if detected.GOGGalaxy != "" && cfg.GOGGalaxyFolder == "" {
					cfg.GOGGalaxyFolder = detected.GOGGalaxy
					merged = true
				}
				if detected.EpicGames != "" && cfg.EpicGamesFolder == "" {
					cfg.EpicGamesFolder = detected.EpicGames
					merged = true
				}
				if detected.XboxApp != "" && cfg.XboxAppFolder == "" {
					cfg.XboxAppFolder = detected.XboxApp
					merged = true
				}
				if merged {
					_ = saveConfig(cfg)
					syncMu.Lock()
					currentCfg = cfg
					syncMu.Unlock()
					log.Printf("tray: detected launcher paths merged into config")
				}
				openConfigInEditor()
			case <-mSyncNow.ClickedCh:
				log.Printf("tray: sync now triggered")
				triggerSyncNow()
			case <-mRefreshManifest.ClickedCh:
				log.Printf("tray: refresh manifest triggered")
				triggerManifestRefresh()
			case <-mRunAtStartup.ClickedCh:
				enabled := !RunAtStartupEnabled()
				if err := SetRunAtStartup(enabled); err != nil {
					log.Printf("tray: run at startup: %v", err)
				} else {
					if enabled {
						mRunAtStartup.Check()
					} else {
						mRunAtStartup.Uncheck()
					}
				}
			case <-mViewLog.ClickedCh:
				path := ClientLogPath()
				if path != "" {
					_ = open.Run(path)
				}
			case <-mOpenDataFolder.ClickedCh:
				dir := ClientDataDir()
				if dir != "" {
					_ = exec.Command("explorer", dir).Start()
				}
			case <-mPauseResume.ClickedCh:
				paused := !SyncPaused.Load()
				SyncPaused.Store(paused)
				if cfg, _ := loadConfig(); cfg != nil {
					cfg.SyncPaused = paused
					_ = saveConfig(cfg)
				}
				mPauseResume.SetTitle(pauseResumeMenuTitle(paused))
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		syncCancel()
	}
}

func pauseResumeMenuTitle(paused bool) string {
	if paused {
		return "Resume syncing"
	}
	return "Pause syncing"
}

func updateSyncIntervalLabel(m *systray.MenuItem, d Duration) {
	interval := d.Duration()
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	m.SetTitle("Sync every " + interval.String())
}

func updateLastSyncLabel(m *systray.MenuItem) {
	at, err := getLastSync()
	if at.IsZero() {
		m.SetTitle("Last sync: —")
		return
	}
	ago := time.Since(at)
	var agoStr string
	if ago < time.Minute {
		agoStr = "just now"
	} else if ago < time.Hour {
		agoStr = fmt.Sprintf("%.0fm ago", ago.Minutes())
	} else if ago < 24*time.Hour {
		agoStr = fmt.Sprintf("%.1fh ago", ago.Hours())
	} else {
		agoStr = fmt.Sprintf("%.0fd ago", ago.Hours()/24)
	}
	status := "ok"
	if err != nil {
		status = "failed"
	}
	m.SetTitle(fmt.Sprintf("Last sync: %s (%s)", agoStr, status))
}

func updateServerLabel(m *systray.MenuItem, url string) {
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

func openConfigInEditor() {
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
	// Try VS Code if in PATH, else default handler (e.g. Notepad).
	if codePath, err := exec.LookPath("code"); err == nil {
		_ = exec.Command(codePath, path).Start()
		return
	}
	_ = open.Run(path)
}

//go:embed icon_32.png
var iconPNG []byte

// iconData is ICO bytes for the tray (from embedded icon_32.png).
var iconData = makeTrayIcon()

// iconSyncing and iconError are programmatic 16x16 icons for tray state.
var (
	iconSyncing = makeMinimalIconWithColor(0x33, 0x99, 0xff) // blue
	iconError   = makeMinimalIconWithColor(0xe0, 0x40, 0x40) // red
)

func makeTrayIcon() []byte {
	if len(iconPNG) == 0 {
		return makeMinimalIcon()
	}
	img, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return makeMinimalIcon()
	}
	return pngToICO(img)
}

func pngToICO(img image.Image) []byte {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h > 256 || w > 256 {
		return makeMinimalIcon()
	}
	// ICO: 6 (dir) + 16 (entry) + 40 (BITMAPINFOHEADER) + image (h*w*4, bottom-up BGRA)
	imgSize := 40 + w*h*4
	totalSize := 6 + 16 + imgSize
	buf := make([]byte, 0, totalSize)
	// ICONDIR
	buf = append(buf, 0, 0, 1, 0, 1, 0)
	// ICONDIRENTRY: width, height, 0, 0, 1, 0, 32bpp, size, offset
	buf = append(buf, byte(w), byte(h), 0, 0, 1, 0, 32, 0)
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], uint32(imgSize))
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], 22)
	buf = append(buf, tmp[:]...)
	// BITMAPINFOHEADER
	binary.LittleEndian.PutUint32(tmp[:], 40)
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], uint32(w))
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], uint32(h*2))
	buf = append(buf, tmp[:]...)
	buf = append(buf, 1, 0, 32, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	// Image data: bottom-up BGRA
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			buf = append(buf, byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}
	return buf
}

func makeMinimalIcon() []byte {
	return makeMinimalIconWithColor(0xf1, 0x66, 0x63)
}

// makeMinimalIconWithColor returns a 16x16 ICO with the given RGB (0-255).
func makeMinimalIconWithColor(r, g, b byte) []byte {
	ico := make([]byte, 0, 62+16*16*4)
	ico = append(ico,
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x10, 0x10, 0x00, 0x00, 0x01, 0x00, 0x20, 0x00,
		0x28, 0x04, 0x00, 0x00, 0x16, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x01, 0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	)
	for i := 0; i < 16*16; i++ {
		ico = append(ico, b, g, r, 0xff)
	}
	return ico
}
