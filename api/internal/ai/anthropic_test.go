package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/shopspring/decimal"
)

func TestToAnthropicMessages(t *testing.T) {
	tests := []struct {
		name     string
		messages []Message
		wantRole anthropic.MessageParamRole
		check    func(t *testing.T, blocks []anthropic.ContentBlockParamUnion)
	}{
		{
			name:     "user message becomes a single text block",
			messages: []Message{{Role: RoleUser, Text: "hello"}},
			wantRole: anthropic.MessageParamRoleUser,
			check: func(t *testing.T, blocks []anthropic.ContentBlockParamUnion) {
				if len(blocks) != 1 || blocks[0].OfText == nil || blocks[0].OfText.Text != "hello" {
					t.Fatalf("blocks = %+v, want one text block %q", blocks, "hello")
				}
			},
		},
		{
			name: "assistant message replays text and tool calls",
			messages: []Message{{
				Role:      RoleAssistant,
				Text:      "let me check",
				ToolCalls: []ToolCall{{ID: "toolu_1", Name: "search_products", Input: json.RawMessage(`{"q":"shirt"}`)}},
			}},
			wantRole: anthropic.MessageParamRoleAssistant,
			check: func(t *testing.T, blocks []anthropic.ContentBlockParamUnion) {
				if len(blocks) != 2 {
					t.Fatalf("blocks len = %d, want 2", len(blocks))
				}
				if blocks[0].OfText == nil || blocks[0].OfText.Text != "let me check" {
					t.Fatalf("blocks[0] = %+v, want text %q", blocks[0], "let me check")
				}
				if blocks[1].OfToolUse == nil || blocks[1].OfToolUse.ID != "toolu_1" || blocks[1].OfToolUse.Name != "search_products" {
					t.Fatalf("blocks[1] = %+v, want tool_use toolu_1/search_products", blocks[1])
				}
			},
		},
		{
			name: "tool message puts every result in one message",
			messages: []Message{{
				Role: RoleTool,
				ToolResults: []ToolResult{
					{CallID: "toolu_1", Content: "3 in stock", IsError: false},
					{CallID: "toolu_2", Content: "not found", IsError: true},
				},
			}},
			wantRole: anthropic.MessageParamRoleUser,
			check: func(t *testing.T, blocks []anthropic.ContentBlockParamUnion) {
				if len(blocks) != 2 {
					t.Fatalf("blocks len = %d, want 2", len(blocks))
				}
				r0, r1 := blocks[0].OfToolResult, blocks[1].OfToolResult
				if r0 == nil || r0.ToolUseID != "toolu_1" || r0.IsError.Value != false {
					t.Fatalf("blocks[0] = %+v, want tool_result toolu_1 isError=false", blocks[0])
				}
				if r1 == nil || r1.ToolUseID != "toolu_2" || r1.IsError.Value != true {
					t.Fatalf("blocks[1] = %+v, want tool_result toolu_2 isError=true", blocks[1])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toAnthropicMessages(tt.messages)
			if err != nil {
				t.Fatalf("toAnthropicMessages: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("toAnthropicMessages() len = %d, want 1", len(got))
			}
			if got[0].Role != tt.wantRole {
				t.Fatalf("Role = %q, want %q", got[0].Role, tt.wantRole)
			}
			tt.check(t, got[0].Content)
		})
	}
}

func TestToAnthropicMessages_malformedToolCallInputIsAnError(t *testing.T) {
	messages := []Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "toolu_1", Name: "search_products", Input: json.RawMessage(`{not json`)}},
	}}
	if _, err := toAnthropicMessages(messages); err == nil {
		t.Fatal("toAnthropicMessages(malformed tool call input) = nil error, want one")
	}
}

func TestToAnthropicTools(t *testing.T) {
	tools := []Tool{{
		Name:        "search_products",
		Description: "Search the shop's products",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	}}

	got, err := toAnthropicTools(tools)
	if err != nil {
		t.Fatalf("toAnthropicTools: %v", err)
	}
	if len(got) != 1 || got[0].OfTool == nil {
		t.Fatalf("got = %+v, want one OfTool", got)
	}
	tool := got[0].OfTool
	if tool.Name != "search_products" {
		t.Fatalf("Name = %q, want search_products", tool.Name)
	}
	if tool.Description.Value != "Search the shop's products" {
		t.Fatalf("Description = %q, want %q", tool.Description.Value, "Search the shop's products")
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "query" {
		t.Fatalf("Required = %+v, want [query]", tool.InputSchema.Required)
	}
	props, ok := tool.InputSchema.Properties.(map[string]any)
	if !ok || props["query"] == nil {
		t.Fatalf("Properties = %+v, want a map with a %q key", tool.InputSchema.Properties, "query")
	}
}

func TestToAnthropicSchema_invalidJSONIsAnError(t *testing.T) {
	if _, err := toAnthropicSchema(json.RawMessage(`{not json`)); err == nil {
		t.Fatal("toAnthropicSchema(invalid) = nil error, want one")
	}
}

func TestMapAnthropicError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{"429 rate limited", http.StatusTooManyRequests, ErrRateLimited},
		{"400 bad request", http.StatusBadRequest, ErrBadRequest},
		{"401 unauthorized maps to bad request", http.StatusUnauthorized, ErrBadRequest},
		{"404 not found maps to bad request", http.StatusNotFound, ErrBadRequest},
		{"422 unprocessable maps to bad request", http.StatusUnprocessableEntity, ErrBadRequest},
		{"413 payload too large maps to bad request", http.StatusRequestEntityTooLarge, ErrBadRequest},
		{"500 maps to provider unavailable", http.StatusInternalServerError, ErrProviderUnavailable},
		{"529 overloaded maps to provider unavailable", 529, ErrProviderUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := &anthropic.Error{
				StatusCode: tt.statusCode,
				Request:    &http.Request{Method: "POST", URL: &url.URL{Path: "/v1/messages"}},
				Response:   &http.Response{StatusCode: tt.statusCode},
			}
			got := mapAnthropicError(apiErr)
			if !errors.Is(got, tt.wantErr) {
				t.Fatalf("mapAnthropicError(status=%d) = %v, want errors.Is(_, %v)", tt.statusCode, got, tt.wantErr)
			}
		})
	}

	t.Run("a non-API error maps to provider unavailable", func(t *testing.T) {
		got := mapAnthropicError(errors.New("connection refused"))
		if !errors.Is(got, ErrProviderUnavailable) {
			t.Fatalf("mapAnthropicError(network error) = %v, want errors.Is(_, ErrProviderUnavailable)", got)
		}
	})
}

// --- End-to-end Chat tests against a real anthropic.Client pointed at an
// httptest.Server via option.WithBaseURL (+ option.WithAPIKey("test"), so
// none of these need ANTHROPIC_API_KEY or the network) — unlike
// TestToAnthropicMessages/TestToAnthropicTools/TestMapAnthropicError above,
// which exercise this file's pure helpers directly, these drive
// anthropicClient.Chat itself through the real SDK request/response cycle.

func newTestAnthropicClient(serverURL string) *anthropicClient {
	return &anthropicClient{
		sdk: anthropic.NewClient(
			option.WithBaseURL(serverURL),
			option.WithAPIKey("test"),
			option.WithMaxRetries(1),
			option.WithRequestTimeout(10*time.Second),
		),
		model:    "test-model",
		priceIn:  decimal.RequireFromString("2.00"),
		priceOut: decimal.RequireFromString("10.00"),
	}
}

func TestAnthropicClient_Chat_textAndToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test-model",
			"content": []map[string]any{
				{"type": "text", "text": "there are "},
				{"type": "text", "text": "3 in stock"},
				{"type": "tool_use", "id": "toolu_1", "name": "variant_availability", "input": map[string]any{"sku": "X"}},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 1500, "output_tokens": 300},
		})
	}))
	defer server.Close()

	c := newTestAnthropicClient(server.URL)
	resp, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "is it in stock?"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Multi-text-block concatenation.
	if resp.Text != "there are 3 in stock" {
		t.Fatalf("Text = %q, want %q", resp.Text, "there are 3 in stock")
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_1" || resp.ToolCalls[0].Name != "variant_availability" {
		t.Fatalf("ToolCalls = %+v, want one toolu_1/variant_availability", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Input) != `{"sku":"X"}` {
		t.Fatalf("ToolCalls[0].Input = %s, want %s", resp.ToolCalls[0].Input, `{"sku":"X"}`)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if resp.Usage.Provider != "anthropic" || resp.Usage.Model != "test-model" {
		t.Fatalf("Usage = %+v, want provider anthropic, model test-model", resp.Usage)
	}
	if resp.Usage.InputTokens != 1500 || resp.Usage.OutputTokens != 300 {
		t.Fatalf("Usage tokens = %+v, want 1500/300", resp.Usage)
	}
	// (2.00*1500 + 10.00*300) / 1e6 = (3000+3000)/1e6 = 0.006
	if resp.Usage.CostEstimate != "0.006000" {
		t.Fatalf("CostEstimate = %q, want %q", resp.Usage.CostEstimate, "0.006000")
	}
}

func TestAnthropicClient_Chat_thinkingBlockNeverLandsInText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test-model",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "let me reason about the customer's question step by step", "signature": "sig"},
				{"type": "text", "text": "we open at 9am"},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer server.Close()

	c := newTestAnthropicClient(server.URL)
	resp, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "when do you open?"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Text != "we open at 9am" {
		t.Fatalf("Text = %q, want it to hold only the text block", resp.Text)
	}
	if strings.Contains(resp.Text, "reason about") {
		t.Fatalf("Text = %q, must never contain a thinking block's content", resp.Text)
	}
}

func TestAnthropicClient_Chat_refusalReturnsEmptyContentWithUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "test-model",
			"content": []map[string]any{
				{"type": "text", "text": "partial thought before the classifier stepped in"},
			},
			"stop_reason": "refusal",
			"stop_details": map[string]any{
				"type": "refusal", "category": "general_harms", "explanation": "policy",
			},
			"usage": map[string]any{"input_tokens": 42, "output_tokens": 7},
		})
	}))
	defer server.Close()

	c := newTestAnthropicClient(server.URL)
	resp, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "..."}}})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Chat error = %v, want errors.Is(_, ErrRefused)", err)
	}
	if resp.Text != "" {
		t.Fatalf("Text = %q, want empty on refusal", resp.Text)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %+v, want empty on refusal", resp.ToolCalls)
	}
	if resp.Usage.InputTokens != 42 || resp.Usage.OutputTokens != 7 {
		t.Fatalf("Usage = %+v, want real usage kept on refusal (42/7)", resp.Usage)
	}
}

func TestAnthropicClient_Chat_rateLimitedRetriesOnceThenErrors(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"do not log this text"}}`))
	}))
	defer server.Close()

	c := newTestAnthropicClient(server.URL)
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Chat error = %v, want errors.Is(_, ErrRateLimited)", err)
	}
	if strings.Contains(err.Error(), "do not log this text") {
		t.Fatalf("Chat error = %v, must never embed the provider's free-text message", err)
	}
	// option.WithMaxRetries(1): one retry after the first attempt.
	if got := atomic.LoadInt32(&requestCount); got != 2 {
		t.Fatalf("request count = %d, want 2 (1 retry)", got)
	}
}

func TestAnthropicClient_Chat_413MapsToErrBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"request_too_large","message":"too big"}}`))
	}))
	defer server.Close()

	c := newTestAnthropicClient(server.URL)
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("Chat error = %v, want errors.Is(_, ErrBadRequest)", err)
	}
}

func TestAnthropicClient_Chat_noMessagesIsBadRequest(t *testing.T) {
	c := newTestAnthropicClient("http://unused.invalid")
	_, err := c.Chat(context.Background(), Request{})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("Chat(no messages) error = %v, want errors.Is(_, ErrBadRequest)", err)
	}
}
