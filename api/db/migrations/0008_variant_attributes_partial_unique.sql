-- +goose Up
-- product_variants_product_id_attributes_key was a plain table-level
-- UNIQUE(product_id, attributes) (0006_products.sql), which also counts a
-- soft-deleted variant's attributes as taken forever — a deleted "L" hoodie
-- variant permanently blocks recreating "L" on the same product. Same
-- partial-index reasoning as product_images_one_cover_key /
-- products_shop_id_sku_key: only live rows should conflict. The index keeps
-- the exact original name so catalog.conflictField's constraint -> field
-- map (api/internal/catalog/errors.go) needs no change.
ALTER TABLE product_variants DROP CONSTRAINT product_variants_product_id_attributes_key;

CREATE UNIQUE INDEX product_variants_product_id_attributes_key
    ON product_variants (product_id, attributes)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX product_variants_product_id_attributes_key;

ALTER TABLE product_variants
    ADD CONSTRAINT product_variants_product_id_attributes_key UNIQUE (product_id, attributes);
