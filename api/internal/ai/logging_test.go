package ai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLogged_logsUsageOnSuccess(t *testing.T) {
	fake := NewFake(FakeResult{Response: Response{
		Text:       "hi there",
		StopReason: "end_turn",
		Usage: Usage{
			Provider: "anthropic", Model: "claude-sonnet-5",
			InputTokens: 42, OutputTokens: 7, LatencyMs: 123,
			CostEstimate: "0.000450",
		},
	}})

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	client := Logged(fake, logger)

	req := Request{System: "top secret system prompt", Messages: []Message{{Role: RoleUser, Text: "a secret question"}}}
	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Text != "hi there" {
		t.Fatalf("Text = %q, want %q", resp.Text, "hi there")
	}

	logged := buf.String()
	for _, want := range []string{`"provider":"anthropic"`, `"model":"claude-sonnet-5"`, `"input_tokens":42`, `"output_tokens":7`, `"latency_ms":123`, `"cost_estimate_usd":"0.000450"`, `"stop_reason":"end_turn"`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log line = %s, want it to contain %s", logged, want)
		}
	}
	for _, secret := range []string{"top secret system prompt", "a secret question", "hi there"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log line = %s, must never contain prompt/response text %q", logged, secret)
		}
	}
}

func TestLogged_logsUsageOnError(t *testing.T) {
	wantErr := errors.New("ai: rate limited: too many requests")
	fake := NewFake(FakeResult{
		Response: Response{Usage: Usage{Provider: "anthropic", Model: "claude-sonnet-5", LatencyMs: 5, CostEstimate: "0.000000"}},
		Err:      wantErr,
	})

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	client := Logged(fake, logger)

	_, err := client.Chat(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Chat error = %v, want %v", err, wantErr)
	}

	logged := buf.String()
	if !strings.Contains(logged, `"level":"ERROR"`) {
		t.Fatalf("log line = %s, want an ERROR-level entry", logged)
	}
	if !strings.Contains(logged, `"provider":"anthropic"`) {
		t.Fatalf("log line = %s, want it to still carry provider on error", logged)
	}
}
