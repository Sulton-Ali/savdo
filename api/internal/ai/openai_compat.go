package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// openAICompatClient is the "any OpenAI-compatible endpoint" provider
// (ADR-009, O-29): a thin stdlib net/http client, no SDK, for self-hosted
// backends (Ollama, vLLM, …) that speak the OpenAI Chat Completions
// function-calling shape. Configured by two environment variables this
// provider alone needs — AI_BASE_URL (required, e.g.
// "http://localhost:11434/v1") and AI_API_KEY (optional: many self-hosted
// endpoints need no auth) — read directly here rather than added to
// Config, since every other Config value has no use for them.
type openAICompatClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	priceIn    decimal.Decimal
	priceOut   decimal.Decimal
}

func newOpenAICompatClient(cfg Config) (Client, error) {
	priceIn, priceOut, err := parsePrices(cfg)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(os.Getenv("AI_BASE_URL"), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("ai: AI_BASE_URL is required for provider %q: %w", cfg.Provider, ErrBadRequest)
	}
	return &openAICompatClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    baseURL,
		apiKey:     os.Getenv("AI_API_KEY"),
		model:      cfg.Model,
		maxTokens:  cfg.MaxTokens,
		priceIn:    priceIn,
		priceOut:   priceOut,
	}, nil
}

func (c *openAICompatClient) Chat(ctx context.Context, req Request) (Response, error) {
	body := openAIChatRequest{
		Model:     c.model,
		Messages:  toOpenAIMessages(req.System, req.Messages),
		Tools:     toOpenAITools(req.Tools),
		MaxTokens: maxTokensOrDefault(req.MaxTokens, c.maxTokens),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("ai: %w: encode request: %v", ErrBadRequest, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("ai: %w: %v", ErrBadRequest, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	usage := Usage{Provider: "openai_compat", Model: c.model, CostEstimate: "0.000000"}

	start := time.Now()
	httpResp, err := c.httpClient.Do(httpReq)
	usage.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: read response: %v", ErrProviderUnavailable, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return Response{Usage: usage}, mapOpenAICompatError(httpResp.StatusCode, respBody)
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: decode response: %v", ErrProviderUnavailable, err)
	}
	if len(parsed.Choices) == 0 {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: response had no choices", ErrProviderUnavailable)
	}

	usage.InputTokens = parsed.Usage.PromptTokens
	usage.OutputTokens = parsed.Usage.CompletionTokens
	usage.CostEstimate = costEstimate(c.priceIn, c.priceOut, int64(parsed.Usage.PromptTokens), int64(parsed.Usage.CompletionTokens))

	choice := parsed.Choices[0]
	out := Response{Text: choice.Message.Content, StopReason: choice.FinishReason, Usage: usage}
	for _, tc := range choice.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(tc.Function.Arguments)})
	}
	return out, nil
}

// The request/response shapes below are this provider's own — no SDK, so
// they are declared here rather than generated (ADR-002's "no
// hand-declared request/response shapes" governs contracts/openapi.yaml,
// not a third-party wire format this codebase does not own).

type openAIChatRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAITool    `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name string `json:"name"`
	// Arguments is a JSON-encoded object, not a nested JSON value — the
	// OpenAI function-calling shape's own convention.
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// toOpenAIMessages converts a Request's system prompt and history into
// the OpenAI Chat Completions message list: system prompt first (role
// "system"), then one message per Message — a RoleTool message expands
// into one role:"tool" message per ToolResult (unlike Anthropic, which
// puts them all in a single user message: this wire format has no
// equivalent of multiple content blocks per message for tool results).
func toOpenAIMessages(system string, messages []Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages)+1)
	if system != "" {
		out = append(out, openAIMessage{Role: "system", Content: system})
	}
	for _, m := range messages {
		switch m.Role {
		case RoleTool:
			for _, tr := range m.ToolResults {
				out = append(out, openAIMessage{Role: "tool", Content: tr.Content, ToolCallID: tr.CallID})
			}
		case RoleAssistant:
			msg := openAIMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: openAIToolCallFunction{Name: tc.Name, Arguments: string(tc.Input)},
				})
			}
			out = append(out, msg)
		default: // RoleUser
			out = append(out, openAIMessage{Role: "user", Content: m.Text})
		}
	}
	return out
}

func toOpenAITools(tools []Tool) []openAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// mapOpenAICompatError maps a non-200 response to this package's typed
// errors by HTTP status — the OpenAI error envelope has no stable typed
// Go representation to switch on the way the Anthropic SDK's errors do,
// so status code is the only reliable signal; the parsed message (if any)
// is included for logs, never matched on.
func mapOpenAICompatError(status int, body []byte) error {
	var parsed openAIErrorResponse
	_ = json.Unmarshal(body, &parsed)
	msg := parsed.Error.Message

	switch status {
	case http.StatusTooManyRequests:
		return fmt.Errorf("ai: %w: %s", ErrRateLimited, msg)
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
		return fmt.Errorf("ai: %w: %s", ErrBadRequest, msg)
	default:
		return fmt.Errorf("ai: %w: status %d: %s", ErrProviderUnavailable, status, msg)
	}
}
