package ai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
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
			got := toAnthropicMessages(tt.messages)
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
