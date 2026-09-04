package media

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorage_PutOpenDelete(t *testing.T) {
	root := t.TempDir()
	s := NewLocalStorage(root, "/media")
	ctx := context.Background()
	key := "shop-1/2026/09/abc.jpg"
	content := []byte("hello media")

	if err := s.Put(ctx, key, bytes.NewReader(content), int64(len(content)), "image/jpeg"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The file landed at the expected path, world-readable, not world/
	// group-writable — never 0o777 defaults from an unset umask override.
	full := filepath.Join(root, filepath.FromSlash(key))
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("file mode = %o, want 0644", perm)
	}

	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}

	if got := s.URL(key); got != "/media/"+key {
		t.Fatalf("URL = %q, want %q", got, "/media/"+key)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("Stat after Delete: err = %v, want IsNotExist", err)
	}

	// Deleting an already-deleted key is not an error.
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete (already gone): %v", err)
	}
}

func TestLocalStorage_PutIsAtomic(t *testing.T) {
	root := t.TempDir()
	s := NewLocalStorage(root, "/media")
	ctx := context.Background()
	key := "shop-1/2026/09/atomic.jpg"

	if err := s.Put(ctx, key, bytes.NewReader([]byte("v1")), 2, "image/jpeg"); err != nil {
		t.Fatalf("Put v1: %v", err)
	}

	// Put never leaves a stray temp file behind in the destination
	// directory once it succeeds.
	entries, err := os.ReadDir(filepath.Dir(filepath.Join(root, filepath.FromSlash(key))))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(filepath.FromSlash(key)) {
			t.Fatalf("unexpected leftover entry %q after Put", e.Name())
		}
	}
}

func TestLocalStorage_RejectsHostileKeys(t *testing.T) {
	root := t.TempDir()
	s := NewLocalStorage(root, "/media")
	ctx := context.Background()

	hostile := []string{
		"../escape.jpg",
		"a/../../escape.jpg",
		"/etc/passwd",
		"",
	}

	for _, key := range hostile {
		t.Run(key, func(t *testing.T) {
			if err := s.Put(ctx, key, bytes.NewReader([]byte("x")), 1, "image/jpeg"); err == nil {
				t.Fatalf("Put(%q): want error, got nil", key)
			}
			if _, err := s.Open(ctx, key); err == nil {
				t.Fatalf("Open(%q): want error, got nil", key)
			}
			if err := s.Delete(ctx, key); err == nil {
				t.Fatalf("Delete(%q): want error, got nil", key)
			}
		})
	}
}

func TestLocalStorage_OpenMissingKey(t *testing.T) {
	root := t.TempDir()
	s := NewLocalStorage(root, "/media")

	if _, err := s.Open(context.Background(), "shop-1/2026/09/missing.jpg"); err == nil {
		t.Fatal("Open of a missing key: want error, got nil")
	}
}
