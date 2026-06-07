package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"

	"github.com/gsbs/gsbs/pkg/ico"
)

//go:embed icon_32.png
var iconPNG []byte

//go:embed icon.ico
var iconICOEmbed []byte

var (
	iconVariantsOnce sync.Once

	iconSyncingPNG []byte
	iconSyncingICO []byte

	iconRecoveringPNG []byte
	iconRecoveringICO []byte
)

// IconIdle returns platform-appropriate idle tray icon bytes (ICO on Windows, PNG elsewhere).
func IconIdle() []byte {
	if runtime.GOOS == "windows" {
		if len(iconICOEmbed) > 0 {
			return iconICOEmbed
		}
		return trayIconFromPNG()
	}
	if len(iconPNG) > 0 {
		return iconPNG
	}
	return encodePNG(16, 0xf1, 0x66, 0x63)
}

// IconSyncing returns platform-appropriate syncing icon (green GSBS logo).
func IconSyncing() []byte {
	initIconVariants()
	if runtime.GOOS == "windows" {
		if len(iconSyncingICO) > 0 {
			return iconSyncingICO
		}
		return ico.EncodeSolid(16, 0x2e, 0xc2, 0x7e)
	}
	if len(iconSyncingPNG) > 0 {
		return iconSyncingPNG
	}
	return encodePNG(16, 0x2e, 0xc2, 0x7e)
}

// IconRecovering returns platform-appropriate watcher recovery icon (yellow GSBS logo).
func IconRecovering() []byte {
	initIconVariants()
	if runtime.GOOS == "windows" {
		if len(iconRecoveringICO) > 0 {
			return iconRecoveringICO
		}
		return ico.EncodeSolid(16, 0xff, 0xc1, 0x07)
	}
	if len(iconRecoveringPNG) > 0 {
		return iconRecoveringPNG
	}
	return encodePNG(16, 0xff, 0xc1, 0x07)
}

// IconError returns platform-appropriate error icon (red).
func IconError() []byte {
	if runtime.GOOS == "windows" {
		return ico.EncodeSolid(16, 0xe0, 0x40, 0x40)
	}
	return encodePNG(16, 0xe0, 0x40, 0x40)
}

// IconSetup returns platform-appropriate setup/wizard icon (amber).
func IconSetup() []byte {
	if runtime.GOOS == "windows" {
		return ico.EncodeSolid(16, 0xff, 0xbb, 0x33)
	}
	return encodePNG(16, 0xff, 0xbb, 0x33)
}

func trayIconFromPNG() []byte {
	if len(iconPNG) == 0 {
		return ico.EncodeSolid(16, 0xf1, 0x66, 0x63)
	}
	b, err := ico.EncodePNG(iconPNG)
	if err != nil {
		return ico.EncodeSolid(16, 0xf1, 0x66, 0x63)
	}
	return b
}

func encodePNG(size int, r, g, b byte) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func initIconVariants() {
	iconVariantsOnce.Do(func() {
		iconSyncingPNG, iconSyncingICO = buildTintedVariants(0x2e, 0xc2, 0x7e)
		iconRecoveringPNG, iconRecoveringICO = buildTintedVariants(0xff, 0xc1, 0x07)
	})
}

func buildTintedVariants(r, g, b byte) ([]byte, []byte) {
	if len(iconPNG) == 0 {
		return nil, nil
	}
	src, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return nil, nil
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cr, cg, cb, ca := src.At(x, y).RGBA()
			if ca == 0 {
				dst.SetRGBA(x, y, color.RGBA{})
				continue
			}
			rr := uint8(cr >> 8)
			gg := uint8(cg >> 8)
			bb := uint8(cb >> 8)
			aa := uint8(ca >> 8)

			// Preserve anti-aliased shape and luminance while tinting to state color.
			lum := int(rr) + int(gg) + int(bb)
			lum = lum / 3
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8((int(r) * lum) / 255),
				G: uint8((int(g) * lum) / 255),
				B: uint8((int(b) * lum) / 255),
				A: aa,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, nil
	}
	pngBytes := buf.Bytes()
	icoBytes, err := ico.EncodePNG(pngBytes)
	if err != nil {
		return pngBytes, nil
	}
	return pngBytes, icoBytes
}
