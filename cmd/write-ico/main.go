// Generates client/icon.ico (tray + Windows resources) from client/icon_32.png.
// Run from repo root: go run ./cmd/write-ico
package main

import (
	"image"
	"image/png"
	"os"

	"github.com/gsbs/gsbs/pkg/ico"
	"golang.org/x/image/draw"
)

func main() {
	f, err := os.Open("client/icon_32.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img32, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	img16 := resize(img32, 16)
	icoBytes := ico.EncodeImages(img16, img32)
	if len(icoBytes) == 0 {
		panic("empty ico")
	}
	if err := os.WriteFile("client/icon.ico", icoBytes, 0644); err != nil {
		panic(err)
	}
	// Server Windows build embeds server/rsrc.syso from the same asset.
	if err := os.WriteFile("server/icon.ico", icoBytes, 0644); err != nil {
		panic(err)
	}
}

func resize(src image.Image, size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}
