-- +goose Up
-- next_purchase_number: same shape as shops.next_sale_number (0001_shops.sql)
-- — a per-shop, gap-free counter advanced only under row lock by
-- stock.Service.ReceivePurchase's NextPurchaseNumber query (D-45), never by
-- a settings update.
ALTER TABLE shops
    ADD COLUMN next_purchase_number bigint NOT NULL DEFAULT 1,
    ADD COLUMN low_stock_threshold integer NOT NULL DEFAULT 2;

-- Per-product override of the shop's default; NULL means "use the shop
-- default" (D-44). Nullable, unlike shops.low_stock_threshold.
ALTER TABLE products
    ADD COLUMN low_stock_threshold integer;

-- +goose Down
ALTER TABLE products DROP COLUMN low_stock_threshold;
ALTER TABLE shops DROP COLUMN low_stock_threshold;
ALTER TABLE shops DROP COLUMN next_purchase_number;
