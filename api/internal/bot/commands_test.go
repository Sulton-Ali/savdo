package bot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// noChatClient panics if Chat is ever called — every test below scripts
// exactly the ai.Fake results a static-command turn needs (none), so a
// stray free-text turn calling the model at all is a test bug worth
// failing loudly on.
func noChatClient() ai.Client { return ai.NewFake() }

func lastReply(t *testing.T, texts []string) string {
	t.Helper()
	if len(texts) == 0 {
		t.Fatalf("no reply was sent")
	}
	return texts[len(texts)-1]
}

func TestHandleUpdate_start_greetsWithShopName(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start"))

	got := lastReply(t, env.sender.allReplyTexts())
	if !strings.Contains(got, env.shop.Name) {
		t.Fatalf("greeting = %q, want it to contain shop name %q", got, env.shop.Name)
	}
}

func TestHandleUpdate_start_link_noLinkerConfigured(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start link_abc123"))

	got := lastReply(t, env.sender.allReplyTexts())
	// T5 has not wired a TelegramLinker yet (service.go's own doc
	// comment) — this must never look like a successful link.
	if strings.Contains(got, "muvaffaqiyatli") {
		t.Fatalf("reply = %q, want the 'not available yet' text, not a success message", got)
	}
}

// fakeLinker records every LinkTelegram call — Sonnet/Opus MAJOR 4's own
// tests need to see exactly which telegramUserID/telegramUsername
// handleStart passed it.
type fakeLinker struct {
	calls              int
	lastTelegramUserID int64
	lastUsername       string
	err                error
}

func (f *fakeLinker) LinkTelegram(_ context.Context, _ string, telegramUserID int64, telegramUsername string) error {
	f.calls++
	f.lastTelegramUserID = telegramUserID
	f.lastUsername = telegramUsername
	return f.err
}

// TestHandleUpdate_startLink_usesLiveUpdateUser_notStoredConversationUser
// pins Sonnet/Opus MAJOR 4: a bot_conversations row's own
// TelegramUserID is whoever's message first created it (userA below) —
// /start link_<code> from a *different* Telegram user (userB) in the
// same chat must redeem the code for userB, the update's own live
// message.from.id, never conv.TelegramUserID. Getting this wrong is an
// account-takeover bug the moment a real Linker (T5) is wired in: any
// second speaker in the same chat could link *their own* Telegram
// account to whatever code the first speaker's admin panel minted.
func TestHandleUpdate_startLink_usesLiveUpdateUser_notStoredConversationUser(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	linker := &fakeLinker{}
	env.rebuildServiceWithLinker(noChatClient(), linker)
	const chatID = int64(1)
	const userA, userB = int64(100), int64(200)

	// userA's own message creates the conversation — conv.TelegramUserID
	// is now userA's id.
	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, userA, "alice", "uz", "/hours"))

	// userB posts /start link_<code> in the *same* chat.
	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, userB, "bob", "uz", "/start link_abc123"))

	if linker.calls != 1 {
		t.Fatalf("LinkTelegram called %d times, want 1", linker.calls)
	}
	if linker.lastTelegramUserID != userB {
		t.Fatalf("LinkTelegram telegramUserID = %d, want the live update's userB (%d), not conv's stored userA (%d)", linker.lastTelegramUserID, userB, userA)
	}
	if linker.lastUsername != "bob" {
		t.Fatalf("LinkTelegram telegramUsername = %q, want %q", linker.lastUsername, "bob")
	}
}

// TestHandleUpdate_startLink_groupChatRefused pins the other half of
// MAJOR 4: /start link_<code> is refused outright in a non-private chat
// — a link code is a one-person credential, and "whoever sends /start
// link_<code> next" in a group is never guaranteed to be the Telegram
// account the owner meant to link.
func TestHandleUpdate_startLink_groupChatRefused(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	linker := &fakeLinker{}
	env.rebuildServiceWithLinker(noChatClient(), linker)

	env.svc.HandleUpdate(context.Background(), updateInChat(1, 100, "alice", "uz", "/start link_abc123", models.ChatTypeGroup))

	if linker.calls != 0 {
		t.Fatalf("LinkTelegram called %d times, want 0 (group chat must never redeem a link code)", linker.calls)
	}
	got := lastReply(t, env.sender.allReplyTexts())
	if strings.Contains(got, "muvaffaqiyatli") {
		t.Fatalf("reply = %q, want a refusal, not a success message", got)
	}
}

func TestHandleUpdate_unknownCommand(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/frobnicate"))

	got := lastReply(t, env.sender.allReplyTexts())
	if got == "" {
		t.Fatalf("want a non-empty unknown-command reply")
	}
}

func TestHandleUpdate_hours_unavailableThenConfigured(t *testing.T) {
	env := newTestEnv(t, noChatClient())

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/hours"))
	unavailable := lastReply(t, env.sender.allReplyTexts())

	env.putContent(t, gen.Hours, validHours())
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/hours"))
	configured := lastReply(t, env.sender.allReplyTexts())

	if unavailable == configured {
		t.Fatalf("hours reply did not change after content was configured")
	}
	if !strings.Contains(configured, "09:00") {
		t.Fatalf("configured hours reply = %q, want it to contain the opening time", configured)
	}
}

func TestHandleUpdate_address_unavailableThenConfigured(t *testing.T) {
	env := newTestEnv(t, noChatClient())

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/address"))
	unavailable := lastReply(t, env.sender.allReplyTexts())

	env.putContent(t, gen.Contacts, validContacts())
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/address"))
	configured := lastReply(t, env.sender.allReplyTexts())

	if unavailable == configured {
		t.Fatalf("address reply did not change after content was configured")
	}
	if !strings.Contains(configured, "Tashkent") {
		t.Fatalf("configured address reply = %q, want it to contain the seeded address", configured)
	}
}

func TestHandleUpdate_catalog_emptyThenListsCategories(t *testing.T) {
	env := newTestEnv(t, noChatClient())

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/catalog"))
	empty := lastReply(t, env.sender.allReplyTexts())

	ctx := context.Background()
	cat, err := env.q.CreateCategory(ctx, db.CreateCategoryParams{ID: uuid.New(), ShopID: env.shop.ID, Slug: "shoes", IsActive: true})
	if err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if err := env.q.UpsertCategoryTranslation(ctx, db.UpsertCategoryTranslationParams{CategoryID: cat.ID, Locale: "uz", Name: "Poyabzal"}); err != nil {
		t.Fatalf("seed category translation: %v", err)
	}

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/catalog"))
	withCategory := lastReply(t, env.sender.allReplyTexts())

	if empty == withCategory {
		t.Fatalf("catalog reply did not change after a category was added")
	}
	if !strings.Contains(withCategory, "Poyabzal") || !strings.Contains(withCategory, "https://savdo.test/uz/c/shoes") {
		t.Fatalf("catalog reply = %q, want the category name and its site link", withCategory)
	}
}

// TestHandleUpdate_nonTextUpdatesAreIgnored pins D-111's "customer mode
// only": a nil update, one with no Message, an empty chat ID or empty
// text must never send a reply.
func TestHandleUpdate_nonTextUpdatesAreIgnored(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	ctx := context.Background()

	env.svc.HandleUpdate(ctx, nil)
	env.svc.HandleUpdate(ctx, &models.Update{})
	env.svc.HandleUpdate(ctx, &models.Update{Message: &models.Message{Chat: models.Chat{ID: 0}, Text: "hi"}})
	env.svc.HandleUpdate(ctx, textUpdate(1, 100, "alice", "uz", "   "))

	if got := len(env.sender.allReplyTexts()); got != 0 {
		t.Fatalf("got %d replies, want 0 for updates with no usable text message", got)
	}
}
