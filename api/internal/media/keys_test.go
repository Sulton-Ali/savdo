package media

import (
	"testing"

	"github.com/google/uuid"
)

func TestKeyStem(t *testing.T) {
	shopID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	got := keyStem(shopID, id, 2026, 9)
	want := shopID.String() + "/2026/09/" + id.String()
	if got != want {
		t.Fatalf("keyStem = %q, want %q", got, want)
	}
}

func TestDerivativeKey(t *testing.T) {
	tests := []struct {
		stem   string
		suffix string
		want   string
	}{
		{"shop/2026/09/id", suffixThumb, "shop/2026/09/id_thumb.webp"},
		{"shop/2026/09/id", suffixCard, "shop/2026/09/id_card.webp"},
		{"shop/2026/09/id", suffixFull, "shop/2026/09/id_full.webp"},
	}
	for _, tt := range tests {
		if got := derivativeKey(tt.stem, tt.suffix); got != tt.want {
			t.Errorf("derivativeKey(%q, %q) = %q, want %q", tt.stem, tt.suffix, got, tt.want)
		}
	}
}

func TestURLs(t *testing.T) {
	got := URLs("/media", "shop/2026/09/id")
	want := struct{ Thumb, Card, Full string }{
		Thumb: "/media/shop/2026/09/id_thumb.webp",
		Card:  "/media/shop/2026/09/id_card.webp",
		Full:  "/media/shop/2026/09/id_full.webp",
	}
	if got.Thumb != want.Thumb || got.Card != want.Card || got.Full != want.Full {
		t.Fatalf("URLs = %+v, want %+v", got, want)
	}

	// A trailing slash on baseURL must not produce a double slash.
	got2 := URLs("/media/", "shop/2026/09/id")
	if got2.Thumb != want.Thumb {
		t.Fatalf("URLs with trailing-slash baseURL: Thumb = %q, want %q", got2.Thumb, want.Thumb)
	}
}
