package seed

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// placeholderWidth and placeholderHeight are the seeded products' generated
// image dimensions — comfortably under media.Service's per-upload
// dimension/megapixel caps, and small enough that the PNG it encodes to
// stays well under the ~60 KB a solid-colour placeholder needs.
const (
	placeholderWidth  = 640
	placeholderHeight = 480
)

// hexColor parses a 6-digit hex string ("1f6feb") into an opaque
// color.RGBA. Malformed input — never expected, every bgHex in
// catalog_data.go is a literal checked at review time — falls back to a
// mid-gray rather than erroring, since a placeholder's exact shade is
// cosmetic.
func hexColor(hex string) color.RGBA {
	var r, g, b uint8
	if n, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil || n != 3 {
		return color.RGBA{R: 128, G: 128, B: 128, A: 255}
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// luminanceIsDark reports whether c is dark enough that white text reads
// better on it than black, via the standard perceived-luminance weighting
// computed entirely in integers (no float64 anywhere in this package).
func luminanceIsDark(c color.RGBA) bool {
	luma := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
	return luma < 140
}

// generatePlaceholderImage draws label (ASCII only — basicfont.Face7x13
// covers Basic Latin) centered on a solid bgHex background and encodes the
// result as PNG. This is the whole of "generate placeholder images
// programmatically at seed time" (docs/06-ROADMAP.md Phase 2 T5 spec):
// solid colour blocks with the product name drawn, built with Go's stdlib
// image package plus golang.org/x/image/font (already a pinned dependency
// via the media module's own derivative resizing) — no new dependency.
func generatePlaceholderImage(bgHex, label string) ([]byte, error) {
	bg := hexColor(bgHex)
	img := image.NewRGBA(image.Rect(0, 0, placeholderWidth, placeholderHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	textColor := color.RGBA{A: 255} // black
	if luminanceIsDark(bg) {
		textColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}

	face := basicfont.Face7x13
	textWidth := font.MeasureString(face, label).Ceil()
	x := (placeholderWidth - textWidth) / 2
	if x < 0 {
		x = 0
	}
	y := placeholderHeight/2 + face.Ascent/2

	drawer := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: textColor},
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	drawer.DrawString(label)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("seed: encode placeholder png: %w", err)
	}
	return buf.Bytes(), nil
}
