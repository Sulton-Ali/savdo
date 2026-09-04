package shop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// seedShop creates one shop directly through sqlc's generated queries.
func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Test Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop: %v", err)
	}
	return shopRow
}

// seedUser creates one active user, hashing password the same way
// CreateStaff does.
func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string, role db.UserRole) db.User {
	t.Helper()
	hash, err := auth.Hash("correct-horse-battery")
	if err != nil {
		t.Fatalf("seedUser: Hash: %v", err)
	}
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: hash,
		FullName: "Test User " + username, Role: role, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser: CreateUser: %v", err)
	}
	return user
}

// seedSession creates one active (unrevoked) session for user, so tests
// can assert a deactivation or password reset revokes it.
func seedSession(ctx context.Context, t *testing.T, q *db.Queries, shopID, userID uuid.UUID) db.Session {
	t.Helper()
	id := uuid.New()
	sess, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID: id, ShopID: shopID, UserID: userID,
		TokenHash: []byte("token-" + id.String()),
		Client:    db.SessionClientWeb,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seedSession: %v", err)
	}
	return sess
}

func errStatus(t *testing.T, err error) int {
	t.Helper()
	apiErr, ok := err.(*apierr.Error)
	if !ok {
		t.Fatalf("error type = %T, want *apierr.Error", err)
	}
	return apiErr.Status
}

func newTestService(t *testing.T) (*Service, *db.Queries, context.Context) {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	return NewService(pool, q), q, context.Background()
}

func TestServiceGetAndUpdateShop(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	got, err := svc.GetShop(ctx, shopRow.ID)
	if err != nil {
		t.Fatalf("GetShop() error = %v", err)
	}
	if got.ID != shopRow.ID {
		t.Fatalf("GetShop().ID = %v, want %v", got.ID, shopRow.ID)
	}

	newName := "Renamed Shop"
	newTZ := "Asia/Samarkand"
	updated, err := svc.UpdateShop(ctx, shopRow.ID, UpdateShopInput{Name: &newName, Timezone: &newTZ})
	if err != nil {
		t.Fatalf("UpdateShop() error = %v", err)
	}
	if updated.Name != newName || updated.Timezone != newTZ {
		t.Fatalf("UpdateShop() = %+v, want name=%q timezone=%q", updated, newName, newTZ)
	}
	if updated.Slug != shopRow.Slug || updated.Currency != shopRow.Currency {
		t.Fatalf("UpdateShop() changed immutable slug/currency: %+v", updated)
	}
}

// TestListLocationsIsolation is the Phase 1 T5 Done-when check: seeding
// shop A and shop B each with a location, a Service.ListLocations call
// with shop B's id must never see shop A's row, and vice versa.
func TestListLocationsIsolation(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	if _, err := svc.CreateLocation(ctx, shopA.ID, "A Store", db.LocationKindStore, false); err != nil {
		t.Fatalf("CreateLocation(A): %v", err)
	}
	if _, err := svc.CreateLocation(ctx, shopB.ID, "B Store", db.LocationKindStore, false); err != nil {
		t.Fatalf("CreateLocation(B): %v", err)
	}

	aLocations, err := svc.ListLocations(ctx, shopA.ID, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListLocations(A): %v", err)
	}
	if len(aLocations) != 1 || aLocations[0].Name != "A Store" {
		t.Fatalf("ListLocations(A) = %+v, want exactly [A Store]", aLocations)
	}

	bLocations, err := svc.ListLocations(ctx, shopB.ID, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListLocations(B): %v", err)
	}
	if len(bLocations) != 1 || bLocations[0].Name != "B Store" {
		t.Fatalf("ListLocations(B) = %+v, want exactly [B Store]", bLocations)
	}
}

// TestListLocationsQueryLevelIsolation is the query-level companion the
// task spec calls out explicitly: db.Queries.ListLocations(ctx, shopA)
// after inserting only shop B's location returns zero rows — proving the
// sqlc query itself (not just the Service wrapping it) filters by
// shop_id.
func TestListLocationsQueryLevelIsolation(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	q := db.New(pool)
	ctx := context.Background()

	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	if _, err := q.CreateLocation(ctx, db.CreateLocationParams{
		ID: uuid.New(), ShopID: shopB.ID, Name: "B Store", Kind: db.LocationKindStore, IsDefault: true, IsActive: true,
	}); err != nil {
		t.Fatalf("CreateLocation(B): %v", err)
	}

	rows, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: shopA.ID, Limit: 50})
	if err != nil {
		t.Fatalf("ListLocations(A): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListLocations(A) = %+v, want zero rows (shop A has no locations, only shop B does)", rows)
	}
}

func TestCreateLocationFirstBecomesDefaultAutomatically(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	loc, err := svc.CreateLocation(ctx, shopRow.ID, "First", db.LocationKindStore, false)
	if err != nil {
		t.Fatalf("CreateLocation() error = %v", err)
	}
	if !loc.IsDefault {
		t.Fatalf("first location IsDefault = false, want true (auto-default)")
	}
}

func TestCreateLocationSecondIsNotDefaultUnlessRequested(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	if _, err := svc.CreateLocation(ctx, shopRow.ID, "First", db.LocationKindStore, false); err != nil {
		t.Fatalf("CreateLocation(first): %v", err)
	}
	second, err := svc.CreateLocation(ctx, shopRow.ID, "Second", db.LocationKindWarehouse, false)
	if err != nil {
		t.Fatalf("CreateLocation(second): %v", err)
	}
	if second.IsDefault {
		t.Fatalf("second location IsDefault = true, want false")
	}

	third, err := svc.CreateLocation(ctx, shopRow.ID, "Third", db.LocationKindWarehouse, true)
	if err != nil {
		t.Fatalf("CreateLocation(third, wantDefault): %v", err)
	}
	if !third.IsDefault {
		t.Fatalf("third location IsDefault = false, want true (explicitly requested)")
	}

	// The previous default (the first location) must have been cleared.
	locs, err := svc.ListLocations(ctx, shopRow.ID, 10, nil, nil)
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}
	defaults := 0
	for _, l := range locs {
		if l.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("locations with IsDefault=true = %d, want exactly 1", defaults)
	}
}

func TestCreateLocationDuplicateNameConflict(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	if _, err := svc.CreateLocation(ctx, shopRow.ID, "Store", db.LocationKindStore, false); err != nil {
		t.Fatalf("CreateLocation(first): %v", err)
	}
	_, err := svc.CreateLocation(ctx, shopRow.ID, "Store", db.LocationKindStore, false)
	if err == nil {
		t.Fatal("CreateLocation(duplicate name) error = nil, want a conflict")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok {
		t.Fatalf("error type = %T, want *apierr.Error", err)
	}
	if apiErr.Status != 409 || apiErr.Details["field"] != "name" {
		t.Fatalf("error = %+v, want 409 with details.field=name", apiErr)
	}
}

func TestUpdateLocationRefusesUnsettingTheOnlyDefault(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	loc, err := svc.CreateLocation(ctx, shopRow.ID, "Only", db.LocationKindStore, true)
	if err != nil {
		t.Fatalf("CreateLocation: %v", err)
	}

	notDefault := false
	_, err = svc.UpdateLocation(ctx, shopRow.ID, loc.ID, LocationPatchInput{IsDefault: &notDefault})
	if err == nil {
		t.Fatal("UpdateLocation(unset only default) error = nil, want validation error")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Status != 400 || apiErr.Details["fields"].(map[string]string)["isDefault"] != "invalid" {
		t.Fatalf("error = %+v, want 400 fields.isDefault=invalid", err)
	}
}

func TestUpdateLocationRefusesDeactivatingTheDefault(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	loc, err := svc.CreateLocation(ctx, shopRow.ID, "Only", db.LocationKindStore, true)
	if err != nil {
		t.Fatalf("CreateLocation: %v", err)
	}

	inactive := false
	_, err = svc.UpdateLocation(ctx, shopRow.ID, loc.ID, LocationPatchInput{IsActive: &inactive})
	if err == nil {
		t.Fatal("UpdateLocation(deactivate default) error = nil, want validation error")
	}
	apiErr, ok := err.(*apierr.Error)
	// Deactivating the default reports fields.isActive (the field the
	// caller actually sent), distinct from fields.isDefault, which is
	// reserved for an attempt to unset isDefault itself.
	if !ok || apiErr.Status != 400 || apiErr.Details["fields"].(map[string]string)["isActive"] != "invalid" {
		t.Fatalf("error = %+v, want 400 fields.isActive=invalid", err)
	}
}

func TestUpdateLocationOtherShopIs404(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")

	loc, err := svc.CreateLocation(ctx, shopA.ID, "A Store", db.LocationKindStore, true)
	if err != nil {
		t.Fatalf("CreateLocation: %v", err)
	}

	newName := "Hijacked"
	_, err = svc.UpdateLocation(ctx, shopB.ID, loc.ID, LocationPatchInput{Name: &newName})
	if err == nil {
		t.Fatal("UpdateLocation(other shop's location) error = nil, want 404")
	}
	if errStatus(t, err) != 404 {
		t.Fatalf("status = %d, want 404", errStatus(t, err))
	}
}

func TestCreateStaffDuplicateUsernameConflict(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	in := CreateStaffInput{Username: "cashier1", Password: "correct-horse-battery", FullName: "Cashier One", Role: db.UserRoleCashier}
	if _, err := svc.CreateStaff(ctx, shopRow.ID, in); err != nil {
		t.Fatalf("CreateStaff(first): %v", err)
	}
	_, err := svc.CreateStaff(ctx, shopRow.ID, in)
	if err == nil {
		t.Fatal("CreateStaff(duplicate username) error = nil, want conflict")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Status != 409 || apiErr.Details["field"] != "username" {
		t.Fatalf("error = %+v, want 409 with details.field=username", err)
	}
}

func TestCreateStaffDuplicatePhoneConflict(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")
	phone := "+998901234567"

	first := CreateStaffInput{Username: "cashier1", Password: "correct-horse-battery", FullName: "Cashier One", Role: db.UserRoleCashier, Phone: &phone}
	if _, err := svc.CreateStaff(ctx, shopRow.ID, first); err != nil {
		t.Fatalf("CreateStaff(first): %v", err)
	}
	second := CreateStaffInput{Username: "cashier2", Password: "correct-horse-battery", FullName: "Cashier Two", Role: db.UserRoleCashier, Phone: &phone}
	_, err := svc.CreateStaff(ctx, shopRow.ID, second)
	if err == nil {
		t.Fatal("CreateStaff(duplicate phone) error = nil, want conflict")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok || apiErr.Status != 409 || apiErr.Details["field"] != "phone" {
		t.Fatalf("error = %+v, want 409 with details.field=phone", err)
	}
}

func TestCreateStaffDefaultsLocaleToShopDefault(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	user, err := svc.CreateStaff(ctx, shopRow.ID, CreateStaffInput{
		Username: "cashier1", Password: "correct-horse-battery", FullName: "Cashier One", Role: db.UserRoleCashier,
	})
	if err != nil {
		t.Fatalf("CreateStaff: %v", err)
	}
	if user.Locale != shopRow.DefaultLocale {
		t.Fatalf("Locale = %q, want shop default %q", user.Locale, shopRow.DefaultLocale)
	}
}

func TestUpdateStaffOwnerProtections(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)
	manager := seedUser(ctx, t, q, shopRow.ID, "manager1", db.UserRoleManager)

	newRole := db.UserRoleManager
	_, err := svc.UpdateStaff(ctx, shopRow.ID, manager.ID, owner.ID, StaffPatchInput{Role: &newRole})
	if err == nil || errStatus(t, err) != 400 {
		t.Fatalf("changing the owner's role: err = %v, want 400", err)
	}

	inactive := false
	_, err = svc.UpdateStaff(ctx, shopRow.ID, manager.ID, owner.ID, StaffPatchInput{IsActive: &inactive})
	if err == nil || errStatus(t, err) != 400 {
		t.Fatalf("deactivating the owner: err = %v, want 400", err)
	}
}

func TestUpdateStaffSelfProtections(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")
	manager := seedUser(ctx, t, q, shopRow.ID, "manager1", db.UserRoleManager)

	newRole := db.UserRoleCashier
	_, err := svc.UpdateStaff(ctx, shopRow.ID, manager.ID, manager.ID, StaffPatchInput{Role: &newRole})
	if err == nil || errStatus(t, err) != 400 {
		t.Fatalf("changing own role: err = %v, want 400", err)
	}

	inactive := false
	_, err = svc.UpdateStaff(ctx, shopRow.ID, manager.ID, manager.ID, StaffPatchInput{IsActive: &inactive})
	if err == nil || errStatus(t, err) != 400 {
		t.Fatalf("self-deactivation: err = %v, want 400", err)
	}
}

func TestUpdateStaffOtherShopIs404(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	owner := seedUser(ctx, t, q, shopA.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shopA.ID, "cashier1", db.UserRoleCashier)

	newName := "Hijacked"
	_, err := svc.UpdateStaff(ctx, shopB.ID, owner.ID, cashier.ID, StaffPatchInput{FullName: &newName})
	if err == nil || errStatus(t, err) != 404 {
		t.Fatalf("err = %v, want 404", err)
	}
}

func TestUpdateStaffDeactivateRevokesAllSessions(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	seedSession(ctx, t, q, shopRow.ID, cashier.ID)
	seedSession(ctx, t, q, shopRow.ID, cashier.ID)

	inactive := false
	updated, err := svc.UpdateStaff(ctx, shopRow.ID, owner.ID, cashier.ID, StaffPatchInput{IsActive: &inactive})
	if err != nil {
		t.Fatalf("UpdateStaff(deactivate): %v", err)
	}
	if updated.IsActive {
		t.Fatal("updated.IsActive = true, want false")
	}

	sessions, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: shopRow.ID, UserID: cashier.ID})
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.RevokedAt == nil {
			t.Fatalf("session %s not revoked after deactivation", s.ID)
		}
	}
}

func TestSetStaffPasswordRevokesAllSessions(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")
	cashier := seedUser(ctx, t, q, shopRow.ID, "cashier1", db.UserRoleCashier)
	seedSession(ctx, t, q, shopRow.ID, cashier.ID)

	if err := svc.SetStaffPassword(ctx, shopRow.ID, cashier.ID, "a-new-password-1"); err != nil {
		t.Fatalf("SetStaffPassword: %v", err)
	}

	sessions, err := q.ListUserSessions(ctx, db.ListUserSessionsParams{ShopID: shopRow.ID, UserID: cashier.ID})
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].RevokedAt == nil {
		t.Fatalf("sessions = %+v, want the one session revoked", sessions)
	}

	updatedUser, err := q.GetUserByID(ctx, db.GetUserByIDParams{ShopID: shopRow.ID, ID: cashier.ID})
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if ok, err := auth.Verify(updatedUser.PasswordHash, "a-new-password-1"); err != nil || !ok {
		t.Fatalf("new password does not verify: ok=%v err=%v", ok, err)
	}
}

func TestSetStaffPasswordOtherShopIs404(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopA := seedShop(ctx, t, q, "shop-a")
	shopB := seedShop(ctx, t, q, "shop-b")
	cashier := seedUser(ctx, t, q, shopA.ID, "cashier1", db.UserRoleCashier)

	err := svc.SetStaffPassword(ctx, shopB.ID, cashier.ID, "a-new-password-1")
	if err == nil || errStatus(t, err) != 404 {
		t.Fatalf("err = %v, want 404", err)
	}
}

// TestUpdateLocationTakeoverClearsOldDefault is MAJOR-1's sequential
// "PATCH takeover" case: PATCHing a non-default location to
// isDefault:true must succeed and clear the previous default, the same
// invariant TestCreateLocationSecondIsNotDefaultUnlessRequested checks
// through CreateLocation rather than UpdateLocation.
func TestUpdateLocationTakeoverClearsOldDefault(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	first, err := svc.CreateLocation(ctx, shopRow.ID, "First", db.LocationKindStore, false)
	if err != nil {
		t.Fatalf("CreateLocation(first): %v", err)
	}
	second, err := svc.CreateLocation(ctx, shopRow.ID, "Second", db.LocationKindWarehouse, false)
	if err != nil {
		t.Fatalf("CreateLocation(second): %v", err)
	}
	if !first.IsDefault || second.IsDefault {
		t.Fatalf("setup: first.IsDefault=%v second.IsDefault=%v, want true/false", first.IsDefault, second.IsDefault)
	}

	wantDefault := true
	updated, err := svc.UpdateLocation(ctx, shopRow.ID, second.ID, LocationPatchInput{IsDefault: &wantDefault})
	if err != nil {
		t.Fatalf("UpdateLocation(takeover): %v", err)
	}
	if !updated.IsDefault {
		t.Fatal("updated.IsDefault = false, want true after taking over as default")
	}

	refetchedFirst, err := q.GetLocation(ctx, db.GetLocationParams{ShopID: shopRow.ID, ID: first.ID})
	if err != nil {
		t.Fatalf("GetLocation(first): %v", err)
	}
	if refetchedFirst.IsDefault {
		t.Fatal("first location is still IsDefault=true after second took over — ClearDefaultLocation did not run")
	}
}

// TestUpdateLocationConcurrentTakeoverExactlyOneDefaultRemains is
// MAJOR-1's concurrency check: two goroutines each try to make a
// different (currently non-default) location the shop's default at the
// same time. Per the review, both outcomes below are acceptable — what
// is never acceptable is a 500 or more than one default surviving:
// locations_shop_id_default_key (mapped to 409 CONFLICT
// details.field=isDefault by conflictField) is the backstop for
// whichever request loses the race.
func TestUpdateLocationConcurrentTakeoverExactlyOneDefaultRemains(t *testing.T) {
	svc, q, ctx := newTestService(t)
	shopRow := seedShop(ctx, t, q, "shop-a")

	first, err := svc.CreateLocation(ctx, shopRow.ID, "First", db.LocationKindStore, false)
	if err != nil {
		t.Fatalf("CreateLocation(first): %v", err)
	}
	second, err := svc.CreateLocation(ctx, shopRow.ID, "Second", db.LocationKindWarehouse, false)
	if err != nil {
		t.Fatalf("CreateLocation(second): %v", err)
	}
	third, err := svc.CreateLocation(ctx, shopRow.ID, "Third", db.LocationKindWarehouse, false)
	if err != nil {
		t.Fatalf("CreateLocation(third): %v", err)
	}
	if !first.IsDefault {
		t.Fatal("setup: first location is not the default")
	}

	wantDefault := true
	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []uuid.UUID{second.ID, third.ID}
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.UpdateLocation(ctx, shopRow.ID, targets[i], LocationPatchInput{IsDefault: &wantDefault})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			continue
		}
		apiErr, ok := err.(*apierr.Error)
		if !ok || apiErr.Status != 409 || apiErr.Details["field"] != "isDefault" {
			t.Fatalf("goroutine %d: err = %v (%T), want nil or 409 CONFLICT details.field=isDefault", i, err, err)
		}
	}

	all, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: shopRow.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}
	defaults := 0
	for _, l := range all {
		if l.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("locations with IsDefault=true = %d, want exactly 1 (all locations: %+v)", defaults, all)
	}
}

// TestClampLimit locks the pagination limit-clamping behaviour Review A
// asked to be pinned: 0, negative and > maxLimit all clamp; maxLimit
// itself and an in-range value pass through unchanged.
func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   *int
		want int32
	}{
		{"nil uses default", nil, defaultLimit},
		{"zero uses default", intPtr(0), defaultLimit},
		{"negative uses default", intPtr(-5), defaultLimit},
		{"over max clamps to max", intPtr(maxLimit + 1), maxLimit},
		{"exactly max passes through", intPtr(maxLimit), maxLimit},
		{"in range passes through", intPtr(1), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampLimit(tt.in); got != tt.want {
				t.Fatalf("clampLimit(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func intPtr(n int) *int { return &n }
