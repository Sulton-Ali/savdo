package bot_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/bot"
)

// otpMessageUzFmt/RuFmt/EnFmt mirror texts.go's own otpMessage entries
// verbatim, one per locale (same convention as commands_test.go's
// startLink*TextUz consts).
const otpMessageUzFmt = "Savdo kodingiz: %s. 5 daqiqa amal qiladi. Agar buni siz so'ramagan bo'lsangiz, e'tiborsiz qoldiring."
const otpMessageRuFmt = "Ваш код Savdo: %s. Действителен 5 минут. Если вы не запрашивали его, проигнорируйте это сообщение."
const otpMessageEnFmt = "Your Savdo code: %s. Valid 5 minutes. If you did not request it, ignore this message."

// otpTTLMinutesForTest documents minor 7's own coupling: texts.go's own
// otpMessage hardcodes "5" (daqiqa/минут/minutes) in all three locales,
// matching internal/auth/otp.go's own otpTTL (5 * time.Minute) — otpTTL
// itself cannot be referenced here: it is unexported, and internal/auth
// is out of this task's own file scope (exporting it would be a change
// there; the coordinator's own instruction was to stop and report
// instead of making one). This constant, and the test below, are the
// closest coupling check reachable from this package; if either otpTTL
// or texts.go's own wording changes without the other, this constant
// has to be updated by hand and is the one place that would catch the
// drift.
const otpTTLMinutesForTest = 5

// TestSendOTP_deliversCodeInLocaleText pins MAJOR 2: SendOTP sends the
// code, formatted into the locale's own text, to chat id ==
// telegramUserID (a Telegram user's private chat with the bot always
// shares that id).
func TestSendOTP_deliversCodeInLocaleText(t *testing.T) {
	env := newTestEnv(t, noChatClient())
	const telegramUserID = int64(424242)
	const code = "913274"

	if err := env.svc.SendOTP(context.Background(), telegramUserID, code, "uz"); err != nil {
		t.Fatalf("SendOTP: %v", err)
	}

	msgs := env.sender.allReplyTexts()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	want := strings.Replace(otpMessageUzFmt, "%s", code, 1)
	if msgs[0] != want {
		t.Fatalf("message = %q, want %q", msgs[0], want)
	}
	if len(env.sender.Messages) != 1 || env.sender.Messages[0].ChatID != telegramUserID {
		t.Fatalf("Messages = %+v, want one message to chat id %d (== telegramUserID)", env.sender.Messages, telegramUserID)
	}
}

// TestSendOTP_mentionsAuthsOwnOTPValidityMinutes pins minor 7: every
// locale's own otpMessage text is exactly right (matching texts.go
// verbatim, not just "mentions 5 somewhere") and its minute figure
// matches otpTTLMinutesForTest's own documented value.
func TestSendOTP_mentionsAuthsOwnOTPValidityMinutes(t *testing.T) {
	tests := []struct {
		locale string
		want   string // %s format string, texts.go's own otpMessage
	}{
		{"uz", otpMessageUzFmt},
		{"ru", otpMessageRuFmt},
		{"en", otpMessageEnFmt},
	}
	for i, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			env := newTestEnv(t, noChatClient())
			const code = "246810"
			telegramUserID := int64(701 + i)
			if err := env.svc.SendOTP(context.Background(), telegramUserID, code, tt.locale); err != nil {
				t.Fatalf("SendOTP: %v", err)
			}
			got := lastReply(t, env.sender.allReplyTexts())
			want := strings.Replace(tt.want, "%s", code, 1)
			if got != want {
				t.Fatalf("otpMessage(%s) = %q, want %q", tt.locale, got, want)
			}
			wantMinutes := fmt.Sprintf("%d", otpTTLMinutesForTest)
			if !strings.Contains(got, wantMinutes) {
				t.Fatalf("otpMessage(%s) = %q, want it to mention %q minutes (documented to match internal/auth/otp.go's own otpTTL)", tt.locale, got, wantMinutes)
			}
		})
	}
}

// TestSendOTP_neverLogsTheCode pins MAJOR 2's own "never log the code"
// requirement (hard rule 9): capture every log line SendOTP's own call
// chain could produce (a real slog.Logger writing to a buffer, both the
// success path and a forced send failure via a broken Sender) and assert
// the code substring never appears in it.
func TestSendOTP_neverLogsTheCode(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	const code = "778899"

	env := newTestEnv(t, noChatClient())
	svc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, env.sender, nil, env.cfg, env.clock.now, logger)
	if err := svc.SendOTP(context.Background(), 555, code, "en"); err != nil {
		t.Fatalf("SendOTP: %v", err)
	}

	failSvc := bot.NewService(env.pool, env.q, noChatClient(), env.pub, env.content, brokenSender{}, nil, env.cfg, env.clock.now, logger)
	if err := failSvc.SendOTP(context.Background(), 556, code, "en"); err == nil {
		t.Fatalf("SendOTP over a broken sender: want an error")
	}

	if strings.Contains(logBuf.String(), code) {
		t.Fatalf("log output contains the OTP code: %s", logBuf.String())
	}
}

// brokenSender always fails — TestSendOTP_neverLogsTheCode's own "even on
// a delivery failure" case.
type brokenSender struct{}

func (brokenSender) SendMessage(context.Context, int64, string) error {
	return errBrokenSender
}
func (brokenSender) SendPhoto(context.Context, int64, string, string) error {
	return errBrokenSender
}

var errBrokenSender = errors.New("broken sender: simulated delivery failure")
