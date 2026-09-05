-- +goose Up
CREATE TABLE customers (
    id                 uuid PRIMARY KEY,
    shop_id            uuid NOT NULL REFERENCES shops (id),
    full_name          text NOT NULL,
    phone              text,
    telegram_username  text,
    note               text,
    tags               text[] NOT NULL DEFAULT '{}',
    deleted_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Partial, same reasoning as suppliers_shop_id_name_key
-- (0010_suppliers.sql): a plain UNIQUE(shop_id, phone) would count a
-- soft-deleted customer's phone as taken forever, so only live rows
-- participate; NULL phones are already excluded from a plain unique
-- index but stated explicitly for clarity, matching users_shop_id_phone_key
-- (0002_users_sessions.sql).
CREATE UNIQUE INDEX customers_shop_id_phone_key ON customers (shop_id, phone) WHERE phone IS NOT NULL AND deleted_at IS NULL;

-- Supports ListCustomers' name lookups and any future exact/prefix match
-- on full_name. Like suppliers' ILIKE search (0010_suppliers.sql — "the
-- suppliers directory is small, no trigram index needed"), the '%q%'
-- substring search in ListCustomers still falls back to a sequential
-- scan; this plain btree index is not for that, only for shop_id-scoped
-- name ordering/lookup.
CREATE INDEX customers_shop_id_full_name_idx ON customers (shop_id, full_name) WHERE deleted_at IS NULL;

-- Supports ListCustomers' (created_at, id) DESC keyset cursor directly,
-- the same reasoning as sales_shop_id_completed_at_idx (0016_sales.sql).
CREATE INDEX customers_shop_id_created_at_idx ON customers (shop_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE customers;
