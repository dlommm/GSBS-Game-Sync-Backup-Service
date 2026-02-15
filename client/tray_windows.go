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
	"syscall"
	"time"

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

var (
	syncCancel context.CancelFunc
	syncNowCh  chan struct{}
)

func onReady() {
	systray.SetTitle("GSBS")
	systray.SetTooltip("Game Sync & Backup Service")
	// Set icon after a delay so the tray is ready (systray may log a false-positive error but the icon still shows).
	if len(iconData) > 0 {
		go func() {
			time.Sleep(500 * time.Millisecond)
			systray.SetIcon(iconData)
		}()
	}

	mServer := systray.AddMenuItem("Server: (not set)", "Current server URL")
	mServer.Disable()

	mOpenServer := systray.AddMenuItem("Open server in browser", "Open server URL in default browser")
	mLogin := systray.AddMenuItem("Login / Setup...", "Open setup page in browser (server URL, username, password, client name)")
	mLoginConsole := systray.AddMenuItem("Login (console)...", "Open a console window to log in")
	mEditConfig := systray.AddMenuItem("Edit config file", "Open config in Notepad")
	mSyncNow := systray.AddMenuItem("Sync now", "Run a sync immediately")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit GSBS")

	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = blankConfig()
	}
	updateServerLabel(mServer, cfg.ServerURL)

	// currentCfg is used by menu handlers; updated when user logs in successfully
	currentCfg := cfg

	// Start local setup server so "Login" opens the browser to a form (works reliably on Windows).
	setupURL := StartSetupServer()

	syncNowCh = make(chan struct{})
	var ctx context.Context
	ctx, syncCancel = context.WithCancel(context.Background())
	go func() {
		if err := runSync(ctx, currentCfg, syncNowCh); err != nil {
			log.Println("sync:", err)
		}
	}()

	// First run: if not logged in, open the setup page in the browser after a short delay.
	go func() {
		if (currentCfg.ServerURL == "" || currentCfg.Token == "") && setupURL != "" {
			time.Sleep(800 * time.Millisecond)
			open.Run(setupURL)
			// Poll for config change so we can update tray and restart sync when user logs in via browser
			for i := 0; i < 60; i++ {
				time.Sleep(2 * time.Second)
				if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
					currentCfg = reload
					updateServerLabel(mServer, currentCfg.ServerURL)
					syncCancel()
					syncNowCh = make(chan struct{})
					ctx, syncCancel = context.WithCancel(context.Background())
					go func() {
						if err := runSync(ctx, currentCfg, syncNowCh); err != nil {
							log.Println("sync:", err)
						}
					}()
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-mOpenServer.ClickedCh:
				if currentCfg.ServerURL == "" {
					// Could show a message box; for now just don't open
					continue
				}
				open.Run(currentCfg.ServerURL)
			case <-mLogin.ClickedCh:
				// Open setup page in browser (reliable on Windows; no dialogs or console).
				if url := GetSetupURL(); url != "" {
					open.Run(url)
				}
				// After a short delay, reload config in case user just logged in in the browser
				go func() {
					time.Sleep(3 * time.Second)
					if reload, _ := loadConfig(); reload != nil && reload.Token != "" {
						currentCfg = reload
						updateServerLabel(mServer, currentCfg.ServerURL)
						syncCancel()
						syncNowCh = make(chan struct{})
						ctx, syncCancel = context.WithCancel(context.Background())
						go func() {
							if err := runSync(ctx, currentCfg, syncNowCh); err != nil {
								log.Println("sync:", err)
							}
						}()
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
					currentCfg = reload
					updateServerLabel(mServer, currentCfg.ServerURL)
					syncCancel()
					syncNowCh = make(chan struct{})
					ctx, syncCancel = context.WithCancel(context.Background())
					go func() {
						if err := runSync(ctx, currentCfg, syncNowCh); err != nil {
							log.Println("sync:", err)
						}
					}()
				}
			case <-mEditConfig.ClickedCh:
				openConfigInEditor()
			case <-mSyncNow.ClickedCh:
				select {
				case syncNowCh <- struct{}{}:
				default:
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	if syncCancel != nil {
		syncCancel()
	}
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
	_ = open.Run(path)
}

//go:embed icon_32.png
var iconPNG []byte

// iconData is ICO bytes for the tray (from embedded icon_32.png).
var iconData = makeTrayIcon()

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
	ico := make([]byte, 0, 62+16*16*4)
	ico = append(ico,
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x10, 0x10, 0x00, 0x00, 0x01, 0x00, 0x20, 0x00,
		0x28, 0x04, 0x00, 0x00, 0x16, 0x00, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x01, 0x00, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	)
	const b, g, r = 0xf1, 0x66, 0x63
	for i := 0; i < 16*16; i++ {
		ico = append(ico, b, g, r, 0xff)
	}
	return ico
}
