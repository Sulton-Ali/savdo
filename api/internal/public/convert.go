package public

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

// nullableString, nullableUUID and nullableTime convert a nil-able Go
// value (SQL NULL) to the tri-state nullable.Nullable a `["T","null"]`
// schema field generates — the same conversions catalog's own
// nullableString/nullableUUID/nullableTime apply; duplicated here rather
// than exported from catalog, which this package does not otherwise
// depend on (its own db/query reads are all direct, not through
// catalog.Service).
func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableUUID(v *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if v == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}

// nullableNumeric converts a scanned NUMERIC column (n.Valid false = SQL
// NULL) to a nullable.Nullable[string] decimal (ADR-007) — Product/
// VariantPublic's promoPrice/priceOverride shape.
func nullableNumeric(n pgtype.Numeric) (nullable.Nullable[string], error) {
	if !n.Valid {
		return nullable.NewNullNullable[string](), nil
	}
	d, err := money.FromNumeric(n)
	if err != nil {
		return nullable.Nullable[string]{}, err
	}
	return nullable.NewNullableWithValue(money.String(d)), nil
}

// toProductImage builds a gen.ProductImage from a
// ListCoverImagesForProductsRow/ListProductImagesRow-shaped set of
// fields — the same fields catalog.Handler.toGenProductImage converts,
// duplicated for the same reason as nullableString above (no dependency
// on catalog.Service).
func toProductImage(mediaBaseURL string, id, mediaID uuid.UUID, variantID *uuid.UUID, sortOrder int32, isCover bool, storageKey string) gen.ProductImage {
	img := gen.ProductImage{
		Id: id, MediaId: mediaID, SortOrder: int(sortOrder), IsCover: isCover,
		Urls: media.URLs(mediaBaseURL, storageKey),
	}
	if variantID != nil {
		img.VariantId = nullable.NewNullableWithValue(*variantID)
	} else {
		img.VariantId = nullable.NewNullNullable[uuid.UUID]()
	}
	return img
}

// escapeLikePattern escapes s for safe interpolation into a Postgres
// ILIKE pattern, the same three-character escape catalog.escapeLikePattern
// applies (duplicated for the same reason as nullableString above): a
// public caller searching for a product literally named "50% off" must
// not have `%`/`_` match as SQL wildcards.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// maxSearchLength bounds `?q=`, mirroring catalog.maxSearchLength — a
// public caller gets the same defense against a pathologically long
// ILIKE pattern a cashier's search already has.
const maxSearchLength = 100

// searchTerm trims raw, caps it at maxSearchLength runes and escapes LIKE
// wildcards, returning nil when the trimmed value is empty (no filter).
func searchTerm(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if utf8.RuneCountInString(trimmed) > maxSearchLength {
		r := []rune(trimmed)
		trimmed = string(r[:maxSearchLength])
	}
	escaped := escapeLikePattern(trimmed)
	return &escaped
}

// decodeBlock re-marshals a content.ResolvedBlock's generic
// map[string]interface{} (already validated and normalized once, at
// content.Service.Upsert time) into T, one of the O-19 gen.Content*
// shapes. The data came from content_blocks.data, written only through
// content.Service.Upsert's own validation, so a decode failure here means
// the row was written some other way — reported as an error, not
// silently swallowed, the same posture content.decodeData takes for its
// own (map, not struct) decode.
func decodeBlock[T any](data map[string]interface{}) (T, error) {
	var out T
	raw, err := json.Marshal(data)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

// attributesFrom decodes a product_variants.attributes jsonb column into
// gen.AttributeValues, the same conversion catalog.toGenVariant applies
// to the same column.
func attributesFrom(raw json.RawMessage) (gen.AttributeValues, error) {
	var attrs gen.AttributeValues
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, err
	}
	return attrs, nil
}

// effectiveLocale is what the `locale` response field reports for a
// single translated entity: the resolved localeUsed when a translation
// exists, or the requested locale itself when none does yet (localeUsed
// == "") — never an empty string, which is not a member of gen.Locale's
// uz/ru/en vocabulary. Duplicates catalog's own unexported
// effectiveLocale (internal/catalog/locale.go) byte-for-byte, for the
// same reason as nullableString above (no dependency on catalog.Service)
// — GetPublicProductBySlug is this package's only caller, the same
// single-translated-row shape catalog.Handler.toGenProduct/toGenCategory
// already apply this to. PublicShop.locale does NOT use this: it
// composes six independently-resolved content blocks, so its own doc
// comment (contracts/openapi.yaml) and tests instead pin it to "the
// locale this response was resolved for", unconditionally — there is no
// single row for it to have "come from".
func effectiveLocale(localeUsed, requested string) gen.Locale {
	if localeUsed == "" {
		return gen.Locale(requested)
	}
	return gen.Locale(localeUsed)
}
