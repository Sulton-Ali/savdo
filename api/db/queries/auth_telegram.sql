-- name: GetTelegramAccountByTelegramUserID :one
-- The only lookup that runs before a shop_id is known: an incoming
-- Telegram update carries only the Telegram user's id, and this is how
-- the bot/auth flow resolves which savdo user (and shop) it belongs to —
-- same reasoning as GetSessionByTokenHash (sessions.sql). Every query
-- after this one in the same flow takes shop_id from the resolved
-- account.
SELECT * FROM telegram_accounts
WHERE telegram_user_id = $1;

-- name: GetTelegramAccountByUserID :one
SELECT * FROM telegram_accounts
WHERE shop_id = $1 AND user_id = $2;

-- name: LinkTelegramAccount :one
-- Upsert on user_id (UNIQUE, not the primary key since the review fix in
-- 0019_telegram_accounts.sql gave this table its own id like every other
-- table): a user re-linking a different Telegram account replaces the row
-- in place rather than erroring or leaving a second row behind. If
-- telegram_user_id already belongs to another user, the
-- UNIQUE(telegram_user_id) constraint rejects the insert with a distinct
-- error — one Telegram account must never link to two users; the service
-- surfaces that as its own error code, not handled here. id is
-- app-generated (sqlc.arg) and only used on the insert branch: ON
-- CONFLICT's UPDATE never touches it, so a re-link keeps the row's
-- original id.
INSERT INTO telegram_accounts (id, user_id, shop_id, telegram_user_id, telegram_username, linked_at)
VALUES (sqlc.arg('id'), sqlc.arg('user_id'), sqlc.arg('shop_id'), sqlc.arg('telegram_user_id'), sqlc.narg('telegram_username'), now())
ON CONFLICT (user_id) DO UPDATE
SET telegram_user_id = EXCLUDED.telegram_user_id,
    telegram_username = EXCLUDED.telegram_username,
    linked_at = now()
RETURNING *;

-- name: UnlinkTelegramAccount :execrows
DELETE FROM telegram_accounts
WHERE shop_id = $1 AND user_id = $2;
