package ai

import (
	"context"
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
	body, _ := json.Marshal(openAIErrorResponse{Error: struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	}{Message: marker, Type: "rate_limit_error"}})

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
