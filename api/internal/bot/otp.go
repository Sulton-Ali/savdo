package bot

import (
	"context"
	"fmt"
)

// SendOTP implements auth.OTPSender (internal/auth/otp.go's own
// interface, satisfied structurally the same way *auth.Service satisfies
// bot.TelegramLinker — this package never imports internal/auth just to
// name it) — MAJOR 2. RequestOtp (internal/auth) calls this once it has
// resolved username to a linked Telegram account; telegramUserID doubles
// as the chat id here because a Telegram user's own private chat with
// the bot always has that same id (the same reasoning D-116's product-
// photo `sendPhoto` doesn't need — this is the one place in the package
// that sends to an id that did not come from an inbound update's own
// msg.Chat.ID). Never logs the code itself (hard rule 9) — nothing in
// this method logs at all; RequestOtp's own caller already logs a
// delivery failure by class only ("reason": "send_error", otp.go's own
// doc comment on the goroutine that calls this).
func (s *Service) SendOTP(ctx context.Context, telegramUserID int64, code, locale string) error {
	text := fmt.Sprintf(localeTexts(locale).otpMessage, code)
	return s.sender.SendMessage(ctx, telegramUserID, text)
}
