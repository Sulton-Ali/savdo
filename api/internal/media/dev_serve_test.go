package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestStorage(t *testing.T) (*LocalStorage, string) {
	t.Helper()
	root := t.TempDir()
	s, err := NewLocalStorage(root, "/media")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, root
}

func TestDevHandler_servesAFile(t *testing.T) {
	storage, root := newTestStorage(t)
	if err := os.MkdirAll(filepath.Join(root, "shop-1", "2026", "09"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	full := filepath.Join(root, "shop-1", "2026", "09", "id_thumb.webp")
	if err := os.WriteFile(full, []byte("fake webp bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := DevHandler(storage)
	req := httptest.NewRequest(http.MethodGet, "/shop-1/2026/09/id_thumb.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want the one-year immutable value", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Body.String() != "fake webp bytes" {
		t.Fatalf("body = %q, want file contents", rec.Body.String())
	}
}

func TestDevHandler_rejectsDirectoryListing(t *testing.T) {
	storage, root := newTestStorage(t)
	if err := os.MkdirAll(filepath.Join(root, "shop-1"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	h := DevHandler(storage)

	for _, path := range []string{"/", "/shop-1", "/shop-1/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404 (no directory listing)", path, rec.Code)
			}
		})
	}
}

func TestDevHandler_rejectsPathTraversal(t *testing.T) {
	storage, root := newTestStorage(t)
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	defer func() { _ = os.Remove(secret) }()

	h := DevHandler(storage)
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (path traversal rejected); body: %s", rec.Code, rec.Body.String())
	}
}

func TestDevHandler_missingFile(t *testing.T) {
	storage, _ := newTestStorage(t)
	h := DevHandler(storage)

	req := httptest.NewRequest(http.MethodGet, "/does/not/exist.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestDevHandler_rejectsDotSegments proves LocalStorage's own ".tmp"
// scratch directory (storage.go) — and, by the same rule, any
// ".upload-*" atomic-write temp file — can never be served, regardless
// of whether a real file happens to sit there.
func TestDevHandler_rejectsDotSegments(t *testing.T) {
	storage, root := newTestStorage(t)
	// storage.go's TempDir() already created root/.tmp; put a real file
	// in it to prove the rejection is about the path, not a missing file.
	if err := os.WriteFile(filepath.Join(root, tempDirName, "upload-123"), []byte("spool bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := DevHandler(storage)

	for _, path := range []string{"/.tmp/upload-123", "/shop-1/.upload-abc"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404", path, rec.Code)
			}
		})
	}
}

// TestDevHandler_rejectsEscapingSymlinkedFile proves a symlink under the
// media root whose target escapes the root is never followed and served
// (Review B MINOR 6) — os.Root.Open refuses this at open time, regardless
// of the leaf's own file mode, which is what lets DevHandler serve
// through http.ServeContent without an Lstat-based check of its own.
func TestDevHandler_rejectsEscapingSymlinkedFile(t *testing.T) {
	storage, root := newTestStorage(t)
	if err := os.MkdirAll(filepath.Join(root, "shop-1"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "escaped.webp")
	if err := os.WriteFile(target, []byte("do not serve me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(root, "shop-1", "link.webp")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported on this filesystem: %v", err)
	}

	h := DevHandler(storage)
	req := httptest.NewRequest(http.MethodGet, "/shop-1/link.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (escaping symlink rejected); body: %s", rec.Code, rec.Body.String())
	}
}

// TestDevHandler_rejectsEscapingSymlinkedIntermediateDirectory is Review B
// MINOR 6's named case: the escaping symlink is an intermediate path
// *directory*, not the requested file itself — os.Root.Open re-resolves
// every path component against the root, so this is refused the same way
// a directly-symlinked file is, even though the final path segment
// ("real.webp") is an entirely ordinary regular file once you follow the
// link.
func TestDevHandler_rejectsEscapingSymlinkedIntermediateDirectory(t *testing.T) {
	storage, root := newTestStorage(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "secret"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret", "real.webp"), []byte("do not serve me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	linkedDir := filepath.Join(root, "shop-1")
	if err := os.Symlink(filepath.Join(outside, "secret"), linkedDir); err != nil {
		t.Skipf("symlinks unsupported on this filesystem: %v", err)
	}

	h := DevHandler(storage)
	req := httptest.NewRequest(http.MethodGet, "/shop-1/real.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (escaping symlinked intermediate directory rejected); body: %s", rec.Code, rec.Body.String())
	}
}
