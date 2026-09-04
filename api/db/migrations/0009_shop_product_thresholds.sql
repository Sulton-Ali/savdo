-- +goose Up
-- Shop-wide default low-stock threshold (D-44).
ALTER TABLE shops
    ADD COLUMN low_stock_threshold integer NOT NULL DEFAULT 2;

-- Per-product override of the shop's default; NULL means "use the shop
-- default" (D-44). Nullable, unlike shops.low_stock_threshold.
ALTER TABLE products
    ADD COLUMN low_stock_threshold integer;

-- +goose Down
ALTER TABLE products DROP COLUMN low_stock_threshold;
ALTER TABLE shops DROP COLUMN low_stock_threshold;
