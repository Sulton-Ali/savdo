package reports_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
)

// encodeRawCursor base64-encodes raw the same way
// reports.encodeByProductCursor would, without going through that
// unexported function (this package is reports_test, external) — lets
// these tests hand ListSalesByProduct a cursor whose revenue component is
// not one money.String itself would ever produce.
func encodeRawCursor(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// assertCursorInvalid asserts err is 400 VALIDATION_FAILED
// {"fields":{"cursor":"invalid"}} — the shape every malformed-cursor case
// below must produce.
func assertCursorInvalid(t *testing.T, err error) {
	t.Helper()
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error = %v, want 400 VALIDATION_FAILED", err)
	}
	fields, _ := apiErr.Details["fields"].(map[string]string)
	if fields["cursor"] != "invalid" {
		t.Fatalf("details.fields = %v, want cursor invalid", apiErr.Details)
	}
}

// TestListSalesByProduct_cursorRejectsExponentNotation is the regression
// test for the reviewed vulnerability: a cursor whose revenue component
// uses exponent notation (`1e-1000000`, `1e400`) used to reach
// decimal.NewFromString unchecked, producing a Decimal with an
// astronomical Exponent() that made money.ToNumeric's result expensive
// for pgx's numeric encoder to rescale (tens of seconds of CPU measured in
// review). money.ParseSignedAmount's pattern excludes exponent notation
// entirely, so these must now fail fast with 400, not hang.
func TestListSalesByProduct_cursorRejectsExponentNotation(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "cursor-exponent")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	productID := uuid.NewString()
	tests := []string{
		"1e-1000000|" + productID,
		"1e400|" + productID,
		"-1e400|" + productID,
	}
	now := time.Now()
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			cursor := encodeRawCursor(raw)
			start := time.Now()
			_, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now, now, nil, &cursor))
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("cursor decode took %s, want well under a second", elapsed)
			}
			assertCursorInvalid(t, err)
		})
	}
}

// TestListSalesByProduct_cursorMalformed covers the plain malformed-input
// cases: invalid base64, and valid base64 missing the revenue/product-id
// separator.
func TestListSalesByProduct_cursorMalformed(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "cursor-malformed")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)
	now := time.Now()

	tests := map[string]string{
		"not valid base64":           "!!!not-base64!!!",
		"valid base64, no separator": encodeRawCursor("10.00" + uuid.NewString()),
	}
	for name, cursor := range tests {
		t.Run(name, func(t *testing.T) {
			cursor := cursor
			_, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now, now, nil, &cursor))
			assertCursorInvalid(t, err)
		})
	}
}

// TestListSalesByProduct_cursorRejectsOversized proves the 128-byte cap
// (convert.go's maxByProductCursorLen) rejects a large cursor before any
// base64 decoding or parsing runs.
func TestListSalesByProduct_cursorRejectsOversized(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "cursor-oversized")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	cursor := strings.Repeat("a", 10*1024) // 10 KB
	now := time.Now()
	_, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now, now, nil, &cursor))
	assertCursorInvalid(t, err)
}

// TestListSalesByProduct_cursorAcceptsNegativeRevenue proves
// money.ParseSignedAmount's whole reason for existing still works: a
// legitimately negative revenue cursor (a period where returns exceeded
// sales) is accepted, not rejected as "invalid" alongside the exponent
// payloads above.
func TestListSalesByProduct_cursorAcceptsNegativeRevenue(t *testing.T) {
	_, q := newTestQueries(t)
	ctx := context.Background()
	h := reports.NewHandler(reports.NewService(q))

	shopRow := seedShop(ctx, t, q, "cursor-negative")
	owner := seedUser(ctx, t, q, shopRow.ID, "owner1", db.UserRoleOwner)

	cursor := encodeRawCursor("-5.00|" + uuid.NewString())
	now := time.Now()
	if _, err := h.ListSalesByProduct(ctxAs(shopRow.ID, owner), byProductParams(now, now, nil, &cursor)); err != nil {
		t.Fatalf("ListSalesByProduct with a negative-revenue cursor: %v", err)
	}
}
