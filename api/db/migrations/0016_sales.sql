-- +goose Up
CREATE TYPE sale_kind AS ENUM ('sale', 'return');
CREATE TYPE sale_status AS ENUM ('completed', 'voided');
CREATE TYPE payment_method AS ENUM ('cash', 'card', 'transfer');

-- The sales ledger (ADR-014, § 04-DATA-MODEL.md § 4): a sale row is
-- written once, complete, at checkout time — there is no draft state like
-- purchases. number comes from shops.next_sale_number (0001_shops.sql,
-- already present) under row lock via this migration's own
-- NextSaleNumber query, never client-supplied (hard rule 8), exactly the
-- same shape as shops.next_purchase_number / NextPurchaseNumber
-- (0011_purchases.sql, D-45) — no second counter is added here.
CREATE TABLE sales (
    id                 uuid PRIMARY KEY,
    shop_id            uuid NOT NULL REFERENCES shops (id),
    number             bigint NOT NULL,
    kind               sale_kind NOT NULL,
    status             sale_status NOT NULL DEFAULT 'completed',
    location_id        uuid NOT NULL REFERENCES locations (id),
    customer_id        uuid REFERENCES customers (id),
    cashier_id         uuid NOT NULL REFERENCES users (id),
    -- Set only on a return (D-58): the sale a return refunds
    -- against. Self-reference. The CHECK below only ties this column's
    -- presence to *this* row's own kind (return -> set, sale -> null); it
    -- does NOT verify the referenced row is itself kind = 'sale' (a
    -- return pointing at another return) — that invariant is enforced by
    -- the sales service, not the database.
    original_sale_id   uuid REFERENCES sales (id),
    subtotal           numeric(14,2) NOT NULL,
    discount_amount    numeric(14,2) NOT NULL DEFAULT 0,
    discount_reason    text,
    total              numeric(14,2) NOT NULL,
    note               text,
    completed_at       timestamptz NOT NULL DEFAULT now(),
    -- Void columns (ADR-014): the only four columns an UPDATE may ever
    -- touch, enforced by the trigger below, and only once (completed ->
    -- voided).
    voided_at          timestamptz,
    voided_by          uuid REFERENCES users (id),
    void_reason        text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, number),
    CHECK (subtotal >= 0),
    CHECK (discount_amount >= 0),
    CHECK (total >= 0),
    CHECK (discount_amount <= subtotal),
    CHECK (total = subtotal - discount_amount),
    -- A return must reference the sale it refunds; a sale must not
    -- (D-58).
    CHECK (
        (kind = 'return' AND original_sale_id IS NOT NULL)
        OR (kind = 'sale' AND original_sale_id IS NULL)
    ),
    -- Ties the void columns to status (ADR-014): a completed sale carries
    -- none of them, a voided one carries at least voided_at/voided_by.
    -- The sales_immutable trigger below only constrains which columns an
    -- UPDATE may change; it does not by itself stop `UPDATE sales SET
    -- status = 'voided'` from leaving voided_at/voided_by NULL — this
    -- CHECK is what rejects that (and a directly-inserted 'voided' row
    -- with no voided_at/voided_by). void_reason stays optional even when
    -- voided.
    CHECK (
        (status = 'completed' AND voided_at IS NULL AND voided_by IS NULL AND void_reason IS NULL)
        OR (status = 'voided' AND voided_at IS NOT NULL AND voided_by IS NOT NULL)
    )
);

CREATE INDEX sales_location_id_idx ON sales (location_id);
CREATE INDEX sales_voided_by_idx ON sales (voided_by);
-- ListSalesForStaff/ForCashier's default (no filter) listing, newest
-- first; id included so the (completed_at, id) keyset cursor can range-scan
-- this index directly instead of re-sorting.
CREATE INDEX sales_shop_id_completed_at_idx ON sales (shop_id, completed_at DESC, id DESC);
-- Customer purchase history (§ 04-DATA-MODEL.md § 5: "purchase history is
-- a query on sales").
CREATE INDEX sales_shop_id_customer_id_idx ON sales (shop_id, customer_id);
-- Per-cashier reports, e.g. a cashier's own-day sales (§ 04-DATA-MODEL.md
-- § 7 permission matrix).
CREATE INDEX sales_shop_id_cashier_id_completed_at_idx ON sales (shop_id, cashier_id, completed_at);
-- Finding a sale's returns (has_returns / CountCompletedReturnsForSale)
-- and a return's original sale.
CREATE INDEX sales_shop_id_original_sale_id_idx ON sales (shop_id, original_sale_id);

-- +goose StatementBegin
CREATE FUNCTION sales_enforce_immutability() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'sales rows are immutable: DELETE is not allowed';
    END IF;

    -- The only UPDATE ever allowed is a void, and only starting from a
    -- completed sale (ADR-014) — this also rejects voiding an
    -- already-voided sale (OLD.status would be 'voided', not 'completed').
    IF OLD.status <> 'completed' OR NEW.status <> 'voided' THEN
        RAISE EXCEPTION 'sales rows are immutable: only a completed -> voided transition is allowed';
    END IF;

    -- Every other column must be unchanged; only status, voided_at,
    -- voided_by and void_reason may move.
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.shop_id IS DISTINCT FROM OLD.shop_id
        OR NEW.number IS DISTINCT FROM OLD.number
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.location_id IS DISTINCT FROM OLD.location_id
        OR NEW.customer_id IS DISTINCT FROM OLD.customer_id
        OR NEW.cashier_id IS DISTINCT FROM OLD.cashier_id
        OR NEW.original_sale_id IS DISTINCT FROM OLD.original_sale_id
        OR NEW.subtotal IS DISTINCT FROM OLD.subtotal
        OR NEW.discount_amount IS DISTINCT FROM OLD.discount_amount
        OR NEW.discount_reason IS DISTINCT FROM OLD.discount_reason
        OR NEW.total IS DISTINCT FROM OLD.total
        OR NEW.note IS DISTINCT FROM OLD.note
        OR NEW.completed_at IS DISTINCT FROM OLD.completed_at
        OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'sales rows are immutable: only status, voided_at, voided_by, void_reason may change';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER sales_immutable
    BEFORE UPDATE OR DELETE ON sales
    FOR EACH ROW EXECUTE FUNCTION sales_enforce_immutability();

-- Append-only, same reasoning and shape as stock_movements
-- (0012_stock_ledger.sql). shop_id is denormalised here (not just
-- inherited via sale_id) the same way purchase_items denormalises it
-- (0011_purchases.sql), so every query can filter on it directly (hard
-- rule 1). variant_id has no cascade (§ 04-DATA-MODEL.md rule 7): a
-- variant referenced by a sale item can never be hard-deleted.
CREATE TABLE sale_items (
    id                      uuid PRIMARY KEY,
    shop_id                 uuid NOT NULL REFERENCES shops (id),
    sale_id                 uuid NOT NULL REFERENCES sales (id),
    variant_id              uuid NOT NULL REFERENCES product_variants (id),
    qty                     numeric(12,3) NOT NULL,
    -- Price at sale time, after promo (never client-supplied, hard rule 8).
    unit_price              numeric(14,2) NOT NULL,
    -- Frozen cost_price/cost_override at sale time, for margin reports.
    -- Never returned to a cashier or public/bot code path (§
    -- 04-DATA-MODEL.md rule 8) — kept out of ListSaleItemsForCashier.
    unit_cost               numeric(14,2) NOT NULL,
    line_total              numeric(14,2) NOT NULL,
    -- Set only on a return-kind sale's items (D-61): the original sold
    -- line this row refunds, used to compute "already returned" per line.
    -- The database only enforces the FK (the referenced row must exist);
    -- that it belongs to the specific sale named by this row's own sale's
    -- original_sale_id (D-58) is enforced by the sales service, not a
    -- CHECK here.
    original_sale_item_id   uuid REFERENCES sale_items (id),
    created_at              timestamptz NOT NULL DEFAULT now(),
    CHECK (qty > 0),
    CHECK (unit_price >= 0),
    CHECK (unit_cost >= 0),
    CHECK (line_total >= 0)
);

CREATE INDEX sale_items_sale_id_idx ON sale_items (sale_id);
CREATE INDEX sale_items_shop_id_variant_id_idx ON sale_items (shop_id, variant_id);
CREATE INDEX sale_items_original_sale_item_id_idx ON sale_items (original_sale_item_id);

-- +goose StatementBegin
CREATE FUNCTION sale_items_no_update_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'sale_items is append-only: % is not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER sale_items_immutable
    BEFORE UPDATE OR DELETE ON sale_items
    FOR EACH ROW EXECUTE FUNCTION sale_items_no_update_delete();

-- One row per sale in MVP (D-54); the unique(sale_id) is the enforcement,
-- additive to relax later if splits are ever needed. Append-only, same
-- shape as sale_items above.
CREATE TABLE sale_payments (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    sale_id     uuid NOT NULL REFERENCES sales (id),
    method      payment_method NOT NULL,
    amount      numeric(14,2) NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (sale_id),
    CHECK (amount >= 0)
);

-- unique(sale_id) above already indexes sale_id; this covers a plain
-- shop_id filter (hard rule 1/10), matching sessions_shop_id_idx
-- (0002_users_sessions.sql) and stock_movements' shop-scoped index.
CREATE INDEX sale_payments_shop_id_idx ON sale_payments (shop_id);

-- +goose StatementBegin
CREATE FUNCTION sale_payments_no_update_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'sale_payments is append-only: % is not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER sale_payments_immutable
    BEFORE UPDATE OR DELETE ON sale_payments
    FOR EACH ROW EXECUTE FUNCTION sale_payments_no_update_delete();

-- +goose Down
DROP TRIGGER sale_payments_immutable ON sale_payments;
DROP FUNCTION sale_payments_no_update_delete();
DROP TABLE sale_payments;
DROP TRIGGER sale_items_immutable ON sale_items;
DROP FUNCTION sale_items_no_update_delete();
DROP TABLE sale_items;
DROP TRIGGER sales_immutable ON sales;
DROP FUNCTION sales_enforce_immutability();
DROP TABLE sales;
DROP TYPE payment_method;
DROP TYPE sale_status;
DROP TYPE sale_kind;
