// Package ico encodes PNG/RGBA images into Windows .ico bytes (XOR + AND mask).
package ico

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/draw"
)

// EncodePNG decodes PNG bytes and returns a multi-size ICO (16 and 32 px when source allows).
func EncodePNG(pngData []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}
	img16 := resizeNearest(img, 16)
	return EncodeImages(img16, img), nil
}

func resizeNearest(src image.Image, size int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// EncodeImages builds an ICO file from one or more same-aspect images (typically 16×16 and 32×32).
func EncodeImages(imgs ...image.Image) []byte {
	if len(imgs) == 0 {
		return nil
	}
	type part struct {
		w, h int
		dib  []byte
	}
	parts := make([]part, 0, len(imgs))
	for _, img := range imgs {
		if img == nil {
			continue
		}
		b := img.Bounds()
		w, h := b.Dx(), b.Dy()
		if w <= 0 || h <= 0 || w > 256 || h > 256 {
			continue
		}
		parts = append(parts, part{w: w, h: h, dib: encodeDIB(img)})
	}
	if len(parts) == 0 {
		return nil
	}
	dirSize := 6 + 16*len(parts)
	offset := dirSize
	out := make([]byte, 0, dirSize+1024)
	out = append(out, 0, 0, 1, 0, byte(len(parts)), 0)
	for _, p := range parts {
		wB, hB := byte(p.w), byte(p.h)
		if p.w >= 256 {
			wB = 0
		}
		if p.h >= 256 {
			hB = 0
		}
		out = append(out, wB, hB, 0, 0, 1, 0, 32, 0)
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], uint32(len(p.dib)))
		out = append(out, tmp[:]...)
		binary.LittleEndian.PutUint32(tmp[:], uint32(offset))
		out = append(out, tmp[:]...)
		offset += len(p.dib)
	}
	for _, p := range parts {
		out = append(out, p.dib...)
	}
	return out
}

// EncodeSolid returns a single-size opaque ICO filled with rgb.
func EncodeSolid(size int, r, g, b byte) []byte {
	if size <= 0 || size > 256 {
		size = 16
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return EncodeImages(img)
}

func encodeDIB(img image.Image) []byte {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	xorSize := w * h * 4
	andRow := ((w + 31) / 32) * 4
	andSize := andRow * h
	buf := make([]byte, 40+xorSize+andSize)

	binary.LittleEndian.PutUint32(buf[0:4], 40)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(h*2)) // XOR + AND
	binary.LittleEndian.PutUint16(buf[12:14], 1)
	binary.LittleEndian.PutUint16(buf[14:16], 32)

	off := 40
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			buf[off] = byte(b >> 8)
			buf[off+1] = byte(g >> 8)
			buf[off+2] = byte(r >> 8)
			buf[off+3] = byte(a >> 8)
			off += 4
		}
	}

	andOff := 40 + xorSize
	for y := 0; y < h; y++ {
		rowStart := andOff + y*andRow
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if a>>8 >= 128 {
				continue
			}
			byteIdx := rowStart + x/8
			bit := uint(7 - (x % 8))
			buf[byteIdx] |= 1 << bit
		}
	}
	return buf
}
