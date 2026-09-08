package public

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// promoActive reports whether today, in loc (the shop's timezone), falls
// within [from, to] inclusive by calendar date (D-68: "the time part is
// ignored"). This duplicates sales.promoActive (internal/sales/create.go)
// byte-for-byte: that function is unexported and keyed to
// db.GetVariantForSaleRow, and internal/sales is outside this task's file
// scope, so — the same "specialized" duplication
// internal/sales/drafts.go's own effectiveDraftUnitPrice already applies
// relative to create.go's effectiveUnitPrice, for the same reason (a
// different row shape) — this is a second, independent copy of the same
// D-67/D-68 rule rather than an exported shared helper. Any future change
// to the rule must be made in all three places.
func promoActive(from, to *time.Time, now time.Time, loc *time.Location) bool {
	if from == nil || to == nil {
		return false
	}
	const dayFormat = "2006-01-02"
	today := now.In(loc).Format(dayFormat)
	fromDay := from.In(loc).Format(dayFormat)
	toDay := to.In(loc).Format(dayFormat)
	return today >= fromDay && today <= toDay
}

// effectivePrice resolves the D-67 precedence — "the promo price when
// the promo is active, else the variant priceOverride, else the product
// basePrice" — into the PublicPrice the public contract sends: `regular`
// is the pre-promo price (priceOverride when valid, else basePrice),
// `current` is what a customer actually pays right now (the promo price
// when active, replacing `regular` entirely — D-67 says "else", never
// combined with an override).
func effectivePrice(basePrice pgtype.Numeric, promoPrice pgtype.Numeric, promoFrom, promoTo *time.Time, priceOverride pgtype.Numeric, now time.Time, loc *time.Location) (gen.PublicPrice, error) {
	regular, err := money.FromNumeric(basePrice)
	if err != nil {
		return gen.PublicPrice{}, err
	}
	if priceOverride.Valid {
		regular, err = money.FromNumeric(priceOverride)
		if err != nil {
			return gen.PublicPrice{}, err
		}
	}

	current := regular
	active := false
	if promoPrice.Valid && promoActive(promoFrom, promoTo, now, loc) {
		current, err = money.FromNumeric(promoPrice)
		if err != nil {
			return gen.PublicPrice{}, err
		}
		active = true
	}

	return gen.PublicPrice{Regular: money.String(regular), Current: money.String(current), PromoActive: active}, nil
}

// shopLocation resolves shop.Timezone to a *time.Location, falling back
// to UTC when the stored value fails to load (defensive only — every
// timezone this codebase writes is a valid IANA name; a shop row with a
// bad one is a data bug worth a wrong-but-safe fallback here, not a 500
// on every public request).
func shopLocation(shop db.Shop) *time.Location {
	loc, err := time.LoadLocation(shop.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// availabilityRank orders Availability from worst to best, so
// bestAvailability can reduce a set of per-variant values with a simple
// max.
func availabilityRank(a gen.Availability) int {
	switch a {
	case gen.InStock:
		return 2
	case gen.Low:
		return 1
	default:
		return 0
	}
}

// classifyAvailability applies D-44's threshold rule (O-20): out of
// stock at or below zero, low at or below the effective threshold, else
// in stock.
func classifyAvailability(qty, threshold decimal.Decimal) gen.Availability {
	switch {
	case qty.Sign() <= 0:
		return gen.OutOfStock
	case qty.Cmp(threshold) <= 0:
		return gen.Low
	default:
		return gen.InStock
	}
}

// bestAvailability reduces a product's per-variant availability values
// to the single best one (O-20: "product's list-level availability is
// the best of its active variants"). Empty input (a product somehow
// listed with zero variant rows) is out_of_stock, never a zero value
// that would look like a real Availability.
func bestAvailability(values []gen.Availability) gen.Availability {
	best := gen.OutOfStock
	for _, v := range values {
		if availabilityRank(v) > availabilityRank(best) {
			best = v
		}
	}
	return best
}
