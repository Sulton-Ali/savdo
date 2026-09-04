-- +goose Up
CREATE TABLE suppliers (
    id                 uuid PRIMARY KEY,
    shop_id            uuid NOT NULL REFERENCES shops (id),
    name               text NOT NULL,
    contact_name       text,
    phone              text,
    telegram_username  text,
    note               text,
    deleted_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Partial, same reasoning as product_variants_product_id_attributes_key
-- (0008_variant_attributes_partial_unique.sql): a plain UNIQUE(shop_id,
-- name) would count a soft-deleted supplier's name as taken forever, so
-- only live rows participate.
CREATE UNIQUE INDEX suppliers_shop_id_name_key ON suppliers (shop_id, name) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE suppliers;
