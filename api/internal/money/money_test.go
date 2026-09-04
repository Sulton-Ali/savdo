package money_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/Sulton-Ali/savdo/api/internal/money"
)

func numeric(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("numeric(%q): %v", s, err)
	}
	return n
}

func TestFromNumeric_roundTripsThroughToNumeric(t *testing.T) {
	tests := []string{"0.00", "125000.00", "99.99", "1000000.00", "0.01"}
	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			n := numeric(t, s)
			d, err := money.FromNumeric(n)
			if err != nil {
				t.Fatalf("FromNumeric(%q): %v", s, err)
			}
			if money.String(d) != s {
				t.Fatalf("String(FromNumeric(%q)) = %q, want %q", s, money.String(d), s)
			}

			back := money.ToNumeric(d)
			if !back.Valid {
				t.Fatalf("ToNumeric(%v).Valid = false, want true", d)
			}
			d2, err := money.FromNumeric(back)
			if err != nil {
				t.Fatalf("FromNumeric(ToNumeric(%v)): %v", d, err)
			}
			if !d.Equal(d2) {
				t.Fatalf("round trip mismatch: %v != %v", d, d2)
			}
		})
	}
}

func TestFromNumeric_rejectsInvalidNaNAndInfinite(t *testing.T) {
	if _, err := money.FromNumeric(pgtype.Numeric{Valid: false}); err == nil {
		t.Fatal("want an error converting a NULL/invalid Numeric")
	}
	if _, err := money.FromNumeric(pgtype.Numeric{Valid: true, NaN: true}); err == nil {
		t.Fatal("want an error converting NaN")
	}
	if _, err := money.FromNumeric(pgtype.Numeric{Valid: true, InfinityModifier: pgtype.Infinity}); err == nil {
		t.Fatal("want an error converting +Infinity")
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
		want    string
	}{
		{"125000.00", false, "125000.00"},
		{"125000", false, "125000.00"},
		{"0.01", false, "0.01"},
		{"0.1", false, "0.10"},
		{"0", false, "0.00"},
		{"12.345", true, ""},
		{"-5.00", true, ""},
		{"abc", true, ""},
		{"", true, ""},
		{"1e3", true, ""},
		{"1.2.3", true, ""},
		{" 5.00", true, ""},
		{"5.00 ", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			d, err := money.ParseAmount(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseAmount(%q) = %v, want an error", tt.in, d)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAmount(%q): %v", tt.in, err)
			}
			if money.String(d) != tt.want {
				t.Fatalf("ParseAmount(%q) = %q, want %q", tt.in, money.String(d), tt.want)
			}
		})
	}
}

func TestString_fixesTwoDecimals(t *testing.T) {
	d := decimal.RequireFromString("5")
	if got := money.String(d); got != "5.00" {
		t.Fatalf("String(5) = %q, want %q", got, "5.00")
	}
}
