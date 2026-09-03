-- +goose Up
CREATE TYPE user_role AS ENUM ('owner', 'manager', 'cashier');
CREATE TYPE session_client AS ENUM ('web', 'mobile');

CREATE TABLE users (
    id             uuid PRIMARY KEY,
    shop_id        uuid NOT NULL REFERENCES shops (id),
    username       citext NOT NULL,
    password_hash  text NOT NULL,
    full_name      text NOT NULL,
    phone          text,
    role           user_role NOT NULL,
    locale         text NOT NULL DEFAULT 'uz',
    is_active      boolean NOT NULL DEFAULT true,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, username)
);

-- Partial unique index, not a table constraint: a plain UNIQUE(shop_id, phone)
-- would treat every NULL as distinct anyway, but we say it explicitly and this
-- is the form that generalizes to other "unique when present" columns.
CREATE UNIQUE INDEX users_shop_id_phone_key ON users (shop_id, phone) WHERE phone IS NOT NULL;

CREATE TABLE sessions (
    id             uuid PRIMARY KEY,
    shop_id        uuid NOT NULL REFERENCES shops (id),
    user_id        uuid NOT NULL REFERENCES users (id),
    token_hash     bytea NOT NULL UNIQUE,
    client         session_client NOT NULL,
    user_agent     text,
    ip             inet,
    expires_at     timestamptz NOT NULL,
    last_seen_at   timestamptz NOT NULL DEFAULT now(),
    revoked_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_shop_id_idx ON sessions (shop_id);
CREATE INDEX sessions_user_id_revoked_at_idx ON sessions (user_id, revoked_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
DROP TYPE session_client;
DROP TYPE user_role;
