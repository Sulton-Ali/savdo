-- +goose Up
-- bot_conversations.telegram_username (§ 04-DATA-MODEL.md § 6, Phase 7 T4
-- follow-up): the contract (T3, docs/05-API.md) and the admin conversation
-- list have described this field as "if known" since Phase 7's first
-- migration (0021_bot_conversations.sql), but that migration never added
-- the column, so the bot layer hard-coded it to null (api/internal/bot/
-- convert.go's own doc comment). Nullable: Telegram gives no username for
-- every chat (a user with no @handle set), and this column is only ever
-- populated from a live update's `message.from.username`, never
-- backfilled for rows written before this migration.
ALTER TABLE bot_conversations ADD COLUMN telegram_username text;

-- +goose Down
ALTER TABLE bot_conversations DROP COLUMN telegram_username;
