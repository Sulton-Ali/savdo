package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/config"
)

// TestValidateBotWebhookConfig pins item 10's startup guard: a
// configured BOT_WEBHOOK_SECRET with no TELEGRAM_BOT_TOKEN must fail
// fast (run returns an error before ever building botSender), never
// start up into a webhook that can never send a reply.
func TestValidateBotWebhookConfig(t *testing.T) {
	tests := []struct {
		name          string
		webhookSecret string
		botToken      string
		wantErr       bool
	}{
		{name: "secret set, token empty: fails fast", webhookSecret: "s3cr3t", botToken: "", wantErr: true},
		{name: "secret set, token set: ok", webhookSecret: "s3cr3t", botToken: "123:abc", wantErr: false},
		{name: "secret unset, token empty: ok (webhook route 404s regardless)", webhookSecret: "", botToken: "", wantErr: false},
		{name: "secret unset, token set: ok", webhookSecret: "", botToken: "123:abc", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{BotWebhookSecret: tt.webhookSecret, TelegramBotToken: tt.botToken}
			err := validateBotWebhookConfig(cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBotWebhookConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestUnavailableAIClient_reportsProviderUnavailable pins MINOR 6:
// cmd/api's own ai.New-failure fallback must satisfy ai.Client and
// report ai.ErrProviderUnavailable — the error class internal/bot's own
// runFreeText already treats as a normal "answer statically" outcome
// (O-24), so a bad AI_PROVIDER value degrades the bot's free-text
// answers, never cmd/api's own startup.
func TestUnavailableAIClient_reportsProviderUnavailable(t *testing.T) {
	var client ai.Client = unavailableAIClient{}
	_, err := client.Chat(context.Background(), ai.Request{})
	if !errors.Is(err, ai.ErrProviderUnavailable) {
		t.Fatalf("Chat() error = %v, want ai.ErrProviderUnavailable", err)
	}
}
