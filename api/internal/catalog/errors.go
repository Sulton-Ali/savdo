package catalog

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// conflictField maps a unique-violation (pgx error code 23505) to the
// API's `details.field` name by constraint name — never by parsing the
// driver's error message text — per the task's constraint -> field map.
// Returns ok=false for any other error, so callers fall back to wrapping
// the error as a 500 rather than mis-reporting it as a conflict.
func conflictField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "products_shop_id_slug_key", "categories_shop_id_slug_key":
		return "slug", true
	case "products_shop_id_sku_key", "product_variants_shop_id_sku_key":
		return "sku", true
	case "product_variants_shop_id_barcode_key":
		return "barcode", true
	case "product_variants_product_id_attributes_key":
		return "attributes", true
	case "attribute_definitions_shop_id_code_key":
		return "code", true
	case "product_images_product_id_media_id_key":
		return "mediaId", true
	case "product_images_one_cover_key":
		// Backstop only: AddProductImage always runs ClearCover before
		// SetCover/is_cover=true inside one transaction, so this partial
		// unique index should never actually fire in practice.
		return "isCover", true
	default:
		return "", false
	}
}

// invalidFKField maps a foreign-key violation (pgx error code 23503) to
// the API's `details.fields` name by constraint name, per the task's
// constraint -> field map: these are 400 VALIDATION_FAILED `invalid`, not
// 409 conflicts — the client referenced an id that doesn't exist (or
// doesn't belong to this shop), not a duplicate of something that does.
func invalidFKField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "products_category_id_fkey":
		return "categoryId", true
	case "products_unit_id_fkey":
		return "unitId", true
	case "categories_parent_id_fkey":
		return "parentId", true
	case "categories_image_id_fkey":
		return "imageId", true
	case "product_images_media_id_fkey":
		return "mediaId", true
	case "product_images_variant_id_fkey":
		return "variantId", true
	default:
		return "", false
	}
}
