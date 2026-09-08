-- +goose Up
-- bot_conversations (§ 04-DATA-MODEL.md § 6, ADR-009, O-26): one row per
-- Telegram chat the bot has talked to, in this shop. mode is a real enum
-- with a single value today ('customer') — Phase 7 ships customer mode
-- only (D-111); adding 'staff' later (post-MVP) is additive
-- (ALTER TYPE ... ADD VALUE), not a new migration that touches this one.
CREATE TYPE bot_mode AS ENUM ('customer');

CREATE TABLE bot_conversations (
    id                 uuid PRIMARY KEY,
    shop_id            uuid NOT NULL REFERENCES shops (id),
    telegram_chat_id   bigint NOT NULL,
    telegram_user_id   bigint NOT NULL,
    -- Resolved lazily by the bot once a chat's phone/identity matches an
    -- existing customer (out of this migration's scope); most
    -- conversations never get one, so no NOT NULL.
    customer_id        uuid REFERENCES customers (id),
    mode               bot_mode NOT NULL DEFAULT 'customer',
    message_count      integer NOT NULL DEFAULT 0,
    -- Null until the first message is written (TouchBotConversation sets
    -- it); ListBotConversations' "newest-activity-first" order falls back
    -- to created_at for a conversation row created but not yet touched,
    -- see the index below.
    last_message_at    timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    -- O-26: exactly one conversation per (shop, chat) — GetBotConversationByChat
    -- and the bot's own upsert-on-first-message flow rely on this.
    UNIQUE (shop_id, telegram_chat_id)
);

-- index every FK (top-of-file convention).
CREATE INDEX bot_conversations_customer_id_idx ON bot_conversations (customer_id);

-- ListBotConversations' keyset cursor: newest activity first, where
-- "activity" is last_message_at if the conversation has ever been
-- touched, else its own created_at. An expression index (unusual in this
-- schema so far, but the only way to make that COALESCE sargable) so the
-- admin's conversation list does not have to scan every row for the shop.
CREATE INDEX bot_conversations_shop_activity_idx
    ON bot_conversations (shop_id, (COALESCE(last_message_at, created_at)) DESC, id DESC);

-- +goose Down
DROP TABLE bot_conversations;
DROP TYPE bot_mode;
