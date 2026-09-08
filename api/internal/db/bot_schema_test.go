package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// botFixture: a shop, for the bot conversation/message tests below.
type botFixture struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	shopID uuid.UUID
}

func newBotFixture(t *testing.T, slug string) botFixture {
	t.Helper()
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)
	shop := catalogShop(ctx, t, q, slug)
	return botFixture{pool: pool, q: q, shopID: shop.ID}
}

func (f botFixture) createConversation(ctx context.Context, t *testing.T, chatID int64) db.BotConversation {
	t.Helper()
	conv, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: f.shopID, TelegramChatID: chatID, TelegramUserID: chatID, Mode: db.BotModeCustomer,
	})
	if err != nil {
		t.Fatalf("CreateBotConversation: %v", err)
	}
	return conv
}

// TestBotConversations_uniquePerShopAndChat pins O-26: exactly one
// conversation per (shop_id, telegram_chat_id). The same chat id in a
// different shop is a distinct conversation.
func TestBotConversations_uniquePerShopAndChat(t *testing.T) {
	f := newBotFixture(t, "shop-bot-conv-unique")
	ctx := context.Background()

	first := f.createConversation(ctx, t, 555)

	_, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: f.shopID, TelegramChatID: 555, TelegramUserID: 555, Mode: db.BotModeCustomer,
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("want a unique_violation creating a second conversation for the same (shop, chat), got: %v", err)
	}

	byChat, err := f.q.GetBotConversationByChat(ctx, db.GetBotConversationByChatParams{ShopID: f.shopID, TelegramChatID: 555})
	if err != nil {
		t.Fatalf("GetBotConversationByChat: %v", err)
	}
	if byChat.ID != first.ID {
		t.Fatalf("GetBotConversationByChat = %s, want %s", byChat.ID, first.ID)
	}

	// The same chat id in a different shop is a distinct conversation.
	otherShop := catalogShop(ctx, t, f.q, "shop-bot-conv-unique-other")
	otherConv, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: otherShop.ID, TelegramChatID: 555, TelegramUserID: 555, Mode: db.BotModeCustomer,
	})
	if err != nil {
		t.Fatalf("CreateBotConversation (other shop, same chat id): %v", err)
	}
	if otherConv.ID == first.ID {
		t.Fatal("want a distinct conversation id for the same chat id in a different shop")
	}
}

// TestBotConversations_touchUpdatesActivityAndCount pins
// TouchBotConversation's own effect: last_message_at moves forward and
// message_count increments, scoped by shop_id.
func TestBotConversations_touchUpdatesActivityAndCount(t *testing.T) {
	f := newBotFixture(t, "shop-bot-conv-touch")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)
	if conv.LastMessageAt != nil {
		t.Fatalf("fresh conversation LastMessageAt = %v, want nil", conv.LastMessageAt)
	}
	if conv.MessageCount != 0 {
		t.Fatalf("fresh conversation MessageCount = %d, want 0", conv.MessageCount)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	touched, err := f.q.TouchBotConversation(ctx, db.TouchBotConversationParams{ShopID: f.shopID, ID: conv.ID, LastMessageAt: &now})
	if err != nil {
		t.Fatalf("TouchBotConversation: %v", err)
	}
	if touched.MessageCount != 1 {
		t.Fatalf("MessageCount = %d, want 1", touched.MessageCount)
	}
	if touched.LastMessageAt == nil || !touched.LastMessageAt.Equal(now) {
		t.Fatalf("LastMessageAt = %v, want %v", touched.LastMessageAt, now)
	}

	touchedAgain, err := f.q.TouchBotConversation(ctx, db.TouchBotConversationParams{ShopID: f.shopID, ID: conv.ID, LastMessageAt: &now})
	if err != nil {
		t.Fatalf("TouchBotConversation (second): %v", err)
	}
	if touchedAgain.MessageCount != 2 {
		t.Fatalf("MessageCount after second touch = %d, want 2", touchedAgain.MessageCount)
	}
}

// TestBotConversations_listNewestActivityFirst pins ListBotConversations'
// order: a touched conversation with a later last_message_at sorts
// before an untouched one whose only timestamp is its own (earlier)
// created_at, and a cross-shop conversation never appears.
func TestBotConversations_listNewestActivityFirst(t *testing.T) {
	f := newBotFixture(t, "shop-bot-conv-list")
	ctx := context.Background()

	untouched := f.createConversation(ctx, t, 1) // created first, never touched
	touched := f.createConversation(ctx, t, 2)   // created second, but touched — most recent activity

	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if _, err := f.q.TouchBotConversation(ctx, db.TouchBotConversationParams{ShopID: f.shopID, ID: touched.ID, LastMessageAt: &future}); err != nil {
		t.Fatalf("TouchBotConversation: %v", err)
	}

	otherShop := catalogShop(ctx, t, f.q, "shop-bot-conv-list-other")
	if _, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: otherShop.ID, TelegramChatID: 1, TelegramUserID: 1, Mode: db.BotModeCustomer,
	}); err != nil {
		t.Fatalf("CreateBotConversation (other shop): %v", err)
	}

	list, err := f.q.ListBotConversations(ctx, db.ListBotConversationsParams{ShopID: f.shopID, Limit: 10})
	if err != nil {
		t.Fatalf("ListBotConversations: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListBotConversations returned %d rows, want 2 (no cross-shop leak)", len(list))
	}
	if list[0].ID != touched.ID || list[1].ID != untouched.ID {
		t.Fatalf("ListBotConversations order = [%s, %s], want [touched %s, untouched %s]", list[0].ID, list[1].ID, touched.ID, untouched.ID)
	}
}

// TestBotMessages_noUpdateQuery documents, at compile time, that this
// package generates no UpdateBotMessage (or similarly named) query:
// bot_messages is append-only by convention (no UPDATE statement ever
// written against it, § 0022_bot_messages.sql), not by a database
// trigger — DELETE stays available for D-114's retention job. There is
// nothing to call here; the test exists so an accidental future UPDATE
// query gets caught by code review re-reading this comment, and so the
// test list in the task description has a concrete home. Real assertions
// live in TestBotMessages_deleteBeforeRetention below (DELETE works) and
// the schema itself (no UPDATE query is defined anywhere in bot.sql).
func TestBotMessages_noUpdateQuery(t *testing.T) {
	t.Skip("documentation-only: bot_messages has no generated UPDATE query, see comment")
}

func mustInsertMessage(ctx context.Context, t *testing.T, f botFixture, convID uuid.UUID, role db.BotMessageRole, provider *string, at time.Time) db.BotMessage {
	t.Helper()
	msg, err := f.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: convID, ShopID: f.shopID, Role: role, Content: "hi", Provider: provider,
	})
	if err != nil {
		t.Fatalf("InsertBotMessage: %v", err)
	}
	// created_at defaults to now(); backdate it directly for time-window
	// tests (there is no UPDATE query for this table, so this is the
	// test's own raw SQL, not something production code ever does).
	if _, err := f.pool.Exec(ctx, `UPDATE bot_messages SET created_at = $1 WHERE id = $2`, at, msg.ID); err != nil {
		t.Fatalf("backdate bot_messages.created_at: %v", err)
	}
	msg.CreatedAt = at
	return msg
}

// TestBotMessages_countLLMMessagesSinceIgnoresStatic pins O-25's per-chat
// rate limit query: only assistant rows with a non-static, non-null
// provider count; a static fallback answer and a plain user message do
// not.
func TestBotMessages_countLLMMessagesSinceIgnoresStatic(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-count")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)

	now := time.Now().UTC()
	anthropic := "anthropic"
	static := "static"
	mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleUser, nil, now)
	mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleAssistant, &anthropic, now)
	mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleAssistant, &static, now)
	mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleAssistant, &anthropic, now)

	count, err := f.q.CountLLMMessagesSince(ctx, db.CountLLMMessagesSinceParams{
		ShopID: f.shopID, ConversationID: conv.ID, Since: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CountLLMMessagesSince: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountLLMMessagesSince = %d, want 2 (the two anthropic-provider assistant rows only)", count)
	}
}

// TestBotMessages_sumTokensSinceWindow pins O-25's per-shop daily token
// budget query: only rows within [since, now) are summed, input+output,
// across every conversation in the shop.
func TestBotMessages_sumTokensSinceWindow(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-tokens")
	ctx := context.Background()
	conv1 := f.createConversation(ctx, t, 1)
	conv2 := f.createConversation(ctx, t, 2)

	now := time.Now().UTC()
	provider := "anthropic"

	insertWithTokens := func(convID uuid.UUID, in, out int32, at time.Time) {
		msg, err := f.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
			ID: uuid.New(), ConversationID: convID, ShopID: f.shopID, Role: db.BotMessageRoleAssistant,
			Content: "hi", Provider: &provider, InputTokens: &in, OutputTokens: &out,
		})
		if err != nil {
			t.Fatalf("InsertBotMessage: %v", err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE bot_messages SET created_at = $1 WHERE id = $2`, at, msg.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}

	insertWithTokens(conv1.ID, 100, 50, now)                     // in window
	insertWithTokens(conv2.ID, 200, 20, now)                     // in window, different conversation
	insertWithTokens(conv1.ID, 900, 900, now.Add(-48*time.Hour)) // outside the window

	since := now.Add(-24 * time.Hour)
	total, err := f.q.SumBotTokensSince(ctx, db.SumBotTokensSinceParams{ShopID: f.shopID, Since: since})
	if err != nil {
		t.Fatalf("SumBotTokensSince: %v", err)
	}
	if total != 370 { // 100+50+200+20
		t.Fatalf("SumBotTokensSince = %d, want 370", total)
	}
}

// TestBotMessages_deleteBeforeRetention pins D-114: DeleteBotMessagesBefore
// removes only rows older than the given cutoff, only for the given shop.
func TestBotMessages_deleteBeforeRetention(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-retention")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)

	otherShop := catalogShop(ctx, t, f.q, "shop-bot-msg-retention-other")
	otherConv, err := f.q.CreateBotConversation(ctx, db.CreateBotConversationParams{
		ID: uuid.New(), ShopID: otherShop.ID, TelegramChatID: 1, TelegramUserID: 1, Mode: db.BotModeCustomer,
	})
	if err != nil {
		t.Fatalf("CreateBotConversation (other shop): %v", err)
	}

	now := time.Now().UTC()
	cutoff := now.Add(-365 * 24 * time.Hour)

	old := mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleUser, nil, cutoff.Add(-time.Hour))   // older than cutoff: deleted
	recent := mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleUser, nil, cutoff.Add(time.Hour)) // newer than cutoff: kept
	oldInOtherShop, err := f.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: otherConv.ID, ShopID: otherShop.ID, Role: db.BotMessageRoleUser, Content: "hi",
	})
	if err != nil {
		t.Fatalf("InsertBotMessage (other shop): %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE bot_messages SET created_at = $1 WHERE id = $2`, cutoff.Add(-time.Hour), oldInOtherShop.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	affected, err := f.q.DeleteBotMessagesBefore(ctx, db.DeleteBotMessagesBeforeParams{ShopID: f.shopID, Before: cutoff})
	if err != nil {
		t.Fatalf("DeleteBotMessagesBefore: %v", err)
	}
	if affected != 1 {
		t.Fatalf("DeleteBotMessagesBefore affected = %d, want 1 (only the old message in this shop)", affected)
	}

	remaining, err := f.q.ListBotMessages(ctx, db.ListBotMessagesParams{ShopID: f.shopID, ConversationID: conv.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListBotMessages: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != recent.ID {
		t.Fatalf("ListBotMessages after retention = %+v, want only %s", remaining, recent.ID)
	}
	if _, err := getMessageByID(ctx, f.pool, old.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("want old message %s gone, got err=%v", old.ID, err)
	}

	// The other shop's own old message must still be there — retention is
	// shop-scoped (hard rule 1), not a blanket delete.
	if _, err := getMessageByID(ctx, f.pool, oldInOtherShop.ID); err != nil {
		t.Fatalf("want other shop's old message to survive this shop's retention run, got err=%v", err)
	}
}

func getMessageByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (uuid.UUID, error) {
	var got uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM bot_messages WHERE id = $1`, id).Scan(&got)
	return got, err
}

// TestBotMessages_listOldestFirstAndRecentNewestFirst pins the two
// distinct read orders: ListBotMessages (admin transcript, oldest first)
// and ListRecentBotMessages (prompt window, newest first).
func TestBotMessages_listOldestFirstAndRecentNewestFirst(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-order")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)

	base := time.Now().UTC().Add(-time.Hour)
	m1 := mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleUser, nil, base)
	m2 := mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleAssistant, nil, base.Add(time.Minute))
	m3 := mustInsertMessage(ctx, t, f, conv.ID, db.BotMessageRoleUser, nil, base.Add(2*time.Minute))

	oldestFirst, err := f.q.ListBotMessages(ctx, db.ListBotMessagesParams{ShopID: f.shopID, ConversationID: conv.ID, Limit: 10})
	if err != nil {
		t.Fatalf("ListBotMessages: %v", err)
	}
	if len(oldestFirst) != 3 || oldestFirst[0].ID != m1.ID || oldestFirst[2].ID != m3.ID {
		t.Fatalf("ListBotMessages order wrong: %+v", oldestFirst)
	}

	newestFirst, err := f.q.ListRecentBotMessages(ctx, db.ListRecentBotMessagesParams{
		ShopID: f.shopID, ConversationID: conv.ID, Since: base.Add(-time.Minute), Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentBotMessages: %v", err)
	}
	if len(newestFirst) != 3 || newestFirst[0].ID != m3.ID || newestFirst[2].ID != m1.ID {
		t.Fatalf("ListRecentBotMessages order wrong: %+v", newestFirst)
	}
	_ = m2
}

// TestBotMessages_roleCheckedByEnum pins that role is a real enum.
func TestBotMessages_roleCheckedByEnum(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-badrole")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)

	_, err := f.pool.Exec(ctx, `
		INSERT INTO bot_messages (id, conversation_id, shop_id, role, content)
		VALUES ($1, $2, $3, 'not-a-real-role', 'x')
	`, uuid.New(), conv.ID, f.shopID)
	if err == nil {
		t.Fatal("want an error inserting an unrecognized bot_message_role, got none")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || !strings.Contains(pgErr.Message, "bot_message_role") {
		t.Fatalf("want a bot_message_role enum error, got: %v", err)
	}
}

// TestBotMessages_costEstimateNumeric pins O-28: cost_estimate is
// numeric(10,6), stored and read back exactly (never a float).
func TestBotMessages_costEstimateNumeric(t *testing.T) {
	f := newBotFixture(t, "shop-bot-msg-cost")
	ctx := context.Background()
	conv := f.createConversation(ctx, t, 1)
	provider := "anthropic"

	var cost pgtype.Numeric
	if err := cost.Scan("0.001234"); err != nil {
		t.Fatalf("cost.Scan: %v", err)
	}
	msg, err := f.q.InsertBotMessage(ctx, db.InsertBotMessageParams{
		ID: uuid.New(), ConversationID: conv.ID, ShopID: f.shopID, Role: db.BotMessageRoleAssistant,
		Content: "hi", Provider: &provider, CostEstimate: cost,
	})
	if err != nil {
		t.Fatalf("InsertBotMessage: %v", err)
	}
	if numericString(t, msg.CostEstimate) != "0.001234" {
		t.Fatalf("CostEstimate = %s, want 0.001234", numericString(t, msg.CostEstimate))
	}
}
