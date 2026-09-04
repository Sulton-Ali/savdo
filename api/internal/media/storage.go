// Package media implements image upload, storage and WebP derivatives
// (docs/03-ARCHITECTURE.md § Module map: "media" owns media_files and
// product_images; ADR-008). Service (service.go) validates an upload,
// generates thumb/card/full WebP derivatives (derive.go) and dedupes by
// sha256 per shop; Storage (this file) abstracts where the bytes actually
// live so a future S3-backed implementation can replace LocalStorage
// without any caller changing; Handler (handler.go) adapts Service to the
// oapi-codegen strict server interface's UploadMedia operation; URLs
// (keys.go) is the one place a storage_key becomes the three derivative
// URLs a MediaFile or ProductImage response exposes — catalog (Phase 2
// T4, product_images) calls it too, given a media_files row's
// storage_key and Config.MediaBaseURL.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Storage is how Service reads and writes media bytes. LocalStorage (below)
// is the MVP implementation (ADR-008: "writes under /data/media/<shop_id>/…
// on a Docker volume"); an S3 implementation can satisfy the same methods
// later without touching Service.
type Storage interface {
	// Put writes size bytes read from r under key, replacing any existing
	// content there. contentType is passed through for implementations
	// that need it at write time (e.g. an S3 PutObject's Content-Type);
	// LocalStorage does not use it — a served file's content type comes
	// from the sniffed value recorded in media_files, not the filesystem.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// Open returns a reader for key's bytes. The caller must Close it.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes key. Deleting a key that no longer exists is not an
	// error — Service's best-effort cleanup on a partial-write failure
	// relies on this.
	Delete(ctx context.Context, key string) error
	// URL returns key's public URL, under the configured MEDIA_BASE_URL.
	URL(key string) string
	// TempDir returns a scratch directory Service can spool an upload's
	// raw bytes into before it knows the final storage key — decoding and
	// deriving always happens against a local file regardless of which
	// Storage backend the derivatives ultimately land in (Review B MINOR
	// 5: never os.TempDir(), so this stays under the same disk/quota and
	// permission boundary as everything else this module writes).
	// LocalStorage's is ".tmp" under its own root, named so the dev
	// static handler (dev_serve.go) — which refuses any path segment
	// starting with "." — never serves it.
	TempDir(ctx context.Context) (string, error)
}

// ErrInvalidKey is returned by LocalStorage's methods when key fails
// validation: empty, containing "..", absolute, or (as a final check)
// resolving outside root once cleaned. Every key Service generates is
// well-formed by construction (keys.go), so this only ever fires in a
// test that deliberately tries a hostile key, or guards a future bug
// upstream that let something client-controlled reach here (gosec G304 —
// never build a filesystem path from unvalidated input without confirming
// where it lands).
var ErrInvalidKey = errors.New("media: invalid storage key")

// tempDirName is LocalStorage's scratch subdirectory (TempDir), and the
// name SweepTemp treats as fair game for cleanup regardless of a file's
// own name inside it.
const tempDirName = ".tmp"

// LocalStorage implements Storage on the local filesystem, rooted at root
// (Config.MediaDir). Put is atomic — written to a temp file in the same
// directory as the destination, then renamed — so a concurrent Open or
// the dev static handler (dev_serve.go) never observes a partially
// written file. Files are created 0o644, directories 0o755 (world-
// readable/traversable: in production this tree is served by Caddy,
// typically running as a different uid inside the same container, which
// needs to traverse and read it — see the #nosec G301 comments below).
type LocalStorage struct {
	root    string
	baseURL string
}

// NewLocalStorage builds a LocalStorage rooted at root, whose URL method
// prefixes a key with baseURL (e.g. "/media"). It creates root and its
// TempDir eagerly — "created with the storage", not lazily on first
// upload — so a fresh deployment fails fast on a permission problem
// instead of surfacing it as the first request's 500, and so a restart's
// startup sweep (SweepTemp, called from cmd/api) always has a directory
// to sweep.
func NewLocalStorage(root, baseURL string) (*LocalStorage, error) {
	s := &LocalStorage{root: filepath.Clean(root), baseURL: strings.TrimSuffix(baseURL, "/")}
	if err := os.MkdirAll(s.root, 0o755); err != nil { // #nosec G301 -- served by Caddy in prod
		return nil, fmt.Errorf("media: create media dir %s: %w", s.root, err)
	}
	if _, err := s.TempDir(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// resolve validates key and returns the absolute filesystem path it maps
// to under root. Joining against filepath.Clean("/"+key) first means a
// key that somehow still contained ".." after the explicit Contains check
// below cannot climb above root once Clean collapses it against that
// synthetic leading "/"; the filepath.Rel check afterward is the final,
// independent confirmation that the join actually landed under root. The
// dev static handler (dev_serve.go) reuses this exact method — the same
// confinement check on the read side as on the write side, rather than a
// second, independently-maintained one (Review B MINOR 6/7).
func (s *LocalStorage) resolve(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") || filepath.IsAbs(key) {
		return "", ErrInvalidKey
	}
	full := filepath.Join(s.root, filepath.Clean(string(filepath.Separator)+key))
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	return full, nil
}

// Put implements Storage.
func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, size int64, _ string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- served by Caddy in prod
		return fmt.Errorf("media: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("media: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// Removing an already-renamed temp file always fails with
	// ErrNotExist (the path is gone) — expected once Put succeeds, so
	// this defer is only ever load-bearing on an error path below. A
	// leftover ".upload-*" from a process that crashed between here and
	// the rename is swept by SweepTemp on the next startup.
	defer func() { _ = os.Remove(tmpName) }()

	written, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("media: write %s: %w", key, err)
	}
	if written != size {
		_ = tmp.Close()
		return fmt.Errorf("media: write %s: wrote %d bytes, want %d", key, written, size)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("media: chmod %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("media: close temp file for %s: %w", key, err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("media: rename into place %s: %w", key, err)
	}
	return nil
}

// Open implements Storage.
func (s *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full) // #nosec G304 -- full is confined to s.root by resolve above
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Delete implements Storage. Deleting a key that does not exist on disk is
// not an error.
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	full, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("media: delete %s: %w", key, err)
	}
	return nil
}

// URL implements Storage.
func (s *LocalStorage) URL(key string) string {
	return joinURL(s.baseURL, key)
}

// TempDir implements Storage.
func (s *LocalStorage) TempDir(_ context.Context) (string, error) {
	dir := filepath.Join(s.root, tempDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- served by Caddy in prod
		return "", fmt.Errorf("media: create temp dir %s: %w", dir, err)
	}
	return dir, nil
}

// SweepTemp best-effort removes stale temporary files a crashed or
// interrupted process may have left behind: Service's pre-processing
// spool files under TempDir(), and Put's atomic-write ".upload-*" files
// under any destination directory (Review B MINOR 10). Anything modified
// more than maxAge ago is removed; anything newer is left alone — it may
// be a genuinely in-flight upload. Intended to run once at startup
// (cmd/api/main.go, before serving traffic); this is housekeeping, not a
// correctness requirement, so a failure to remove any one file is logged
// and otherwise ignored rather than aborting the sweep.
func (s *LocalStorage) SweepTemp(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	// os.Root confines every operation below to s.root even under a
	// concurrent rename/symlink race (gosec G122: a plain filepath.WalkDir
	// + os.Remove pairing is TOCTOU-prone — the path a callback receives
	// can change identity between the walk's stat and the eventual
	// Remove; Root re-resolves each call within its own directory handle
	// instead of trusting an assembled path string).
	root, err := os.OpenRoot(s.root)
	if err != nil {
		slog.Warn("media: sweep: failed to open media root", "path", s.root, "error", err)
		return
	}
	defer func() { _ = root.Close() }()

	// fs.FS paths are always "/"-separated (io/fs's contract), regardless
	// of OS — unlike the filepath-based paths the rest of this file uses.
	tempPrefix := tempDirName + "/"
	_ = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		underTempDir := path == tempDirName || strings.HasPrefix(path, tempPrefix)
		isUploadTemp := strings.HasPrefix(d.Name(), ".upload-")
		if !underTempDir && !isUploadTemp {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return nil
		}
		if err := root.Remove(path); err != nil {
			slog.Warn("media: sweep: failed to remove stale temp file", "path", path, "error", err)
		}
		return nil
	})
}

// joinURL prefixes key with baseURL, trimming baseURL's trailing slash
// first so the two never collide into a double slash. Shared by
// LocalStorage.URL and the package-level URLs helper (keys.go) so a
// stored key becomes a URL exactly the same way regardless of which of
// the two call sites builds it.
func joinURL(baseURL, key string) string {
	return strings.TrimSuffix(baseURL, "/") + "/" + key
}
