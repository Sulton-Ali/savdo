package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
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

func newAnthropicClient(cfg Config) (Client, error) {
	priceIn, priceOut, err := parsePrices(cfg)
	if err != nil {
		return nil, err
	}
	return &anthropicClient{
		sdk:       anthropic.NewClient(),
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		priceIn:   priceIn,
		priceOut:  priceOut,
	}, nil
}

func (c *anthropicClient) Chat(ctx context.Context, req Request) (Response, error) {
	tools, err := toAnthropicTools(req.Tools)
	if err != nil {
		return Response{}, fmt.Errorf("ai: %w: %v", ErrBadRequest, err)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokensOrDefault(req.MaxTokens, c.maxTokens)),
		Messages:  toAnthropicMessages(req.Messages),
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

	usage.InputTokens = int(resp.Usage.InputTokens)
	usage.OutputTokens = int(resp.Usage.OutputTokens)
	usage.CostEstimate = costEstimate(c.priceIn, c.priceOut, resp.Usage.InputTokens, resp.Usage.OutputTokens)

	out := Response{StopReason: string(resp.StopReason), Usage: usage}
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			out.Text += v.Text
		case anthropic.ToolUseBlock:
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: v.ID, Name: v.Name, Input: v.Input})
		}
	}

	if resp.StopReason == anthropic.StopReasonRefusal {
		// A refusal is a normal, successful API response (StopDetails
		// carries the classifier's category) — surfaced as an error so
		// the bot's tool loop treats it the same way it treats any other
		// "answer this from a static fallback" case, per the task's SDK
		// reference doc.
		return out, fmt.Errorf("ai: %w: category=%q", ErrRefused, resp.StopDetails.Category)
	}
	return out, nil
}

// toAnthropicMessages converts a Request's history into the SDK's
// MessageParam shape. A RoleTool message becomes a user message carrying
// every one of its ToolResults as tool_result blocks in a single message
// (ADR-009: "all results of one turn in one message"); a RoleAssistant
// message becomes an assistant message replaying its Text and/or
// ToolCalls; everything else becomes a user message.
func toAnthropicMessages(messages []Message) []anthropic.MessageParam {
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
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Input, tc.Name))
			}
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		default: // RoleUser
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	return out
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
// match on its message) to this package's typed errors by HTTP status.
// A non-API error (network failure, context cancellation — "not wrapped
// by this SDK" per its own doc) maps to ErrProviderUnavailable: the
// caller's request never reached a point where the provider could say
// anything more specific.
func mapAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests:
			return fmt.Errorf("ai: %w: %s", ErrRateLimited, apiErr.Type())
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
			return fmt.Errorf("ai: %w: %s", ErrBadRequest, apiErr.Type())
		default:
			return fmt.Errorf("ai: %w: %s (status %d)", ErrProviderUnavailable, apiErr.Type(), apiErr.StatusCode)
		}
	}
	return fmt.Errorf("ai: %w: %v", ErrProviderUnavailable, err)
}
