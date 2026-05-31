package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"runtime"

	"github.com/gsbs/gsbs/pkg/ico"
)

//go:embed icon_32.png
var iconPNG []byte

//go:embed icon.ico
var iconICOEmbed []byte

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

// IconSyncing returns platform-appropriate syncing icon (blue).
func IconSyncing() []byte {
	if runtime.GOOS == "windows" {
		return ico.EncodeSolid(16, 0x33, 0x99, 0xff)
	}
	return encodePNG(16, 0x33, 0x99, 0xff)
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
