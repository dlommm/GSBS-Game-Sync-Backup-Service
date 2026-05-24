package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
)

//go:embed icon_32.png
var iconPNG []byte

// IconIdle returns platform-appropriate idle tray icon bytes (ICO on Windows, PNG elsewhere).
func IconIdle() []byte {
	if runtime.GOOS == "windows" {
		return iconICO
	}
	if len(iconPNG) > 0 {
		return iconPNG
	}
	return makeMinimalPNG(0xf1, 0x66, 0x63)
}

// IconSyncing returns platform-appropriate syncing icon (blue).
func IconSyncing() []byte {
	if runtime.GOOS == "windows" {
		return iconSyncingICO
	}
	return makeMinimalPNG(0x33, 0x99, 0xff)
}

// IconError returns platform-appropriate error icon (red).
func IconError() []byte {
	if runtime.GOOS == "windows" {
		return iconErrorICO
	}
	return makeMinimalPNG(0xe0, 0x40, 0x40)
}

// IconSetup returns platform-appropriate setup/wizard icon (amber).
func IconSetup() []byte {
	if runtime.GOOS == "windows" {
		return iconSetupICO
	}
	return makeMinimalPNG(0xff, 0xbb, 0x33)
}

var (
	iconICO        = makeTrayIconICO()
	iconSyncingICO = makeMinimalIconWithColor(0x33, 0x99, 0xff)
	iconErrorICO   = makeMinimalIconWithColor(0xe0, 0x40, 0x40)
	iconSetupICO   = makeMinimalIconWithColor(0xff, 0xbb, 0x33)
)

func makeTrayIconICO() []byte {
	if len(iconPNG) == 0 {
		return makeMinimalIconWithColor(0xf1, 0x66, 0x63)
	}
	img, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return makeMinimalIconWithColor(0xf1, 0x66, 0x63)
	}
	return pngToICO(img)
}

func pngToICO(img image.Image) []byte {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h > 256 || w > 256 {
		return makeMinimalIconWithColor(0xf1, 0x66, 0x63)
	}
	imgSize := 40 + w*h*4
	totalSize := 6 + 16 + imgSize
	buf := make([]byte, 0, totalSize)
	buf = append(buf, 0, 0, 1, 0, 1, 0)
	buf = append(buf, byte(w), byte(h), 0, 0, 1, 0, 32, 0)
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], uint32(imgSize))
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], 22)
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], 40)
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], uint32(w))
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], uint32(h*2))
	buf = append(buf, tmp[:]...)
	buf = append(buf, 1, 0, 32, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			buf = append(buf, byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}
	return buf
}

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

func makeMinimalPNG(r, g, b byte) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	c := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
