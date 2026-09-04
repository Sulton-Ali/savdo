package media

import (
	"bytes"
	"fmt"
	"image"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

// webpQuality is the encode quality for every derivative (docs/06-
// ROADMAP.md Phase 2 T3 spec: "encode WebP quality 82"). Re-encoding at
// this quality — rather than copying the source's compressed bytes — is
// also what drops any EXIF the original carried; the original file itself
// is stored unmodified (service.go) and keeps whatever metadata it had.
const webpQuality = 82

// maxDimension rejects an upload whose width or height exceeds this many
// pixels (docs/06-ROADMAP.md Phase 2 T3 spec: "reject dimensions > 8000 px
// either side").
const maxDimension = 8000

// resize scales src so its longest side is at most longestSide pixels,
// preserving aspect ratio. It never upscales — an image already at or
// under longestSide on its longest side is returned unchanged (spec:
// "full 1600, never upscale"; the same rule is harmless for thumb/card,
// since nothing this small should already be under 200 or 600). Uses the
// Catmull-Rom kernel: slower than a bilinear scaler, but this runs once
// per uploaded byte sequence (deduped by sha256), not per request, so the
// extra CPU cost buys quality for free.
func resize(src image.Image, longestSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || (w <= longestSide && h <= longestSide) {
		return src
	}

	var dw, dh int
	if w >= h {
		dw = longestSide
		dh = maxInt(1, int(float64(h)*float64(longestSide)/float64(w)))
	} else {
		dh = longestSide
		dw = maxInt(1, int(float64(w)*float64(longestSide)/float64(h)))
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// encodeWebP encodes img as WebP at webpQuality.
func encodeWebP(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, webp.Options{Quality: webpQuality}); err != nil {
		return nil, fmt.Errorf("media: encode webp: %w", err)
	}
	return buf.Bytes(), nil
}
