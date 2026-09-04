package media

import (
	"testing"

	"github.com/google/uuid"
)

func TestOriginalKey(t *testing.T) {
	shopID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	got := originalKey(shopID, id, 2026, 9, "jpg")
	want := shopID.String() + "/2026/09/" + id.String() + ".jpg"
	if got != want {
		t.Fatalf("originalKey = %q, want %q", got, want)
	}
}

func TestDerivativeKey(t *testing.T) {
	tests := []struct {
		original string
		suffix   string
		want     string
	}{
		{"shop/2026/09/id.jpg", suffixThumb, "shop/2026/09/id_thumb.webp"},
		{"shop/2026/09/id.png", suffixCard, "shop/2026/09/id_card.webp"},
		{"shop/2026/09/id.webp", suffixFull, "shop/2026/09/id_full.webp"},
	}
	for _, tt := range tests {
		if got := derivativeKey(tt.original, tt.suffix); got != tt.want {
			t.Errorf("derivativeKey(%q, %q) = %q, want %q", tt.original, tt.suffix, got, tt.want)
		}
	}
}

func TestURLs(t *testing.T) {
	got := URLs("/media", "shop/2026/09/id.jpg")
	want := struct{ Thumb, Card, Full string }{
		Thumb: "/media/shop/2026/09/id_thumb.webp",
		Card:  "/media/shop/2026/09/id_card.webp",
		Full:  "/media/shop/2026/09/id_full.webp",
	}
	if got.Thumb != want.Thumb || got.Card != want.Card || got.Full != want.Full {
		t.Fatalf("URLs = %+v, want %+v", got, want)
	}

	// A trailing slash on baseURL must not produce a double slash.
	got2 := URLs("/media/", "shop/2026/09/id.jpg")
	if got2.Thumb != want.Thumb {
		t.Fatalf("URLs with trailing-slash baseURL: Thumb = %q, want %q", got2.Thumb, want.Thumb)
	}
}
