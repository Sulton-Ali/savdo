package reports_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// assertValidationField asserts err is 400 VALIDATION_FAILED with
// details.fields[field] == want.
func assertValidationField(t *testing.T, err error, field, want string) {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	fields, _ := apiErr.Details["fields"].(map[string]string)
	if fields[field] != want {
		t.Fatalf("details.fields = %v, want %s: %s", apiErr.Details, field, want)
	}
}

func TestGetSalesSummaryReport_fromAfterTo(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "summary-from-after-to")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	now := time.Now()
	_, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, owner), summaryParams(now, now.AddDate(0, 0, -1)))
	assertValidationField(t, err, "to", "invalid")
}

func TestListSalesByProduct_fromAfterTo(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "byproduct-from-after-to")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	now := time.Now()
	_, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now, now.AddDate(0, 0, -1), nil, nil))
	assertValidationField(t, err, "to", "invalid")
}

// crossShopLocationFixture seeds two shops, each with its own owner and
// location — enough to prove a locationId naming a real location that
// simply belongs to a different shop is rejected the same way a wholly
// made-up id would be (never a cross-shop leak of "does this exist").
type crossShopLocationFixture struct {
	shopA, shopB uuid.UUID
	ownerA       db.User
	locationB    db.Location
}

func newCrossShopLocationFixture(ctx context.Context, t *testing.T, q *db.Queries, slug string) crossShopLocationFixture {
	t.Helper()
	shopA := seedShop(ctx, t, q, slug+"-a")
	shopB := seedShop(ctx, t, q, slug+"-b")
	ownerA := seedUser(ctx, t, q, shopA.ID, "owner-a", db.UserRoleOwner)
	locationB := seedLocation(ctx, t, q, shopB.ID, "Main B")
	return crossShopLocationFixture{shopA: shopA.ID, shopB: shopB.ID, ownerA: ownerA, locationB: locationB}
}

func TestGetSalesSummaryReport_locationFromAnotherShop(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newCrossShopLocationFixture(ctx, t, q, "summary-cross-shop-location")
	now := time.Now()
	params := summaryParams(now.AddDate(0, 0, -1), now)
	params.Params.LocationId = &f.locationB.ID

	_, err := h.GetSalesSummaryReport(ctxAs(f.shopA, f.ownerA), params)
	assertValidationField(t, err, "locationId", "invalid")
}

func TestListSalesByProduct_locationFromAnotherShop(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	f := newCrossShopLocationFixture(ctx, t, q, "byproduct-cross-shop-location")
	now := time.Now()
	params := byProductParams(now.AddDate(0, 0, -1), now, nil, nil)
	params.Params.LocationId = &f.locationB.ID

	_, err := h.ListSalesByProduct(ctxAs(f.shopA, f.ownerA), params)
	assertValidationField(t, err, "locationId", "invalid")
}

func TestGetSalesSummaryReport_emptyPeriod(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "summary-empty-period")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	now := time.Now()
	resp, err := h.GetSalesSummaryReport(ctxAs(shopRow.ID, owner), summaryParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)))
	if err != nil {
		t.Fatalf("GetSalesSummaryReport: %v", err)
	}
	body := resp.(gen.GetSalesSummaryReport200JSONResponse)

	if body.SalesCount != 0 || body.ReturnsCount != 0 {
		t.Fatalf("counts = (%d, %d), want (0, 0)", body.SalesCount, body.ReturnsCount)
	}
	assertDecimal(t, "revenue", body.Revenue, "0.00")
	assertDecimal(t, "discounts", body.Discounts, "0.00")
	assertDecimal(t, "refunds", body.Refunds, "0.00")
	assertDecimal(t, "netRevenue", body.NetRevenue, "0.00")
	if body.Cost == nil || *body.Cost != "0.00" {
		t.Fatalf("cost = %v, want 0.00", body.Cost)
	}
	if body.Margin == nil || *body.Margin != "0.00" {
		t.Fatalf("margin = %v, want 0.00", body.Margin)
	}
}

func TestListSalesByProduct_emptyPeriod(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "byproduct-empty-period")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	now := time.Now()
	resp, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now.AddDate(0, 0, -1), now.AddDate(0, 0, 1), nil, nil))
	if err != nil {
		t.Fatalf("ListSalesByProduct: %v", err)
	}
	body := resp.(gen.ListSalesByProduct200JSONResponse)
	if len(body.Items) != 0 {
		t.Fatalf("items = %+v, want none", body.Items)
	}
}
