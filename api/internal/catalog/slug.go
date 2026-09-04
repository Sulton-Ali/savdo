package catalog

import (
	"regexp"
	"strconv"
	"strings"
)

// cyrillicToLatin approximates the common Cyrillic -> Latin transliteration
// used for Uzbek/Russian names, just enough for slugify to produce a
// readable ASCII slug from a Cyrillic-script translation. It is not a
// general-purpose transliterator.
var cyrillicToLatin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "j", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "x", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sh",
	'ъ': "", 'ы': "i", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
	'ў': "o", 'қ': "q", 'ғ': "g", 'ҳ': "h",
}

// slugRE is the shape every stored slug — generated or client-supplied —
// must have: lowercase ASCII letters/digits, hyphen-separated, no leading,
// trailing or doubled hyphen.
var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// glottalStops are Uzbek Latin's oʻ/gʻ glottal-stop marks — modifier
// letter turned comma (ʻ, U+02BB), modifier letter apostrophe (ʼ,
// U+02BC), and the plain ASCII apostrophe some input methods substitute
// for either — treated as letters to drop (not word separators), so
// "Koʻylak" -> "koylak" and "Gʻishtli" -> "gishtli", not "ko-ylak"/
// "g-ishtli".
var glottalStops = map[rune]bool{'ʻ': true, 'ʼ': true, '\'': true}

// slugify converts name into a lowercase ASCII slug: known Cyrillic
// letters transliterated (cyrillicToLatin), Uzbek Latin's glottal-stop
// marks (glottalStops) dropped, and any other punctuation/space treated
// as a separator; runs of separators collapse to one hyphen, and
// leading/trailing hyphens are trimmed. Used to derive a category/product
// slug from its default-locale translation name when the caller does not
// supply one.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if glottalStops[r] {
			continue
		}
		if repl, ok := cyrillicToLatin[r]; ok {
			b.WriteString(repl)
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return collapseHyphens(b.String())
}

func collapseHyphens(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// validSlug reports whether s matches slugRE and is non-empty — the shape
// check applied to a client-supplied slug (an auto-generated one from
// slugify always already matches, mechanically, except for the
// empty-input edge case slugCandidates below handles).
func validSlug(s string) bool {
	return s != "" && slugRE.MatchString(s)
}

// slugCandidates returns up to attempts candidate slugs to try, in order:
// the bare slugify(name) first (or "item" if that came out empty — a name
// that is entirely punctuation/digits-only-after-transliteration, an edge
// case worth a stable fallback rather than an empty slug), then
// "<base>-2", "<base>-3", ....
func slugCandidates(name string, attempts int) []string {
	base := slugify(name)
	if base == "" {
		base = "item"
	}
	candidates := make([]string, attempts)
	candidates[0] = base
	for i := 1; i < attempts; i++ {
		candidates[i] = base + "-" + strconv.Itoa(i+1)
	}
	return candidates
}
