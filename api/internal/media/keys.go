package media

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// Derivative suffixes and their longest-side target in pixels (ADR-008;
// docs/04-DATA-MODEL.md § 2: "Variants (thumb/card/full) derived by key
// suffix").
const (
	suffixThumb = "_thumb"
	suffixCard  = "_card"
	suffixFull  = "_full"

	thumbLongestSide = 200
	cardLongestSide  = 600
	fullLongestSide  = 1600
)

// originalKey builds the storage key for an uploaded original:
// "<shop_id>/<yyyy>/<mm>/<media_id>.<ext>" (docs/06-ROADMAP.md Phase 2 T3
// spec). ext comes from the sniffed content type (service.go) — never
// from the client's filename.
func originalKey(shopID, id uuid.UUID, year int, month int, ext string) string {
	return fmt.Sprintf("%s/%04d/%02d/%s.%s", shopID, year, month, id, ext)
}

// derivativeKey rewrites an original storage key into one of its three
// WebP derivatives by replacing the original extension with
// "<suffix>.webp" — e.g. ".../abc.jpg" + suffixThumb -> ".../abc_thumb.webp".
func derivativeKey(original, suffix string) string {
	ext := filepath.Ext(original)
	return strings.TrimSuffix(original, ext) + suffix + ".webp"
}

// URLs builds the MediaUrls (thumb/card/full) a MediaFile or ProductImage
// response exposes for a media_files row's storage_key, each prefixed
// with baseURL (Config.MediaBaseURL). This is the one place that
// translates a stored original key into its derivative URLs — the media
// module's own Handler uses it (handler.go), and catalog (Phase 2 T4,
// product_images) calls it the same way: media.URLs(cfg.MediaBaseURL,
// mediaFileRow.StorageKey), given only the storage_key a
// db.GetMediaFile/db.GetMediaFileBySHA256 row carries — no Storage or
// Service instance required.
func URLs(baseURL, storageKey string) gen.MediaUrls {
	return gen.MediaUrls{
		Thumb: joinURL(baseURL, derivativeKey(storageKey, suffixThumb)),
		Card:  joinURL(baseURL, derivativeKey(storageKey, suffixCard)),
		Full:  joinURL(baseURL, derivativeKey(storageKey, suffixFull)),
	}
}
