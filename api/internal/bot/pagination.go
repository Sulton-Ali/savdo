package bot

import (
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

// defaultLimit/maxLimit mirror every other collection's own copy
// (crm.defaultLimit/maxLimit, catalog's, stock's — docs/05-API.md §
// Conventions: "limit is 1-200, default 50").
const (
	defaultLimit = 50
	maxLimit     = 200
)

func clampLimit(requested *gen.Limit) int32 {
	limit := defaultLimit
	if requested != nil {
		limit = *requested
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return int32(limit)
}

func decodeCursor(requested *gen.Cursor) (time.Time, uuid.UUID, error) {
	if requested == nil || *requested == "" {
		return time.Time{}, uuid.Nil, nil
	}
	return pagination.Decode(*requested)
}

func cursorPtr(t time.Time, id uuid.UUID) (*time.Time, *uuid.UUID) {
	if t.IsZero() {
		return nil, nil
	}
	return &t, &id
}

// paginateConversations trims rows (fetched with limit+1) to at most
// limit and reports the next page's cursor — ListBotConversations'
// keyset is COALESCE(last_message_at, created_at) DESC, id DESC
// (bot.sql's own doc comment), so the cursor is built from
// last_message_at when set, else created_at, matching that exact
// expression.
func paginateConversations(rows []db.BotConversation, limit int32) ([]db.BotConversation, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	activityAt := last.CreatedAt
	if last.LastMessageAt != nil {
		activityAt = *last.LastMessageAt
	}
	cursor := pagination.Encode(activityAt, last.ID)
	return items, &cursor
}

// paginateMessages trims rows (fetched with limit+1) to at most limit
// and reports the next page's cursor — ListBotConversationMessages is
// oldest-first, created_at ASC, id ASC (bot.sql's own doc comment).
func paginateMessages(rows []db.BotMessage, limit int32) ([]db.BotMessage, *string) {
	if len(rows) <= int(limit) {
		return rows, nil
	}
	items := rows[:limit]
	last := items[len(items)-1]
	cursor := pagination.Encode(last.CreatedAt, last.ID)
	return items, &cursor
}
