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
// also what drops any EXIF the original carried; per orchestrator
// decision O-16 the original itself is never stored at all (only these
// three derivatives are), so this is the only copy of the pixel data
// that ever reaches disk.
const webpQuality = 82

// maxDimension rejects an upload whose width or height exceeds this many
// pixels (docs/06-ROADMAP.md Phase 2 T3 spec: "reject dimensions > 8000 px
// either side"), checked against image.DecodeConfig's header-only read
// before the real image.Decode ever runs (service.go) — see maxPixels
// below for why maxDimension alone is not enough.
const maxDimension = 8000

// maxPixels additionally rejects an upload whose width×height exceeds 24
// megapixels even when both dimensions individually pass maxDimension —
// e.g. 6000×5000 stays under 8000px per side but still decodes into a
// pixel buffer starting around 96 MB (6000*5000*4 bytes for RGBA), and a
// small file can declare much worse: a few-hundred-byte PNG header
// claiming 30000×30000 would otherwise make image.Decode allocate on the
// order of 3.6 GB per request (Review B CRITICAL 1 — a decompression
// bomb). 24,000,000 comfortably covers this module's real use case (a
// product photo) while keeping a worst-case allocation tolerable even
// under the concurrency cap (service.go's decode/derive semaphore).
const maxPixels = 24_000_000

// resize scales src so its longest side is at most longestSide pixels,
// preserving aspect ratio. It never upscales — an image already at or
// under longestSide on its longest side is returned unchanged (spec:
// "full 1600, never upscale"; the same rule is harmless for thumb/card,
// since nothing this small should already be under 200 or 600). Uses the
// Catmull-Rom kernel: slower than a bilinear scaler, but this runs once
// per uploaded byte sequence (deduped by sha256), not per request, so the
// extra CPU cost buys quality for free.
//
// resize does not read or apply EXIF orientation — a sideways or
// upside-down source (common from a phone camera that stores orientation
// as metadata rather than baking it into the pixels) stays sideways or
// upside-down in every derivative. Correcting that is explicitly out of
// scope for this task (docs/06-ROADMAP.md Phase 2 T3 spec note) and is
// moot for anything a browser already auto-rotates from EXIF on display,
// but note it here since it is easy to assume "we decode the image, so
// we must have normalized it".
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
