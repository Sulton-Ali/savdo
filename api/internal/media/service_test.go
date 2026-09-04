package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

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
// JPEG's SOI marker — enough for a test to assert the original keeps it
// and every WebP derivative (re-encoded from the decoded pixels, not
// copied bytes) does not. Go's image/jpeg decoder skips any APPn segment
// it doesn't specifically parse (APP0/APP14), so this is decodable like
// any ordinary EXIF-carrying JPEG a camera or phone produces.
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

func newTestService(t *testing.T) (*Service, *db.Queries, db.Shop, db.User) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()
	shopRow := seedShop(ctx, t, q, "media-shop")
	userRow := seedUser(ctx, t, q, shopRow.ID, "uploader")

	storage := NewLocalStorage(t.TempDir(), "/media")
	svc := NewService(q, storage, "/media", 10<<20)
	return svc, q, shopRow, userRow
}

func TestUpload_pngCreatesOriginalAndThreeDerivatives(t *testing.T) {
	svc, q, shopRow, userRow := newTestService(t)
	ctx := context.Background()

	data := pngBytes(t, 800, 600)
	row, err := svc.Upload(ctx, shopRow.ID, userRow.ID, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if row.Mime != "image/png" {
		t.Errorf("Mime = %q, want image/png", row.Mime)
	}
	if row.Width == nil || *row.Width != 800 || row.Height == nil || *row.Height != 600 {
		t.Errorf("dimensions = %+v/%+v, want 800/600", row.Width, row.Height)
	}
	if row.SizeBytes != int64(len(data)) {
		t.Errorf("SizeBytes = %d, want %d", row.SizeBytes, len(data))
	}

	// The row is actually persisted (not just returned in memory).
	fetched, err := q.GetMediaFile(ctx, db.GetMediaFileParams{ShopID: shopRow.ID, ID: row.ID})
	if err != nil {
		t.Fatalf("GetMediaFile: %v", err)
	}
	if fetched.StorageKey != row.StorageKey {
		t.Errorf("fetched StorageKey = %q, want %q", fetched.StorageKey, row.StorageKey)
	}

	// Original bytes are stored unmodified.
	rc, err := svc.storage.Open(ctx, row.StorageKey)
	if err != nil {
		t.Fatalf("Open original: %v", err)
	}
	originalOnDisk, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if !bytes.Equal(originalOnDisk, data) {
		t.Error("stored original bytes differ from the uploaded bytes")
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
		img, format, err := image.Decode(rc)
		_ = rc.Close()
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

func TestUpload_jpegDerivativesDropEXIF(t *testing.T) {
	// Orientation correctness is explicitly out of scope here (docs/06-
	// ROADMAP.md Phase 2 T3 spec note); this only asserts the EXIF bytes
	// themselves are gone from every re-encoded derivative while the
	// stored original — never re-encoded — keeps them.
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

	rc, err := svc.storage.Open(ctx, row.StorageKey)
	if err != nil {
		t.Fatalf("Open original: %v", err)
	}
	originalOnDisk, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if !bytes.Contains(originalOnDisk, []byte("Exif")) {
		t.Error("stored original lost its EXIF segment; expected it stored unmodified")
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
			t.Errorf("derivative %s retained an EXIF segment; re-encoding should have dropped it", suffix)
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
