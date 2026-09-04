package media

import (
	"net/http"
	"strings"
)

// DevHandler serves media files straight from storage's root over HTTP,
// for local development only: router.go mounts it at "/media/" (stripped
// of that prefix first) only when Config.Env != "prod" (docs/07-DEVOPS.md
// § Local development: "Media files in dev go to infra/data/media/"). In
// production Caddy serves the same volume directly (docs/07-DEVOPS.md §
// Production) — this handler never runs there.
//
// It reuses storage's own resolve method for the same string-level
// confinement check Put/Open/Delete apply on the write side (no "..", not
// absolute), then opens the file through storage's os.Root (Review B
// MINOR 6): unlike a path assembled with filepath.Join and opened with a
// plain os.Open, os.Root.Open re-resolves every path component against
// the root directory itself, so a symlink anywhere in the chain —
// including an intermediate directory — that would escape the root is
// refused at open time, not just checked at the leaf file the way an
// os.Lstat-based check does. http.ServeContent then serves that already-
// open file directly — the file that was opened is the file that gets
// served, never a second, separately-resolved path the way handing a
// root string to http.FileServer would. Every path segment starting with
// "." is rejected outright, which is what keeps LocalStorage's own
// ".tmp" scratch directory and any ".upload-*" atomic-write temp file
// (storage.go) from ever being reachable here. Every served response
// carries a one-year immutable Cache-Control (safe because every key is
// content-addressed by media id, docs/06-ROADMAP.md Phase 2 T3 spec — the
// bytes at a given key never change) and X-Content-Type-Options: nosniff,
// so a browser never second-guesses the Content-Type http.ServeContent
// derives from the file's own (always-webp, per O-16) extension.
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

		if _, err := storage.resolve(key); err != nil {
			http.NotFound(w, r)
			return
		}

		f, err := storage.osRoot.Open(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}
