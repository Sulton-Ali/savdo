package catalog

import (
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestTranslationsToMapAndBack(t *testing.T) {
	desc := "a description"
	in := gen.Translations{
		Uz: &gen.TranslationEntry{Name: "Koylak", Description: &desc},
		Ru: &gen.TranslationEntry{Name: "Rubashka"},
	}
	entries := translationsToMap(in)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries["uz"].Name != "Koylak" || entries["uz"].Description == nil || *entries["uz"].Description != desc {
		t.Fatalf("entries[uz] = %+v", entries["uz"])
	}

	out := buildTranslations(entries)
	if out == nil || out.Uz == nil || out.Uz.Name != "Koylak" {
		t.Fatalf("buildTranslations round trip = %+v", out)
	}
	if out.En != nil {
		t.Fatalf("out.En = %+v, want nil (no en entry was provided)", out.En)
	}
}

func TestBuildTranslations_emptyIsNil(t *testing.T) {
	if got := buildTranslations(map[string]translationEntry{}); got != nil {
		t.Fatalf("buildTranslations(empty) = %+v, want nil", got)
	}
}

func TestHasNonEmptyName(t *testing.T) {
	entries := map[string]translationEntry{"uz": {Name: "Koylak"}, "ru": {Name: "  "}}
	if !hasNonEmptyName(entries, "uz") {
		t.Error("hasNonEmptyName(uz) = false, want true")
	}
	if hasNonEmptyName(entries, "en") {
		t.Error("hasNonEmptyName(en) = true, want false (no entry)")
	}
}

func TestValidateTranslationNames(t *testing.T) {
	if !validateTranslationNames(map[string]translationEntry{"uz": {Name: "Koylak"}}) {
		t.Error("expected valid")
	}
	if validateTranslationNames(map[string]translationEntry{"uz": {Name: "  "}}) {
		t.Error("expected invalid: blank name")
	}
}

func TestAttributeCodeRegex(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"size", true},
		{"color", true},
		{"a", false}, // too short: needs at least 2 chars
		{"a1", true},
		{"Size", false},   // uppercase
		{"1size", false},  // must start with a letter
		{"size-x", false}, // hyphen not allowed
		{"", false},
	}
	for _, tt := range tests {
		if got := attributeCodeRE.MatchString(tt.code); got != tt.want {
			t.Errorf("attributeCodeRE.MatchString(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
