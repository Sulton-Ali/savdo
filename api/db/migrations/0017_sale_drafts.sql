-- +goose Up
-- Draft sales (D-87..D-89, § 04-DATA-MODEL.md § 4): a mutable, shared,
-- unpaid order — deliberately its own pair of tables, NOT a status on
-- `sales` (0016_sales.sql), so `sales` stays exactly `completed|voided`
-- and ADR-014 immutability and every report built on it are untouched. A
-- draft carries no sale number (only assigned on completion, from
-- shops.next_sale_number) and no prices (D-67 unit prices and the
-- estimated total are computed at read time by the service; completion
-- recomputes everything through the existing CreateSale path in the same
-- transaction as the draft's own deletion). Drafts do not move stock
-- (D-88): no stock_movements row exists for a draft line, ever.
--
-- discount_type is a real enum, not free text (top-of-file convention:
-- "real enum types"), mirroring contracts/openapi.yaml's DiscountType
-- schema (percent|fixed) — `sales` itself has no equivalent column
-- (it stores the already-resolved discount_amount, D-52/D-57), so this
-- type is new here, not reused from 0016_sales.sql.
CREATE TYPE discount_type AS ENUM ('percent', 'fixed');

CREATE TABLE sale_drafts (
    id               uuid PRIMARY KEY,
    shop_id          uuid NOT NULL REFERENCES shops (id),
    location_id      uuid NOT NULL REFERENCES locations (id),
    customer_id      uuid REFERENCES customers (id),
    discount_type    discount_type,
    discount_value   numeric(14,2),
    discount_reason  text,
    note             text,
    -- Who created the draft (D-89: editable/deletable by its creator or
    -- manager+). Nullable only in the defensive sense every other
    -- created_by FK in this schema already is (purchases.created_by,
    -- 0011_purchases.sql) — the service always sets it.
    created_by       uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (discount_type IS NULL AND discount_value IS NULL)
        OR (discount_type IS NOT NULL AND discount_value IS NOT NULL)
    ),
    CHECK (discount_value IS NULL OR discount_value >= 0)
);

CREATE INDEX sale_drafts_location_id_idx ON sale_drafts (location_id);
CREATE INDEX sale_drafts_customer_id_idx ON sale_drafts (customer_id);
CREATE INDEX sale_drafts_created_by_idx ON sale_drafts (created_by);
-- ListSaleDrafts' keyset cursor (shop_id, created_at DESC, id DESC),
-- same shape as sales_shop_id_completed_at_idx (0016_sales.sql).
CREATE INDEX sale_drafts_shop_id_created_at_idx ON sale_drafts (shop_id, created_at DESC, id DESC);

-- Join rows (§ 04-DATA-MODEL.md rule 7: hard-delete join rows), so
-- ON DELETE CASCADE here is the schema enforcing that a draft's lines
-- never outlive the draft — unlike sale_items/purchase_items, which
-- reference an immutable or append-only parent and are never bulk
-- hard-deleted this way. No price or cost column (D-87): prices are
-- computed at read time and frozen only on completion, in sale_items.
CREATE TABLE sale_draft_items (
    id            uuid PRIMARY KEY,
    shop_id       uuid NOT NULL REFERENCES shops (id),
    -- Named sale_draft_id, not draft_id (docs/04-DATA-MODEL.md § 4's own
    -- "sale_draft_items — sale_draft_id, ..." column list — the doc is
    -- the spec; this migration is still unmerged, so the column follows
    -- it rather than the other way around) — mirrors the
    -- table-name-prefixed FK convention every other join column in this
    -- schema already uses (sale_items.sale_id, purchase_items.purchase_id).
    sale_draft_id uuid NOT NULL REFERENCES sale_drafts (id) ON DELETE CASCADE,
    -- No cascade off product_variants (§ 04-DATA-MODEL.md rule 7): a
    -- variant referenced by a draft line can never be hard-deleted,
    -- same as sale_items.variant_id/purchase_items.variant_id.
    variant_id    uuid NOT NULL REFERENCES product_variants (id),
    qty           numeric(12,3) NOT NULL,
    -- Display order within the draft, service-assigned (0-based or
    -- 1-based, the service's choice) so PATCH .../drafts/{id} replacing
    -- `items` reproduces the client's line order on the next read.
    position      integer NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (qty > 0)
);

CREATE INDEX sale_draft_items_shop_id_idx ON sale_draft_items (shop_id);
CREATE INDEX sale_draft_items_variant_id_idx ON sale_draft_items (variant_id);
-- ListSaleDraftItems' own lookup and ordering (by draft, by position).
CREATE INDEX sale_draft_items_sale_draft_id_position_idx ON sale_draft_items (sale_draft_id, position);

-- +goose Down
DROP TABLE sale_draft_items;
DROP TABLE sale_drafts;
DROP TYPE discount_type;
