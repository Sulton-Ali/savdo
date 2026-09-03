package seed_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
	"github.com/Sulton-Ali/savdo/api/internal/seed"
)

func countCreated(t *testing.T, entities []seed.EntityResult) int {
	t.Helper()
	n := 0
	for _, e := range entities {
		if e.Created {
			n++
		}
	}
	return n
}

func TestSeedCreatesShopLocationsAndUsersOnAFreshDatabase(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	report, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	if report.ShopID == uuid.Nil {
		t.Fatal("Seed() returned a zero ShopID")
	}
	if got, want := len(report.Entities), 1+2+3; got != want {
		t.Fatalf("len(Entities) = %d, want %d (1 shop + 2 locations + 3 users)", got, want)
	}
	if got, want := countCreated(t, report.Entities), 1+2+3; got != want {
		t.Fatalf("created count = %d, want %d on a fresh database", got, want)
	}

	q := db.New(pool)

	locations, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: report.ShopID, Limit: 10})
	if err != nil {
		t.Fatalf("ListLocations() error = %v", err)
	}
	if len(locations) != 2 {
		t.Fatalf("len(locations) = %d, want 2", len(locations))
	}
	var defaultLocation *db.Location
	for i := range locations {
		if locations[i].IsDefault {
			defaultLocation = &locations[i]
		}
	}
	if defaultLocation == nil {
		t.Fatal("no location is marked default")
	}
	if defaultLocation.Name != seed.LocationStoreName {
		t.Fatalf("default location = %q, want %q", defaultLocation.Name, seed.LocationStoreName)
	}

	for _, tc := range []struct {
		username string
		password string
		role     db.UserRole
	}{
		{"owner", seed.DevOwnerPassword, db.UserRoleOwner},
		{"manager", seed.DevManagerPassword, db.UserRoleManager},
		{"cashier", seed.DevCashierPassword, db.UserRoleCashier},
	} {
		user, err := q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: report.ShopID, Username: tc.username})
		if err != nil {
			t.Fatalf("GetUserByUsername(%q) error = %v", tc.username, err)
		}
		if user.Role != tc.role {
			t.Fatalf("user %q role = %q, want %q", tc.username, user.Role, tc.role)
		}
		if user.Locale != "uz" {
			t.Fatalf("user %q locale = %q, want uz", tc.username, user.Locale)
		}
		ok, err := auth.Verify(user.PasswordHash, tc.password)
		if err != nil {
			t.Fatalf("Verify(%q) error = %v", tc.username, err)
		}
		if !ok {
			t.Fatalf("Verify(%q, %q) = false, want true", tc.username, tc.password)
		}
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	first, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("first Seed() error = %v", err)
	}

	q := db.New(pool)
	ownerBefore, err := q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: first.ShopID, Username: "owner"})
	if err != nil {
		t.Fatalf("GetUserByUsername(owner) before second seed: %v", err)
	}

	second, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("second Seed() error = %v", err)
	}

	if second.ShopID != first.ShopID {
		t.Fatalf("second Seed() ShopID = %v, want %v (same shop)", second.ShopID, first.ShopID)
	}
	if got := countCreated(t, second.Entities); got != 0 {
		t.Fatalf("second Seed() created count = %d, want 0 (everything already exists)", got)
	}
	if len(second.Entities) != len(first.Entities) {
		t.Fatalf("second Seed() reported %d entities, want %d (same counts)", len(second.Entities), len(first.Entities))
	}

	ownerAfter, err := q.GetUserByUsername(ctx, db.GetUserByUsernameParams{ShopID: first.ShopID, Username: "owner"})
	if err != nil {
		t.Fatalf("GetUserByUsername(owner) after second seed: %v", err)
	}
	if ownerAfter.PasswordHash != ownerBefore.PasswordHash {
		t.Fatal("second Seed() changed the owner's password hash; it must leave an existing user's password untouched")
	}

	locations, err := q.ListLocations(ctx, db.ListLocationsParams{ShopID: first.ShopID, Limit: 10})
	if err != nil {
		t.Fatalf("ListLocations() error = %v", err)
	}
	if len(locations) != 2 {
		t.Fatalf("len(locations) after second seed = %d, want 2 (no duplicates)", len(locations))
	}
}

func TestSeedLeavesAnExistingShopsSettingsUntouched(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()

	q := db.New(pool)
	existing, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: seed.DefaultShopSlug, Name: "Custom Name Set By The Owner"})
	if err != nil {
		t.Fatalf("CreateShop() error = %v", err)
	}
	// Simulate the owner having already customized settings via UpdateShop.
	if _, err := q.UpdateShop(ctx, db.UpdateShopParams{ID: existing.ID, Name: ptr("Custom Name Set By The Owner"), Currency: ptr("USD")}); err != nil {
		t.Fatalf("UpdateShop() error = %v", err)
	}

	report, err := seed.Seed(ctx, pool, seed.DefaultShopSlug)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	if report.ShopID != existing.ID {
		t.Fatalf("Seed() ShopID = %v, want %v (the pre-existing shop)", report.ShopID, existing.ID)
	}

	shop, err := q.GetShop(ctx, existing.ID)
	if err != nil {
		t.Fatalf("GetShop() error = %v", err)
	}
	if shop.Name != "Custom Name Set By The Owner" {
		t.Fatalf("shop.Name = %q, want it left untouched", shop.Name)
	}
	if shop.Currency != "USD" {
		t.Fatalf("shop.Currency = %q, want it left untouched (USD)", shop.Currency)
	}
}

func ptr[T any](v T) *T { return &v }
