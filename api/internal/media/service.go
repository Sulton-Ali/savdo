package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the "jpeg" image.Decode format
	_ "image/png"  // registers the "png" image.Decode format
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	_ "github.com/gen2brain/webp" // registers the "webp" image.Decode format (init); Encode is called directly from derive.go

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// extByMime maps the three formats this module accepts to the extension
// its stored original uses (docs/06-ROADMAP.md Phase 2 T3 spec: "ext from
// the sniffed type, never from the filename"). Any other sniffed MIME —
// including a text file, a GIF, or anything http.DetectContentType
// doesn't recognize as an image at all — is rejected before decoding is
// even attempted.
var extByMime = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

// decodeFormatByMime is the name image.Decode reports for each of the
// three accepted MIME types, via the decoders registered above and in
// derive.go's blank/named gen2brain/webp import. Upload compares this
// against the format image.Decode actually returns, so a body whose magic
// bytes claim one type but whose content decodes as something else (or
// not at all) is caught rather than trusted.
var decodeFormatByMime = map[string]string{
	"image/jpeg": "jpeg",
	"image/png":  "png",
	"image/webp": "webp",
}

// Service validates, stores and records uploaded images.
type Service struct {
	q        *db.Queries
	storage  Storage
	baseURL  string
	maxBytes int64
}

// NewService builds a Service. maxBytes is Config.MediaMaxBytes; baseURL is
// Config.MediaBaseURL (used to build the MediaUrls a caller of ToMediaFile
// gets back).
func NewService(q *db.Queries, storage Storage, baseURL string, maxBytes int64) *Service {
	return &Service{q: q, storage: storage, baseURL: baseURL, maxBytes: maxBytes}
}

// BaseURL returns the configured media base URL, for Handler.
func (s *Service) BaseURL() string { return s.baseURL }

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// shop.newID.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// Upload validates part (the multipart "file" part of a POST /media
// request — Handler.UploadMedia locates it), stores the original plus
// three WebP derivatives, and records a media_files row. If shopID has
// already uploaded these exact bytes, the existing row is returned
// unchanged instead — no new files, no new row (sha256 dedupe,
// docs/06-ROADMAP.md Phase 2 T3 spec).
//
// Every returned *apierr.Error is one of the two the spec calls for:
// fields.file: too_long (over MediaMaxBytes) or fields.file: invalid
// (unsupported/undecodable format, or a dimension over 8000px). Any other
// error is wrapped for the caller to turn into a 500.
func (s *Service) Upload(ctx context.Context, shopID, userID uuid.UUID, part io.Reader) (db.MediaFile, error) {
	tmp, size, err := s.spool(part)
	if err != nil {
		return db.MediaFile{}, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	mime, err := sniff(tmp)
	if err != nil {
		return db.MediaFile{}, fmt.Errorf("media: sniff upload: %w", err)
	}
	ext, ok := extByMime[mime]
	if !ok {
		return db.MediaFile{}, apierr.Validation(map[string]string{"file": "invalid"})
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: seek upload: %w", err)
	}
	img, format, err := image.Decode(tmp)
	if err != nil || format != decodeFormatByMime[mime] {
		return db.MediaFile{}, apierr.Validation(map[string]string{"file": "invalid"})
	}
	bounds := img.Bounds()
	if bounds.Dx() > maxDimension || bounds.Dy() > maxDimension {
		return db.MediaFile{}, apierr.Validation(map[string]string{"file": "invalid"})
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: seek upload: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, tmp); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: hash upload: %w", err)
	}
	sum := h.Sum(nil)

	if existing, ok, err := s.findBySHA256(ctx, shopID, sum); err != nil {
		return db.MediaFile{}, err
	} else if ok {
		return existing, nil
	}

	id := newID()
	now := time.Now().UTC()
	key := originalKey(shopID, id, now.Year(), int(now.Month()), ext)

	var written []string
	cleanup := func() {
		for _, k := range written {
			_ = s.storage.Delete(context.Background(), k)
		}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: seek upload: %w", err)
	}
	if err := s.storage.Put(ctx, key, tmp, size, mime); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: store original: %w", err)
	}
	written = append(written, key)

	derivatives := []struct {
		suffix string
		side   int
	}{
		{suffixThumb, thumbLongestSide},
		{suffixCard, cardLongestSide},
		{suffixFull, fullLongestSide},
	}
	for _, d := range derivatives {
		buf, err := encodeWebP(resize(img, d.side))
		if err != nil {
			cleanup()
			return db.MediaFile{}, fmt.Errorf("media: build derivative: %w", err)
		}
		dKey := derivativeKey(key, d.suffix)
		if err := s.storage.Put(ctx, dKey, bytes.NewReader(buf), int64(len(buf)), "image/webp"); err != nil {
			cleanup()
			return db.MediaFile{}, fmt.Errorf("media: store derivative: %w", err)
		}
		written = append(written, dKey)
	}

	width := dimensionToInt32(bounds.Dx())
	height := dimensionToInt32(bounds.Dy())
	row, err := s.q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID:         id,
		ShopID:     shopID,
		StorageKey: key,
		Mime:       mime,
		SizeBytes:  size,
		Width:      &width,
		Height:     &height,
		Sha256:     sum,
		UploadedBy: &userID,
	})
	if err != nil {
		cleanup()
		if isSHA256Conflict(err) {
			// Lost a race with a concurrent upload of the same bytes for
			// this shop: our own dedupe check above ran before either
			// insert committed. Return the row that won, exactly as if
			// that check had found it in the first place (spec: "same
			// bytes again -> same id, no new files").
			if existing, ok, getErr := s.findBySHA256(ctx, shopID, sum); getErr == nil && ok {
				return existing, nil
			}
		}
		return db.MediaFile{}, fmt.Errorf("media: create media_files row: %w", err)
	}
	return row, nil
}

// findBySHA256 looks up shopID's existing media_files row for sum, if any.
func (s *Service) findBySHA256(ctx context.Context, shopID uuid.UUID, sum []byte) (db.MediaFile, bool, error) {
	row, err := s.q.GetMediaFileBySHA256(ctx, db.GetMediaFileBySHA256Params{ShopID: shopID, Sha256: sum})
	switch {
	case err == nil:
		return row, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return db.MediaFile{}, false, nil
	default:
		return db.MediaFile{}, false, fmt.Errorf("media: look up by sha256: %w", err)
	}
}

// isSHA256Conflict reports whether err is a unique-violation on
// media_files' (shop_id, sha256) constraint.
func isSHA256Conflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "media_files_shop_id_sha256_key"
}

// spool copies part into a new temp file, capped at maxBytes+1 so an
// over-limit upload is detected without ever buffering more than
// maxBytes+1 bytes in flight (docs/06-ROADMAP.md Phase 2 T3 known traps:
// "Do not load the whole body into memory before the cap check"). The
// caller owns the returned file — it is left open and seeked to the
// start, ready to read — and must close and remove it.
func (s *Service) spool(part io.Reader) (*os.File, int64, error) {
	tmp, err := os.CreateTemp("", "savdo-media-upload-*")
	if err != nil {
		return nil, 0, fmt.Errorf("media: create temp file: %w", err)
	}

	n, err := io.Copy(tmp, io.LimitReader(part, s.maxBytes+1))
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("media: spool upload: %w", err)
	}
	if n > s.maxBytes {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, apierr.Validation(map[string]string{"file": "too_long"})
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("media: seek temp file: %w", err)
	}
	return tmp, n, nil
}

// dimensionToInt32 converts a decoded image's pixel width or height —
// already checked against maxDimension (derive.go) before Upload ever
// calls this — to the int32 media_files.width/height columns use. The
// clamp here, not just the earlier validation, is what makes the
// narrowing conversion provably safe at its own call site (gosec G115):
// an image.Rectangle's Dx()/Dy() can never be negative for a decoded
// image, and maxDimension (8000) is far under int32's range either way.
func dimensionToInt32(n int) int32 {
	if n < 0 {
		n = 0
	}
	if n > maxDimension {
		n = maxDimension
	}
	return int32(n)
}

// sniff reads up to the first 512 bytes of f (http.DetectContentType never
// needs more) and returns the sniffed MIME type. The caller is responsible
// for seeking f back to the start afterward.
func sniff(f *os.File) (string, error) {
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read: %w", err)
	}
	return http.DetectContentType(buf[:n]), nil
}
