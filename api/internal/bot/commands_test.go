package bot_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// startLinkAlreadyLinkedTextUz/startLinkRateLimitedTextUz mirror
// texts.go's own uz map entries verbatim — an external _test package
// cannot call the unexported localeTexts these replies are built from
// (limits_test.go's own rateLimitedTextUz/fallbackTextUz do the same).
const startLinkAlreadyLinkedTextUz = "Bu Telegram hisobi allaqachon boshqa foydalanuvchiga bog'langan."
const startLinkRateLimitedTextUz = "Urinishlar soni juda ko'p. Iltimos, birozdan so'ng qayta urining."

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

// fakeLinker records every CompleteLink call — Sonnet/Opus MAJOR 4's own
// tests need to see exactly which telegramUserID/telegramUsername
// handleStart passed it.
type fakeLinker struct {
	calls              int
	lastCode           string
	lastTelegramUserID int64
	lastUsername       string
	err                error
}

func (f *fakeLinker) CompleteLink(_ context.Context, code string, telegramUserID int64, telegramUsername string) error {
	f.calls++
	f.lastCode = code
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
		t.Fatalf("CompleteLink called %d times, want 1", linker.calls)
	}
	if linker.lastTelegramUserID != userB {
		t.Fatalf("CompleteLink telegramUserID = %d, want the live update's userB (%d), not conv's stored userA (%d)", linker.lastTelegramUserID, userB, userA)
	}
	if linker.lastUsername != "bob" {
		t.Fatalf("CompleteLink telegramUsername = %q, want %q", linker.lastUsername, "bob")
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
		t.Fatalf("CompleteLink called %d times, want 0 (group chat must never redeem a link code)", linker.calls)
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

// TestHandleUpdate_startLink_alreadyLinkedText pins M1's error-class
// mapping: auth.ErrTelegramAlreadyLinked gets its own text, distinct
// from the generic startLinkFailed — retrying the same code can never
// help when the problem is the Telegram account, not the code
// (internal/auth/telegram.go's own doc comment on CompleteLink).
func TestHandleUpdate_startLink_alreadyLinkedText(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	linker := &fakeLinker{err: auth.ErrTelegramAlreadyLinked}
	env.rebuildServiceWithLinker(noChatClient(), linker)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start link_abc123"))

	got := lastReply(t, env.sender.allReplyTexts())
	if got != startLinkAlreadyLinkedTextUz {
		t.Fatalf("reply = %q, want %q", got, startLinkAlreadyLinkedTextUz)
	}
}

// TestHandleUpdate_startLink_rateLimitedText pins the other mapped
// class: a *apierr.Error with Code RATELIMITED (CompleteLink's shared
// D-118 budget) gets its own "too many attempts" text, distinct from
// "invalid code".
func TestHandleUpdate_startLink_rateLimitedText(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	linker := &fakeLinker{err: apierr.RateLimited(30)}
	env.rebuildServiceWithLinker(noChatClient(), linker)

	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start link_abc123"))

	got := lastReply(t, env.sender.allReplyTexts())
	if got != startLinkRateLimitedTextUz {
		t.Fatalf("reply = %q, want %q", got, startLinkRateLimitedTextUz)
	}
}

// TestHandleUpdate_startLink_completesRealLink is M1's own integration
// test: a real auth.Service (not a fake) mints a link code
// (CreateTelegramLink, the same call POST /auth/telegram/link makes),
// the bot redeems it via a real /start link_<code> update, and
// GetTelegramLink confirms the account is actually linked afterward —
// proving the bot.TelegramLinker/auth.TelegramLinker rename actually
// wires *auth.Service in as a working linker, not just that the two
// interfaces happen to compile against each other.
func TestHandleUpdate_startLink_completesRealLink(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	authSvc := auth.NewService(env.pool, env.q, config.Config{LoginRateIPPerMin: 1000, LoginRateUserPerMin: 1000}, env.shop.ID)
	env.rebuildServiceWithLinker(noChatClient(), authSvc)

	link, err := authSvc.CreateTelegramLink(context.Background(), env.shop.ID, env.owner.ID)
	if err != nil {
		t.Fatalf("CreateTelegramLink: %v", err)
	}

	const telegramUserID = int64(555000)
	env.svc.HandleUpdate(context.Background(), textUpdate(1, telegramUserID, "owner_tg", "uz", "/start link_"+link.Code))

	got := lastReply(t, env.sender.allReplyTexts())
	if strings.Contains(got, startLinkAlreadyLinkedTextUz) || got == "" {
		t.Fatalf("reply = %q, want a success reply", got)
	}
	linked, username, err := authSvc.GetTelegramLink(context.Background(), env.shop.ID, env.owner.ID)
	if err != nil {
		t.Fatalf("GetTelegramLink: %v", err)
	}
	if !linked {
		t.Fatalf("GetTelegramLink linked = false, want true after redeeming a real code")
	}
	if username == nil || *username != "owner_tg" {
		t.Fatalf("GetTelegramLink username = %v, want \"owner_tg\"", username)
	}
}

// TestHandleUpdate_startLink_redactsCodeBeforePersisting pins MAJOR 3:
// an un-redeemed (or even just-redeemed) link code is a bearer
// credential for whoever's account minted it — a manager reading the
// admin transcript (GET /bot/conversations/{id}/messages) must never see
// it, even though the /start command itself stays visible.
func TestHandleUpdate_startLink_redactsCodeBeforePersisting(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	linker := &fakeLinker{}
	env.rebuildServiceWithLinker(noChatClient(), linker)
	const chatID = int64(1)
	const code = "VERYSECRETCODE234567" // base32 shape (A-Z, 2-7) — the alphabet a real link code uses

	env.svc.HandleUpdate(context.Background(), textUpdate(chatID, 100, "alice", "uz", "/start link_"+code))

	transcript := env.messageTranscript(t, chatID)
	if strings.Contains(transcript, code) {
		t.Fatalf("transcript contains the raw link code: %q", transcript)
	}
	if !strings.Contains(transcript, "/start link_***") {
		t.Fatalf("transcript = %q, want the redacted \"/start link_***\" command still visible", transcript)
	}
	// The linker itself must still have received the real, unredacted
	// code — only the persisted transcript is redacted.
	if linker.calls != 1 {
		t.Fatalf("CompleteLink called %d times, want 1", linker.calls)
	}
	if linker.lastCode != code {
		t.Fatalf("CompleteLink code = %q, want the real unredacted code %q", linker.lastCode, code)
	}
}

// TestHandleUpdate_startLink_redactsEveryFormOfTheCode is the MAJOR
// (round 4) regression table: redactLinkCode must catch a link code
// wherever it appears in a message, not just inside an exact "/start
// link_<code>" command — a code pasted as plain text, the full deep-link
// URL pasted instead of tapped, and a case/whitespace/@suffix variant of
// the command itself that splitCommand now also has to actually
// recognize (so the link still gets *redeemed*, not just silently
// dropped as an unknown command with its own code un-redacted). Every
// row uses a base32-shaped fake code (A-Z, 2-7 — internal/auth/otp.go's
// own selectorEncoding alphabet) since that is exactly what
// redactLinkCode's own regex matches.
func TestHandleUpdate_startLink_redactsEveryFormOfTheCode(t *testing.T) {
	const code = "ABCDEFGH234567IJKLMNOP"
	tests := []struct {
		name      string
		text      string
		isCommand bool // true: must reach CompleteLink and never the LLM; false: must reach the LLM and never CompleteLink
	}{
		{"code pasted as plain text, no command at all", "here is my code: link_" + code, false},
		{"the full deep-link URL pasted instead of tapped", "https://t.me/savdo_bot?start=link_" + code, false},
		{"uppercase command", "/START link_" + code, true},
		{"tab between command and payload", "/start\tlink_" + code, true},
		{"newline between command and payload", "/start\nlink_" + code, true},
		{"@botusername suffix (Telegram's own group-chat form)", "/start@savdo_bot link_" + code, true},
		{"the canonical form itself still redeems", "/start link_" + code, true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const chatID = int64(1)
			var env *testEnv
			var linker *fakeLinker
			var fake *ai.Fake
			if tt.isCommand {
				env = newTestEnv(t, noChatClient()) // panics if Chat is ever called
				linker = &fakeLinker{}
				env.rebuildServiceWithLinker(noChatClient(), linker)
			} else {
				fake = ai.NewFake(scriptedAnswer("Sure, I can help with that."))
				env = newTestEnv(t, fake)
			}

			env.svc.HandleUpdate(context.Background(), textUpdate(chatID+int64(i), 100+int64(i), "alice", "uz", tt.text))

			transcript := env.messageTranscript(t, chatID+int64(i))
			if strings.Contains(transcript, code) {
				t.Fatalf("transcript contains the raw link code: %q", transcript)
			}

			if tt.isCommand {
				if linker.calls != 1 {
					t.Fatalf("CompleteLink called %d times, want 1 (the command must still be recognized and redeemed)", linker.calls)
				}
				if linker.lastCode != code {
					t.Fatalf("CompleteLink code = %q, want the real unredacted code %q", linker.lastCode, code)
				}
			} else {
				if len(fake.Requests) != 1 {
					t.Fatalf("Chat called %d times, want 1", len(fake.Requests))
				}
				for _, msg := range fake.Requests[0].Messages {
					if strings.Contains(msg.Text, code) {
						t.Fatalf("the model's own prompt contains the raw link code: %q", msg.Text)
					}
				}
			}
		})
	}
}
