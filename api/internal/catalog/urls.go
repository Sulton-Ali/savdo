package catalog

import (
	"os"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// mediaBaseURLEnv is read directly here, not through internal/config.Config:
// this task's file scope is catalog/**, not config.go (T3's media module
// owns config.go in this Phase 2 split — see the task's "Parallel" note).
// KNOWN DUPLICATION FOR THE MERGER: if T3 lands a media.URLs(key) helper
// (or a config.MediaBaseURL field) before/at merge time, replace this file
// with a call to that instead of keeping a second implementation of the
// same derivation.
const mediaBaseURLEnv = "MEDIA_BASE_URL"

// defaultMediaBaseURL matches ADR-008: Caddy serves the disk volume at
// `/media/*` in production; the same path works against the dev compose
// stack's static file serving.
const defaultMediaBaseURL = "/media"

// mediaBaseURL returns the configured base path media files are served
// from, or defaultMediaBaseURL if MEDIA_BASE_URL is unset.
func mediaBaseURL() string {
	if v := os.Getenv(mediaBaseURLEnv); v != "" {
		return v
	}
	return defaultMediaBaseURL
}

// mediaURLs derives the thumb/card/full URLs for a media_files row from
// its storage_key (docs/04-DATA-MODEL.md § 2: "Variants (thumb/card/full)
// derived by key suffix"; ADR-008: thumb 200/card 600/full 1600, WebP).
func mediaURLs(storageKey string) gen.MediaUrls {
	base := mediaBaseURL() + "/" + storageKey
	return gen.MediaUrls{
		Thumb: base + "_thumb.webp",
		Card:  base + "_card.webp",
		Full:  base + "_full.webp",
	}
}
