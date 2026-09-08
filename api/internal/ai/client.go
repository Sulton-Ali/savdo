// Package ai is the provider-agnostic LLM adapter ADR-009 describes: a
// Client the bot calls with a system prompt, message history, tool
// definitions and tool results, getting back text and/or tool calls plus
// usage and a cost estimate. Providers live behind the same Client
// interface (anthropic.go, openai_compat.go); gemini is a registered name
// that returns a typed "not built" error (O-29). Nothing in this package
// ever logs a prompt, a tool result or a secret — only provider, model,
// token counts, latency and cost (logging.go, D-112, hard rule 9).
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role identifies whose turn a Message represents.
type Role string

const (
	// RoleUser is a message from the end user (or, for the bot's own use,
	// the grounding context assembled around their question).
	RoleUser Role = "user"
	// RoleAssistant is a prior model turn being replayed as history — its
	// Text and/or ToolCalls are exactly what a Response once returned.
	RoleAssistant Role = "assistant"
	// RoleTool carries the caller's ToolResults for tool calls the model
	// asked for on the previous assistant turn.
	RoleTool Role = "tool"
)

// Message is one turn of a Request's conversation history.
type Message struct {
	Role Role
	// Text is the message's plain-text content. Set for RoleUser and,
	// optionally alongside ToolCalls, for RoleAssistant.
	Text string
	// ToolCalls is set on a RoleAssistant message that asked the model's
	// caller to run one or more tools.
	ToolCalls []ToolCall
	// ToolResults is set on a RoleTool message: the caller's answer to
	// every ToolCall from the immediately preceding assistant turn, all
	// in the one message (ADR-009: "all results of one turn in one
	// message").
	ToolResults []ToolResult
}

// Tool is a function definition the model may call, described as a JSON
// Schema object (ADR-009: "tool definitions").
type Tool struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object (`{"type":"object","properties":
	// {...},"required":[...]}`) describing the tool's input shape.
	InputSchema json.RawMessage
}

// ToolCall is one invocation the model asked for.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the caller's answer to one ToolCall.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Request is one call to Client.Chat.
type Request struct {
	System   string
	Messages []Message
	Tools    []Tool
	// MaxTokens caps the model's generated output. Zero means "use the
	// provider's configured default" (O-26: 1024 per request unless the
	// caller overrides it here).
	MaxTokens int
}

// Response is what the model returned.
type Response struct {
	Text      string
	ToolCalls []ToolCall
	// StopReason is the provider's stop reason, normalized to the
	// Anthropic vocabulary ADR-009 names: "end_turn", "tool_use",
	// "max_tokens", "refusal", etc. — the openai_compat provider maps its
	// own finish_reason values onto the same strings where they exist, and
	// passes the raw value through otherwise.
	StopReason string
	Usage      Usage
}

// Usage is what ADR-009 requires every call to log: provider, model,
// tokens, latency and a cost estimate. A provider fills every field it
// can, even when Chat returns an error — a rate-limited or refused call
// still has a provider, a model and a latency worth logging.
type Usage struct {
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	// CostEstimate is a decimal USD string, NUMERIC(10,6)-compatible
	// (O-28) — ADR-007 forbids a floating-point type here.
	CostEstimate string
}

// Client is the provider-agnostic interface the bot calls (ADR-009).
type Client interface {
	Chat(ctx context.Context, req Request) (Response, error)
}

// Typed errors every provider maps its failures onto (never by matching
// an error string), so callers such as the bot's tool loop can branch with
// errors.Is regardless of which provider is configured.
var (
	// ErrRateLimited means the provider throttled this request; the caller
	// should back off (ADR-009's per-chat/per-shop limits are a separate,
	// earlier gate — this is the provider's own limit).
	ErrRateLimited = errors.New("ai: rate limited")
	// ErrRefused means the model declined to answer on policy grounds
	// (Anthropic's stop_reason "refusal"). The bot's Reference doc calls
	// this out explicitly: treat it as a normal error result and fall back
	// to a static reply, not as a transient failure to retry.
	ErrRefused = errors.New("ai: refused")
	// ErrProviderUnavailable covers everything that means "try again
	// later, or fall back": network failures, timeouts, 5xx responses, an
	// overloaded provider, or a registered-but-not-built provider name
	// (O-29: "gemini").
	ErrProviderUnavailable = errors.New("ai: provider unavailable")
	// ErrBadRequest means the request itself was malformed — a bug in the
	// caller (an invalid tool schema, an unrecognized provider name), not
	// a transient condition.
	ErrBadRequest = errors.New("ai: bad request")
)

// statusError pairs one of the sentinel errors above with the HTTP status a
// provider returned and a short classification string — never the
// provider's own free-text error message (hard rule 9, D-112). Both
// mapAnthropicError (anthropic.go) and mapOpenAICompatError
// (openai_compat.go) build one instead of losing the status inside a plain
// fmt.Errorf, so logging.go can log it via errors.As without depending on
// either provider's own error type.
type statusError struct {
	sentinel error
	status   int
	// class is a short machine classification (e.g. the provider's own
	// "type" or "code" field) — safe to log, unlike a free-text message.
	class string
}

func newStatusError(sentinel error, status int, class string) *statusError {
	return &statusError{sentinel: sentinel, status: status, class: class}
}

func (e *statusError) Error() string {
	if e.class == "" {
		return fmt.Sprintf("%s: status %d", e.sentinel, e.status)
	}
	return fmt.Sprintf("%s: status %d: %s", e.sentinel, e.status, e.class)
}

// Unwrap lets errors.Is/errors.As resolve through to the sentinel.
func (e *statusError) Unwrap() error { return e.sentinel }

// HTTPStatus is read by logging.go (via errors.As) to log the status
// without ever touching a provider's free-text message.
func (e *statusError) HTTPStatus() int { return e.status }

// errorClass maps err to a short classification string via errors.Is
// against the sentinels above. logging.go uses this instead of ever logging
// err.Error() directly, since an inner Client's error may embed a
// provider's free-text message (hard rule 9, D-112).
func errorClass(err error) string {
	switch {
	case errors.Is(err, ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, ErrRefused):
		return "refused"
	case errors.Is(err, ErrBadRequest):
		return "bad_request"
	case errors.Is(err, ErrProviderUnavailable):
		return "provider_unavailable"
	default:
		return "unknown"
	}
}
