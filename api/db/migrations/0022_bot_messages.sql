-- +goose Up
-- bot_messages (§ 04-DATA-MODEL.md § 6, ADR-009): the full transcript of
-- every bot conversation — one row per turn (user question, assistant
-- reply, or a tool call/result the model made along the way).
CREATE TYPE bot_message_role AS ENUM ('user', 'assistant', 'tool');

-- Append-only, same spirit as stock_movements (ADR-006) but NOT the same
-- enforcement: stock_movements' trigger blocks both UPDATE and DELETE
-- because nothing may ever remove a ledger entry. bot_messages allows
-- DELETE — D-114's retention job deletes rows older than 365 days — it
-- only forbids UPDATE (a message, once written, is never edited). The
-- trigger below is the UPDATE-only twin of
-- stock_movements_no_update_delete (0012_stock_ledger.sql).
CREATE TABLE bot_messages (
    id               uuid PRIMARY KEY,
    conversation_id  uuid NOT NULL REFERENCES bot_conversations (id),
    shop_id          uuid NOT NULL REFERENCES shops (id),
    role             bot_message_role NOT NULL,
    content          text NOT NULL,
    tool_calls       jsonb,
    provider         text,
    model            text,
    input_tokens     integer,
    output_tokens    integer,
    latency_ms       integer,
    -- O-28: estimated USD cost of this call, from provider+model pricing
    -- at call time; null for rows with no LLM call behind them (a plain
    -- user message, a static fallback answer with provider = 'static').
    cost_estimate    numeric(10,6),
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- ListBotMessages' own page (shop-scoped, by conversation) and
-- ListRecentBotMessages' prompt-window read (by conversation, since
-- clause) both lead on conversation_id; CountLLMMessagesSince (O-25's
-- per-chat rolling-hour rate limit) uses the same shape.
CREATE INDEX bot_messages_conversation_id_created_at_idx ON bot_messages (conversation_id, created_at);
-- SumBotTokensSince (O-25's per-shop daily token budget) and
-- DeleteBotMessagesBefore (D-114's retention) both scope by shop_id and a
-- created_at bound.
CREATE INDEX bot_messages_shop_id_created_at_idx ON bot_messages (shop_id, created_at);

-- No UPDATE is ever issued against a written bot_messages row — a
-- message, once stored, is never edited (unlike DELETE, which D-114's
-- retention job needs and this trigger deliberately does not block).
-- +goose StatementBegin
CREATE FUNCTION bot_messages_no_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'bot_messages is append-only: UPDATE is not allowed';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER bot_messages_immutable
    BEFORE UPDATE ON bot_messages
    FOR EACH ROW EXECUTE FUNCTION bot_messages_no_update();

-- +goose Down
DROP TRIGGER bot_messages_immutable ON bot_messages;
DROP FUNCTION bot_messages_no_update();
DROP TABLE bot_messages;
DROP TYPE bot_message_role;
