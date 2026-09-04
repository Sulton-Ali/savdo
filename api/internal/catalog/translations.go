package catalog

import (
	"strings"
	"unicode/utf8"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// maxTranslationNameLength and maxTranslationDescriptionLength bound one
// locale entry's Name and Description, counted in runes (not bytes) so a
// multi-byte uz/ru character counts once.
const (
	maxTranslationNameLength        = 200
	maxTranslationDescriptionLength = 2000
)

// translationEntry is one locale's name/description, independent of
// whichever wire shape it arrived in — gen.Translations' fixed en/ru/uz
// fields (products/categories/attribute definitions' create/patch
// bodies), or a plain map (ListAttributeDefinitions' aggregated
// `translations` column).
type translationEntry struct {
	Name        string
	Description *string
}

// translationsToMap flattens a gen.Translations (fixed En/Ru/Uz fields)
// into a map keyed by locale, containing only the locales the caller
// actually provided.
func translationsToMap(t gen.Translations) map[string]translationEntry {
	out := map[string]translationEntry{}
	if t.Uz != nil {
		out["uz"] = translationEntry{Name: t.Uz.Name, Description: t.Uz.Description}
	}
	if t.Ru != nil {
		out["ru"] = translationEntry{Name: t.Ru.Name, Description: t.Ru.Description}
	}
	if t.En != nil {
		out["en"] = translationEntry{Name: t.En.Name, Description: t.En.Description}
	}
	return out
}

// buildTranslations assembles a *gen.Translations from per-locale
// entries, or nil when entries is empty — matching the contract's
// `translations` field being entirely absent (`omitempty`) rather than an
// empty object when there is nothing to report yet.
func buildTranslations(entries map[string]translationEntry) *gen.Translations {
	if len(entries) == 0 {
		return nil
	}
	out := &gen.Translations{}
	if e, ok := entries["uz"]; ok {
		out.Uz = &gen.TranslationEntry{Name: e.Name, Description: e.Description}
	}
	if e, ok := entries["ru"]; ok {
		out.Ru = &gen.TranslationEntry{Name: e.Name, Description: e.Description}
	}
	if e, ok := entries["en"]; ok {
		out.En = &gen.TranslationEntry{Name: e.Name, Description: e.Description}
	}
	return out
}

// hasNonEmptyName reports whether entries has a usable (non-blank) name
// for locale — the create-time invariant the Translations schema doc
// describes: "an entry for the shop's own default locale is required".
func hasNonEmptyName(entries map[string]translationEntry, locale string) bool {
	e, ok := entries[locale]
	return ok && strings.TrimSpace(e.Name) != ""
}

// translationsFieldReason validates every provided entry: a non-blank
// Name (an entry with only a Description and a blank Name is meaningless
// — there is nothing to display for that locale), a Name within
// maxTranslationNameLength runes, and a Description (if any) within
// maxTranslationDescriptionLength runes. Returns the O-12 reason to
// report as `fields.translations` — "invalid" for a blank name,
// "too_long" for either limit — or "" when every entry is valid.
func translationsFieldReason(entries map[string]translationEntry) string {
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			return "invalid"
		}
		if utf8.RuneCountInString(name) > maxTranslationNameLength {
			return "too_long"
		}
		if e.Description != nil && utf8.RuneCountInString(*e.Description) > maxTranslationDescriptionLength {
			return "too_long"
		}
	}
	return ""
}
