package bot

import (
	"context"
	"errors"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// errSenderNotConfigured is nilSender's own error -- never wraps a
// secret, never logs a token (hard rule 9): just states the fact.
var errSenderNotConfigured = errors.New("bot: sender not configured (TELEGRAM_BOT_TOKEN unset)")

// nilSender is what NewService installs when its sender argument is nil
// (cmd/api/main.go's own doc comment: an unconfigured TELEGRAM_BOT_TOKEN
// leaves botSender a nil bot.Sender) -- every call turns into
// errSenderNotConfigured instead of a nil-interface panic the moment a
// webhook update actually needs to reply (item 10's own guard;
// cmd/api/main.go's own startup check makes the one combination that
// would hit this in practice, BOT_WEBHOOK_SECRET set with no token, fail
// fast instead, but this is the last line of defense if that combination
// is ever reached anyway).
type nilSender struct{}

func (nilSender) SendMessage(context.Context, int64, string) error { return errSenderNotConfigured }
func (nilSender) SendPhoto(context.Context, int64, string, string) error {
	return errSenderNotConfigured
}
func (nilSender) SendTyping(context.Context, int64) error { return errSenderNotConfigured }

// TelegramSender adapts a real *telegram.Bot to the Sender interface
// HandleUpdate calls to reply — the only thing in this package that
// talks to the actual Telegram Bot API. cmd/bot and cmd/api's webhook
// wiring construct one around the same *telegram.Bot they use for
// polling/webhook registration; tests use a scripted fake instead
// (D-112).
type TelegramSender struct {
	Bot *telegram.Bot
}

// SendMessage implements Sender. No parse_mode is set — format.go's own
// doc comment explains why this package always sends plain text.
func (s TelegramSender) SendMessage(ctx context.Context, chatID int64, text string) error {
	_, err := s.Bot.SendMessage(ctx, &telegram.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

// SendPhoto implements Sender. photoURL must already be absolute
// (format.go's absoluteMediaURL) — Telegram fetches it itself rather
// than receiving bytes (models.InputFileString: a URL/file_id string,
// not multipart upload).
func (s TelegramSender) SendPhoto(ctx context.Context, chatID int64, photoURL, caption string) error {
	_, err := s.Bot.SendPhoto(ctx, &telegram.SendPhotoParams{
		ChatID: chatID, Photo: &models.InputFileString{Data: photoURL}, Caption: caption,
	})
	return err
}

// SendTyping implements Sender: Telegram's "typing…" chat action
// (O-30), shown while chat.go's runFreeText waits on a model call. A
// chat action is a fire-and-forget status, not a message — Telegram
// itself displays it for about 5 seconds (Bot API docs, sendChatAction),
// which is why the typing loop (chat.go's startTyping) re-sends it
// periodically rather than once.
func (s TelegramSender) SendTyping(ctx context.Context, chatID int64) error {
	_, err := s.Bot.SendChatAction(ctx, &telegram.SendChatActionParams{ChatID: chatID, Action: models.ChatActionTyping})
	return err
}
