package media

import (
	"net/http"
	"os"
	"strings"
)

// DevHandler serves media files straight from storage's root over HTTP,
// for local development only: router.go mounts it at "/media/" (stripped
// of that prefix first) only when Config.Env != "prod" (docs/07-DEVOPS.md
// § Local development: "Media files in dev go to infra/data/media/"). In
// production Caddy serves the same volume directly (docs/07-DEVOPS.md §
// Production) — this handler never runs there.
//
// It reuses storage's own resolve method for path confinement — the exact
// same check Put/Open/Delete apply on the write side, not a second,
// independently-maintained one (Review B MINOR 6/7) — and serves the
// resolved path with http.ServeFile so the path that was checked is the
// path that gets served, never a second, separately-resolved one the way
// handing the original root to http.FileServer would. os.Lstat, not
// os.Stat, is what the "regular file" check runs against: Lstat reports a
// symlink as a symlink (ModeSymlink set, never IsRegular) without
// following it, so a symlink anywhere under the media root is refused
// regardless of what it points to — dev convenience is not worth the
// alternative of possibly serving an arbitrary file elsewhere on disk.
// Every path segment starting with "." is rejected outright, which is
// what keeps LocalStorage's own ".tmp" scratch directory and any
// ".upload-*" atomic-write temp file (storage.go) from ever being
// reachable here. Every served response carries a one-year immutable
// Cache-Control (safe because every key is content-addressed by media id,
// docs/06-ROADMAP.md Phase 2 T3 spec — the bytes at a given key never
// change) and X-Content-Type-Options: nosniff, so a browser never
// second-guesses the Content-Type http.ServeFile derives from the file's
// own (always-webp, per O-16) extension.
func DevHandler(storage *LocalStorage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		if key == "" {
			http.NotFound(w, r)
			return
		}
		for _, seg := range strings.Split(key, "/") {
			if strings.HasPrefix(seg, ".") {
				http.NotFound(w, r)
				return
			}
		}

		full, err := storage.resolve(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		info, err := os.Lstat(full) // #nosec G304 -- full is confined to storage's root by resolve above
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, full)
	})
}
