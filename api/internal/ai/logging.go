package ai

import (
	"context"
	"errors"
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
		// Never resp err.Error() directly — an inner Client's error may
		// embed a provider's own free-text message (hard rule 9, D-112).
		// Only the errors.Is classification, plus the HTTP status when the
		// error carries one (statusError, built by mapAnthropicError and
		// mapOpenAICompatError), is safe to log.
		errAttrs := append(attrs, "error_class", errorClass(err))
		var se *statusError
		if errors.As(err, &se) {
			errAttrs = append(errAttrs, "http_status", se.HTTPStatus())
		}
		l.logger.ErrorContext(ctx, "ai chat", errAttrs...)
		return resp, err
	}
	l.logger.InfoContext(ctx, "ai chat", attrs...)
	return resp, nil
}
