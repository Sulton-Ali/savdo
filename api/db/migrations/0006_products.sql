-- +goose Up
-- Needed for the trigram GIN index on product_translations.name below
-- (ILIKE / similarity search across product names).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE products (
    id            uuid PRIMARY KEY,
    shop_id       uuid NOT NULL REFERENCES shops (id),
    category_id   uuid REFERENCES categories (id),
    unit_id       uuid NOT NULL REFERENCES units (id),
    slug          text NOT NULL,
    sku           text,
    base_price    numeric(14,2) NOT NULL,
    cost_price    numeric(14,2),
    promo_price   numeric(14,2),
    promo_from    timestamptz,
    promo_to      timestamptz,
    is_active     boolean NOT NULL DEFAULT true,
    is_featured   boolean NOT NULL DEFAULT false,
    has_variants  boolean NOT NULL DEFAULT false,
    deleted_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, slug)
);

-- Partial: sku is optional, and a plain UNIQUE(shop_id, sku) would treat
-- every NULL as distinct anyway, but this is the form that generalizes (see
-- users_shop_id_phone_key for the same reasoning).
CREATE UNIQUE INDEX products_shop_id_sku_key ON products (shop_id, sku) WHERE sku IS NOT NULL;
CREATE INDEX products_shop_id_category_id_idx ON products (shop_id, category_id);
CREATE INDEX products_unit_id_idx ON products (unit_id);

CREATE TABLE product_translations (
    product_id   uuid NOT NULL REFERENCES products (id),
    locale       text NOT NULL,
    name         text NOT NULL,
    description  text,
    PRIMARY KEY (product_id, locale)
);

CREATE INDEX product_translations_name_trgm_idx ON product_translations USING gin (name gin_trgm_ops);

CREATE TABLE product_variants (
    id              uuid PRIMARY KEY,
    shop_id         uuid NOT NULL REFERENCES shops (id),
    product_id      uuid NOT NULL REFERENCES products (id),
    sku             text,
    barcode         text,
    -- e.g. {"size": "L", "color": "blue"}; '{}' for a product with no
    -- options (every product has at least one variant, § 04-DATA-MODEL.md).
    attributes      jsonb NOT NULL DEFAULT '{}',
    price_override  numeric(14,2),
    cost_override   numeric(14,2),
    is_active       boolean NOT NULL DEFAULT true,
    deleted_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, attributes)
);

-- Partial, same reasoning as products_shop_id_sku_key: sku/barcode are
-- optional per variant.
CREATE UNIQUE INDEX product_variants_shop_id_sku_key ON product_variants (shop_id, sku) WHERE sku IS NOT NULL;
CREATE UNIQUE INDEX product_variants_shop_id_barcode_key ON product_variants (shop_id, barcode) WHERE barcode IS NOT NULL;
CREATE INDEX product_variants_shop_id_product_id_idx ON product_variants (shop_id, product_id);

-- +goose Down
DROP TABLE product_variants;
DROP TABLE product_translations;
DROP TABLE products;
DROP EXTENSION pg_trgm;
