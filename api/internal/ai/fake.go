package ai

import (
	"context"
	"encoding/json"
	"sync"
)

// FakeResult is one scripted Chat outcome for Fake.
type FakeResult struct {
	Response Response
	Err      error
}

// Fake is a scripted Client for tests: it returns a fixed queue of
// FakeResult values in order, one per Chat call, and records every
// Request it was called with. Exported so callers outside this package
// (the bot's tool-loop tests, once built) can script and assert against
// it without a real provider.
type Fake struct {
	mu      sync.Mutex
	results []FakeResult
	next    int
	// Requests holds every Request Chat has been called with, in order.
	// It is kept exported for the common case — a single-threaded test
	// that reads it only after every Chat call has returned — but reading
	// it while another goroutine may still be calling Chat is a data race:
	// the mutex below guards writes to it but a direct field read bypasses
	// that guard. A concurrent test must use RecordedRequests instead.
	Requests []Request
}

// NewFake builds a Fake that returns results in order, one per Chat call.
func NewFake(results ...FakeResult) *Fake {
	return &Fake{results: results}
}

// Chat implements Client. Calling it more times than there are scripted
// results is a test-authoring bug: it panics rather than silently
// returning a zero Response a test might mistake for a real answer.
func (f *Fake) Chat(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Requests = append(f.Requests, req)
	if f.next >= len(f.results) {
		panic("ai.Fake: Chat called more times than scripted results")
	}
	r := f.results[f.next]
	f.next++
	return r.Response, r.Err
}

// RecordedRequests returns a deep copy of every Request Chat has been
// called with, in order — safe to call concurrently with further Chat
// calls, unlike reading the Requests field directly (see its doc comment).
// Each Request's slices (Messages, Tools, and their own nested ToolCalls/
// ToolResults/InputSchema) are copied so a caller mutating the result can
// never race with or corrupt Fake's own state.
func (f *Fake) RecordedRequests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]Request, len(f.Requests))
	for i, req := range f.Requests {
		out[i] = deepCopyRequest(req)
	}
	return out
}

func deepCopyRequest(req Request) Request {
	out := Request{System: req.System, MaxTokens: req.MaxTokens}
	if req.Messages != nil {
		out.Messages = make([]Message, len(req.Messages))
		for i, m := range req.Messages {
			out.Messages[i] = deepCopyMessage(m)
		}
	}
	if req.Tools != nil {
		out.Tools = make([]Tool, len(req.Tools))
		for i, t := range req.Tools {
			out.Tools[i] = Tool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: append(json.RawMessage(nil), t.InputSchema...),
			}
		}
	}
	return out
}

func deepCopyMessage(m Message) Message {
	out := Message{Role: m.Role, Text: m.Text}
	if m.ToolCalls != nil {
		out.ToolCalls = make([]ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			out.ToolCalls[i] = ToolCall{ID: tc.ID, Name: tc.Name, Input: append(json.RawMessage(nil), tc.Input...)}
		}
	}
	if m.ToolResults != nil {
		out.ToolResults = make([]ToolResult, len(m.ToolResults))
		copy(out.ToolResults, m.ToolResults) // ToolResult holds only string/bool fields.
	}
	return out
}
