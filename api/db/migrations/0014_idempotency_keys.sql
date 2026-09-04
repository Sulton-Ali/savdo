-- +goose Up
-- Backs the `Idempotency-Key` request header (docs/05-API.md § Conventions):
-- a replay with the same shop_id + key returns the stored response instead
-- of repeating the operation. No id/updated_at — the (shop_id, key) pair is
-- the natural key and a row is written once, after the operation completes.
CREATE TABLE idempotency_keys (
    shop_id          uuid NOT NULL REFERENCES shops (id),
    key              text NOT NULL,
    request_hash     text NOT NULL,
    response_status  integer NOT NULL,
    response_body    jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (shop_id, key)
);

-- +goose Down
DROP TABLE idempotency_keys;
