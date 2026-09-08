package bot

import (
	"context"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

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
