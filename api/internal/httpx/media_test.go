package httpx

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
)

// mediaTestFixture wires a full router (real auth middleware, a real
// media.Handler backed by media.LocalStorage under a temp directory, and
// dev static serving mounted the same way cmd/api mounts it) against a
// real Postgres, with one shop and one seeded user per role — end-to-end
// coverage of POST /media's permission gate and the dev GET /media/<key>
// path together, the same as a real request would exercise them.
type mediaTestFixture struct {
	router                           http.Handler
	shopID                           uuid.UUID
	ownerUsername, ownerPassword     string
	managerUsername, managerPassword string
	cashierUsername, cashierPassword string
}

func newMediaTestFixture(t *testing.T, maxBytes int64) mediaTestFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "media-shop", Name: "Media Shop"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}

	const password = "correct-horse-battery"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	makeUser := func(username string, role db.UserRole) {
		t.Helper()
		if _, err := q.CreateUser(ctx, db.CreateUserParams{
			ID: uuid.New(), ShopID: shopRow.ID, Username: username, PasswordHash: hash,
			FullName: "Test " + username, Role: role, Locale: "uz",
		}); err != nil {
			t.Fatalf("CreateUser(%s): %v", username, err)
		}
	}
	makeUser("owner1", db.UserRoleOwner)
	makeUser("manager1", db.UserRoleManager)
	makeUser("cashier1", db.UserRoleCashier)

	cfg := config.Config{
		SessionWebTTL:       7 * 24 * time.Hour,
		SessionMobileTTL:    30 * 24 * time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
		CookieSecure:        true,
	}
	authSvc := auth.NewService(q, cfg, shopRow.ID)
	shopSvc := shop.NewService(pool, q)

	mediaDir := t.TempDir()
	storage := media.NewLocalStorage(mediaDir, "/media")
	mediaSvc := media.NewService(q, storage, "/media", maxBytes)

	return mediaTestFixture{
		router:          NewRouter(testLogger(), pool, authSvc, shopSvc, mediaSvc, mediaDir),
		shopID:          shopRow.ID,
		ownerUsername:   "owner1",
		ownerPassword:   password,
		managerUsername: "manager1",
		managerPassword: password,
		cashierUsername: "cashier1",
		cashierPassword: password,
	}
}

func (f mediaTestFixture) login(t *testing.T, username, password string) []*http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password, "client": "web"})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login(%s) status = %d, body = %s", username, rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()
}

// upload builds and sends a multipart POST /v1/media request with one
// part named "file" (filename/content as given) preceded by an unrelated
// "note" part, so tests also exercise "ignore other parts". cookies may
// be nil (unauthenticated).
func (f mediaTestFixture) upload(t *testing.T, cookies []*http.Cookie, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("note", "irrelevant form field"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/media", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Requested-With", "savdo")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func pngBytesForTest(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestUploadMedia_ownerSucceedsAndDevServingWorks(t *testing.T) {
	f := newMediaTestFixture(t, 10<<20)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword)

	rec := f.upload(t, cookies, "photo.png", pngBytesForTest(t, 300, 200))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}

	var body gen.MediaFile
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Mime != "image/png" {
		t.Errorf("Mime = %q, want image/png", body.Mime)
	}
	if body.Width != 300 || body.Height != 200 {
		t.Errorf("dimensions = %dx%d, want 300x200", body.Width, body.Height)
	}
	if body.Urls.Thumb == "" || body.Urls.Card == "" || body.Urls.Full == "" {
		t.Fatalf("Urls incomplete: %+v", body.Urls)
	}

	// The dev static handler (mounted because mediaTestFixture passes a
	// non-empty devMediaDir, matching cmd/api's ENV != "prod" branch)
	// serves the thumb URL straight back, with the immutable cache
	// header — no auth required, same as production Caddy.
	thumbPath := body.Urls.Thumb
	if u, err := url.Parse(thumbPath); err == nil {
		thumbPath = u.Path
	}
	getRec := httptest.NewRecorder()
	f.router.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, thumbPath, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200, body = %s", thumbPath, getRec.Code, getRec.Body.String())
	}
	if got := getRec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want the one-year immutable value", got)
	}
	if ct := getRec.Header().Get("Content-Type"); !strings.Contains(ct, "webp") {
		t.Errorf("Content-Type = %q, want image/webp", ct)
	}
}

func TestUploadMedia_permissions(t *testing.T) {
	f := newMediaTestFixture(t, 10<<20)
	png := pngBytesForTest(t, 100, 100)

	t.Run("manager allowed", func(t *testing.T) {
		cookies := f.login(t, f.managerUsername, f.managerPassword)
		rec := f.upload(t, cookies, "a.png", png)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cashier forbidden", func(t *testing.T) {
		cookies := f.login(t, f.cashierUsername, f.cashierPassword)
		rec := f.upload(t, cookies, "a.png", png)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		rec := f.upload(t, nil, "a.png", png)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestUploadMedia_filenameNeverReachesStorageKey(t *testing.T) {
	f := newMediaTestFixture(t, 10<<20)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword)

	rec := f.upload(t, cookies, "../../evil.php", pngBytesForTest(t, 64, 64))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var body gen.MediaFile
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, u := range []string{body.Urls.Thumb, body.Urls.Card, body.Urls.Full} {
		if strings.Contains(u, "..") || strings.Contains(u, "evil") {
			t.Errorf("url %q leaked the client filename or a path segment", u)
		}
	}
}

func TestUploadMedia_invalidFileIs400(t *testing.T) {
	f := newMediaTestFixture(t, 10<<20)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword)

	rec := f.upload(t, cookies, "x.png", []byte("this is not a png, just text pretending to be one"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fields, _ := (*body.Error.Details)["fields"].(map[string]any)
	if fields["file"] != "invalid" {
		t.Fatalf("details.fields.file = %v, want invalid", fields["file"])
	}
}

func TestUploadMedia_tooLargeIs400(t *testing.T) {
	// A small MediaMaxBytes stands in for the real 10 MiB default so the
	// test doesn't need to build an 11 MiB body; bodylimit.go's 12 MiB
	// route limit is well above this, so the file-content cap
	// (media.Service, not the request-body cap) is what fires here.
	f := newMediaTestFixture(t, 100)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword)

	rec := f.upload(t, cookies, "big.png", bytes.Repeat([]byte{0xAB}, 200))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	fields, _ := (*body.Error.Details)["fields"].(map[string]any)
	if fields["file"] != "too_long" {
		t.Fatalf("details.fields.file = %v, want too_long", fields["file"])
	}
}

func TestUploadMedia_sameBytesDedupe(t *testing.T) {
	f := newMediaTestFixture(t, 10<<20)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword)
	png := pngBytesForTest(t, 120, 90)

	first := f.upload(t, cookies, "a.png", png)
	if first.Code != http.StatusCreated {
		t.Fatalf("first upload status = %d, body = %s", first.Code, first.Body.String())
	}
	var firstBody gen.MediaFile
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	second := f.upload(t, cookies, "b.png", png)
	if second.Code != http.StatusCreated {
		t.Fatalf("second upload status = %d, body = %s", second.Code, second.Body.String())
	}
	var secondBody gen.MediaFile
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if secondBody.Id != firstBody.Id {
		t.Fatalf("second upload id = %s, want same id %s (sha256 dedupe)", secondBody.Id, firstBody.Id)
	}
}
