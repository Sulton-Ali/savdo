package catalog

import (
	"strings"

	"github.com/Sulton-Ali/savdo/api/gen"
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

// validateTranslationNames reports whether every provided entry has a
// non-blank Name (an entry with only a Description and a blank Name is
// meaningless — there is nothing to display for that locale).
func validateTranslationNames(entries map[string]translationEntry) bool {
	for _, e := range entries {
		if strings.TrimSpace(e.Name) == "" {
			return false
		}
	}
	return true
}
