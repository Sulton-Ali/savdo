-- +goose Up
-- citext is needed by 0002_users_sessions.sql (users.username); created here so
-- the extension exists before any table that depends on it.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE shops (
    id                       uuid PRIMARY KEY,
    slug                     text NOT NULL UNIQUE,
    name                     text NOT NULL,
    currency                 char(3) NOT NULL DEFAULT 'UZS',
    timezone                 text NOT NULL DEFAULT 'Asia/Tashkent',
    default_locale           text NOT NULL DEFAULT 'uz',
    allow_negative_stock     boolean NOT NULL DEFAULT false,
    update_cost_on_purchase  boolean NOT NULL DEFAULT true,
    next_sale_number         bigint NOT NULL DEFAULT 1,
    ai_daily_token_budget    integer,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE shops;
-- citext is left installed: dropping an extension used by later tables would
-- break their columns; extensions are not per-migration state to unwind.
