package sales

// White-box unit tests for this package's own pure pricing/date-range
// helpers — no DB, no testcontainers, deterministic (unlike the
// integration tests in this package's *_test.go files, package
// sales_test): promoActive is tested here with a controlled `now`, the
// only way to pin the exact "ends today late in the day" edge case this
// task calls out precisely (an integration test through CreateSaleTx can
// only ever use the real wall clock — see list_test.go's own
// TestListSales_dateFilterExcludesOutOfRangeDays doc comment for why the
// date-filter's own timezone edge is tested the same way, here, instead).

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	oapitypes "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

func mustNumeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("mustNumeric(%q): %v", s, err)
	}
	return n
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

// TestPromoActive_endsTodayStillAppliesLateInTheDay is this task's own
// named edge case: promo_to stored as the *start* (midnight) of its last
// day must still be active for the rest of that day in the shop's
// timezone — a naive `now <= promo_to` instant comparison would end the
// promo before that day even begins.
func TestPromoActive_endsTodayStillAppliesLateInTheDay(t *testing.T) {
	tashkent := mustLocation(t, "Asia/Tashkent")
	// promo_to = 2026-06-15T00:00:00+05:00 (midnight, the start of its
	// last day) — "now" is 23:00 local time on that same day.
	to := time.Date(2026, 6, 15, 0, 0, 0, 0, tashkent)
	from := to.AddDate(0, 0, -6)
	lateInDay := time.Date(2026, 6, 15, 23, 0, 0, 0, tashkent)

	if !promoActive(&from, &to, lateInDay, tashkent) {
		t.Fatal("promoActive = false, want true (23:00 on promo_to's own calendar day)")
	}

	// One minute into the next day, the promo must no longer be active.
	nextDay := time.Date(2026, 6, 16, 0, 1, 0, 0, tashkent)
	if promoActive(&from, &to, nextDay, tashkent) {
		t.Fatal("promoActive = true, want false (the day after promo_to)")
	}

	// The instant promo_from itself, at midnight, is active — the range
	// is inclusive at both ends.
	if !promoActive(&from, &to, from, tashkent) {
		t.Fatal("promoActive = false, want true at promo_from itself")
	}

	// One minute before promo_from's calendar day starts: not active.
	beforeFrom := from.AddDate(0, 0, -1).Add(23*time.Hour + 59*time.Minute)
	if promoActive(&from, &to, beforeFrom, tashkent) {
		t.Fatal("promoActive = true, want false (the day before promo_from)")
	}
}

func TestPromoActive_nilBoundsNeverActive(t *testing.T) {
	tashkent := mustLocation(t, "Asia/Tashkent")
	now := time.Now()
	to := now
	if promoActive(nil, &to, now, tashkent) {
		t.Fatal("promoActive = true, want false when promo_from is nil")
	}
	if promoActive(&now, nil, now, tashkent) {
		t.Fatal("promoActive = true, want false when promo_to is nil")
	}
}

func TestEffectiveUnitPrice_overridePromoPrecedence(t *testing.T) {
	tashkent := mustLocation(t, "Asia/Tashkent")
	today := time.Date(2026, 1, 10, 12, 0, 0, 0, tashkent)
	activeFrom := today.AddDate(0, 0, -1)
	activeTo := today

	// No override, no promo: base_price.
	row := db.GetVariantForSaleRow{BasePrice: mustNumeric(t, "100.00")}
	price, err := effectiveUnitPrice(row, today, tashkent)
	if err != nil {
		t.Fatalf("effectiveUnitPrice: %v", err)
	}
	if !price.Equal(decimal.RequireFromString("100.00")) {
		t.Fatalf("price = %s, want 100.00", price)
	}

	// price_override beats base_price when no promo is active.
	row = db.GetVariantForSaleRow{BasePrice: mustNumeric(t, "100.00"), PriceOverride: mustNumeric(t, "80.00")}
	price, err = effectiveUnitPrice(row, today, tashkent)
	if err != nil {
		t.Fatalf("effectiveUnitPrice: %v", err)
	}
	if !price.Equal(decimal.RequireFromString("80.00")) {
		t.Fatalf("price = %s, want 80.00 (price_override)", price)
	}

	// An active promo beats both base_price and price_override (promo
	// pricing is product-level only, docs/05-API.md's own promo bullet).
	row = db.GetVariantForSaleRow{
		BasePrice: mustNumeric(t, "100.00"), PriceOverride: mustNumeric(t, "80.00"),
		PromoPrice: mustNumeric(t, "50.00"), PromoFrom: &activeFrom, PromoTo: &activeTo,
	}
	price, err = effectiveUnitPrice(row, today, tashkent)
	if err != nil {
		t.Fatalf("effectiveUnitPrice: %v", err)
	}
	if !price.Equal(decimal.RequireFromString("50.00")) {
		t.Fatalf("price = %s, want 50.00 (active promo)", price)
	}

	// An expired promo falls back to price_override, not base_price.
	expiredFrom := today.AddDate(0, 0, -10)
	expiredTo := today.AddDate(0, 0, -5)
	row = db.GetVariantForSaleRow{
		BasePrice: mustNumeric(t, "100.00"), PriceOverride: mustNumeric(t, "80.00"),
		PromoPrice: mustNumeric(t, "50.00"), PromoFrom: &expiredFrom, PromoTo: &expiredTo,
	}
	price, err = effectiveUnitPrice(row, today, tashkent)
	if err != nil {
		t.Fatalf("effectiveUnitPrice: %v", err)
	}
	if !price.Equal(decimal.RequireFromString("80.00")) {
		t.Fatalf("price = %s, want 80.00 (expired promo, price_override applies)", price)
	}
}

func TestEffectiveUnitCost_precedenceAndZeroFallback(t *testing.T) {
	// Neither set: zero, never an error (sale_items.unit_cost is NOT
	// NULL).
	cost, err := effectiveUnitCost(db.GetVariantForSaleRow{})
	if err != nil {
		t.Fatalf("effectiveUnitCost: %v", err)
	}
	if !cost.IsZero() {
		t.Fatalf("cost = %s, want zero", cost)
	}

	// cost_price alone.
	cost, err = effectiveUnitCost(db.GetVariantForSaleRow{CostPrice: mustNumeric(t, "40.00")})
	if err != nil {
		t.Fatalf("effectiveUnitCost: %v", err)
	}
	if !cost.Equal(decimal.RequireFromString("40.00")) {
		t.Fatalf("cost = %s, want 40.00", cost)
	}

	// cost_override beats cost_price.
	cost, err = effectiveUnitCost(db.GetVariantForSaleRow{CostPrice: mustNumeric(t, "40.00"), CostOverride: mustNumeric(t, "55.00")})
	if err != nil {
		t.Fatalf("effectiveUnitCost: %v", err)
	}
	if !cost.Equal(decimal.RequireFromString("55.00")) {
		t.Fatalf("cost = %s, want 55.00 (cost_override)", cost)
	}
}

func TestComputeDiscountAmount(t *testing.T) {
	subtotal := decimal.RequireFromString("300.00")

	tests := []struct {
		name       string
		discount   *gen.SaleDiscount
		want       string
		wantErr    bool
		wantStatus int
	}{
		{name: "nil discount is zero", discount: nil, want: "0.00"},
		{name: "10 percent of 300", discount: &gen.SaleDiscount{Type: gen.Percent, Value: "10"}, want: "30.00"},
		{name: "fixed 50", discount: &gen.SaleDiscount{Type: gen.Fixed, Value: "50.00"}, want: "50.00"},
		{name: "percent over 100 is invalid", discount: &gen.SaleDiscount{Type: gen.Percent, Value: "150"}, wantErr: true, wantStatus: 400},
		{name: "malformed value is invalid", discount: &gen.SaleDiscount{Type: gen.Fixed, Value: "abc"}, wantErr: true, wantStatus: 400},
		{name: "unknown type is invalid", discount: &gen.SaleDiscount{Type: "bogus", Value: "1.00"}, wantErr: true, wantStatus: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeDiscountAmount(tt.discount, subtotal)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got none")
				}
				var apiErr *apierr.Error
				if ok := asAPIErr(err, &apiErr); !ok || apiErr.Status != tt.wantStatus {
					t.Fatalf("err = %v, want status %d", err, tt.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("computeDiscountAmount: %v", err)
			}
			if got.StringFixed(2) != tt.want {
				t.Fatalf("got = %s, want %s", got.StringFixed(2), tt.want)
			}
		})
	}
}

// asAPIErr is errors.As inlined for this file's one call site, so
// pricing_test.go does not need its own "errors" import solely for it.
func asAPIErr(err error, target **apierr.Error) bool {
	apiErr, ok := err.(*apierr.Error)
	if ok {
		*target = apiErr
	}
	return ok
}

func TestDayBounds_anchorsInShopTimezoneNotUTC(t *testing.T) {
	tashkent := mustLocation(t, "Asia/Tashkent") // UTC+5, no DST
	date := oapitypes.Date{Time: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)}

	got := dayBounds(date, tashkent)
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, tashkent)
	if !got.Equal(want) {
		t.Fatalf("dayBounds = %v, want %v", got, want)
	}
	// In UTC, that instant is 2026-06-14T19:00:00Z — five hours earlier,
	// not midnight UTC on the 15th (the bug this guards against: treating
	// the wire date's own UTC-parsed midnight as if it were already the
	// shop's local midnight).
	wantUTC := time.Date(2026, 6, 14, 19, 0, 0, 0, time.UTC)
	if !got.UTC().Equal(wantUTC) {
		t.Fatalf("dayBounds in UTC = %v, want %v", got.UTC(), wantUTC)
	}
}

func TestSaleDateRange_halfOpenBoundsAndOrdering(t *testing.T) {
	tashkent := mustLocation(t, "Asia/Tashkent")
	from := oapitypes.Date{Time: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)}
	to := oapitypes.Date{Time: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)}

	fromPtr, toPtr, err := saleDateRange(&from, &to, tashkent)
	if err != nil {
		t.Fatalf("saleDateRange: %v", err)
	}
	wantFrom := time.Date(2026, 6, 10, 0, 0, 0, 0, tashkent)
	wantTo := time.Date(2026, 6, 13, 0, 0, 0, 0, tashkent) // to + 1 day, exclusive upper bound
	if !fromPtr.Equal(wantFrom) {
		t.Fatalf("from = %v, want %v", fromPtr, wantFrom)
	}
	if !toPtr.Equal(wantTo) {
		t.Fatalf("to = %v, want %v (half-open, inclusive `to` day + 1)", toPtr, wantTo)
	}

	// from > to is rejected.
	_, _, err = saleDateRange(&to, &from, tashkent)
	if err == nil {
		t.Fatal("want a validation error when from > to, got none")
	}
	var apiErr *apierr.Error
	if !asAPIErr(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("err = %v, want 400 VALIDATION_FAILED", err)
	}

	// Neither bound sent: both nil.
	fromPtr, toPtr, err = saleDateRange(nil, nil, tashkent)
	if err != nil || fromPtr != nil || toPtr != nil {
		t.Fatalf("saleDateRange(nil, nil) = (%v, %v, %v), want (nil, nil, nil)", fromPtr, toPtr, err)
	}
}
