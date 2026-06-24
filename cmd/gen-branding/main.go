// Command gen-branding regenerates every icon/branding derivative from the
// professional master assets in assets/images/. It is the single source of
// truth for branding and replaces the older cmd/write-ico and cmd/resize-icon
// one-offs. Pure Go (golang.org/x/image) — no ImageMagick/rsvg dependency.
//
// Run from the repo root:
//
//	go run ./cmd/gen-branding
//
// Outputs:
//   - client/icon.ico, server/icon.ico   (multi-size Windows .ico)
//   - client/icon_32.png                 (tray master, tinted at runtime)
//   - flatpak/icons/<size>x<size>/io.github.dlommm.GSBS.png (hicolor set)
//   - script/packaging/windows/branding/wizard-large.bmp, wizard-small.bmp
//   - client/webui/static/{favicon,logo}.png, server/webui/static/{favicon,logo}.png
//   - docs/images/gsbs-{icon,logo,logo-sm}.png  (README/docs/wiki/DockerHub)
//   - assets/client-logo.png, client/icon.png
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"github.com/gsbs/gsbs/pkg/ico"
	"golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
)

const appID = "io.github.dlommm.GSBS"

// brandDark is the deep-navy background of the GSBS emblem, used for the
// installer's left-side wizard banner so it reads as an intentional brand panel.
var brandDark = color.RGBA{R: 0x0c, G: 0x10, B: 0x1a, A: 0xff}

// Master assets (relative to repo root).
const (
	masterWindowsIcon = "assets/images/windows-app-icon.png"
	masterLinuxIcon   = "assets/images/linux-app-icon.png"
	masterLogoIcon    = "assets/images/Logo-Icon-Only.png"
	masterWordmark    = "assets/images/primary-logo.png"
)

func main() {
	if _, err := os.Stat("go.mod"); err != nil {
		fail("run from the repo root (go.mod not found): %v", err)
	}

	winMaster := mustLoad(masterWindowsIcon)
	linuxMaster := mustLoad(masterLinuxIcon)
	logoIcon := mustLoad(masterLogoIcon)
	wordmark := mustLoad(masterWordmark)

	// --- Windows .ico: serves both the tray (//go:embed) and the exe icon
	// resource (goversioninfo). pkg/ico PNG-compresses entries >=128px, so
	// the 256px entry stays small and conformant. ---
	icoSizes := []int{16, 24, 32, 48, 64, 128, 256}
	icoImgs := make([]image.Image, 0, len(icoSizes))
	for _, s := range icoSizes {
		icoImgs = append(icoImgs, fit(winMaster, s))
	}
	icoBytes := ico.EncodeImages(icoImgs...)
	if len(icoBytes) == 0 {
		fail("ico encode produced no bytes")
	}
	mustWrite("client/icon.ico", icoBytes)
	mustWrite("server/icon.ico", icoBytes)

	// --- Tray master PNG (square, transparent) tinted at runtime ---
	writePNG("client/icon_32.png", fit(logoIcon, 32))

	// --- Flatpak hicolor PNG set ---
	for _, s := range []int{16, 32, 48, 64, 128, 256, 512} {
		dst := filepath.Join("flatpak", "icons", fmt.Sprintf("%dx%d", s, s), appID+".png")
		writePNG(dst, fit(linuxMaster, s))
	}

	// --- Inno Setup wizard images (BMP, opaque) ---
	// Large: the tall left-side banner shown on the Welcome/Finished pages —
	// the emblem centered on the brand's dark background. Small: 55x55 header
	// logo on white (matches the white wizard page).
	writeBMP("script/packaging/windows/branding/wizard-large.bmp",
		banner(logoIcon, 164, 314, brandDark, 0.78))
	writeBMP("script/packaging/windows/branding/wizard-small.bmp",
		fitOn(logoIcon, 55, 55, color.White))

	// --- Web UI favicon + logo (client and server) ---
	writePNG("client/webui/static/favicon.png", fit(logoIcon, 64))
	writePNG("server/webui/static/favicon.png", fit(logoIcon, 64))
	writePNG("client/webui/static/logo.png", fitWidth(wordmark, 320))
	writePNG("server/webui/static/logo.png", fitWidth(wordmark, 320))

	// --- Docs / wiki / DockerHub marketing logos ---
	// Referenced by README, docs/ARCHITECTURE.md, docs/DOCKERHUB.md and the
	// wiki via stable filenames, so refreshing them in place updates every
	// reference to the current brand without editing each document.
	writePNG("docs/images/gsbs-icon.png", fit(logoIcon, 512))
	writePNG("docs/images/gsbs-logo.png", fitWidth(wordmark, 1024))
	writePNG("docs/images/gsbs-logo-sm.png", fitWidth(wordmark, 640))

	// --- Misc app logos pulled from the masters ---
	// assets/client-logo.png is the setup server's on-disk logo fallback;
	// client/icon.png is a stray full-size PNG of the app icon.
	writePNG("assets/client-logo.png", fitWidth(wordmark, 512))
	writePNG("client/icon.png", fit(linuxMaster, 512))

	fmt.Println("branding assets regenerated from assets/images/")
}

// fit scales src into a size×size transparent canvas, preserving aspect ratio.
func fit(src image.Image, size int) image.Image {
	return fitOn(src, size, size, color.Transparent)
}

// fitOn scales src to fit within w×h (contain), centered on a background fill.
func fitOn(src image.Image, w, h int, bg color.Color) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if _, _, _, a := bg.RGBA(); a > 0 {
		draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	}
	sb := src.Bounds()
	scale := min(float64(w)/float64(sb.Dx()), float64(h)/float64(sb.Dy()))
	dw, dh := int(float64(sb.Dx())*scale), int(float64(sb.Dy())*scale)
	off := image.Pt((w-dw)/2, (h-dh)/2)
	r := image.Rect(off.X, off.Y, off.X+dw, off.Y+dh)
	xdraw.CatmullRom.Scale(dst, r, src, sb, xdraw.Over, nil)
	return dst
}

// banner renders src centered on a solid w×h panel, scaled to occupy `frac`
// of the smaller dimension — used for the installer's left-side wizard image.
func banner(src image.Image, w, h int, bg color.Color, frac float64) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	sb := src.Bounds()
	box := int(float64(min(w, h)) * frac)
	scale := min(float64(box)/float64(sb.Dx()), float64(box)/float64(sb.Dy()))
	dw, dh := int(float64(sb.Dx())*scale), int(float64(sb.Dy())*scale)
	off := image.Pt((w-dw)/2, (h-dh)/2)
	r := image.Rect(off.X, off.Y, off.X+dw, off.Y+dh)
	xdraw.CatmullRom.Scale(dst, r, src, sb, xdraw.Over, nil)
	return dst
}

// fitWidth scales src to a maximum width, preserving aspect ratio.
func fitWidth(src image.Image, maxW int) image.Image {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if w > maxW {
		h = h * maxW / w
		w = maxW
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	return dst
}

func mustLoad(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fail("open %s: %v", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		fail("decode %s: %v", path, err)
	}
	return img
}

func writePNG(path string, img image.Image) {
	mustMkdir(path)
	f, err := os.Create(path)
	if err != nil {
		fail("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fail("encode png %s: %v", path, err)
	}
	fmt.Printf("  wrote %s\n", path)
}

func writeBMP(path string, img image.Image) {
	mustMkdir(path)
	f, err := os.Create(path)
	if err != nil {
		fail("create %s: %v", path, err)
	}
	defer f.Close()
	if err := bmp.Encode(f, img); err != nil {
		fail("encode bmp %s: %v", path, err)
	}
	fmt.Printf("  wrote %s\n", path)
}

func mustWrite(path string, b []byte) {
	mustMkdir(path)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fail("write %s: %v", path, err)
	}
	fmt.Printf("  wrote %s (%d bytes)\n", path, len(b))
}

func mustMkdir(path string) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("mkdir %s: %v", dir, err)
		}
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-branding: "+format+"\n", args...)
	os.Exit(1)
}
