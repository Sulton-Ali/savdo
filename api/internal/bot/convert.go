package bot

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/money"
)

func nullableUUID(v *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if v == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableTime(v *time.Time) nullable.Nullable[time.Time] {
	if v == nil {
		return nullable.NewNullNullable[time.Time]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

func nullableInt(v *int32) nullable.Nullable[int] {
	if v == nil {
		return nullable.NewNullNullable[int]()
	}
	return nullable.NewNullableWithValue(int(*v))
}

// toGenBotConversation converts one db.BotConversation row into the
// contract's shape. TelegramUsername is always null: 0021_bot_
// conversations.sql (T1) stores no such column, even though the contract
// (T3) and docs/05-API.md § Bot both describe it as "if known" — this
// package cannot add the column (out of its own file scope, and never
// edits a merged migration), so it reports the field as never known, the
// literal reading "if known" allows. Flagged in T4's own final report as
// a follow-up: a migration adding bot_conversations.telegram_username,
// populated from the Telegram update's `From.Username` alongside
// telegram_user_id, would let this stop being permanently null.
func toGenBotConversation(row db.BotConversation) gen.BotConversation {
	return gen.BotConversation{
		Id: row.ID, CreatedAt: row.CreatedAt, MessageCount: int(row.MessageCount),
		Mode: gen.BotConversationMode(row.Mode), CustomerId: nullableUUID(row.CustomerID),
		LastMessageAt:    nullableTime(row.LastMessageAt),
		TelegramChatId:   formatInt64(row.TelegramChatID),
		TelegramUsername: nullable.NewNullNullable[string](),
	}
}

func formatInt64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func toGenBotMessage(row db.BotMessage) (gen.BotMessage, error) {
	msg := gen.BotMessage{
		Id: row.ID, CreatedAt: row.CreatedAt, Content: row.Content, Role: gen.BotMessageRole(row.Role),
		Provider: nullableString(row.Provider), Model: nullableString(row.Model),
		InputTokens: nullableInt(row.InputTokens), OutputTokens: nullableInt(row.OutputTokens),
		LatencyMs: nullableInt(row.LatencyMs), ToolCalls: nullable.NewNullNullable[map[string]interface{}](),
	}
	if row.CostEstimate.Valid {
		d, err := money.FromNumeric(row.CostEstimate)
		if err != nil {
			return gen.BotMessage{}, err
		}
		msg.CostEstimate = nullable.NewNullableWithValue(money.String(d))
	} else {
		msg.CostEstimate = nullable.NewNullNullable[string]()
	}
	if len(row.ToolCalls) > 0 {
		// chat.go persists tool_calls as {"calls": [...toolCallRecord]} —
		// an object, matching the contract's generic
		// `map[string]interface{}` (gen.BotMessage.ToolCalls, OpenAPI's
		// untyped-object escape hatch, the same one gen.ContentBlock.Data
		// uses) rather than a bare JSON array.
		var v map[string]interface{}
		if err := json.Unmarshal(row.ToolCalls, &v); err == nil {
			msg.ToolCalls = nullable.NewNullableWithValue(v)
		}
	}
	return msg, nil
}
