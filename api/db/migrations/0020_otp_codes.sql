-- +goose Up
-- otp_codes (§ 04-DATA-MODEL.md § 1, ADR-005): one-time codes delivered by
-- the bot to a user's linked Telegram account. purpose is a real enum
-- (top-of-file convention), not free text: password_reset (Q-11's admin
-- reset stays available separately before the bot exists), link_telegram
-- (proving control of a Telegram account before telegram_accounts gets a
-- row), confirm_action (sensitive in-app confirmations, e.g. role change).
CREATE TYPE otp_purpose AS ENUM ('password_reset', 'link_telegram', 'confirm_action');

CREATE TABLE otp_codes (
    id          uuid PRIMARY KEY,
    -- Denormalized from users.shop_id, same reasoning as
    -- telegram_accounts.shop_id (0019_telegram_accounts.sql) and
    -- sessions.shop_id (0002_users_sessions.sql): otp_codes is a "per
    -- user" table but still needs its own shop_id so every query can
    -- filter by it directly (hard rule 1) without a join to users.
    shop_id     uuid NOT NULL REFERENCES shops (id),
    user_id     uuid NOT NULL REFERENCES users (id),
    purpose     otp_purpose NOT NULL,
    -- Hashed, never the plain 6-digit code (hard rule 9) — same bytea
    -- shape as sessions.token_hash.
    code_hash   bytea NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    attempts    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- GetActiveOTPCode's own lookup: newest unused, unexpired code for a
-- (user, purpose) pair. Leading on user_id (not shop_id) because every
-- caller of this table already has a user_id in hand (the OTP flow starts
-- from an authenticated or half-authenticated user, never a shop-wide
-- listing) — shop_id itself only needs its own FK index below.
CREATE INDEX otp_codes_user_id_purpose_expires_at_idx ON otp_codes (user_id, purpose, expires_at);
CREATE INDEX otp_codes_shop_id_idx ON otp_codes (shop_id);

-- +goose Down
DROP TABLE otp_codes;
DROP TYPE otp_purpose;
