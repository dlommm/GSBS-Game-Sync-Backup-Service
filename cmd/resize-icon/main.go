// One-off: run from repo root to create client/icon_32.png, server/webui/static/logo.png, and favicon.png.
// Run: go run ./cmd/resize-icon
package main

import (
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	// Icon 32x32 for tray
	{
		f, _ := os.Open("docs/images/gsbs-icon.png")
		img, _, _ := image.Decode(f)
		f.Close()
		const size = 32
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		out, _ := os.Create("client/icon_32.png")
		png.Encode(out, dst)
		out.Close()
	}
	// Logo max width 320 for WebUI
	{
		f, _ := os.Open("docs/images/gsbs-logo.png")
		img, _, _ := image.Decode(f)
		f.Close()
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		const maxW = 320
		if w > maxW {
			h = h * maxW / w
			w = maxW
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		out, _ := os.Create("server/webui/static/logo.png")
		png.Encode(out, dst)
		out.Close()
	}
	// Favicon 32x32 for WebUI
	{
		f, _ := os.Open("docs/images/gsbs-icon.png")
		img, _, _ := image.Decode(f)
		f.Close()
		const size = 32
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		out, _ := os.Create("server/webui/static/favicon.png")
		png.Encode(out, dst)
		out.Close()
	}
}
