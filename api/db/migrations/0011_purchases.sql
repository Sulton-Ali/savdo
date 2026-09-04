-- +goose Up
-- Same shape as shops.next_sale_number (0001_shops.sql) — a per-shop,
-- gap-free counter advanced only under row lock by this migration's own
-- NextPurchaseNumber query (D-45), never by a settings update.
ALTER TABLE shops
    ADD COLUMN next_purchase_number bigint NOT NULL DEFAULT 1;

CREATE TYPE purchase_status AS ENUM ('draft', 'received', 'cancelled');

CREATE TABLE purchases (
    id                   uuid PRIMARY KEY,
    shop_id              uuid NOT NULL REFERENCES shops (id),
    supplier_id          uuid NOT NULL REFERENCES suppliers (id),
    location_id          uuid NOT NULL REFERENCES locations (id),
    -- Server-generated "P-000001" (D-45), formatted by the service from
    -- NextPurchaseNumber (shops.next_purchase_number under row lock);
    -- never client-supplied.
    number               text NOT NULL,
    -- The supplier's own document number, free text, optional (D-45).
    supplier_invoice_no  text,
    status               purchase_status NOT NULL DEFAULT 'draft',
    received_at          timestamptz,
    note                 text,
    total_cost           numeric(14,2) NOT NULL DEFAULT 0,
    created_by           uuid REFERENCES users (id),
    cancelled_at         timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, number)
);

CREATE INDEX purchases_shop_id_status_idx ON purchases (shop_id, status);
CREATE INDEX purchases_shop_id_supplier_id_idx ON purchases (shop_id, supplier_id);
CREATE INDEX purchases_location_id_idx ON purchases (location_id);
CREATE INDEX purchases_created_by_idx ON purchases (created_by);

CREATE TABLE purchase_items (
    id           uuid PRIMARY KEY,
    shop_id      uuid NOT NULL REFERENCES shops (id),
    purchase_id  uuid NOT NULL REFERENCES purchases (id),
    -- Never hard-delete a variant referenced here (§ 04-DATA-MODEL.md rule
    -- 7); the FK is enough, it is never weakened.
    variant_id   uuid NOT NULL REFERENCES product_variants (id),
    qty          numeric(12,3) NOT NULL,
    unit_cost    numeric(14,2) NOT NULL,
    line_total   numeric(14,2) NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (qty > 0)
);

CREATE INDEX purchase_items_shop_id_purchase_id_idx ON purchase_items (shop_id, purchase_id);
CREATE INDEX purchase_items_variant_id_idx ON purchase_items (variant_id);

-- +goose Down
DROP TABLE purchase_items;
DROP TABLE purchases;
DROP TYPE purchase_status;
ALTER TABLE shops DROP COLUMN next_purchase_number;
