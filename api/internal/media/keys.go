package media

import (
	"fmt"

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

// keyStem builds the storage key stem an upload's three WebP derivatives
// share: "<shop_id>/<yyyy>/<mm>/<media_id>" — this is also
// media_files.storage_key (orchestrator decision O-16). There is
// deliberately no extension and no key for the original at all: only the
// three derivatives (below) are ever written to disk. The original's
// bytes are decoded, hashed and then discarded — never stored, so there
// is nothing to serve under a guessable name and no path for an
// uploaded file's own bytes (EXIF, or a crafted polyglot payload sharing
// the file with some other format) to reach an HTTP response.
func keyStem(shopID, id uuid.UUID, year int, month int) string {
	return fmt.Sprintf("%s/%04d/%02d/%s", shopID, year, month, id)
}

// derivativeKey appends one of the three WebP derivative suffixes to a
// storage key stem — e.g. stem + suffixThumb -> "<stem>_thumb.webp".
func derivativeKey(stem, suffix string) string {
	return stem + suffix + ".webp"
}

// URLs builds the MediaUrls (thumb/card/full) a MediaFile or ProductImage
// response exposes for a media_files row's storage_key (which, per O-16,
// is the key *stem* — there is no original to link to), each prefixed
// with baseURL (Config.MediaBaseURL). This is the one place that
// translates a stored stem into its derivative URLs — the media module's
// own Handler uses it (handler.go), and catalog (Phase 2 T4,
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
