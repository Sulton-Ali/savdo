package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("ai: AI_BASE_URL %q is not a valid absolute URL: %w", baseURL, ErrBadRequest)
	}
	switch u.Scheme {
	case "https":
		// Always fine.
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("ai: AI_BASE_URL %q: %w: http is only allowed for a loopback host (localhost, 127.0.0.1, ::1); use https for anything else", baseURL, ErrBadRequest)
		}
	default:
		return nil, fmt.Errorf("ai: AI_BASE_URL %q: %w: scheme must be http or https", baseURL, ErrBadRequest)
	}
	// Scheme and host only — never the API key (hard rule 9, D-112).
	slog.Default().Info("ai: openai_compat provider configured", "scheme", u.Scheme, "host", u.Host)

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

// isLoopbackHost reports whether host (already stripped of any port by
// url.URL.Hostname) is one of the loopback names/addresses this codebase
// treats as "local, so plaintext http is acceptable" — the same set D-81
// uses for the mobile app's server-URL rule, narrowed here to exact
// loopback only (no private-network ranges): this provider is meant for a
// self-hosted backend running on the same machine as the API, not a LAN
// service.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// maxOpenAICompatResponseBytes bounds how much of a response body this
// client reads into memory. A self-hosted endpoint (this provider's only
// use case, O-29) is not a trusted upstream the way Anthropic's API is;
// without a bound, a misbehaving or malicious backend could stream an
// unbounded body at the process. Overflow classifies as
// ErrProviderUnavailable: the provider did not give us a usable response.
const maxOpenAICompatResponseBytes = 4 * 1024 * 1024 // 4 MiB

// errUnsupportedToolCallType is logged, never returned, when a tool_calls
// entry has a type other than "function" — this wire format defines no
// other type, but a permissive self-hosted backend might send one; the
// entry is skipped rather than derailing the whole response over one
// unexpected item.
var errUnsupportedToolCallType = errors.New("ai: openai_compat: unsupported tool_call type")

func (c *openAICompatClient) Chat(ctx context.Context, req Request) (Response, error) {
	if len(req.Messages) == 0 {
		return Response{}, fmt.Errorf("ai: %w: request has no messages", ErrBadRequest)
	}

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

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, maxOpenAICompatResponseBytes+1))
	if err != nil {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: read response: %v", ErrProviderUnavailable, err)
	}
	if len(respBody) > maxOpenAICompatResponseBytes {
		return Response{Usage: usage}, fmt.Errorf("ai: %w: response body exceeds %d bytes", ErrProviderUnavailable, maxOpenAICompatResponseBytes)
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

	usage.InputTokens = clampNonNegative(parsed.Usage.PromptTokens)
	usage.OutputTokens = clampNonNegative(parsed.Usage.CompletionTokens)
	usage.CostEstimate = costEstimate(c.priceIn, c.priceOut, int64(usage.InputTokens), int64(usage.OutputTokens))

	choice := parsed.Choices[0]
	if choice.FinishReason == "content_filter" || choice.Message.Refusal != "" {
		// Refusal parity with the anthropic provider (StopReasonRefusal):
		// a normal, successful HTTP response that the model declined to
		// answer maps to ErrRefused with usage kept and content dropped,
		// never partially served.
		return Response{Usage: usage}, fmt.Errorf("ai: %w: finish_reason=%q", ErrRefused, choice.FinishReason)
	}

	out := Response{Text: choice.Message.Content, StopReason: choice.FinishReason, Usage: usage}
	for _, tc := range choice.Message.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			slog.Default().WarnContext(ctx, "ai: openai_compat: skipping unsupported tool_call type", "error", errUnsupportedToolCallType, "type", tc.Type)
			continue
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: encodeToolCallID(tc), Name: tc.Function.Name, Input: json.RawMessage(tc.Function.Arguments)})
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
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
	// Refusal is set by the provider instead of Content when it declines
	// to answer; this client never sends it, only reads it (mapped to
	// ErrRefused).
	Refusal    string `json:"refusal,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
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
	// ExtraContent is an opaque provider extension attached to a
	// tool_calls entry that some OpenAI-compatible backends require
	// echoed back verbatim on the next round's replayed assistant
	// message — Gemini's own openai-compat endpoint rejects a replay
	// that omits it with "Function call is missing a thought_signature
	// in functionCall parts" (400 INVALID_ARGUMENT, captured live).
	// Never inspected, only round-tripped byte for byte via
	// encodeToolCallID/decodeToolCallID, since this package has no
	// reason to know what any provider puts in it.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
}

// toolCallIDSeparator joins a tool_calls entry's own id with any
// ExtraContent a provider attached to it (encodeToolCallID). ASCII unit
// separator: not a character any provider's own id is expected to
// contain, and never produced by this client's own id generation (it
// never generates one — ids are always the provider's).
const toolCallIDSeparator = '\x1f'

// encodeToolCallID folds tc's own id and ExtraContent (if any) into the
// single string this file hands the rest of the package back as
// ai.ToolCall.ID. ai.ToolCall is shared by every provider (ADR-009), so
// it has no field for a provider-specific extension; the caller
// (internal/bot/chat.go) already treats a ToolCall's ID as an opaque
// token it only ever echoes back unmodified as a ToolResult.CallID
// (bot/tools.go's toolOK/toolErrorResult never parse or display it, and
// it is never persisted — toolCallRecord carries Name/Input/Result, not
// ID) — so smuggling ExtraContent through it here, and reversing that in
// decodeToolCallID below when this file builds the next request, is
// safe and needs no shared state or new field anywhere else.
func encodeToolCallID(tc openAIToolCall) string {
	if len(tc.ExtraContent) == 0 {
		return tc.ID
	}
	return tc.ID + string(toolCallIDSeparator) + base64.RawURLEncoding.EncodeToString(tc.ExtraContent)
}

// decodeToolCallID reverses encodeToolCallID: realID is what the provider
// itself issued (goes in the wire request's "id" or "tool_call_id");
// extra is the ExtraContent to echo back on a replayed assistant
// tool_calls entry, or nil when there was none (a provider that never
// sent one, e.g. a plain self-hosted OpenAI-compatible backend). An id
// with no separator — the only shape any provider other than this file's
// own encoding ever produces — decodes to itself with no extra content;
// a corrupt-looking suffix (never expected from this client's own
// output) does the same rather than failing the whole request over it.
func decodeToolCallID(id string) (realID string, extra json.RawMessage) {
	i := strings.IndexByte(id, toolCallIDSeparator)
	if i < 0 {
		return id, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id[i+1:])
	if err != nil {
		return id[:i], nil
	}
	return id[:i], decoded
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
	Error openAIErrorDetail `json:"error"`
}

// openAIErrorDetail is the "error" object both the standard OpenAI
// envelope and Gemini's own openai-compat one send, with the two fields
// Gemini adds/changes: Code is a JSON number there (its HTTP status
// echoed back) rather than the short string a standard OpenAI-compatible
// backend sends, so it is decoded as raw JSON and only ever stringified,
// never parsed as either type; Status is Gemini's own machine
// classification (e.g. "RESOURCE_EXHAUSTED", "INVALID_ARGUMENT"),
// standard OpenAI-compatible backends do not send it.
type openAIErrorDetail struct {
	Message string          `json:"message"`
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
	Status  string          `json:"status"`
}

// parseOpenAIError decodes body as this file's error envelope, tolerating
// the one shape difference captured live from Gemini's openai-compat
// endpoint: a single-element JSON array (`[{"error": {...}}]`) instead of
// the bare object (`{"error": {...}}`) every other provider and Gemini's
// own other error responses use. Any other malformed body decodes to a
// zero openAIErrorDetail, same as the plain json.Unmarshal this replaced
// (mapOpenAICompatError already tolerates an empty class).
func parseOpenAIError(body []byte) openAIErrorDetail {
	var single openAIErrorResponse
	if err := json.Unmarshal(body, &single); err == nil {
		if single.Error.Message != "" || single.Error.Type != "" || single.Error.Status != "" || len(single.Error.Code) != 0 {
			return single.Error
		}
	}
	var list []openAIErrorResponse
	if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 {
		return list[0].Error
	}
	return openAIErrorDetail{}
}

// classFromError picks mapOpenAICompatError's class: the envelope's own
// "type" (the standard OpenAI field) first, then Gemini's "status", then
// its numeric "code" stringified — never "message" (hard rule 9, D-112).
func classFromError(d openAIErrorDetail) string {
	if d.Type != "" {
		return d.Type
	}
	if d.Status != "" {
		return d.Status
	}
	return strings.Trim(string(d.Code), `"`)
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
				realID, _ := decodeToolCallID(tr.CallID)
				out = append(out, openAIMessage{Role: "tool", Content: tr.Content, ToolCallID: realID})
			}
		case RoleAssistant:
			msg := openAIMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				realID, extra := decodeToolCallID(tc.ID)
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:           realID,
					Type:         "function",
					Function:     openAIToolCallFunction{Name: tc.Name, Arguments: string(tc.Input)},
					ExtraContent: extra,
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
// errors by HTTP status, via statusError so the status survives for
// logging.go — the OpenAI error envelope has no stable typed Go
// representation to switch on the way the Anthropic SDK's errors do, so
// status code is the only reliable signal. class is the envelope's own
// "type" (falling back to "code") — a short machine classification, never
// the free-text "message", which is never read here or included in any
// returned error (hard rule 9, D-112).
func mapOpenAICompatError(status int, body []byte) error {
	class := classFromError(parseOpenAIError(body))

	switch status {
	case http.StatusTooManyRequests:
		return newStatusError(ErrRateLimited, status, class)
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity:
		return newStatusError(ErrBadRequest, status, class)
	default:
		return newStatusError(ErrProviderUnavailable, status, class)
	}
}
