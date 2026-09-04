package media

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/gen2brain/webp"
)

func solidImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func TestResize_scalesDownPreservingAspectRatio(t *testing.T) {
	src := solidImage(800, 600)

	out := resize(src, cardLongestSide)
	b := out.Bounds()
	if b.Dx() != cardLongestSide {
		t.Fatalf("width = %d, want %d (longest side)", b.Dx(), cardLongestSide)
	}
	wantHeight := 450 // 600 * 600/800
	if b.Dy() != wantHeight {
		t.Fatalf("height = %d, want %d", b.Dy(), wantHeight)
	}
}

func TestResize_neverUpscales(t *testing.T) {
	src := solidImage(100, 80)

	out := resize(src, fullLongestSide)
	b := out.Bounds()
	if b.Dx() != 100 || b.Dy() != 80 {
		t.Fatalf("bounds = %dx%d, want unchanged 100x80 (never upscale)", b.Dx(), b.Dy())
	}
}

func TestResize_tallImage(t *testing.T) {
	src := solidImage(600, 800)

	out := resize(src, thumbLongestSide)
	b := out.Bounds()
	if b.Dy() != thumbLongestSide {
		t.Fatalf("height = %d, want %d (longest side)", b.Dy(), thumbLongestSide)
	}
	wantWidth := 150 // 600 * 200/800
	if b.Dx() != wantWidth {
		t.Fatalf("width = %d, want %d", b.Dx(), wantWidth)
	}
}

func TestEncodeWebP_roundTrips(t *testing.T) {
	src := solidImage(64, 48)

	data, err := encodeWebP(src)
	if err != nil {
		t.Fatalf("encodeWebP: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("encodeWebP produced no bytes")
	}

	decoded, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("webp.Decode round-trip: %v", err)
	}
	b := decoded.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("decoded bounds = %dx%d, want 64x48", b.Dx(), b.Dy())
	}
}
