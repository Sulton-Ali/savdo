package catalog

import "testing"

func TestEscapeLikePattern(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"50% off", `50\% off`},
		{"a_b", `a\_b`},
		{`back\slash`, `back\\slash`},
		{"100%_off\\now", `100\%\_off\\now`},
		{"", ""},
	}
	for _, tt := range tests {
		if got := escapeLikePattern(tt.in); got != tt.want {
			t.Errorf("escapeLikePattern(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCapRunes(t *testing.T) {
	if got := capRunes("hello", 10); got != "hello" {
		t.Errorf("capRunes(short) = %q, want unchanged", got)
	}
	if got := capRunes("hello", 3); got != "hel" {
		t.Errorf("capRunes(long) = %q, want %q", got, "hel")
	}
	// Multi-byte runes (Cyrillic) must not be split mid-encoding.
	in := "Кепка" // 5 runes, more bytes
	if got := capRunes(in, 3); got != "Кеп" {
		t.Errorf("capRunes(multibyte) = %q, want %q", got, "Кеп")
	}
}
