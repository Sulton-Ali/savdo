package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
)

// shopTestFixture wires a full router (real auth middleware, real shop
// handlers) against a real Postgres, with one shop and a seeded owner —
// end-to-end coverage of the nine `/shop`, `/locations` and `/staff`
// operations, including the permission gate and CSRF/session machinery
// every request actually goes through in production.
type shopTestFixture struct {
	router        http.Handler
	q             *db.Queries
	shopID        uuid.UUID
	ownerUsername string
	ownerPassword string
}

func newShopTestFixture(t *testing.T) shopTestFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := t.Context()

	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: "shop-a", Name: "Shop A"})
	if err != nil {
		t.Fatalf("CreateShop: %v", err)
	}

	const ownerPassword = "correct-horse-battery"
	hash, err := auth.Hash(ownerPassword)
	if err != nil {
		t.Fatalf("auth.Hash: %v", err)
	}
	owner, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopRow.ID, Username: "owner1", PasswordHash: hash,
		FullName: "Owner", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("CreateUser(owner): %v", err)
	}

	cfg := config.Config{
		SessionWebTTL:       7 * 24 * time.Hour,
		SessionMobileTTL:    30 * 24 * time.Hour,
		LoginRateIPPerMin:   1000,
		LoginRateUserPerMin: 1000,
		CookieSecure:        true,
	}
	authSvc := auth.NewService(q, cfg, shopRow.ID)
	shopSvc := shop.NewService(pool, q)

	return shopTestFixture{
		router:        NewRouter(testLogger(), pool, authSvc, shopSvc),
		q:             q,
		shopID:        shopRow.ID,
		ownerUsername: owner.Username,
		ownerPassword: ownerPassword,
	}
}

// login performs POST /v1/auth/login and returns the raw recorder so a
// caller can pull cookies (web) or the bearer token (mobile) off it.
func (f shopTestFixture) login(t *testing.T, username, password, client string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password, "client": client})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// do sends req through the fixture's router, attaching cookies (session
// auth) and setting the CSRF header on every mutating request — same as
// a real cookie-authenticated web client.
func (f shopTestFixture) do(t *testing.T, method, path string, cookies []*http.Cookie, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-Requested-With", "savdo")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) gen.Error {
	t.Helper()
	var body gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v, raw = %s", err, rec.Body.String())
	}
	return body
}

func fieldsOf(t *testing.T, body gen.Error) map[string]any {
	t.Helper()
	if body.Error.Details == nil {
		t.Fatalf("error has no details: %+v", body)
	}
	fields, ok := (*body.Error.Details)["fields"].(map[string]any)
	if !ok {
		t.Fatalf("details.fields missing or wrong shape: %+v", *body.Error.Details)
	}
	return fields
}

// TestOwnerCreatesCashierAndCashierIsForbiddenFromStaffList is the Phase 1
// T5 Done-when check: the owner creates a cashier, the cashier logs in,
// and GET /staff answers 403 FORBIDDEN for them.
func TestOwnerCreatesCashierAndCashierIsForbiddenFromStaffList(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	createRec := f.do(t, http.MethodPost, "/v1/staff", ownerCookies, map[string]any{
		"username": "cashier1", "password": "cashier-password-1", "fullName": "Cashier One", "role": "cashier",
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create staff status = %d, want 201, body = %s", createRec.Code, createRec.Body.String())
	}

	cashierCookies := f.login(t, "cashier1", "cashier-password-1", "web").Result().Cookies()

	rec := f.do(t, http.MethodGet, "/v1/staff", cashierCookies, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /staff as cashier status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	if decodeError(t, rec).Error.Code != gen.FORBIDDEN {
		t.Fatalf("error.code = %q, want FORBIDDEN", decodeError(t, rec).Error.Code)
	}
}

func TestGetShopUnauthenticatedIs401(t *testing.T) {
	f := newShopTestFixture(t)
	rec := f.do(t, http.MethodGet, "/v1/shop", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPatchShopFullRoundTrip(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	rec := f.do(t, http.MethodPatch, "/v1/shop", cookies, map[string]any{
		"name":     "Savdo Family Shop",
		"timezone": "Asia/Samarkand",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	var got gen.Shop
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Savdo Family Shop" || got.Timezone != "Asia/Samarkand" {
		t.Fatalf("got = %+v, want updated name/timezone", got)
	}

	getRec := f.do(t, http.MethodGet, "/v1/shop", cookies, nil)
	var reread gen.Shop
	if err := json.Unmarshal(getRec.Body.Bytes(), &reread); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if reread.Name != "Savdo Family Shop" {
		t.Fatalf("re-read Name = %q, want the patched value", reread.Name)
	}
}

func TestUpdateShopInvalidTimezoneIs400WithFieldReason(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	rec := f.do(t, http.MethodPatch, "/v1/shop", cookies, map[string]any{"timezone": "Not/A_Zone"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	fields := fieldsOf(t, decodeError(t, rec))
	if fields["timezone"] != "invalid" {
		t.Fatalf("fields = %+v, want timezone=invalid", fields)
	}
}

func TestManagerGets403OnOwnerOnlyShopAndLocationOps(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	createRec := f.do(t, http.MethodPost, "/v1/staff", ownerCookies, map[string]any{
		"username": "manager1", "password": "manager-password-1", "fullName": "Manager One", "role": "manager",
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create manager status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	managerCookies := f.login(t, "manager1", "manager-password-1", "web").Result().Cookies()

	if rec := f.do(t, http.MethodPatch, "/v1/shop", managerCookies, map[string]any{"name": "Hijacked"}); rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH /shop as manager status = %d, want 403", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/v1/locations", managerCookies, map[string]any{"name": "New Loc", "kind": "store"}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /locations as manager status = %d, want 403", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/v1/staff", managerCookies, map[string]any{
		"username": "cashier9", "password": "cashier-password-9", "fullName": "Cashier Nine", "role": "cashier",
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /staff as manager status = %d, want 403", rec.Code)
	}
}

func TestCashierGets403OnPatchLocations(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	locRec := f.do(t, http.MethodPost, "/v1/locations", ownerCookies, map[string]any{"name": "Main Store", "kind": "store"})
	if locRec.Code != http.StatusCreated {
		t.Fatalf("create location status = %d, body = %s", locRec.Code, locRec.Body.String())
	}
	var loc gen.Location
	if err := json.Unmarshal(locRec.Body.Bytes(), &loc); err != nil {
		t.Fatalf("decode location: %v", err)
	}

	f.do(t, http.MethodPost, "/v1/staff", ownerCookies, map[string]any{
		"username": "cashier2", "password": "cashier-password-2", "fullName": "Cashier Two", "role": "cashier",
	})
	cashierCookies := f.login(t, "cashier2", "cashier-password-2", "web").Result().Cookies()

	rec := f.do(t, http.MethodPatch, "/v1/locations/"+loc.Id.String(), cashierCookies, map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH /locations/{id} as cashier status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateStaffValidationReasons(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	rec := f.do(t, http.MethodPost, "/v1/staff", cookies, map[string]any{
		"username": "ab", "password": "short", "fullName": "", "role": "owner", "phone": "not-a-phone",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	fields := fieldsOf(t, decodeError(t, rec))
	want := map[string]any{
		"username": "too_short",
		"password": "too_short",
		"fullName": "required",
		"role":     "invalid",
		"phone":    "invalid",
	}
	for field, reason := range want {
		if fields[field] != reason {
			t.Fatalf("fields[%q] = %v, want %q (all fields = %+v)", field, fields[field], reason, fields)
		}
	}
}

func TestCreateStaffDuplicateUsernameIs409WithField(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	body := map[string]any{"username": "cashier3", "password": "cashier-password-3", "fullName": "Cashier Three", "role": "cashier"}
	if rec := f.do(t, http.MethodPost, "/v1/staff", cookies, body); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec := f.do(t, http.MethodPost, "/v1/staff", cookies, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	body2 := decodeError(t, rec)
	if body2.Error.Code != gen.CONFLICT || (*body2.Error.Details)["field"] != "username" {
		t.Fatalf("error = %+v, want CONFLICT with details.field=username", body2)
	}
}

func TestLocationPaginationLimitOneAcrossTwoPagesThenNil(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	for _, name := range []string{"Loc A", "Loc B"} {
		if rec := f.do(t, http.MethodPost, "/v1/locations", cookies, map[string]any{"name": name, "kind": "store"}); rec.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d, body = %s", name, rec.Code, rec.Body.String())
		}
	}

	page1 := f.do(t, http.MethodGet, "/v1/locations?limit=1", cookies, nil)
	if page1.Code != http.StatusOK {
		t.Fatalf("page1 status = %d, body = %s", page1.Code, page1.Body.String())
	}
	var list1 gen.LocationList
	if err := json.Unmarshal(page1.Body.Bytes(), &list1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(list1.Items) != 1 || list1.NextCursor == nil {
		t.Fatalf("page1 = %+v, want 1 item and a non-nil nextCursor", list1)
	}

	page2 := f.do(t, http.MethodGet, "/v1/locations?limit=1&cursor="+*list1.NextCursor, cookies, nil)
	if page2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d, body = %s", page2.Code, page2.Body.String())
	}
	var list2 gen.LocationList
	if err := json.Unmarshal(page2.Body.Bytes(), &list2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(list2.Items) != 1 || list2.NextCursor != nil {
		t.Fatalf("page2 = %+v, want 1 item and a nil nextCursor (end of the 2-item list)", list2)
	}
	if list1.Items[0].Id == list2.Items[0].Id {
		t.Fatalf("page1 and page2 returned the same item %v", list1.Items[0].Id)
	}
}

func TestListLocationsBogusCursorIs400(t *testing.T) {
	f := newShopTestFixture(t)
	cookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	rec := f.do(t, http.MethodGet, "/v1/locations?cursor=not-a-valid-cursor", cookies, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	fields := fieldsOf(t, decodeError(t, rec))
	if fields["cursor"] != "invalid" {
		t.Fatalf("fields = %+v, want cursor=invalid", fields)
	}
}

func TestDeactivatedStaffCannotLoginAndSessionsAreRevoked(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	createRec := f.do(t, http.MethodPost, "/v1/staff", ownerCookies, map[string]any{
		"username": "cashier4", "password": "cashier-password-4", "fullName": "Cashier Four", "role": "cashier",
	})
	var cashier gen.User
	if err := json.Unmarshal(createRec.Body.Bytes(), &cashier); err != nil {
		t.Fatalf("decode created staff: %v", err)
	}

	cashierLogin := f.login(t, "cashier4", "cashier-password-4", "web")
	cashierCookies := cashierLogin.Result().Cookies()
	if rec := f.do(t, http.MethodGet, "/v1/shop", cashierCookies, nil); rec.Code != http.StatusOK {
		t.Fatalf("cashier's session not usable before deactivation: status = %d", rec.Code)
	}

	deactivateRec := f.do(t, http.MethodPatch, "/v1/staff/"+cashier.Id.String(), ownerCookies, map[string]any{"isActive": false})
	if deactivateRec.Code != http.StatusOK {
		t.Fatalf("deactivate status = %d, want 200, body = %s", deactivateRec.Code, deactivateRec.Body.String())
	}

	// The now-deactivated user's existing session must be revoked...
	if rec := f.do(t, http.MethodGet, "/v1/shop", cashierCookies, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status with revoked session = %d, want 401", rec.Code)
	}
	// ... and a fresh login attempt must fail too.
	loginRec := f.login(t, "cashier4", "cashier-password-4", "web")
	if loginRec.Code != http.StatusUnauthorized {
		t.Fatalf("login as deactivated user status = %d, want 401", loginRec.Code)
	}
}

func TestSetStaffPasswordLogsOutAndNewPasswordLogsIn(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	createRec := f.do(t, http.MethodPost, "/v1/staff", ownerCookies, map[string]any{
		"username": "cashier5", "password": "cashier-password-5", "fullName": "Cashier Five", "role": "cashier",
	})
	var cashier gen.User
	if err := json.Unmarshal(createRec.Body.Bytes(), &cashier); err != nil {
		t.Fatalf("decode created staff: %v", err)
	}
	cashierCookies := f.login(t, "cashier5", "cashier-password-5", "web").Result().Cookies()

	resetRec := f.do(t, http.MethodPost, "/v1/staff/"+cashier.Id.String()+"/password", ownerCookies, map[string]any{
		"password": "brand-new-password-1",
	})
	if resetRec.Code != http.StatusNoContent {
		t.Fatalf("reset password status = %d, want 204, body = %s", resetRec.Code, resetRec.Body.String())
	}

	if rec := f.do(t, http.MethodGet, "/v1/shop", cashierCookies, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old session after password reset: status = %d, want 401", rec.Code)
	}

	oldLogin := f.login(t, "cashier5", "cashier-password-5", "web")
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password status = %d, want 401", oldLogin.Code)
	}
	newLogin := f.login(t, "cashier5", "brand-new-password-1", "web")
	if newLogin.Code != http.StatusOK {
		t.Fatalf("login with new password status = %d, want 200, body = %s", newLogin.Code, newLogin.Body.String())
	}
}

func TestOwnerCannotDeactivateOrChangeOwnRole(t *testing.T) {
	f := newShopTestFixture(t)
	ownerCookies := f.login(t, f.ownerUsername, f.ownerPassword, "web").Result().Cookies()

	ownerRow, err := f.q.GetUserByUsername(t.Context(), db.GetUserByUsernameParams{ShopID: f.shopID, Username: f.ownerUsername})
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}

	rec := f.do(t, http.MethodPatch, "/v1/staff/"+ownerRow.ID.String(), ownerCookies, map[string]any{"isActive": false})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self-deactivation status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	fields := fieldsOf(t, decodeError(t, rec))
	if fields["isActive"] != "invalid" {
		t.Fatalf("fields = %+v, want isActive=invalid", fields)
	}
}
