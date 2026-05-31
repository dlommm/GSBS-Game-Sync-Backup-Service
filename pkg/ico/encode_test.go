package ico

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeSolidSize(t *testing.T) {
	ico := EncodeSolid(16, 0x33, 0x99, 0xff)
	if len(ico) < 22 {
		t.Fatalf("ico too short: %d", len(ico))
	}
	if got := binary.LittleEndian.Uint16(ico[4:6]); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestEncodePNGFromClientIcon(t *testing.T) {
	root := filepath.Join("..", "..")
	pngPath := filepath.Join(root, "client", "icon_32.png")
	data, err := os.ReadFile(pngPath)
	if err != nil {
		t.Skip("client/icon_32.png not available:", err)
	}
	ico, err := EncodePNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(ico) < 100 {
		t.Fatalf("ico too short: %d", len(ico))
	}
	if n := int(ico[4]); n < 1 {
		t.Fatalf("expected at least one image, got %d", n)
	}
}

func TestEncodePreservesAlpha(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 0})
	img.SetRGBA(1, 0, color.RGBA{R: 255, A: 255})
	dib := encodeDIB(img)
	if len(dib) != 40+4*4*4+((4+31)/32)*4*4 {
		t.Fatalf("unexpected dib size %d", len(dib))
	}
}

func TestEncodePNGRoundTripHeader(t *testing.T) {
	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 100, G: 120, B: 200, A: 255})
		}
	}
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	ico, err := EncodePNG(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if ico[0] != 0 || ico[1] != 0 || ico[2] != 1 || ico[3] != 0 {
		t.Fatalf("bad magic: %v", ico[:4])
	}
}
