-- name: CreateBotConversation :one
-- O-26: one conversation per (shop, chat) — the bot's own handler calls
-- GetBotConversationByChat first and only reaches this on a miss; the
-- UNIQUE(shop_id, telegram_chat_id) constraint (0021_bot_conversations.sql)
-- is the actual guarantee, this insert is not itself an upsert.
-- telegram_username (0023_bot_conversations_telegram_username.sql) is
-- whatever the triggering update's message.from.username was, NULL when
-- Telegram gave none — nullable, never backfilled.
INSERT INTO bot_conversations (id, shop_id, telegram_chat_id, telegram_user_id, customer_id, mode, telegram_username)
VALUES (sqlc.arg('id'), sqlc.arg('shop_id'), sqlc.arg('telegram_chat_id'), sqlc.arg('telegram_user_id'), sqlc.narg('customer_id'), sqlc.arg('mode'), sqlc.narg('telegram_username'))
RETURNING *;

-- name: GetBotConversationByChat :one
SELECT * FROM bot_conversations
WHERE shop_id = $1 AND telegram_chat_id = $2;

-- name: GetBotConversation :one
SELECT * FROM bot_conversations
WHERE shop_id = $1 AND id = $2;

-- name: TouchBotConversation :one
-- Called once per turn the bot writes to bot_messages: bumps
-- message_count and moves last_message_at forward, so
-- ListBotConversations' "newest activity" ordering (below) reflects it
-- immediately. telegram_username (0023_bot_conversations_telegram_
-- username.sql) is refreshed opportunistically from the same update: the
-- caller passes it whenever it has one at hand (a user's turn, where
-- message.from.username is available) and NULL otherwise (an assistant
-- reply's own persist call, which has no Telegram update to read it
-- from) — COALESCE keeps the previously stored value in that case rather
-- than wiping it back to null.
UPDATE bot_conversations
SET last_message_at = sqlc.arg('last_message_at'), message_count = message_count + 1,
    telegram_username = COALESCE(sqlc.narg('telegram_username'), telegram_username)
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: ListBotConversations :many
-- Admin's GET /bot/conversations (§ 04-DATA-MODEL.md § 7: owner/manager
-- only). Newest-activity-first: a conversation's "activity" is
-- last_message_at once it has been touched, else its own created_at (a
-- conversation row can exist with zero messages for a moment between
-- CreateBotConversation and the first InsertBotMessage/TouchBotConversation
-- pair) — same COALESCE the index in 0021_bot_conversations.sql was built
-- for, kept sargable by matching its exact expression.
SELECT * FROM bot_conversations
WHERE shop_id = sqlc.arg('shop_id')
    AND (
        sqlc.narg('cursor_activity_at')::timestamptz IS NULL
        OR (COALESCE(last_message_at, created_at), id) < (sqlc.narg('cursor_activity_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY COALESCE(last_message_at, created_at) DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: InsertBotMessage :one
-- Append-only: bot_messages_immutable (0022_bot_messages.sql) rejects any
-- UPDATE on this table at the database level; DELETE stays available for
-- D-114's retention job, DeleteBotMessagesBefore below.
INSERT INTO bot_messages (
    id, conversation_id, shop_id, role, content, tool_calls,
    provider, model, input_tokens, output_tokens, latency_ms, cost_estimate
)
VALUES (
    sqlc.arg('id'), sqlc.arg('conversation_id'), sqlc.arg('shop_id'), sqlc.arg('role'), sqlc.arg('content'), sqlc.narg('tool_calls'),
    sqlc.narg('provider'), sqlc.narg('model'), sqlc.narg('input_tokens'), sqlc.narg('output_tokens'), sqlc.narg('latency_ms'), sqlc.narg('cost_estimate')
)
RETURNING *;

-- name: ListBotMessages :many
-- The admin conversation view (§ 04-DATA-MODEL.md § 7): oldest-first, the
-- order a human reads a transcript in. Cursor is exclusive-after, so a
-- page's last row's (created_at, id) is the next page's cursor.
SELECT * FROM bot_messages
WHERE shop_id = sqlc.arg('shop_id') AND conversation_id = sqlc.arg('conversation_id')
    AND (
        sqlc.narg('cursor_created_at')::timestamptz IS NULL
        OR (created_at, id) > (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid)
    )
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('limit');

-- name: ListRecentBotMessages :many
-- The bot's own prompt-window read: the last `limit` messages of a
-- conversation no older than `since`, newest first — the caller reverses
-- this slice into chronological order before handing it to
-- ai.Client.Chat as history. Bounding by `since` (not just LIMIT) keeps a
-- long-idle conversation's prompt from dragging in a stale exchange from
-- days ago.
SELECT * FROM bot_messages
WHERE shop_id = sqlc.arg('shop_id') AND conversation_id = sqlc.arg('conversation_id') AND created_at >= sqlc.arg('since')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: CountLLMMessagesSince :one
-- O-25's per-chat rolling-hour rate limit (ADR-009: 20 messages/hour):
-- counts only assistant replies that actually reached the LLM — a static
-- fallback answer (provider = 'static', ADR-009's "over budget, fall back
-- to static answers") does not consume a chat's hourly allowance, and a
-- plain 'user'/'tool' row was never itself an LLM call.
SELECT count(*) FROM bot_messages
WHERE shop_id = sqlc.arg('shop_id') AND conversation_id = sqlc.arg('conversation_id')
    AND created_at >= sqlc.arg('since')
    AND role = 'assistant'
    AND provider IS NOT NULL AND provider <> 'static';

-- name: SumBotTokensSince :one
-- O-25's per-shop daily token budget: input+output summed across every
-- conversation in the shop since `since`. The caller computes `since` as
-- the start of the current Asia/Tashkent day (O-25: the day boundary is
-- computed by the caller, not by this query) and passes it as a plain
-- timestamptz.
SELECT COALESCE(SUM(COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0)), 0)::bigint AS total_tokens
FROM bot_messages
WHERE shop_id = sqlc.arg('shop_id') AND created_at >= sqlc.arg('since');

-- name: DeleteBotMessagesBefore :execrows
-- D-114's retention job: delete this shop's messages older than 365 days.
-- Scoped by shop_id (hard rule 1) even though a scheduled job would
-- naturally run per shop anyway — a bare "DELETE ... WHERE created_at <
-- $1" would delete every shop's old messages in one call, which is not
-- what a single-shop-scoped caller expects.
DELETE FROM bot_messages
WHERE shop_id = sqlc.arg('shop_id') AND created_at < sqlc.arg('before');
