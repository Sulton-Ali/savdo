-- +goose Up
CREATE TYPE stock_movement_kind AS ENUM (
    'purchase_in', 'sale_out', 'sale_void_in', 'return_in',
    'adjustment', 'transfer_out', 'transfer_in'
);
CREATE TYPE adjustment_reason AS ENUM ('count_correction', 'damaged', 'lost', 'found', 'other');

-- Append-only ledger (ADR-006, § 04-DATA-MODEL.md § 3): the source of
-- truth for stock. No updated_at — a row is written once and never
-- touched again; see the trigger below. stock_levels, further down, is
-- the only thing derived from it, and only by stock.Service.Move.
CREATE TABLE stock_movements (
    id                 uuid PRIMARY KEY,
    shop_id            uuid NOT NULL REFERENCES shops (id),
    variant_id         uuid NOT NULL REFERENCES product_variants (id),
    location_id        uuid NOT NULL REFERENCES locations (id),
    kind               stock_movement_kind NOT NULL,
    -- Signed: positive in, negative out.
    qty                numeric(12,3) NOT NULL,
    unit_cost          numeric(14,2),
    -- The kind of record ref_id points to, e.g. 'purchase', 'sale', 'transfer'.
    ref_type           text,
    ref_id             uuid,
    -- Set for 'adjustment' movements only (D-46); enforced below.
    adjustment_reason  adjustment_reason,
    -- Free-text note, independent of adjustment_reason; may accompany any kind.
    reason             text,
    created_by         uuid REFERENCES users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    CHECK (qty <> 0),
    CHECK (
        (kind = 'adjustment' AND adjustment_reason IS NOT NULL)
        OR (kind <> 'adjustment' AND adjustment_reason IS NULL)
    )
);

-- Serves ListMovements' filters (variant/location/kind/from/to) and its
-- (created_at, id) cursor tiebreaker, shop_id leading (hard rule 1 and 10).
CREATE INDEX stock_movements_shop_variant_location_created_idx
    ON stock_movements (shop_id, variant_id, location_id, created_at, id);
-- Default movement-history listing (no variant/location filter): every
-- ListMovements call still orders by (created_at, id) DESC, so this
-- narrower index serves the common "just show recent movements" case
-- without the variant_id/location_id columns in between.
CREATE INDEX stock_movements_shop_created_idx
    ON stock_movements (shop_id, created_at DESC, id DESC);
CREATE INDEX stock_movements_location_id_idx ON stock_movements (location_id);
CREATE INDEX stock_movements_created_by_idx ON stock_movements (created_by);

-- Nothing may ever UPDATE or DELETE a movement (ADR-006, § 04-DATA-MODEL.md
-- rule 2): stock_levels and every rebuild (`savdo stock rebuild`) trust
-- this table completely, and that trust only holds if it is genuinely
-- append-only. Seeds and services insert through stock.Service.Move, never
-- raw SQL. Deliberately NOT covered: TRUNCATE — Postgres row-level
-- triggers (BEFORE UPDATE OR DELETE) never fire on it, so `TRUNCATE
-- stock_movements` would bypass this trigger entirely. testdb.Truncate
-- relies on exactly that (it truncates every table between tests,
-- including this one). The application's database role having TRUNCATE
-- privilege at all is a gap for a least-privilege role to close as a
-- Phase 8 hardening item, not something a trigger can prevent.
-- +goose StatementBegin
CREATE FUNCTION stock_movements_no_update_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'stock_movements is append-only: % is not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER stock_movements_immutable
    BEFORE UPDATE OR DELETE ON stock_movements
    FOR EACH ROW EXECUTE FUNCTION stock_movements_no_update_delete();

-- Materialized view over stock_movements, maintained only by
-- stock.Service.Move inside the movement's own transaction with
-- SELECT ... FOR UPDATE (ADR-006). Rebuildable from stock_movements by
-- `savdo stock rebuild`; no trigger needed here because nothing but that
-- one code path is ever allowed to touch it (enforced by review).
CREATE TABLE stock_levels (
    shop_id      uuid NOT NULL REFERENCES shops (id),
    variant_id   uuid NOT NULL REFERENCES product_variants (id),
    location_id  uuid NOT NULL REFERENCES locations (id),
    qty          numeric(12,3) NOT NULL DEFAULT 0,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (shop_id, variant_id, location_id)
);

CREATE INDEX stock_levels_location_id_idx ON stock_levels (location_id);

-- +goose Down
DROP TABLE stock_levels;
DROP TRIGGER stock_movements_immutable ON stock_movements;
DROP FUNCTION stock_movements_no_update_delete();
DROP TABLE stock_movements;
DROP TYPE adjustment_reason;
DROP TYPE stock_movement_kind;
