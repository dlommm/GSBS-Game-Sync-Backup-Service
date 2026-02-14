// One-off: creates server/icon.ico from client/icon_32.png for Windows exe icon.
// Run from repo root: go run ./cmd/write-ico
package main

import (
	"encoding/binary"
	"image"
	"image/png"
	"os"
)

func main() {
	f, err := os.Open("client/icon_32.png")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	ico := pngToICO(img)
	if err := os.WriteFile("server/icon.ico", ico, 0644); err != nil {
		panic(err)
	}
}

func pngToICO(img image.Image) []byte {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	imgSize := 40 + w*h*4
	buf := make([]byte, 0, 6+16+imgSize)
	buf = append(buf, 0, 0, 1, 0, 1, 0)
	buf = append(buf, byte(w), byte(h), 0, 0, 1, 0, 32, 0)
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], uint32(imgSize))
	buf = append(buf, tmp[:]...)
	binary.LittleEndian.PutUint32(tmp[:], 22)
	buf = append(buf, tmp[:]...)
	buf = append(buf, 40, 0, 0, 0)
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
