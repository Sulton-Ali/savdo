package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/shopspring/decimal"
)

// anthropicClient is the Client backed by the anthropic-sdk-go Messages
// API (ADR-009's first provider). The manual tool loop shape — build
// MessageNewParams, call Messages.New, switch on resp.Content's blocks,
// feed tool_result blocks back in a user message — follows the SDK's own
// tool-use reference (docs read for this task; see the package's test
// file for the exact symbols relied on). It authenticates via
// ANTHROPIC_API_KEY, read by anthropic.NewClient()'s own default option
// resolution — never logged (D-112).
type anthropicClient struct {
	sdk       anthropic.Client
	model     string
	maxTokens int
	priceIn   decimal.Decimal
	priceOut  decimal.Decimal
}

// newAnthropicClient builds the SDK client with two explicit options
// beyond the SDK's own defaults:
//
//   - option.WithMaxRetries(1): the SDK's default is higher; retries
//     multiply the cost of every retried call (each attempt bills tokens on
//     a 5xx/429 that did reach the model), so this package caps it at one
//     retry rather than inheriting the SDK default.
//   - option.WithRequestTimeout(60 * time.Second): bounds how long one
//     Chat call — including the SDK's own retries — can block the bot's
//     tool loop (O-26's 5-round cap assumes each round finishes promptly).
func newAnthropicClient(cfg Config) (Client, error) {
	priceIn, priceOut, err := parsePrices(cfg)
	if err != nil {
		return nil, err
	}
	return &anthropicClient{
		sdk: anthropic.NewClient(
			option.WithMaxRetries(1),
			option.WithRequestTimeout(60*time.Second),
		),
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		priceIn:   priceIn,
		priceOut:  priceOut,
	}, nil
}

func (c *anthropicClient) Chat(ctx context.Context, req Request) (Response, error) {
	// Pre-flight, local checks — never reach the network for a caller bug.
	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("ai: %w: request has no messages", ErrBadRequest)
	}

	tools, err := toAnthropicTools(req.Tools)
	if err != nil {
		return Response{}, fmt.Errorf("ai: %w: %v", ErrBadRequest, err)
	}
	messages, err := toAnthropicMessages(req.Messages)
	if err != nil {
		// A malformed ToolCall.Input in a replayed assistant turn is a bug
		// in the caller's history (e.g. a corrupted stored tool call), not
		// a transient provider condition — ErrBadRequest, not
		// ErrProviderUnavailable, and the request never reaches the SDK.
		return Response{}, fmt.Errorf("ai: %w: %v", ErrBadRequest, err)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokensOrDefault(req.MaxTokens, c.maxTokens)),
		Messages:  messages,
		Tools:     tools,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	// Thinking is deliberately left unset: Sonnet 5's thinking is adaptive
	// and budget_tokens is rejected on newer models (O-26; docs read for
	// this task say not to set it at all).

	start := time.Now()
	resp, callErr := c.sdk.Messages.New(ctx, params)
	latencyMs := int(time.Since(start).Milliseconds())

	usage := Usage{Provider: "anthropic", Model: c.model, LatencyMs: latencyMs, CostEstimate: "0.000000"}
	if callErr != nil {
		return Response{Usage: usage}, mapAnthropicError(callErr)
	}

	// cache_creation_input_tokens/cache_read_input_tokens are deliberately
	// excluded from InputTokens/CostEstimate: this package does not use
	// prompt caching yet, so those fields are always zero today; wire them
	// into the cost estimate only once a task turns caching on, per its own
	// (lower) per-token price.
	usage.InputTokens = clampNonNegative(int(resp.Usage.InputTokens))
	usage.OutputTokens = clampNonNegative(int(resp.Usage.OutputTokens))
	usage.CostEstimate = costEstimate(c.priceIn, c.priceOut, int64(usage.InputTokens), int64(usage.OutputTokens))

	if resp.StopReason == anthropic.StopReasonRefusal {
		// A refusal is a normal, successful API response (StopDetails
		// carries the classifier's category) — surfaced as an error so the
		// bot's tool loop treats it the same way it treats any other
		// "answer this from a static fallback" case, per the task's SDK
		// reference doc. Text/ToolCalls stay empty even if content blocks
		// preceded the refusal (observed with a real classifier response):
		// a refused answer is never partially served, only Usage is real.
		return Response{StopReason: string(resp.StopReason), Usage: usage}, fmt.Errorf("ai: %w: category=%q", ErrRefused, resp.StopDetails.Category)
	}

	out := Response{StopReason: string(resp.StopReason), Usage: usage}
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			out.Text += v.Text
		case anthropic.ToolUseBlock:
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: v.ID, Name: v.Name, Input: v.Input})
		}
	}
	return out, nil
}

// toAnthropicMessages converts a Request's history into the SDK's
// MessageParam shape. A RoleTool message becomes a user message carrying
// every one of its ToolResults as tool_result blocks in a single message
// (ADR-009: "all results of one turn in one message"); a RoleAssistant
// message becomes an assistant message replaying its Text and/or
// ToolCalls; everything else becomes a user message. An error means one of
// a replayed RoleAssistant message's ToolCalls carries a malformed
// Input — a caller bug (e.g. corrupted stored history), so Chat maps it to
// ErrBadRequest rather than letting a marshal failure surface deep inside
// the SDK's own request encoding as ErrProviderUnavailable.
func toAnthropicMessages(messages []Message) ([]anthropic.MessageParam, error) {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case RoleTool:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				blocks = append(blocks, anthropic.NewToolResultBlock(tr.CallID, tr.Content, tr.IsError))
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
			if m.Text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Text))
			}
			for _, tc := range m.ToolCalls {
				if !json.Valid(tc.Input) {
					return nil, fmt.Errorf("tool call %q (%s): input is not valid JSON", tc.ID, tc.Name)
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Input, tc.Name))
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default: // RoleUser
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	return out, nil
}

// toAnthropicTools converts every Tool's JSON Schema input into the SDK's
// typed ToolInputSchemaParam.
func toAnthropicTools(tools []Tool) ([]anthropic.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema, err := toAnthropicSchema(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", t.Name, err)
		}
		tool := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: schema,
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

// toAnthropicSchema reads "properties" and "required" out of a JSON
// Schema object — every tool ADR-009 names (search_products,
// variant_availability, shop_info) is a plain object schema. If a future
// tool needs more of JSON Schema than that, this is the place to widen.
func toAnthropicSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	if len(raw) == 0 {
		return anthropic.ToolInputSchemaParam{}, nil
	}
	var doc struct {
		Properties any      `json:"properties"`
		Required   []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return anthropic.ToolInputSchemaParam{}, fmt.Errorf("invalid input schema: %w", err)
	}
	return anthropic.ToolInputSchemaParam{Properties: doc.Properties, Required: doc.Required}, nil
}

// mapAnthropicError maps the SDK's typed *anthropic.Error (never a string
// match on its message) to this package's typed errors by HTTP status,
// via statusError so the status survives for logging.go. apiErr.Type() is
// the SDK's own short classification (e.g. "rate_limit_error"), never the
// free-text message (hard rule 9, D-112). A non-API error (network
// failure, context cancellation — "not wrapped by this SDK" per its own
// doc) maps to ErrProviderUnavailable: the caller's request never reached
// a point where the provider could say anything more specific.
func mapAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		class := string(apiErr.Type())
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests:
			return newStatusError(ErrRateLimited, apiErr.StatusCode, class)
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusRequestEntityTooLarge:
			return newStatusError(ErrBadRequest, apiErr.StatusCode, class)
		default:
			return newStatusError(ErrProviderUnavailable, apiErr.StatusCode, class)
		}
	}
	return fmt.Errorf("ai: %w: %v", ErrProviderUnavailable, err)
}
