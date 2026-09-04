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

// TestDevHandler_rejectsSymlink proves a symlink under the media root is
// never followed and served, even when it points at a legitimate file
// within the same root (Review B MINOR 6/7) — os.Lstat, not os.Stat, is
// what makes this hold regardless of the link's target.
func TestDevHandler_rejectsSymlink(t *testing.T) {
	storage, root := newTestStorage(t)
	if err := os.MkdirAll(filepath.Join(root, "shop-1"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(root, "shop-1", "real.webp")
	if err := os.WriteFile(target, []byte("real bytes"), 0o600); err != nil {
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
		t.Fatalf("status = %d, want 404 (symlink rejected); body: %s", rec.Code, rec.Body.String())
	}
}
