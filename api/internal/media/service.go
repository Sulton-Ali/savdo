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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	_ "github.com/gen2brain/webp" // registers the "webp" image.Decode format (init); Encode is called directly from derive.go

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// decodeFormatByMime is both the set of MIME types this module accepts
// (docs/06-ROADMAP.md Phase 2 T3 spec: jpg|png|webp) and the format name
// image.Decode/image.DecodeConfig report for each, via the decoders
// registered above and in derive.go's blank/named gen2brain/webp import.
// Upload compares this against the format actually decoded, so a body
// whose magic bytes claim one type but whose content decodes as something
// else (or not at all) is caught rather than trusted. Any sniffed MIME
// not in this map — including a text file, a GIF, or anything
// http.DetectContentType doesn't recognize as an image at all — is
// rejected before decoding is even attempted.
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

	// queue is a non-blocking admission gate acquired BEFORE spooling a
	// single byte to disk, held for the entire Upload call (Review B
	// follow-up MAJOR): without it, an unbounded number of requests could
	// each spool up to MediaMaxBytes into TempDir() and then queue up
	// behind sem below, filling MEDIA_DIR/.tmp under sustained
	// concurrency even though only a bounded number can ever be actively
	// decoding at once. A full queue means "the server already has as
	// many uploads in flight as it's willing to hold" — Upload rejects
	// immediately with 429 RATE_LIMITED rather than spooling and then
	// blocking, so a client sees back-pressure instead of the request
	// just hanging.
	queue chan struct{}

	// sem bounds how many uploads may be decoding/deriving at once
	// (Review B MAJOR 4 — image.Decode and the resize/WebP-encode pass
	// are the CPU- and memory-heavy part of an upload; without a cap,
	// concurrent requests each allocating a decoded-image-sized buffer
	// can drive the process's memory far past what any single upload's
	// own size and dimension limits suggest). Smaller than queue: queue
	// bounds how many uploads may be spooled/waiting at all, sem bounds
	// how many of those may actually be decoding at once. A field rather
	// than a literal package-level variable: this process constructs
	// exactly one Service, so the two are equivalent in production, but a
	// field keeps every test's Service — and its limits — isolated from
	// every other test's, instead of every test in this package
	// contending over shared global channels.
	sem chan struct{}

	// inFlight and maxInFlight track how many goroutines are inside sem's
	// guarded section at once. Production code never reads them; they
	// exist so this package's own tests can assert the concurrency cap
	// actually holds (service_test.go's TestUpload_concurrencyIsBounded)
	// instead of only trusting that make(chan struct{}, n) does what it
	// says.
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

// NewService builds a Service. maxBytes is Config.MediaMaxBytes; baseURL
// is Config.MediaBaseURL (used to build the MediaUrls a caller of
// toGenMediaFile gets back); concurrency is Config.MediaConcurrency and
// queueSize is Config.MediaQueue (both at least 1 — a non-positive value
// is treated as 1 rather than creating an unusable zero-capacity channel
// that would reject or block every upload).
func NewService(q *db.Queries, storage Storage, baseURL string, maxBytes int64, concurrency int, queueSize int) *Service {
	if concurrency < 1 {
		concurrency = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	return &Service{
		q: q, storage: storage, baseURL: baseURL, maxBytes: maxBytes,
		sem:   make(chan struct{}, concurrency),
		queue: make(chan struct{}, queueSize),
	}
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
// request — Handler.UploadMedia locates it), stores three WebP
// derivatives and records a media_files row. Per orchestrator decision
// O-16, the original's bytes are never written to disk at all — only
// decoded (to build the derivatives), hashed (for dedupe) and then
// discarded. If shopID has already uploaded these exact bytes, the
// existing row is returned unchanged instead — no new files, no new row
// (sha256 dedupe, docs/06-ROADMAP.md Phase 2 T3 spec).
//
// Every returned *apierr.Error is one of the three the spec (and its
// Review B follow-up) calls for: fields.file: too_long (over
// MediaMaxBytes), fields.file: invalid (unsupported/undecodable format, a
// dimension over 8000px, or more than 24 megapixels — Review B CRITICAL
// 1), or a 429 RATE_LIMITED when the admission queue is full (Review B
// follow-up MAJOR). Any other error is wrapped for the caller to turn
// into a 500.
func (s *Service) Upload(ctx context.Context, shopID, userID uuid.UUID, part io.Reader) (db.MediaFile, error) {
	// Admission gate, tried before a single byte is spooled to disk: a
	// full queue means back-pressure now (429, Retry-After: 5s), never a
	// request that spools its whole file and then blocks indefinitely
	// waiting for a decode slot (Review B follow-up MAJOR).
	select {
	case s.queue <- struct{}{}:
	default:
		return db.MediaFile{}, apierr.RateLimited(5)
	}
	defer func() { <-s.queue }()

	tmp, _, err := s.spool(ctx, part)
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
	wantFormat, ok := decodeFormatByMime[mime]
	if !ok {
		return db.MediaFile{}, apierr.Validation(map[string]string{"file": "invalid"})
	}

	// Read only the header — image.DecodeConfig, never the full
	// image.Decode — to learn the claimed dimensions before committing to
	// an allocation sized by them. This is what makes a decompression
	// bomb (a tiny file whose header claims an enormous width×height)
	// cheap to reject: without this step, the dimension check below would
	// run only *after* image.Decode had already allocated the full pixel
	// buffer (Review B CRITICAL 1).
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: seek upload: %w", err)
	}
	imgCfg, cfgFormat, err := image.DecodeConfig(tmp)
	if err != nil || cfgFormat != wantFormat {
		return db.MediaFile{}, apierr.Validation(map[string]string{"file": "invalid"})
	}
	if imgCfg.Width <= 0 || imgCfg.Height <= 0 ||
		imgCfg.Width > maxDimension || imgCfg.Height > maxDimension ||
		imgCfg.Width*imgCfg.Height > maxPixels {
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

	// The real decode and the resize/encode passes are the expensive part
	// (CPU and, for decode, an allocation on the order of
	// width*height*bytes-per-pixel) — gated by sem so only a bounded
	// number of uploads do this at once process-wide (Review B MAJOR 4).
	// acquireDeriveSlot still respects ctx: a request whose client gave up
	// while queued for a slot must not go on to decode anyway.
	if err := acquireDeriveSlot(ctx, s.sem); err != nil {
		return db.MediaFile{}, fmt.Errorf("media: wait for a decode slot: %w", err)
	}
	derived, err := func() (map[string]derivative, error) {
		defer func() { <-s.sem }()

		cur := s.inFlight.Add(1)
		defer s.inFlight.Add(-1)
		for {
			prevMax := s.maxInFlight.Load()
			if cur <= prevMax || s.maxInFlight.CompareAndSwap(prevMax, cur) {
				break
			}
		}

		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("media: seek upload: %w", err)
		}
		img, format, err := image.Decode(tmp)
		if err != nil || format != wantFormat {
			return nil, apierr.Validation(map[string]string{"file": "invalid"})
		}
		if img.Bounds().Dx() != imgCfg.Width || img.Bounds().Dy() != imgCfg.Height {
			// Never expected for a well-formed file — if it ever
			// happened, the DecodeConfig-based bomb check above would
			// have validated the wrong numbers.
			return nil, apierr.Validation(map[string]string{"file": "invalid"})
		}

		result := make(map[string]derivative, len(derivativeSizes))
		for _, d := range derivativeSizes {
			resized := resize(img, d.side)
			buf, err := encodeWebP(resized)
			if err != nil {
				return nil, fmt.Errorf("media: build derivative: %w", err)
			}
			b := resized.Bounds()
			result[d.suffix] = derivative{data: buf, width: b.Dx(), height: b.Dy()}
		}
		return result, nil
	}()
	if err != nil {
		return db.MediaFile{}, err
	}

	id := newID()
	now := time.Now().UTC()
	stem := keyStem(shopID, id, now.Year(), int(now.Month()))

	var written []string
	cleanup := func() {
		for _, k := range written {
			_ = s.storage.Delete(context.Background(), k)
		}
	}

	for _, d := range derivativeSizes {
		dv := derived[d.suffix]
		dKey := derivativeKey(stem, d.suffix)
		if err := s.storage.Put(ctx, dKey, bytes.NewReader(dv.data), int64(len(dv.data)), "image/webp"); err != nil {
			cleanup()
			return db.MediaFile{}, fmt.Errorf("media: store derivative: %w", err)
		}
		written = append(written, dKey)
	}

	// media_files records the "_full" derivative's own mime/size/
	// dimensions, not the original's (Review B follow-up MINOR, after
	// O-16): "_full" is what a GET actually serves, and the original's
	// bytes never reach disk to measure in the first place.
	full := derived[suffixFull]
	width := dimensionToInt32(full.width)
	height := dimensionToInt32(full.height)
	row, err := s.q.CreateMediaFile(ctx, db.CreateMediaFileParams{
		ID:         id,
		ShopID:     shopID,
		StorageKey: stem,
		Mime:       "image/webp",
		SizeBytes:  int64(len(full.data)),
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

// derivativeSizes is the three WebP derivatives Upload always builds, and
// the longest-side target resize scales each one to.
var derivativeSizes = []struct {
	suffix string
	side   int
}{
	{suffixThumb, thumbLongestSide},
	{suffixCard, cardLongestSide},
	{suffixFull, fullLongestSide},
}

// derivative is one built WebP derivative's bytes and actual pixel
// dimensions (which may be smaller than the original's — resize scales
// down, never up). media_files' mime/size_bytes/width/height are recorded
// from the "_full" derivative specifically (see Upload), not the
// original: it's what GET requests actually receive, and per O-16 the
// original's own bytes never reach disk to measure at all.
type derivative struct {
	data          []byte
	width, height int
}

// acquireDeriveSlot blocks until a slot in sem is free or ctx is done,
// whichever comes first. Split out from Upload so its ctx-cancellation
// behavior — the specific thing Review B MAJOR 4 calls for — can be
// tested directly, deterministically, without needing a real image
// decode to race against.
func acquireDeriveSlot(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

// spool copies part into a new temp file under storage's TempDir (Review
// B MINOR 5 — never os.TempDir(): the scratch file stays under the same
// disk/quota and permission boundary as everything else this module
// writes, and under LocalStorage's ".tmp", which the dev static handler
// refuses to ever serve by name). The copy is capped at maxBytes+1 so an
// over-limit upload is detected without ever buffering more than
// maxBytes+1 bytes in flight (docs/06-ROADMAP.md Phase 2 T3 known traps:
// "Do not load the whole body into memory before the cap check"). The
// caller owns the returned file — it is left open and seeked to the
// start, ready to read — and must close and remove it; a file this
// leaves behind after a crash is swept by LocalStorage.SweepTemp on the
// next startup.
func (s *Service) spool(ctx context.Context, part io.Reader) (*os.File, int64, error) {
	dir, err := s.storage.TempDir(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("media: get temp dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "upload-*")
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
// image.Config's Width/Height can never be negative for a successfully
// decoded header, and maxDimension (8000) is far under int32's range
// either way.
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
