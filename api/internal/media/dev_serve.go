package media

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DevHandler serves media files straight from root over HTTP, for local
// development only: router.go mounts it at "/media/" (stripped of that
// prefix first) only when Config.Env != "prod" (docs/07-DEVOPS.md §
// Local development: "Media files in dev go to infra/data/media/"). In
// production Caddy serves the same volume directly (docs/07-DEVOPS.md §
// Production) — this handler never runs there.
//
// It never lists a directory (a request resolving to anything but a
// regular file is 404) and rejects path traversal independently of
// whatever normalization net/http's ServeMux already does to the
// incoming URL, the same defence-in-depth LocalStorage.resolve applies on
// the write side. Every served response carries a one-year immutable
// Cache-Control: safe because every key is content-addressed by media id
// (docs/06-ROADMAP.md Phase 2 T3 spec) — the bytes at a given key never
// change.
func DevHandler(root string) http.Handler {
	root = filepath.Clean(root)
	fileServer := http.FileServer(http.Dir(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		if key == "" || strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}

		full := filepath.Join(root, filepath.Clean(string(filepath.Separator)+key))
		rel, err := filepath.Rel(root, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(full) // #nosec G304 -- full is confined to root, validated above
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
}
