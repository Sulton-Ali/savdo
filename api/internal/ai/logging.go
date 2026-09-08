package ai

import (
	"context"
	"log/slog"
)

// loggedClient wraps a Client so every call logs provider, model, tokens,
// latency and cost (ADR-009: "Every LLM call logs provider, model,
// tokens, latency and cost estimate") — never the prompt, a tool result
// or any secret (D-112, hard rule 9).
type loggedClient struct {
	inner  Client
	logger *slog.Logger
}

// Logged wraps client so every Chat call is logged at info level (error
// level when Chat itself returns an error). The wrapper reads only
// resp.Usage and resp.StopReason — never req.System, req.Messages or
// resp.Text/ToolCalls — so it can sit in front of any provider without
// risking a prompt or an answer ending up in the log.
func Logged(client Client, logger *slog.Logger) Client {
	return &loggedClient{inner: client, logger: logger}
}

func (l *loggedClient) Chat(ctx context.Context, req Request) (Response, error) {
	resp, err := l.inner.Chat(ctx, req)

	attrs := []any{
		"provider", resp.Usage.Provider,
		"model", resp.Usage.Model,
		"input_tokens", resp.Usage.InputTokens,
		"output_tokens", resp.Usage.OutputTokens,
		"latency_ms", resp.Usage.LatencyMs,
		"cost_estimate_usd", resp.Usage.CostEstimate,
		"stop_reason", resp.StopReason,
	}
	if err != nil {
		l.logger.ErrorContext(ctx, "ai chat", append(attrs, "error", err)...)
		return resp, err
	}
	l.logger.InfoContext(ctx, "ai chat", attrs...)
	return resp, nil
}
