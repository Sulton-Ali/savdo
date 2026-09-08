//go:build live

// Package ai's live test hits the real Anthropic API. It never runs under
// `make verify` or a plain `go test ./...` — only `go test -tags live
// ./internal/ai/...`, and even then it skips unless ANTHROPIC_API_KEY is
// set (never runs in CI, never spends money by accident).
package ai_test

import (
	"context"
	"os"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
)

func TestAnthropicLive_Chat(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live Anthropic call")
	}

	client, err := ai.New(ai.Config{
		Provider:           "anthropic",
		Model:              "claude-sonnet-5",
		PriceInputPerMTok:  "2.00",
		PriceOutputPerMTok: "10.00",
	})
	if err != nil {
		t.Fatalf("ai.New: %v", err)
	}

	resp, err := client.Chat(context.Background(), ai.Request{
		System:   "Reply with exactly the single word: OK",
		Messages: []ai.Message{{Role: ai.RoleUser, Text: "Ping."}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Text == "" {
		t.Fatal("Chat: empty Text in a live response")
	}
	if resp.Usage.Provider != "anthropic" || resp.Usage.Model != "claude-sonnet-5" {
		t.Fatalf("Usage = %+v, want provider=anthropic model=claude-sonnet-5", resp.Usage)
	}
	if resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
		t.Fatalf("Usage = %+v, want non-zero token counts", resp.Usage)
	}
	t.Logf("live response: text=%q usage=%+v", resp.Text, resp.Usage)
}
