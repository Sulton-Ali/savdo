package catalog

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Cotton Shirt", "cotton-shirt"},
		{"  Extra   Spaces  ", "extra-spaces"},
		{"Футболка", "futbolka"},
		{"O'zbek", "ozbek"},
		{"!!!", ""},
		{"Size: L/XL", "size-l-xl"},
		{"Koʻylak", "koylak"},      // modifier letter turned comma (U+02BB)
		{"Gʻishtli", "gishtli"},    // modifier letter turned comma (U+02BB)
		{"Baʼzi", "bazi"},          // modifier letter apostrophe (U+02BC)
		{"Qo'ng'iroq", "qongiroq"}, // ASCII apostrophe, two glottal stops
	}
	for _, tt := range tests {
		if got := slugify(tt.in); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidSlug(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"cotton-shirt", true},
		{"a", true},
		{"a1-b2", true},
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"Has-Upper", false},
		{"double--hyphen", false},
		{"has space", false},
	}
	for _, tt := range tests {
		if got := validSlug(tt.in); got != tt.want {
			t.Errorf("validSlug(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestSlugCandidates(t *testing.T) {
	got := slugCandidates("Cotton Shirt", 3)
	want := []string{"cotton-shirt", "cotton-shirt-2", "cotton-shirt-3"}
	if len(got) != len(want) {
		t.Fatalf("slugCandidates length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slugCandidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSlugCandidates_emptyNameFallsBackToItem(t *testing.T) {
	got := slugCandidates("!!!", 1)
	if got[0] != "item" {
		t.Fatalf("slugCandidates(%q) = %q, want %q", "!!!", got[0], "item")
	}
}
