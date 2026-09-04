package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDevHandler_servesAFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop-1", "2026", "09"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	full := filepath.Join(root, "shop-1", "2026", "09", "id_thumb.webp")
	if err := os.WriteFile(full, []byte("fake webp bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	h := DevHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/shop-1/2026/09/id_thumb.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want the one-year immutable value", got)
	}
	if rec.Body.String() != "fake webp bytes" {
		t.Fatalf("body = %q, want file contents", rec.Body.String())
	}
}

func TestDevHandler_rejectsDirectoryListing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop-1"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	h := DevHandler(root)

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
	root := t.TempDir()
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("do not serve me"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	defer func() { _ = os.Remove(secret) }()

	h := DevHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/../secret.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (path traversal rejected); body: %s", rec.Code, rec.Body.String())
	}
}

func TestDevHandler_missingFile(t *testing.T) {
	root := t.TempDir()
	h := DevHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/does/not/exist.webp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
