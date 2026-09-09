package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func newTestOpenAICompatClient(t *testing.T, serverURL string) *openAICompatClient {
	t.Helper()
	return &openAICompatClient{
		httpClient: http.DefaultClient,
		baseURL:    serverURL,
		apiKey:     "test-key",
		model:      "test-model",
		priceIn:    decimal.RequireFromString("2.00"),
		priceOut:   decimal.RequireFromString("10.00"),
	}
}

func TestOpenAICompatClient_Chat_success(t *testing.T) {
	var gotBody openAIChatRequest
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []openAIChoice{{
				Message: openAIMessage{
					Role:    "assistant",
					Content: "there are 3 in stock",
					ToolCalls: []openAIToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: openAIToolCallFunction{
							Name:      "variant_availability",
							Arguments: `{"sku":"SHIRT-M"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: openAIUsage{PromptTokens: 100, CompletionTokens: 20},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)

	req := Request{
		System: "You are a shop assistant.",
		Messages: []Message{
			{Role: RoleUser, Text: "is the shirt in stock?"},
		},
		Tools: []Tool{{
			Name:        "variant_availability",
			Description: "Check stock for a SKU",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"sku":{"type":"string"}},"required":["sku"]}`),
		}},
		MaxTokens: 512,
	}

	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotBody.Model != "test-model" {
		t.Fatalf("request Model = %q, want test-model", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" || gotBody.Messages[1].Role != "user" {
		t.Fatalf("request Messages = %+v, want [system, user]", gotBody.Messages)
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Function.Name != "variant_availability" {
		t.Fatalf("request Tools = %+v, want one variant_availability tool", gotBody.Tools)
	}
	if gotBody.MaxTokens != 512 {
		t.Fatalf("request MaxTokens = %d, want 512", gotBody.MaxTokens)
	}

	if resp.Text != "there are 3 in stock" {
		t.Fatalf("Text = %q, want %q", resp.Text, "there are 3 in stock")
	}
	if resp.StopReason != "tool_calls" {
		t.Fatalf("StopReason = %q, want tool_calls", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "variant_availability" {
		t.Fatalf("ToolCalls = %+v, want one call_1/variant_availability", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Input) != `{"sku":"SHIRT-M"}` {
		t.Fatalf("ToolCalls[0].Input = %s, want %s", resp.ToolCalls[0].Input, `{"sku":"SHIRT-M"}`)
	}
	if resp.Usage.Provider != "openai_compat" || resp.Usage.Model != "test-model" {
		t.Fatalf("Usage = %+v, want provider openai_compat, model test-model", resp.Usage)
	}
	if resp.Usage.InputTokens != 100 || resp.Usage.OutputTokens != 20 {
		t.Fatalf("Usage tokens = %+v, want 100/20", resp.Usage)
	}
	// (2.00*100 + 10.00*20) / 1e6 = (200+200)/1e6 = 0.0004
	if resp.Usage.CostEstimate != "0.000400" {
		t.Fatalf("CostEstimate = %q, want %q", resp.Usage.CostEstimate, "0.000400")
	}
	if resp.Usage.LatencyMs < 0 {
		t.Fatalf("LatencyMs = %d, want >= 0", resp.Usage.LatencyMs)
	}
}

func TestOpenAICompatClient_Chat_toolResultRoundTrip(t *testing.T) {
	var gotBody openAIChatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []openAIChoice{{Message: openAIMessage{Content: "done"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)

	req := Request{
		Messages: []Message{
			{Role: RoleUser, Text: "is it in stock?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "variant_availability", Input: json.RawMessage(`{"sku":"X"}`)}}},
			{Role: RoleTool, ToolResults: []ToolResult{{CallID: "call_1", Content: "3 in stock"}}},
		},
	}
	if _, err := client.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if len(gotBody.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(gotBody.Messages))
	}
	assistantMsg := gotBody.Messages[1]
	if assistantMsg.Role != "assistant" || len(assistantMsg.ToolCalls) != 1 || assistantMsg.ToolCalls[0].Function.Name != "variant_availability" {
		t.Fatalf("Messages[1] = %+v, want assistant with one variant_availability tool call", assistantMsg)
	}
	toolMsg := gotBody.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Content != "3 in stock" {
		t.Fatalf("Messages[2] = %+v, want tool call_1 %q", toolMsg, "3 in stock")
	}
}

// TestOpenAICompatClient_Chat_toolCallExtraContentRoundTrip covers
// phase-7/t8's bug: a provider (Gemini, live-captured) that attaches an
// opaque extra_content to a tool_calls entry and rejects a replay that
// does not echo it back verbatim on the assistant message, with a 400
// "Function call is missing a thought_signature in functionCall parts".
// Round 1 gets a tool call carrying extra_content; round 2 replays it
// (ai.Message construction mirrors internal/bot/chat.go's runFreeText)
// and must send the *decoded* provider id plus the *same* extra_content
// object back, on both the assistant tool_calls entry and (id only) the
// tool result message.
func TestOpenAICompatClient_Chat_toolCallExtraContentRoundTrip(t *testing.T) {
	const wantExtra = `{"google":{"thought_signature":"sig-abc"}}`

	var round int
	var round2Body openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		round++
		w.Header().Set("Content-Type", "application/json")
		if round == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"tool_calls": []map[string]any{{
							"id":            "call_9",
							"type":          "function",
							"function":      map[string]any{"name": "variant_availability", "arguments": `{"slug":"x"}`},
							"extra_content": json.RawMessage(wantExtra),
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&round2Body)
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []openAIChoice{{Message: openAIMessage{Content: "3 dona bor"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)

	resp1, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "is it in stock?"}}})
	if err != nil {
		t.Fatalf("round 1 Chat: %v", err)
	}
	if len(resp1.ToolCalls) != 1 {
		t.Fatalf("round 1 ToolCalls = %+v, want 1", resp1.ToolCalls)
	}
	call := resp1.ToolCalls[0]
	if call.ID == "call_9" {
		t.Fatalf("round 1 ToolCalls[0].ID = %q, want it to differ from the raw provider id (extra_content must be folded in)", call.ID)
	}

	messages := []Message{
		{Role: RoleUser, Text: "is it in stock?"},
		{Role: RoleAssistant, Text: resp1.Text, ToolCalls: resp1.ToolCalls},
		{Role: RoleTool, ToolResults: []ToolResult{{CallID: call.ID, Content: `{"items":[]}`}}},
	}
	if _, err := client.Chat(context.Background(), Request{Messages: messages}); err != nil {
		t.Fatalf("round 2 Chat: %v", err)
	}

	if len(round2Body.Messages) != 3 {
		t.Fatalf("round 2 Messages len = %d, want 3", len(round2Body.Messages))
	}
	assistantMsg := round2Body.Messages[1]
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("round 2 assistant ToolCalls = %+v, want 1", assistantMsg.ToolCalls)
	}
	gotCall := assistantMsg.ToolCalls[0]
	if gotCall.ID != "call_9" {
		t.Fatalf("round 2 assistant tool_calls[0].id = %q, want the decoded provider id %q", gotCall.ID, "call_9")
	}
	if string(gotCall.ExtraContent) != wantExtra {
		t.Fatalf("round 2 assistant tool_calls[0].extra_content = %s, want %s", gotCall.ExtraContent, wantExtra)
	}
	toolMsg := round2Body.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_9" {
		t.Fatalf("round 2 tool message = %+v, want role tool, tool_call_id %q", toolMsg, "call_9")
	}
}

// TestOpenAICompatClient_Chat_oversizedExtraContentIsDropped covers
// Sonnet/Opus MAJOR (phase-7/t8 fix wave): a tool_calls entry whose
// extra_content exceeds maxToolCallExtraContentBytes must be dropped,
// not folded into the returned ToolCall.ID — a faulty or hostile backend
// must not be able to smuggle several MiB per call into the in-memory
// messages slice, replayed on every remaining round.
func TestOpenAICompatClient_Chat_oversizedExtraContentIsDropped(t *testing.T) {
	oversized := `"` + strings.Repeat("x", maxToolCallExtraContentBytes+1) + `"`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"id":            "call_big",
						"type":          "function",
						"function":      map[string]any{"name": "variant_availability", "arguments": `{"slug":"x"}`},
						"extra_content": json.RawMessage(oversized),
					}},
				},
				"finish_reason": "tool_calls",
			}},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "is it in stock?"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID != "call_big" {
		t.Fatalf("ToolCalls[0].ID = %q, want the raw provider id %q (oversized extra_content must be dropped, not folded)", resp.ToolCalls[0].ID, "call_big")
	}
}

// TestOpenAICompatClient_Chat_toolCallWithoutExtraContentOmitsField
// covers the other direction: a plain OpenAI-compatible provider (no
// extra_content) must never gain the field on replay, and the id must
// pass through byte-for-byte — the encode/decode round trip is a no-op
// when there was nothing to carry (existing self-hosted backends stay
// unaffected by this fix).
func TestOpenAICompatClient_Chat_toolCallWithoutExtraContentOmitsField(t *testing.T) {
	var gotBody openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{Choices: []openAIChoice{{Message: openAIMessage{Content: "done"}, FinishReason: "stop"}}})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Text: "is it in stock?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "variant_availability", Input: json.RawMessage(`{"sku":"X"}`)}}},
			{Role: RoleTool, ToolResults: []ToolResult{{CallID: "call_1", Content: "3 in stock"}}},
		},
	}
	if _, err := client.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	assistantMsg := gotBody.Messages[1]
	if assistantMsg.ToolCalls[0].ID != "call_1" {
		t.Fatalf("assistant tool_calls[0].id = %q, want call_1", assistantMsg.ToolCalls[0].ID)
	}
	if len(assistantMsg.ToolCalls[0].ExtraContent) != 0 {
		t.Fatalf("assistant tool_calls[0].extra_content = %s, want empty/omitted", assistantMsg.ToolCalls[0].ExtraContent)
	}
}

func TestEncodeDecodeToolCallID(t *testing.T) {
	t.Run("no extra content round-trips to the same id", func(t *testing.T) {
		id := encodeToolCallID(openAIToolCall{ID: "call_1"})
		if id != "call_1" {
			t.Fatalf("encodeToolCallID(no extra) = %q, want %q", id, "call_1")
		}
		realID, extra := decodeToolCallID(id)
		if realID != "call_1" || extra != nil {
			t.Fatalf("decodeToolCallID(%q) = (%q, %s), want (call_1, nil)", id, realID, extra)
		}
	})

	t.Run("extra content round-trips exactly", func(t *testing.T) {
		extra := json.RawMessage(`{"google":{"thought_signature":"sig-abc"}}`)
		id := encodeToolCallID(openAIToolCall{ID: "call_9", ExtraContent: extra})
		if id == "call_9" {
			t.Fatalf("encodeToolCallID(extra) = %q, want it to differ from the raw id", id)
		}
		realID, gotExtra := decodeToolCallID(id)
		if realID != "call_9" {
			t.Fatalf("decodeToolCallID(%q) real = %q, want call_9", id, realID)
		}
		if string(gotExtra) != string(extra) {
			t.Fatalf("decodeToolCallID(%q) extra = %s, want %s", id, gotExtra, extra)
		}
	})

	t.Run("an id with no separator (any other provider's own id) decodes to itself", func(t *testing.T) {
		realID, extra := decodeToolCallID("plain-provider-id")
		if realID != "plain-provider-id" || extra != nil {
			t.Fatalf("decodeToolCallID(plain) = (%q, %s), want (plain-provider-id, nil)", realID, extra)
		}
	})

	// The next two cover Sonnet/Opus minor 1 (phase-7/t8 fix wave): on
	// any decode doubt, the *whole original string* must come back as
	// realID — never a truncated id[:i] prefix, which would silently
	// send a wrong id to the provider.
	t.Run("malformed base64 after the separator decodes to the whole string, not a truncated prefix", func(t *testing.T) {
		malformed := "call_1" + string(toolCallIDSeparator) + "not-valid-base64!!!"
		realID, extra := decodeToolCallID(malformed)
		if realID != malformed || extra != nil {
			t.Fatalf("decodeToolCallID(malformed base64) = (%q, %s), want (%q, nil)", realID, extra, malformed)
		}
	})

	t.Run("base64-valid but non-JSON payload decodes to the whole string, not a truncated prefix", func(t *testing.T) {
		notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json at all"))
		id := "call_1" + string(toolCallIDSeparator) + notJSON
		realID, extra := decodeToolCallID(id)
		if realID != id || extra != nil {
			t.Fatalf("decodeToolCallID(non-JSON payload) = (%q, %s), want (%q, nil)", realID, extra, id)
		}
	})
}

func TestOpenAICompatClient_Chat_errorStatusMapsToTypedError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"429 rate limited", http.StatusTooManyRequests, ErrRateLimited},
		{"400 bad request", http.StatusBadRequest, ErrBadRequest},
		{"503 provider unavailable", http.StatusServiceUnavailable, ErrProviderUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(openAIErrorResponse{})
			}))
			defer server.Close()

			client := newTestOpenAICompatClient(t, server.URL)
			resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
			if err == nil {
				t.Fatal("Chat: error = nil, want a typed error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Chat error = %v, want it to wrap %v", err, tt.wantErr)
			}
			if resp.Usage.Provider != "openai_compat" {
				t.Fatalf("Usage.Provider = %q, want openai_compat even on error", resp.Usage.Provider)
			}
		})
	}
}

func TestNewOpenAICompatClient_validatesBaseURL(t *testing.T) {
	basePrices := Config{Provider: "openai_compat", Model: "test-model", PriceInputPerMTok: "2.00", PriceOutputPerMTok: "10.00"}

	t.Run("https any host is accepted", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "https://api.example.com/v1")
		if _, err := newOpenAICompatClient(basePrices); err != nil {
			t.Fatalf("newOpenAICompatClient(https) = %v, want nil", err)
		}
	})

	for _, host := range []string{"http://localhost:11434/v1", "http://127.0.0.1:11434/v1", "http://[::1]:11434/v1"} {
		t.Run("http loopback "+host+" is accepted", func(t *testing.T) {
			t.Setenv("AI_BASE_URL", host)
			if _, err := newOpenAICompatClient(basePrices); err != nil {
				t.Fatalf("newOpenAICompatClient(%q) = %v, want nil", host, err)
			}
		})
	}

	t.Run("http on a non-loopback host is rejected", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "http://ollama.internal.example.com:11434/v1")
		_, err := newOpenAICompatClient(basePrices)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("newOpenAICompatClient(http non-loopback) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})

	t.Run("a scheme other than http/https is rejected", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "ftp://localhost/v1")
		_, err := newOpenAICompatClient(basePrices)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("newOpenAICompatClient(ftp) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})

	t.Run("a URL with no host is rejected", func(t *testing.T) {
		t.Setenv("AI_BASE_URL", "/just/a/path")
		_, err := newOpenAICompatClient(basePrices)
		if !errors.Is(err, ErrBadRequest) {
			t.Fatalf("newOpenAICompatClient(no host) = %v, want errors.Is(_, ErrBadRequest)", err)
		}
	})
}

func TestOpenAICompatClient_Chat_noMessagesIsBadRequest(t *testing.T) {
	client := newTestOpenAICompatClient(t, "http://unused.invalid")
	if _, err := client.Chat(context.Background(), Request{}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("Chat(no messages) error = %v, want errors.Is(_, ErrBadRequest)", err)
	}
}

func TestOpenAICompatClient_Chat_refusalViaFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []openAIChoice{{Message: openAIMessage{Content: "partial before filter"}, FinishReason: "content_filter"}},
			Usage:   openAIUsage{PromptTokens: 12, CompletionTokens: 3},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Chat error = %v, want errors.Is(_, ErrRefused)", err)
	}
	if resp.Text != "" {
		t.Fatalf("Text = %q, want empty on a content_filter finish_reason", resp.Text)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("Usage = %+v, want real usage kept on refusal (12/3)", resp.Usage)
	}
}

func TestOpenAICompatClient_Chat_refusalViaRefusalField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []openAIChoice{{Message: openAIMessage{Refusal: "I can't help with that."}, FinishReason: "stop"}},
			Usage:   openAIUsage{PromptTokens: 8, CompletionTokens: 2},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Chat error = %v, want errors.Is(_, ErrRefused)", err)
	}
	if resp.Text != "" || len(resp.ToolCalls) != 0 {
		t.Fatalf("resp = %+v, want empty Text/ToolCalls on a non-empty refusal field", resp)
	}
}

func TestOpenAICompatClient_Chat_skipsNonFunctionToolCallType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{
						{"id": "call_1", "type": "function", "function": map[string]any{"name": "variant_availability", "arguments": `{"sku":"X"}`}},
						{"id": "call_2", "type": "code_interpreter", "function": map[string]any{"name": "run", "arguments": `{}`}},
					},
				},
				"finish_reason": "tool_calls",
			}},
		})
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("ToolCalls = %+v, want only the function-typed call_1", resp.ToolCalls)
	}
}

func TestOpenAICompatClient_Chat_responseBodyTooLargeIsProviderUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// One byte over the 4 MiB bound.
		_, _ = io.Copy(w, io.LimitReader(zeroReader{}, maxOpenAICompatResponseBytes+1))
	}))
	defer server.Close()

	client := newTestOpenAICompatClient(t, server.URL)
	_, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("Chat(oversized body) error = %v, want errors.Is(_, ErrProviderUnavailable)", err)
	}
}

// zeroReader streams zero bytes forever, so the oversized-body test above
// does not need to build a 4 MiB buffer by hand.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = '0'
	}
	return len(p), nil
}

func TestMapOpenAICompatError_neverEmbedsProviderMessage(t *testing.T) {
	const marker = "MARKER-provider-said-your-card-is-declined-do-not-log-me"
	body, _ := json.Marshal(openAIErrorResponse{Error: openAIErrorDetail{Message: marker, Type: "rate_limit_error"}})

	err := mapOpenAICompatError(http.StatusTooManyRequests, body)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("mapOpenAICompatError = %v, want errors.Is(_, ErrRateLimited)", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("mapOpenAICompatError().Error() = %q, must never contain the provider's free-text message", err.Error())
	}

	var se *statusError
	if !errors.As(err, &se) {
		t.Fatalf("mapOpenAICompatError = %v, want a *statusError in the chain", err)
	}
	if se.HTTPStatus() != http.StatusTooManyRequests {
		t.Fatalf("HTTPStatus() = %d, want %d", se.HTTPStatus(), http.StatusTooManyRequests)
	}
}

// TestMapOpenAICompatError_geminiEnvelope covers the shape captured live
// from Gemini's openai-compat endpoint (phase-7/t8): a single-element
// JSON array wrapping the error object, a numeric "code" instead of a
// string, and its own "status" field ("RESOURCE_EXHAUSTED") in place of
// the standard envelope's "type" — mapOpenAICompatError must still map to
// the right sentinel, classify from "status", and never leak "message".
func TestMapOpenAICompatError_geminiEnvelope(t *testing.T) {
	const marker = "MARKER-quota exceeded, do not log me"
	body := []byte(`[{"error":{"code":429,"message":"` + marker + `","status":"RESOURCE_EXHAUSTED"}}]`)

	err := mapOpenAICompatError(http.StatusTooManyRequests, body)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("mapOpenAICompatError = %v, want errors.Is(_, ErrRateLimited)", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("mapOpenAICompatError().Error() = %q, must never contain the provider's free-text message", err.Error())
	}
	if !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("mapOpenAICompatError().Error() = %q, want it to classify as RESOURCE_EXHAUSTED", err.Error())
	}
}

func TestClassFromError(t *testing.T) {
	tests := []struct {
		name string
		d    openAIErrorDetail
		want string
	}{
		{"type wins over status and code", openAIErrorDetail{Type: "rate_limit_error", Status: "RESOURCE_EXHAUSTED", Code: json.RawMessage(`429`)}, "rate_limit_error"},
		{"status wins over code when type is empty", openAIErrorDetail{Status: "INVALID_ARGUMENT", Code: json.RawMessage(`400`)}, "INVALID_ARGUMENT"},
		{"numeric code stringified when type and status are empty", openAIErrorDetail{Code: json.RawMessage(`400`)}, "400"},
		{"string code unquoted when type and status are empty", openAIErrorDetail{Code: json.RawMessage(`"invalid_request_error"`)}, "invalid_request_error"},
		{"all empty", openAIErrorDetail{}, ""},
		// Sonnet/Opus minor 2 (phase-7/t8 fix wave): code is only ever
		// trusted as a JSON string or number.
		{"object code is ignored", openAIErrorDetail{Code: json.RawMessage(`{"foo":"bar"}`)}, ""},
		{"array code is ignored", openAIErrorDetail{Code: json.RawMessage(`[1,2,3]`)}, ""},
		{"bool code is ignored", openAIErrorDetail{Code: json.RawMessage(`true`)}, ""},
		{"null code is ignored", openAIErrorDetail{Code: json.RawMessage(`null`)}, ""},
		{"malformed code is ignored", openAIErrorDetail{Code: json.RawMessage(`{not valid json`)}, ""},
		// … and the resulting class is capped to maxErrorClassLen
		// regardless of which field it came from.
		{"a long type is capped", openAIErrorDetail{Type: strings.Repeat("a", 100)}, strings.Repeat("a", maxErrorClassLen)},
		{"a long status is capped", openAIErrorDetail{Status: strings.Repeat("b", 100)}, strings.Repeat("b", maxErrorClassLen)},
		{"a long string code is capped", openAIErrorDetail{Code: json.RawMessage(`"` + strings.Repeat("c", 100) + `"`)}, strings.Repeat("c", maxErrorClassLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classFromError(tt.d); got != tt.want {
				t.Fatalf("classFromError(%+v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestParseOpenAIError_toleratesGeminiArrayWrapping(t *testing.T) {
	body := []byte(`[{"error":{"code":400,"message":"m","status":"INVALID_ARGUMENT"}}]`)
	d := parseOpenAIError(body)
	if d.Status != "INVALID_ARGUMENT" || string(d.Code) != "400" {
		t.Fatalf("parseOpenAIError(array) = %+v, want status INVALID_ARGUMENT, code 400", d)
	}
}
