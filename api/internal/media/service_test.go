package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Test Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop: %v", err)
	}
	return shopRow
}

// seedUser creates one active user directly through sqlc's generated
// queries, for tests that only need a valid uploaded_by FK target — the
// password hash is a placeholder, never verified here.
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string) db.User {
	t.Helper()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: "not-a-real-hash",
		FullName: "Test User " + username, Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return user
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage(w, h)); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solidImage(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// withFakeEXIF inserts a synthetic APP1 "Exif" segment right after a
// JPEG's SOI marker — the same mechanism a polyglot file (one crafted to
// also be valid as some other format via an embedded payload) would use
// to smuggle a second payload inside an otherwise-ordinary JPEG. Go's
// image/jpeg decoder skips any APPn segment it doesn't specifically parse
// (APP0/APP14), so this is decodable like any ordinary EXIF-carrying JPEG
// a camera or phone produces.
func withFakeEXIF(jpg []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("FAKE-EXIF-PAYLOAD-FOR-TEST-ONLY")...)
	segLen := len(payload) + 2
	if segLen > 0xFFFF {
		panic("test fixture: fake EXIF payload too large for a 2-byte JPEG segment length")
	}
	var lenBytes [2]byte
	binary.BigEndian.PutUint16(lenBytes[:], uint16(segLen))
	seg := append([]byte{0xFF, 0xE1}, lenBytes[:]...)
	seg = append(seg, payload...)

	out := make([]byte, 0, len(jpg)+len(seg))
	out = append(out, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	return out
}

// craftedPNGHeader hand-builds a minimal, otherwise-empty PNG (signature +
// IHDR + IEND, no pixel data at all) declaring width×height. image.Decode
// would fail on it (there is no IDAT chunk to decode pixels from) — which
// is exactly why these tests use it only to prove image.DecodeConfig-based
// rejection happens *before* Upload ever calls the real image.Decode: a
// well-formed PNG decoder only needs to read through IHDR to answer
// DecodeConfig, so this is enough to make Upload see the claimed
// dimensions without ever needing a real, fully-decodable multi-gigabyte
// image on disk or in memory.
func craftedPNGHeader(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, width)
	_ = binary.Write(&ihdr, binary.BigEndian, height)
	ihdr.WriteByte(8) // bit depth
	ihdr.WriteByte(6) // color type: truecolor + alpha
	ihdr.WriteByte(0) // compression method
	ihdr.WriteByte(0) // filter method
	ihdr.WriteByte(0) // interlace method
	writePNGChunk(&buf, "IHDR", ihdr.Bytes())
	writePNGChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writePNGChunk(buf *bytes.Buffer, typ string, data []byte) {
	n := len(data)
	if n > 0xFFFFFFFF {
		panic("test fixture: chunk data too large for a 4-byte PNG chunk length")
	}
	var lenBytes [4]byte
	binary.BigEndian.PutUint32(lenBytes[:], uint32(n))
	buf.Write(lenBytes[:])
	chunk := append([]byte(typ), data...)
	buf.Write(chunk)
	var crcBytes [4]byte
	binary.BigEndian.PutUint32(crcBytes[:], crc32.ChecksumIEEE(chunk))
	buf.Write(crcBytes[:])
}

func newTestService(t *testing.T) (*Service, *db.Queries, db.Shop, db.User) {
	return newTestServiceWithConcurrency(t, 2)
}

// newTestServiceWithConcurrency uses a generously large admission queue
// (10) — plenty of headroom above any test in this file's own concurrent
// goroutine count — so tests exercising the decode semaphore (sem) don't
// incidentally also hit the outer admission gate (queue) and get an
// unexpected 429. TestUpload_admissionQueueRejectsWhenFull, below, is the
// one test that deliberately wants a small queue and uses
// newTestServiceWithLimits directly instead.
func newTestServiceWithConcurrency(t *testing.T, concurrency int) (*Service, *db.Queries, db.Shop, db.User) {
	return newTestServiceWithLimits(t, concurrency, 10)
}

func newTestServiceWithLimits(t *testing.T, concurrency, queueSize int) (*Service, *db.Queries, db.Shop, db.User) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "media-shop")
	userRow := seedUser(ctx, t, q, shopRow.ID, "uploader")

	storage, err := NewLocalStorage(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	svc := NewService(q, storage, "/media", 10<<20, concurrency, queueSize)
	return svc, q, shopRow, userRow
}

func TestUpload_pngCreatesThreeDerivativesOnly(t *testing.T) {
	svc, q, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := pngBytes(t, 800, 600)
	row, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Review B follow-up MINOR (after O-16): the row records the "_full"
	// derivative's own mime/size/dimensions — what a GET actually
	// serves — not the original PNG's. Source is 800x600, under the full
	// derivative's 1600px target, so its dimensions stay 800x600 (never
	// upscale, docs/06-ROADMAP.md Phase 2 T3 spec); its byte size is
	// whatever WebP re-encoding produced, checked below against the
	// actual stored bytes rather than a hardcoded number.
	if row.Mime != "image/webp" {
		t.Errorf("Mime = %q, want image/webp", row.Mime)
	}
	if row.Width == nil || *row.Width != 800 || row.Height == nil || *row.Height != 600 {
		t.Errorf("dimensions = %+v/%+v, want 800/600", row.Width, row.Height)
	}

	// The row is actually persisted (not just returned in memory).
	fetched, err := q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: shopRow.ID, ID: row.ID})
	if err != nil {
		t.Fatalf("GetMediaFile: %v", err)
	}
	if fetched.StorageKey != row.StorageKey {
		t.Errorf("fetched StorageKey = %q, want %q", fetched.StorageKey, row.StorageKey)
	}

	// O-16: the original is never stored — there is no file at the bare
	// stem itself, only at the three derivative keys.
	if rc, err := svc.storage.Open(ctx, row.StorageKey); err == nil {
		_ = rc.Close()
		t.Fatalf("an original file exists on disk at the bare stem %q; O-16 says it must not", row.StorageKey)
	}

	// Three derivatives exist, are valid WebP, and have the expected
	// longest side.
	wants := map[string]struct{ w, h int }{
		suffixThumb: {200, 150},
		suffixCard:  {600, 450},
		// Source is 800x600, under the full derivative's 1600px target —
		// never upscale (docs/06-ROADMAP.md Phase 2 T3 spec).
		suffixFull: {800, 600},
	}
	for suffix, want := range wants {
		key := derivativeKey(row.StorageKey, suffix)
		rc, err := svc.storage.Open(ctx, key)
		if err != nil {
			t.Fatalf("Open derivative %s: %v", suffix, err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read derivative %s: %v", suffix, err)
		}
		img, format, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("decode derivative %s: %v", suffix, err)
		}
		if format != "webp" {
			t.Errorf("derivative %s format = %q, want webp", suffix, format)
		}
		b := img.Bounds()
		if b.Dx() != want.w || b.Dy() != want.h {
			t.Errorf("derivative %s bounds = %dx%d, want %dx%d", suffix, b.Dx(), b.Dy(), want.w, want.h)
		}
		if suffix == suffixFull && row.SizeBytes != int64(len(raw)) {
			t.Errorf("row.SizeBytes = %d, want %d (the _full derivative's actual byte size)", row.SizeBytes, len(raw))
		}
	}
}

func TestUpload_dedupeBySHA256(t *testing.T) {
	svc, q, shopRow, userRow := newTestService(t)
	ctx := context.Background()
	data := pngBytes(t, 400, 300)

	first, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("first Upload: %v", err)
	}

	second, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("second Upload: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second upload id = %s, want same id %s (dedupe)", second.ID, first.ID)
	}

	rows, err := q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: shopRow.ID, ID: first.ID})
	if err != nil {
		t.Fatalf("GetMediaFile: %v", err)
	}
	if rows.ID != first.ID {
		t.Fatalf("unexpected row after dedupe: %+v", rows)
	}
}

// TestUpload_polyglotJPEGYieldsDerivativesOnly is Review B MAJOR 3 / O-16's
// direct test: a JPEG carrying an embedded second payload (simulated here
// with a fake EXIF segment, the same smuggling mechanism a genuine
// polyglot file — one valid as two different formats at once — would use)
// never reaches disk itself. Only its three re-encoded derivatives do,
// and none of them carry the payload.
func TestUpload_polyglotJPEGYieldsDerivativesOnly(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := withFakeEXIF(jpegBytes(t, 640, 480))
	if !bytes.Contains(data, []byte("Exif")) {
		t.Fatal("test setup bug: fake EXIF not present in source JPEG")
	}

	row, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if rc, err := svc.storage.Open(ctx, row.StorageKey); err == nil {
		_ = rc.Close()
		t.Fatalf("an original file exists on disk at the bare stem %q; O-16 says it must not", row.StorageKey)
	}

	for _, suffix := range []string{suffixThumb, suffixCard, suffixFull} {
		key := derivativeKey(row.StorageKey, suffix)
		rc, err := svc.storage.Open(ctx, key)
		if err != nil {
			t.Fatalf("Open derivative %s: %v", suffix, err)
		}
		derived, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read derivative %s: %v", suffix, err)
		}
		if bytes.Contains(derived, []byte("Exif")) {
			t.Errorf("derivative %s carried the injected payload; re-encoding should have dropped it", suffix)
		}
	}
}

func TestUpload_rejectsNonImage(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, strings.NewReader("this is not an image, just text"))
	assertValidationFile(t, err, "invalid")
}

func TestUpload_rejectsOversizedDimension(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := pngBytes(t, 9000, 100)
	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	assertValidationFile(t, err, "invalid")
}

func TestUpload_rejectsOverMaxBytes(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	svc.maxBytes = 100 // shrink the cap so the test doesn't need to build a real 10 MiB+ file
	ctx := context.Background()

	oversized := bytes.Repeat([]byte{0xAB}, 101)
	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(oversized))
	assertValidationFile(t, err, "too_long")
}

// TestUpload_rejectsDecompressionBombWithoutFullDecode is Review B
// CRITICAL 1's test: a PNG whose header alone declares 30000×30000 (3.6
// billion claimed pixels) must be rejected via image.DecodeConfig's
// cheap, header-only read — never via a real image.Decode, which would
// try to allocate a pixel buffer around 3.6 GB for this. craftedPNGHeader
// builds a file with no pixel data at all, so if Upload's ordering ever
// regressed to decode-then-check, this would fail with either an
// out-of-memory condition or, at minimum, a large jump in allocated
// memory — which the runtime.MemStats comparison below asserts against
// directly, in addition to checking the expected 400.
func TestUpload_rejectsDecompressionBombWithoutFullDecode(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := craftedPNGHeader(t, 30000, 30000)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	assertValidationFile(t, err, "invalid")

	allocated := after.TotalAlloc - before.TotalAlloc
	const tooMuch = 50 << 20 // 50 MiB — a real decode would need ~3.6 GB
	t.Logf("TotalAlloc grew by %d bytes (%.2f MB) rejecting a 30000x30000 header", allocated, float64(allocated)/(1<<20))
	if allocated > tooMuch {
		t.Fatalf("TotalAlloc grew by %d bytes (%.1f MB), want < %d MB — looks like the full image.Decode ran", allocated, float64(allocated)/(1<<20), tooMuch>>20)
	}
}

// TestUpload_rejectsHighMegapixelDimensions covers the other half of
// Review B CRITICAL 1: 6000×5000 stays under maxDimension (8000) on both
// sides individually, but its 30-megapixel product exceeds maxPixels (24
// MP) and must still be rejected — again via the cheap DecodeConfig path,
// not a real decode of a ~30 MP image.
func TestUpload_rejectsHighMegapixelDimensions(t *testing.T) {
	svc, _, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := craftedPNGHeader(t, 6000, 5000)
	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	assertValidationFile(t, err, "invalid")
}

// TestAcquireDeriveSlot_respectsContextCancellation is Review B MAJOR 4's
// direct, deterministic test of the one behavior that's otherwise hard to
// observe through a full Upload call: a request queued for a decode slot
// must give up the instant its context is done, not wait for the slot
// regardless.
func TestAcquireDeriveSlot_respectsContextCancellation(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // fill the only slot so acquiring would otherwise block forever

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := acquireDeriveSlot(ctx, sem)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireDeriveSlot with an already-canceled ctx = %v, want context.Canceled", err)
	}
}

// TestUpload_concurrencyIsBounded is Review B MAJOR 4's test that the
// semaphore is exercised, not merely configured: with concurrency forced
// to 1, three uploads run concurrently must still all succeed, and
// Service's own inFlight/maxInFlight bookkeeping (service.go) must never
// have observed more than one goroutine inside the decode/derive section
// at once — a hard invariant a size-1 buffered channel guarantees
// regardless of how the goroutines happen to be scheduled, so this
// assertion is not a timing-dependent probability.
func TestUpload_concurrencyIsBounded(t *testing.T) {
	svc, _, shopRow, userRow := newTestServiceWithConcurrency(t, 1)
	ctx := context.Background()

	const n = 3
	// Built up front, not inside the goroutines below: t.Fatalf (which
	// pngBytes/t.Helper can reach) is documented as safe only from the
	// goroutine running the test itself.
	datasets := make([][]byte, n)
	for i := range datasets {
		// Distinct pixels per upload so sha256 dedupe doesn't collapse
		// these into a single stored file — each must independently take
		// a turn through the semaphore.
		datasets[i] = pngBytes(t, 64+i, 64)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			row, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(datasets[i]))
			errs[i] = err
			ids[i] = row.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("upload %d: %v", i, err)
		}
	}
	seen := map[uuid.UUID]bool{}
	for i, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if seen[id] {
			t.Errorf("upload %d produced a duplicate id %s", i, id)
		}
		seen[id] = true
	}

	if observedMax := svc.maxInFlight.Load(); observedMax > 1 {
		t.Fatalf("maxInFlight = %d, want <= 1 (concurrency was set to 1)", observedMax)
	}
}

// TestUpload_respectsCtxCancellationWhileWaitingForASlot exercises the
// same behavior as TestAcquireDeriveSlot_respectsContextCancellation but
// through the real Upload path: with the single slot held by one
// in-flight upload, a second upload whose context is already canceled
// must return promptly with that cancellation, not block.
func TestUpload_respectsCtxCancellationWhileWaitingForASlot(t *testing.T) {
	svc, _, shopRow, userRow := newTestServiceWithConcurrency(t, 1)

	// Hold the only slot for the duration of this test.
	svc.sem <- struct{}{}
	defer func() { <-svc.sem }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	data := pngBytes(t, 64, 64)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Upload with a context that will expire while queued = %v, want context.DeadlineExceeded (wrapped)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Upload did not return within 2s of its context expiring while waiting for a decode slot")
	}
}

// TestUpload_admissionQueueRejectsWhenFull is the Review B follow-up
// MAJOR's test: with the admission queue and the decode semaphore both
// forced to size 1, a second upload attempted while the first is still
// in flight (spooled, admitted, but blocked waiting for the — already
// held — decode slot) must be rejected immediately with 429
// RATE_LIMITED, never spool its own bytes and block. Once the first
// upload's decode slot frees up and it finishes, further uploads must
// succeed again sequentially — the queue slot the rejected second
// upload never held is not somehow left stuck.
func TestUpload_admissionQueueRejectsWhenFull(t *testing.T) {
	svc, _, shopRow, userRow := newTestServiceWithLimits(t, 1, 1)
	ctx := context.Background()

	// Hold the only decode slot so the first upload's real Upload call
	// gets past admission (occupying the only queue slot) and then
	// blocks waiting for a decode slot — keeping that queue slot
	// occupied for as long as this test wants, deterministically.
	svc.sem <- struct{}{}

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(pngBytes(t, 64, 64)))
		firstDone <- err
	}()

	// Wait until the first upload has actually taken the queue slot
	// (fast — admission happens before spooling — but still
	// asynchronous relative to this goroutine) before asserting the
	// second is rejected; poll rather than a fixed sleep.
	deadline := time.Now().Add(2 * time.Second)
	for len(svc.queue) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("first upload never occupied the admission queue within 2s")
		}
		time.Sleep(time.Millisecond)
	}

	_, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(pngBytes(t, 65, 64)))
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("second concurrent upload err = %v, want a 429 RATE_LIMITED *apierr.Error", err)
	}

	// Release the artificially-held decode slot so the first upload can
	// proceed and finish.
	<-svc.sem
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first upload: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first upload did not finish within 2s of its decode slot freeing up")
	}

	// Sequential uploads after that must all succeed — the rejected
	// second upload never held (and so never leaked) a queue slot.
	if _, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(pngBytes(t, 66, 64))); err != nil {
		t.Fatalf("sequential upload 1: %v", err)
	}
	if _, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(pngBytes(t, 67, 64))); err != nil {
		t.Fatalf("sequential upload 2: %v", err)
	}
}

// assertValidationFile fails the test unless err is an *apierr.Error
// carrying fields.file = want.
func assertValidationFile(t *testing.T, err error, want string) {
	t.Helper()
	apiErr, ok := err.(*apierr.Error)
	if !ok {
		t.Fatalf("error = %v (%T), want *apierr.Error", err, err)
	}
	fields, _ := apiErr.Details["fields"].(map[string]string)
	if fields["file"] != want {
		t.Fatalf("details.fields.file = %q, want %q", fields["file"], want)
	}
}
