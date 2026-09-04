package catalog

import (
	"strings"
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

func TestTranslationsFieldReason(t *testing.T) {
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: "Koylak"}}); reason != "" {
		t.Errorf("reason = %q, want valid (empty)", reason)
	}
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: "  "}}); reason != "invalid" {
		t.Errorf("reason = %q, want invalid (blank name)", reason)
	}

	longName := strings.Repeat("a", maxTranslationNameLength+1)
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: longName}}); reason != "too_long" {
		t.Errorf("reason = %q, want too_long (name over %d runes)", reason, maxTranslationNameLength)
	}

	okName := strings.Repeat("a", maxTranslationNameLength)
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: okName}}); reason != "" {
		t.Errorf("reason = %q, want valid at exactly the name limit", reason)
	}

	longDesc := strings.Repeat("a", maxTranslationDescriptionLength+1)
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: "Koylak", Description: &longDesc}}); reason != "too_long" {
		t.Errorf("reason = %q, want too_long (description over %d runes)", reason, maxTranslationDescriptionLength)
	}

	okDesc := strings.Repeat("a", maxTranslationDescriptionLength)
	if reason := translationsFieldReason(map[string]translationEntry{"uz": {Name: "Koylak", Description: &okDesc}}); reason != "" {
		t.Errorf("reason = %q, want valid at exactly the description limit", reason)
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
