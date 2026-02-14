//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/skratchdot/open-golang/open"
)

func runTray() {
	systray.Run(onReady, onExit)
}

var (
	syncCancel context.CancelFunc
	syncNowCh  chan struct{}
)

func onReady() {
	systray.SetTitle("GSBS")
	systray.SetTooltip("Game Sync & Backup Service")
	if len(iconData) > 0 {
		systray.SetIcon(iconData)
	}

	mServer := systray.AddMenuItem("Server: (not set)", "Current server URL")
	mServer.Disable()

	mOpenServer := systray.AddMenuItem("Open server in browser", "Open server URL in default browser")
	mLogin := systray.AddMenuItem("Login...", "Connect to server (enter URL, username, password)")
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

	syncNowCh = make(chan struct{})
	var ctx context.Context
	ctx, syncCancel = context.WithCancel(context.Background())
	go func() {
		if err := runSync(ctx, currentCfg, syncNowCh); err != nil {
			log.Println("sync:", err)
		}
	}()

	// On first launch (no server or no token), show login dialog after a short delay so tray appears first
	go func() {
		if currentCfg.ServerURL == "" || currentCfg.Token == "" {
			time.Sleep(400 * time.Millisecond)
			newCfg, _ := showLoginDialog(currentCfg.ServerURL)
			if newCfg != nil {
				currentCfg = newCfg
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
				newCfg, err := showLoginDialog(currentCfg.ServerURL)
				if err != nil && newCfg == nil {
					continue // cancelled or dialog error
				}
				if newCfg != nil {
					currentCfg = newCfg
					updateServerLabel(mServer, currentCfg.ServerURL)
					// Restart sync with new config
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
