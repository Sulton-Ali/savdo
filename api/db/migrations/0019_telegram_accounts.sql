-- +goose Up
-- telegram_accounts (§ 04-DATA-MODEL.md § 1, ADR-005): binds one Telegram
-- user id to one `users` row, for the Telegram Login Widget (web) and the
-- bot deep-link (mobile) auth paths. id uuid PRIMARY KEY (v7,
-- app-generated), the same shape as every other table in this schema
-- (review correction: an earlier version of this migration made user_id
-- itself the primary key — the doc's "user_id unique" phrasing describes
-- a UNIQUE constraint, not a request to break the schema's own id
-- convention). user_id and telegram_user_id each carry their own UNIQUE
-- constraint instead, preserving the same 1:1 guarantees.
--
-- shop_id is not listed among telegram_accounts' own columns in the doc,
-- but the top-of-file convention ("shop_id uuid not null references
-- shops(id)" on every table unless stated) and hard rule 1 (every
-- business table filters by shop_id) both apply, and users already carry
-- shop_id — so this is denormalized from users.shop_id at link time, the
-- same choice sessions.shop_id (0002_users_sessions.sql) already made for
-- a table that is really "per user" but still needs its own shop_id to
-- satisfy ADR-004 without a join on every query.
CREATE TABLE telegram_accounts (
    id                 uuid PRIMARY KEY,
    user_id            uuid NOT NULL UNIQUE REFERENCES users (id),
    shop_id            uuid NOT NULL REFERENCES shops (id),
    telegram_user_id   bigint NOT NULL UNIQUE,
    telegram_username  text,
    linked_at          timestamptz NOT NULL DEFAULT now()
);

-- index every FK (top-of-file convention): user_id and telegram_user_id
-- are already indexed by their own UNIQUE constraints; shop_id is the one
-- FK left uncovered.
CREATE INDEX telegram_accounts_shop_id_idx ON telegram_accounts (shop_id);

-- +goose Down
DROP TABLE telegram_accounts;
